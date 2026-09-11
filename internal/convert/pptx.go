package convert

import (
	"fmt"
	"strconv"
	"strings"
)

func convertPPTX(data []byte) (*Doc, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	pres, err := p.xml("ppt/presentation.xml")
	if err != nil {
		return nil, fmt.Errorf("read PPTX: %w", err)
	}
	rels := p.rels("ppt/presentation.xml")
	var b strings.Builder
	n := 0
	for _, id := range pres.find("sldIdLst").findAll("sldId") {
		part := rels[id.attrRel("id")].Target
		slide, err := p.xml(part)
		if err != nil {
			continue
		}
		n++
		title, body := slideText(slide.find("spTree"))
		if title != "" {
			fmt.Fprintf(&b, "## Slide %d: %s\n\n", n, title)
		} else {
			fmt.Fprintf(&b, "## Slide %d\n\n", n)
		}
		if body != "" {
			b.WriteString(body + "\n\n")
		}
		for _, r := range p.rels(part) {
			if strings.HasSuffix(r.Type, "/notesSlide") {
				if notes, err := p.xml(r.Target); err == nil {
					if t := notesText(notes.find("spTree")); t != "" {
						b.WriteString("Notes: " + t + "\n\n")
					}
				}
			}
		}
	}
	return &Doc{Markdown: cleanText(b.String())}, nil
}

// skipPlaceholders carry slide furniture, not content.
var skipPlaceholders = map[string]bool{"sldNum": true, "dt": true, "ftr": true, "hdr": true, "sldImg": true}

// slideText walks a slide's shape tree in order and returns its title and the
// rest of its text: body placeholders as bullets, other text boxes as lines,
// tables as Markdown tables.
func slideText(tree *xnode) (title, body string) {
	var titles, lines []string
	var walkShapes func(n *xnode)
	walkShapes = func(n *xnode) {
		if n == nil {
			return
		}
		for _, k := range n.kids {
			switch k.name {
			case "sp":
				ph := k.child("nvSpPr").child("nvPr").child("ph")
				typ := ph.attr("type")
				if skipPlaceholders[typ] {
					continue
				}
				paras := drawingParagraphs(k.child("txBody"))
				switch {
				case typ == "title" || typ == "ctrTitle":
					for _, pp := range paras {
						titles = append(titles, pp.text)
					}
				case ph != nil && (typ == "" || typ == "body" || typ == "obj"):
					for _, pp := range paras {
						lines = append(lines, strings.Repeat("  ", pp.level)+"- "+pp.text)
					}
				default:
					for _, pp := range paras {
						lines = append(lines, escapeHeading(pp.text))
					}
				}
			case "grpSp":
				walkShapes(k)
			case "graphicFrame":
				if tbl := k.find("tbl"); tbl != nil {
					if t := drawingTable(tbl); t != "" {
						lines = append(lines, "", t, "")
					}
				}
			case "AlternateContent":
				if c := k.child("Choice"); c != nil {
					walkShapes(c)
				} else {
					walkShapes(k.child("Fallback"))
				}
			}
		}
	}
	walkShapes(tree)
	return strings.Join(titles, " "), strings.TrimSpace(strings.Join(lines, "\n"))
}

func notesText(tree *xnode) string {
	var parts []string
	for _, sp := range tree.findAll("sp") {
		if sp.child("nvSpPr").child("nvPr").child("ph").attr("type") != "body" {
			continue
		}
		for _, pp := range drawingParagraphs(sp.child("txBody")) {
			parts = append(parts, pp.text)
		}
	}
	return strings.Join(parts, " ")
}

type drawingPara struct {
	text  string
	level int
}

// drawingParagraphs returns the non-empty paragraphs of a DrawingML text body.
func drawingParagraphs(txBody *xnode) []drawingPara {
	var out []drawingPara
	if txBody == nil {
		return out
	}
	for _, p := range txBody.kids {
		if p.name != "p" {
			continue
		}
		var t strings.Builder
		for _, r := range p.kids {
			switch r.name {
			case "r", "fld":
				t.WriteString(r.child("t").text)
			case "br":
				t.WriteString(" ")
			}
		}
		text := strings.Join(strings.Fields(t.String()), " ")
		if text == "" {
			continue
		}
		lvl, _ := strconv.Atoi(p.child("pPr").attr("lvl"))
		out = append(out, drawingPara{text: text, level: lvl})
	}
	return out
}

func drawingTable(tbl *xnode) string {
	var rows [][]string
	for _, tr := range tbl.kids {
		if tr.name != "tr" {
			continue
		}
		var row []string
		for _, tc := range tr.kids {
			if tc.name != "tc" {
				continue
			}
			var parts []string
			for _, pp := range drawingParagraphs(tc.child("txBody")) {
				parts = append(parts, pp.text)
			}
			row = append(row, strings.Join(parts, " "))
		}
		rows = append(rows, row)
	}
	return markdownTable(rows)
}
