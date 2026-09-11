package jsonshrink

import (
	"fmt"
	"strconv"
	"strings"
)

// A select expression picks parts of a JSON document. Syntax:
//
//	a.b.c              nested field
//	items.name         a field applied to an array maps over its elements
//	items[]  items[*]  explicit map (on an object: its values)
//	items[0] items[-1] index (negative counts from the end)
//	items[0:5] items[:3]
//	items.{name, owner.login}   pick fields; keys are named after their path
//	{login: owner.login}        pick with an alias
//	name, description   several fields at the top level
//
// A leading "$" or "." (JSONPath / jq habits) is accepted and ignored.

type stepKind int

const (
	keyStep stepKind = iota
	eachStep
	indexStep
	sliceStep
	pickStep
)

type step struct {
	kind   stepKind
	key    string
	index  int
	lo, hi *int
	fields []field
}

type field struct {
	name    string
	aliased bool
	path    []step
}

type parser struct {
	s   string
	pos int
}

// parseSelect compiles a select expression into steps.
func parseSelect(expr string) ([]step, error) {
	p := &parser{s: expr}
	fields, err := p.fieldList()
	if err != nil {
		return nil, err
	}
	p.space()
	if p.pos < len(p.s) {
		return nil, p.errorf("unexpected %q", p.s[p.pos])
	}
	if len(fields) == 1 && !fields[0].aliased {
		return fields[0].path, nil
	}
	return []step{{kind: pickStep, fields: fields}}, nil
}

func fieldText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$")
	return strings.TrimPrefix(s, ".")
}

func (p *parser) errorf(format string, args ...any) error {
	return fmt.Errorf("select: "+format+" at position %d", append(args, p.pos+1)...)
}

func (p *parser) space() {
	for p.pos < len(p.s) && (p.s[p.pos] == ' ' || p.s[p.pos] == '\t' || p.s[p.pos] == '\n') {
		p.pos++
	}
}

func (p *parser) peek() byte {
	if p.pos < len(p.s) {
		return p.s[p.pos]
	}
	return 0
}

func (p *parser) fieldList() ([]field, error) {
	var fields []field
	for {
		p.space()
		start := p.pos
		alias := ""
		if name, ok := p.tryAlias(); ok {
			alias = name
			start = p.pos
		}
		path, err := p.path()
		if err != nil {
			return nil, err
		}
		if len(path) == 0 {
			return nil, p.errorf("expected a field name")
		}
		f := field{name: alias, aliased: alias != "", path: path}
		if !f.aliased {
			f.name = fieldText(p.s[start:p.pos])
		}
		fields = append(fields, f)
		p.space()
		if p.peek() != ',' {
			return fields, nil
		}
		p.pos++
	}
}

// tryAlias consumes `name:` if it is next, leaving the position untouched otherwise.
func (p *parser) tryAlias() (string, bool) {
	save := p.pos
	name, ok := p.ident()
	if ok {
		p.space()
		if p.peek() == ':' {
			p.pos++
			p.space()
			return name, true
		}
	}
	p.pos = save
	return "", false
}

func (p *parser) path() ([]step, error) {
	var steps []step
	if p.peek() == '$' {
		p.pos++
	}
	for {
		switch c := p.peek(); {
		case c == '.':
			p.pos++
			if p.peek() == '*' {
				p.pos++
				steps = append(steps, step{kind: eachStep})
			}
		case c == '[':
			st, err := p.bracket()
			if err != nil {
				return nil, err
			}
			steps = append(steps, st)
		case c == '{':
			p.pos++
			fields, err := p.fieldList()
			if err != nil {
				return nil, err
			}
			p.space()
			if p.peek() != '}' {
				return nil, p.errorf("expected '}'")
			}
			p.pos++
			steps = append(steps, step{kind: pickStep, fields: fields})
		default:
			name, ok := p.ident()
			if !ok {
				return steps, nil
			}
			steps = append(steps, step{kind: keyStep, key: name})
		}
	}
}

