package convert

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/use-tokesaver/tokensaver/internal/source"
)

type docxConverter struct{}

func (docxConverter) Convert(ctx context.Context, src *source.Source, opts Options) (*Doc, error) {
	return convertDOCX(src.Data)
}

type docxStyle struct {
	name, basedOn string
	outline       int // outline level + 1; 0 = none
	numID, ilvl   string
}

type docxWriter struct {
	b      strings.Builder
	styles map[string]docxStyle
	numFmt map[string]map[string]string // numId → ilvl → numFmt
	rels   map[string]rel
	inList bool
}

func convertDOCX(data []byte) (*Doc, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	root, err := p.xml("word/document.xml")
	if err != nil {
		return nil, fmt.Errorf("read DOCX: %w", err)
	}
	w := &docxWriter{
		styles: loadDocxStyles(p),
		numFmt: loadDocxNumbering(p),
		rels:   p.rels("word/document.xml"),
	}
	w.blocks(root.find("body"))
	return &Doc{Markdown: cleanText(w.b.String())}, nil
}

func loadDocxStyles(p *pkg) map[string]docxStyle {
	out := map[string]docxStyle{}
	root, err := p.xml("word/styles.xml")
	if err != nil {
		return out
	}
	for _, s := range root.findAll("style") {
		st := docxStyle{
			name:    strings.ToLower(s.child("name").attr("val")),
			basedOn: s.child("basedOn").attr("val"),
		}
		ppr := s.child("pPr")
		if lvl, err := strconv.Atoi(ppr.child("outlineLvl").attr("val")); err == nil && lvl < 9 {
			st.outline = lvl + 1
		}
		if num := ppr.child("numPr"); num != nil {
			st.numID, st.ilvl = num.child("numId").attr("val"), num.child("ilvl").attr("val")
		}
		out[s.attr("styleId")] = st
	}
	return out
}

func loadDocxNumbering(p *pkg) map[string]map[string]string {
	out := map[string]map[string]string{}
	root, err := p.xml("word/numbering.xml")
	if err != nil {
		return out
	}
	abstract := map[string]map[string]string{}
	for _, a := range root.findAll("abstractNum") {
		lvls := map[string]string{}
		for _, l := range a.findAll("lvl") {
			lvls[l.attr("ilvl")] = l.child("numFmt").attr("val")
		}
		abstract[a.attr("abstractNumId")] = lvls
	}
	for _, n := range root.findAll("num") {
		if lvls, ok := abstract[n.child("abstractNumId").attr("val")]; ok {
			out[n.attr("numId")] = lvls
		}
	}
	return out
}

// headingLevel follows a paragraph style (and what it is based on) to a heading
// level: "Title" → 1, "Heading N" → N, or an explicit outline level.
func (w *docxWriter) headingLevel(styleID string) int {
	for range 10 {
		st, ok := w.styles[styleID]
		if !ok {
			return 0
		}
		if st.name == "title" {
			return 1
		}
		if n, ok := strings.CutPrefix(st.name, "heading "); ok {
			if lvl, err := strconv.Atoi(n); err == nil {
				return min(lvl, 6)
			}
		}
		if st.outline > 0 {
			return min(st.outline, 6)
		}
		styleID = st.basedOn
	}
	return 0
}

// listMarker returns the list marker and nesting depth for a paragraph, or ""
// if it is not a list item.
func (w *docxWriter) listMarker(ppr *xnode, styleID string) (string, int) {
	numID, ilvl := "", "0"
	if num := ppr.child("numPr"); num != nil {
		numID = num.child("numId").attr("val")
		if v := num.child("ilvl").attr("val"); v != "" {
			ilvl = v
		}
	} else if st, ok := w.styles[styleID]; ok && st.numID != "" {
		numID = st.numID
		if st.ilvl != "" {
			ilvl = st.ilvl
		}
	}
	depth, _ := strconv.Atoi(ilvl)
	if numID == "" || numID == "0" {
		name := w.styles[styleID].name
		switch {
		case strings.HasPrefix(name, "list bullet"):
			return "-", 0
		case strings.HasPrefix(name, "list number"):
			return "1.", 0
		}
		return "", 0
	}
	switch w.numFmt[numID][ilvl] {
	case "bullet":
		return "-", depth
	case "none":
		return "", 0
	}
	return "1.", depth
}

func (w *docxWriter) blocks(n *xnode) {
	if n == nil {
		return
	}
	for _, k := range n.kids {
		switch k.name {
		case "p":
			w.paragraph(k)
		case "tbl":
			w.endList()
			w.b.WriteString(markdownTable(w.tableRows(k)) + "\n\n")
		case "sdt":
			w.blocks(k.child("sdtContent"))
		case "customXml", "ins", "smartTag":
			w.blocks(k)
		}
	}
}

func (w *docxWriter) endList() {
	if w.inList {
		w.b.WriteString("\n")
		w.inList = false
	}
}

