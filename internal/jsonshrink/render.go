package jsonshrink

import (
	"fmt"
	"strings"
)

// Render formats n for the LLM. With table=true, an array of objects becomes a
// Markdown table, and an object becomes "key: value" lines with its arrays of
// objects as tables. Otherwise (or when there is nothing tabular) it is compact
// JSON from Pretty.
func Render(n *Node, table bool) string {
	if table {
		if isTabular(n) {
			return renderTable(n)
		}
		if n.Kind == Object && hasTabularChild(n) {
			var b strings.Builder
			for i, k := range n.Keys {
				v := n.Vals[i]
				if isTabular(v) {
					fmt.Fprintf(&b, "\n%s:\n%s\n\n", k, renderTable(v))
				} else {
					fmt.Fprintf(&b, "%s: %s\n", k, cellText(v))
				}
			}
			return strings.TrimSpace(strings.ReplaceAll(b.String(), "\n\n\n", "\n\n"))
		}
	}
	return n.Pretty()
}

// isTabular: an array whose elements are objects, apart from trailing markers
// like "… 480 more items".
func isTabular(n *Node) bool {
	if n.Kind != Array || len(n.Vals) == 0 || n.Vals[0].Kind != Object {
		return false
	}
	seenScalar := false
	for _, v := range n.Vals {
		if v.Kind == Object {
			if seenScalar {
				return false
			}
			continue
		}
		seenScalar = true
	}
	return true
}

func hasTabularChild(n *Node) bool {
	for _, v := range n.Vals {
		if isTabular(v) {
			return true
		}
	}
	return false
}

func renderTable(n *Node) string {
	var cols []string
	index := map[string]int{}
	var rows []map[string]string
	var notes []string
	for _, el := range n.Vals {
		if el.Kind != Object {
			notes = append(notes, cellText(el))
			continue
		}
		row := map[string]string{}
		flatten(el, "", row, func(c string) {
			if _, ok := index[c]; !ok {
				index[c] = len(cols)
				cols = append(cols, c)
			}
		})
		rows = append(rows, row)
	}
	var b strings.Builder
	b.WriteString("|")
	for _, c := range cols {
		b.WriteString(" " + escapeCell(c) + " |")
	}
	b.WriteString("\n|")
	b.WriteString(strings.Repeat("---|", len(cols)))
	for _, r := range rows {
		b.WriteString("\n|")
		for _, c := range cols {
			if v := r[c]; v != "" {
				b.WriteString(" " + v + " |")
			} else {
				b.WriteString(" |")
			}
		}
	}
	for _, note := range notes {
		b.WriteString("\n" + note)
	}
	return b.String()
}

// flatten turns nested objects into dotted columns (owner.login). Arrays of
// scalars become "a, b, c"; other arrays stay compact JSON.
func flatten(n *Node, prefix string, row map[string]string, addCol func(string)) {
	for i, k := range n.Keys {
		name := k
		if prefix != "" {
			name = prefix + "." + k
		}
		v := n.Vals[i]
		if v.Kind == Object && len(v.Vals) > 0 {
			flatten(v, name, row, addCol)
			continue
		}
		addCol(name)
		row[name] = escapeCell(cellText(v))
	}
}

// cellText is the shortest faithful text for a value: strings unquoted, arrays
// of scalars comma-joined, anything else compact JSON.
func cellText(n *Node) string {
	switch n.Kind {
	case String:
		return n.Str
	case Null:
		return "null"
	case Bool, Number:
		return n.Str
	case Array:
		parts := make([]string, 0, len(n.Vals))
		for _, v := range n.Vals {
			if v.Kind == Array || v.Kind == Object {
				return n.Compact()
			}
			parts = append(parts, cellText(v))
		}
		return strings.Join(parts, ", ")
	}
	return n.Compact()
}

func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\r\n", " ")
	return strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
}
