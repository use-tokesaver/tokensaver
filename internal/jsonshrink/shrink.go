package jsonshrink

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"
)

// Options controls how a JSON document is shrunk. Zero values mean "off", except
// that null/empty values are dropped unless KeepEmpty is set.
type Options struct {
	Select        string   // select expression, see select.go
	DropKeys      []string // key names or globs (path.Match syntax) removed at any depth
	KeepEmpty     bool     // keep null, "", [] and {} values
	MaxStr        int      // truncate strings longer than this many characters
	DropListsOver int      // replace arrays longer than this with a count (not the root)
	MaxItems      int      // keep only the first N items of every array
}

// Apply returns the shrunk document. It does not modify root.
func Apply(root *Node, o Options) (*Node, error) {
	n := root
	if s := strings.TrimSpace(o.Select); s != "" {
		steps, err := parseSelect(s)
		if err != nil {
			return nil, err
		}
		if n = eval(root, steps); n == nil {
			return nil, noMatchError(root, s)
		}
	}
	for _, g := range o.DropKeys {
		if _, err := path.Match(g, ""); err != nil {
			return nil, fmt.Errorf("drop_keys: bad pattern %q", g)
		}
	}
	return transform(n, o, true), nil
}

func noMatchError(root *Node, sel string) error {
	msg := fmt.Sprintf("select %q matched nothing", sel)
	switch root.Kind {
	case Object:
		keys := root.Keys
		if len(keys) > 30 {
			keys = append(keys[:30:30], "…")
		}
		msg += "; top-level keys: " + strings.Join(keys, ", ")
	case Array:
		msg += fmt.Sprintf("; the root is an array of %d items (try [0] or [].field)", len(root.Vals))
	}
	return fmt.Errorf("%s", msg)
}

// transform rebuilds n with every option applied bottom-up, so emptiness and list
// lengths are judged after inner noise is gone.
func transform(n *Node, o Options, isRoot bool) *Node {
	switch n.Kind {
	case String:
		if o.MaxStr > 0 && utf8.RuneCountInString(n.Str) > o.MaxStr {
			r := []rune(n.Str)
			return strNode(fmt.Sprintf("%s…(+%d chars)", string(r[:o.MaxStr]), len(r)-o.MaxStr))
		}
		return n
	case Object:
		out := &Node{Kind: Object}
		for i, k := range n.Keys {
			if dropKey(k, o.DropKeys) {
				continue
			}
			v := transform(n.Vals[i], o, false)
			if !o.KeepEmpty && isEmpty(v) {
				continue
			}
			out.Keys = append(out.Keys, k)
			out.Vals = append(out.Vals, v)
		}
		return out
	case Array:
		out := &Node{Kind: Array}
		for _, el := range n.Vals {
			v := transform(el, o, false)
			if !o.KeepEmpty && isEmpty(v) {
				continue
			}
			out.Vals = append(out.Vals, v)
		}
		total := len(out.Vals)
		if !isRoot && o.DropListsOver > 0 && total > o.DropListsOver {
			return strNode(fmt.Sprintf("[%d items omitted]", total))
		}
		if o.MaxItems > 0 && total > o.MaxItems {
			out.Vals = append(out.Vals[:o.MaxItems:o.MaxItems], strNode(fmt.Sprintf("… %d more items", total-o.MaxItems)))
		}
		return out
	}
	return n
}

func dropKey(k string, globs []string) bool {
	for _, g := range globs {
		if ok, _ := path.Match(g, k); ok {
			return true
		}
	}
	return false
}

func isEmpty(n *Node) bool {
	switch n.Kind {
	case Null:
		return true
	case String:
		return n.Str == ""
	case Array, Object:
		return len(n.Vals) == 0
	}
	return false
}