func (p *parser) ident() (string, bool) {
	if p.peek() == '"' {
		end := strings.IndexByte(p.s[p.pos+1:], '"')
		if end < 0 {
			return "", false
		}
		name := p.s[p.pos+1 : p.pos+1+end]
		p.pos += end + 2
		return name, true
	}
	start := p.pos
	for p.pos < len(p.s) && !strings.ContainsRune(".[]{},: \t\n\"", rune(p.s[p.pos])) {
		p.pos++
	}
	return p.s[start:p.pos], p.pos > start
}

func (p *parser) bracket() (step, error) {
	p.pos++ // [
	end := strings.IndexByte(p.s[p.pos:], ']')
	if end < 0 {
		return step{}, p.errorf("missing ']'")
	}
	inner := strings.TrimSpace(p.s[p.pos : p.pos+end])
	p.pos += end + 1
	switch {
	case inner == "" || inner == "*":
		return step{kind: eachStep}, nil
	case strings.HasPrefix(inner, `"`) && strings.HasSuffix(inner, `"`) && len(inner) >= 2:
		return step{kind: keyStep, key: inner[1 : len(inner)-1]}, nil
	case strings.Contains(inner, ":"):
		lo, hi, _ := strings.Cut(inner, ":")
		st := step{kind: sliceStep}
		for _, b := range []struct {
			txt string
			dst **int
		}{{lo, &st.lo}, {hi, &st.hi}} {
			if t := strings.TrimSpace(b.txt); t != "" {
				v, err := strconv.Atoi(t)
				if err != nil {
					return step{}, p.errorf("bad slice bound %q", t)
				}
				*b.dst = &v
			}
		}
		return st, nil
	}
	v, err := strconv.Atoi(inner)
	if err != nil {
		return step{kind: keyStep, key: inner}, nil
	}
	return step{kind: indexStep, index: v}, nil
}

// eval applies steps to n. It returns nil when nothing matches.
func eval(n *Node, steps []step) *Node {
	if n == nil || len(steps) == 0 {
		return n
	}
	s, rest := steps[0], steps[1:]
	switch s.kind {
	case keyStep:
		switch n.Kind {
		case Object:
			return eval(n.Get(s.key), rest)
		case Array:
			return mapFields(n.Vals, steps)
		}
	case eachStep:
		switch n.Kind {
		case Array, Object:
			return mapArray(n.Vals, rest)
		}
	case indexStep:
		if n.Kind == Array {
			i := s.index
			if i < 0 {
				i += len(n.Vals)
			}
			if i >= 0 && i < len(n.Vals) {
				return eval(n.Vals[i], rest)
			}
		}
	case sliceStep:
		if n.Kind == Array {
			lo, hi := bound(s.lo, 0, len(n.Vals)), bound(s.hi, len(n.Vals), len(n.Vals))
			if lo > hi {
				lo = hi
			}
			return eval(&Node{Kind: Array, Vals: n.Vals[lo:hi]}, rest)
		}
	case pickStep:
		switch n.Kind {
		case Object:
			out := &Node{Kind: Object}
			for _, f := range s.fields {
				if v := eval(n, f.path); v != nil {
					out.Keys = append(out.Keys, f.name)
					out.Vals = append(out.Vals, v)
				}
			}
			return eval(out, rest)
		case Array:
			return mapFields(n.Vals, steps)
		}
	}
	return nil
}

// mapFields handles a field (or pick) applied to an array: the run of field/pick
// steps maps over the elements, and later index/slice steps apply to the
// collected list — so items.name[1] is the second name. Use items[].x[1] to index
// inside each element instead.
func mapFields(vals []*Node, steps []step) *Node {
	run := 0
	for run < len(steps) && (steps[run].kind == keyStep || steps[run].kind == pickStep) {
		run++
	}
	return eval(mapArray(vals, steps[:run]), steps[run:])
}

func mapArray(vals []*Node, steps []step) *Node {
	out := &Node{Kind: Array}
	for _, el := range vals {
		if v := eval(el, steps); v != nil {
			out.Vals = append(out.Vals, v)
		}
	}
	return out
}

func bound(p *int, def, n int) int {
	if p == nil {
		return def
	}
	v := *p
	if v < 0 {
		v += n
	}
	return max(0, min(v, n))
}