func (w *docxWriter) paragraph(p *xnode) {
	ppr := p.child("pPr")
	styleID := ppr.child("pStyle").attr("val")
	level := w.headingLevel(styleID)
	if lvl, err := strconv.Atoi(ppr.child("outlineLvl").attr("val")); err == nil && lvl < 9 && level == 0 {
		level = min(lvl+1, 6)
	}
	if level > 0 {
		text := strings.Join(strings.Fields(w.inline(p, false)), " ")
		if text != "" {
			w.endList()
			fmt.Fprintf(&w.b, "%s %s\n\n", strings.Repeat("#", level), text)
		}
		return
	}
	marker, depth := w.listMarker(ppr, styleID)
	if marker != "" {
		text := strings.Join(strings.Fields(w.inline(p, true)), " ")
		if text != "" {
			fmt.Fprintf(&w.b, "%s%s %s\n", strings.Repeat("  ", depth), marker, text)
			w.inList = true
		}
		return
	}
	text := strings.TrimSpace(w.inline(p, true))
	if text == "" {
		return
	}
	w.endList()
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = escapeHeading(strings.TrimSpace(l))
	}
	w.b.WriteString(strings.Join(lines, "\n") + "\n\n")
}

type span struct {
	text         string
	bold, italic bool
}

// inline renders a paragraph's runs, hyperlinks and fields as Markdown text.
// With format=false bold/italic markers are left out (headings, table cells).
func (w *docxWriter) inline(p *xnode, format bool) string {
	var spans []span
	var collect func(n *xnode, link bool)
	collect = func(n *xnode, link bool) {
		if n == nil {
			return
		}
		for _, k := range n.kids {
			switch k.name {
			case "r":
				rpr := k.child("rPr")
				s := span{bold: format && !link && onOff(rpr.child("b")), italic: format && !link && onOff(rpr.child("i"))}
				var t strings.Builder
				for _, c := range k.kids {
					switch c.name {
					case "t":
						t.WriteString(c.text)
					case "tab":
						t.WriteString(" ")
					case "br", "cr":
						t.WriteString("\n")
					case "noBreakHyphen":
						t.WriteString("-")
					}
				}
				s.text = t.String()
				spans = append(spans, s)
			case "hyperlink":
				var inner []span
				saved := spans
				spans = nil
				collect(k, true)
				inner, spans = spans, saved
				var t strings.Builder
				for _, s := range inner {
					t.WriteString(s.text)
				}
				text := strings.TrimSpace(t.String())
				if target := w.rels[k.attrRel("id")].Target; target != "" && text != "" && strings.Contains(target, "://") {
					spans = append(spans, span{text: "[" + text + "](" + target + ")"})
				} else {
					spans = append(spans, span{text: t.String()})
				}
			case "fldSimple", "ins", "smartTag", "customXml", "moveTo":
				collect(k, link)
			case "sdt":
				collect(k.child("sdtContent"), link)
			}
		}
	}
	collect(p, false)
	return renderSpans(spans)
}

// renderSpans merges neighbouring runs with the same formatting (Word splits
// text into runs for spell-check and revision marks) and wraps bold/italic text
// in markers, keeping edge spaces outside them.
func renderSpans(spans []span) string {
	var merged []span
	for _, s := range spans {
		if s.text == "" {
			continue
		}
		if n := len(merged); n > 0 && merged[n-1].bold == s.bold && merged[n-1].italic == s.italic {
			merged[n-1].text += s.text
			continue
		}
		merged = append(merged, s)
	}
	var b strings.Builder
	for _, s := range merged {
		marker := ""
		switch {
		case s.bold && s.italic:
			marker = "***"
		case s.bold:
			marker = "**"
		case s.italic:
			marker = "*"
		}
		core := strings.TrimSpace(s.text)
		if marker == "" || core == "" {
			b.WriteString(s.text)
			continue
		}
		lead := s.text[:len(s.text)-len(strings.TrimLeft(s.text, " \t\n"))]
		trail := s.text[len(strings.TrimRight(s.text, " \t\n")):]
		b.WriteString(lead + marker + core + marker + trail)
	}
	return b.String()
}

// tableRows flattens a table to text cells. Merged cells keep their column
// position (gridSpan adds empty cells).
func (w *docxWriter) tableRows(tbl *xnode) [][]string {
	var rows [][]string
	for _, tr := range tbl.findAllRows() {
		var row []string
		for _, tc := range tr.kids {
			if tc.name != "tc" {
				continue
			}
			var parts []string
			for _, p := range tc.findAll("p") {
				if t := strings.Join(strings.Fields(w.inline(p, false)), " "); t != "" {
					parts = append(parts, t)
				}
			}
			row = append(row, strings.Join(parts, " "))
			if cols, err := strconv.Atoi(tc.child("tcPr").child("gridSpan").attr("val")); err == nil {
				for range cols - 1 {
					row = append(row, "")
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// findAllRows returns the table's own rows, not those of tables nested in cells.
func (n *xnode) findAllRows() []*xnode {
	if n == nil {
		return nil
	}
	var rows []*xnode
	for _, k := range n.kids {
		switch k.name {
		case "tr":
			rows = append(rows, k)
		case "sdt":
			rows = append(rows, k.child("sdtContent").findAllRows()...)
		case "customXml":
			rows = append(rows, k.findAllRows()...)
		}
	}
	return rows
}
