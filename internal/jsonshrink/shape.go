package jsonshrink

import (
	"fmt"
	"strings"
)

const (
	shapeMaxDepth = 12
	shapeMaxKeys  = 60
	shapeInline   = 80
	exampleMax    = 30
)

// Shape describes the structure of n: keys in order, value types, array lengths
// and one example per scalar field. Elements of an array are merged, so a list of
// 500 records reads as one record type; keys missing from some records get "?".
//
//	{
//	 total_count: number (42)
//	 items: [30] {
//	  name: string ("tokensaver")
//	  license?: {key: string ("mit")}|null
//	 }
//	}
func Shape(n *Node) string {
	s := &shape{}
	s.add(n)
	return s.label(0)
}

type shape struct {
	seen    int
	kinds   [6]int // count per Kind
	example string
	keys    []string
	fields  map[string]*shape
	elem    *shape
	minLen  int
	maxLen  int
}

func (s *shape) add(n *Node) {
	s.seen++
	s.kinds[n.Kind]++
	switch n.Kind {
	case Object:
		if s.fields == nil {
			s.fields = map[string]*shape{}
		}
		for i, k := range n.Keys {
			f, ok := s.fields[k]
			if !ok {
				f = &shape{}
				s.fields[k] = f
				s.keys = append(s.keys, k)
			}
			f.add(n.Vals[i])
		}
	case Array:
		l := len(n.Vals)
		if s.kinds[Array] == 1 {
			s.minLen, s.maxLen = l, l
		} else {
			s.minLen, s.maxLen = min(s.minLen, l), max(s.maxLen, l)
		}
		if s.elem == nil {
			s.elem = &shape{}
		}
		for _, v := range n.Vals {
			s.elem.add(v)
		}
	case String, Number, Bool:
		if s.example == "" {
			s.example = exampleOf(n)
		}
	}
}

func exampleOf(n *Node) string {
	if n.Kind != String {
		return n.Str
	}
	r := []rune(n.Str)
	if len(r) > exampleMax {
		return fmt.Sprintf("%q…", string(r[:exampleMax]))
	}
	return fmt.Sprintf("%q", n.Str)
}

func (s *shape) label(depth int) string {
	var parts []string
	if s.kinds[Object] > 0 {
		parts = append(parts, s.objectLabel(depth))
	}
	if s.kinds[Array] > 0 {
		parts = append(parts, s.arrayLabel(depth))
	}
	var scalars []string
	for _, k := range []Kind{String, Number, Bool} {
		if s.kinds[k] > 0 {
			scalars = append(scalars, [...]string{String: "string", Number: "number", Bool: "bool"}[k])
		}
	}
	if len(scalars) > 0 {
		label := strings.Join(scalars, "|")
		if s.example != "" {
			label += " (" + s.example + ")"
		}
		parts = append(parts, label)
	}
	if s.kinds[Null] > 0 {
		parts = append(parts, "null")
	}
	return strings.Join(parts, "|")
}

func (s *shape) objectLabel(depth int) string {
	if len(s.keys) == 0 {
		return "{}"
	}
	if depth >= shapeMaxDepth {
		return "{…}"
	}
	keys := s.keys
	more := 0
	if len(keys) > shapeMaxKeys {
		keys, more = keys[:shapeMaxKeys], len(keys)-shapeMaxKeys
	}
	lines := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		f := s.fields[k]
		name := k
		if f.seen < s.kinds[Object] {
			name += "?"
		}
		lines = append(lines, name+": "+f.label(depth+1))
	}
	if more > 0 {
		lines = append(lines, fmt.Sprintf("… %d more keys", more))
	}
	if inline := "{" + strings.Join(lines, ", ") + "}"; len(inline) <= shapeInline && !strings.Contains(inline, "\n") {
		return inline
	}
	ind := strings.Repeat(" ", depth+1)
	return "{\n" + ind + strings.Join(lines, "\n"+ind) + "\n" + strings.Repeat(" ", depth) + "}"
}

func (s *shape) arrayLabel(depth int) string {
	n := fmt.Sprint(s.minLen)
	if s.maxLen != s.minLen {
		n = fmt.Sprintf("%d-%d", s.minLen, s.maxLen)
	}
	if s.elem == nil || s.elem.seen == 0 {
		return "[" + n + "]"
	}
	return "[" + n + "] " + s.elem.label(depth)
}
