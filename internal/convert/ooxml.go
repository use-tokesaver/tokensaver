package convert

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// maxPartSize guards against zip bombs: no single part of an Office file may
// inflate beyond this.
const maxPartSize = 256 << 20

// pkg is an Office Open XML package (a zip of XML parts).
type pkg struct {
	files map[string]*zip.File
}

func openPkg(data []byte) (*pkg, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a valid Office file: %w", err)
	}
	p := &pkg{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		p.files[strings.TrimPrefix(f.Name, "/")] = f
	}
	return p, nil
}

func (p *pkg) read(name string) ([]byte, error) {
	f, ok := p.files[name]
	if !ok {
		return nil, fmt.Errorf("missing part %s", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxPartSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPartSize {
		return nil, fmt.Errorf("part %s is too large", name)
	}
	return data, nil
}

func (p *pkg) xml(name string) (*xnode, error) {
	data, err := p.read(name)
	if err != nil {
		return nil, err
	}
	return parseXML(data)
}

type rel struct{ Target, Type string }

// rels reads the relationships of a part, resolving targets to package paths.
func (p *pkg) rels(part string) map[string]rel {
	out := map[string]rel{}
	root, err := p.xml(path.Join(path.Dir(part), "_rels", path.Base(part)+".rels"))
	if err != nil {
		return out
	}
	for _, r := range root.findAll("Relationship") {
		target := r.attr("Target")
		if r.attr("TargetMode") != "External" {
			if strings.HasPrefix(target, "/") {
				target = strings.TrimPrefix(target, "/")
			} else {
				target = path.Join(path.Dir(part), target)
			}
		}
		out[r.attr("Id")] = rel{Target: target, Type: r.attr("Type")}
	}
	return out
}

// xnode is a minimal XML element tree. Names are local (namespace prefixes
// dropped), which is all the OOXML converters need. text is the element's own
// character data.
type xnode struct {
	name  string
	attrs []xml.Attr
	kids  []*xnode
	text  string
}

func parseXML(data []byte) (*xnode, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	root := &xnode{}
	stack := []*xnode{root}
	text := []*strings.Builder{{}} // character data of each open element
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bad XML: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &xnode{name: t.Name.Local, attrs: t.Attr}
			parent := stack[len(stack)-1]
			parent.kids = append(parent.kids, n)
			stack = append(stack, n)
			text = append(text, &strings.Builder{})
		case xml.EndElement:
			top := len(stack) - 1
			stack[top].text = text[top].String()
			stack, text = stack[:top], text[:top]
		case xml.CharData:
			text[len(text)-1].Write(t)
		}
	}
	return root, nil
}

// attr returns the attribute with this local name. Relationship ids ("r:id")
// share the local name "id" with plain ids, so prefer attrRel for those.
func (n *xnode) attr(local string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.attrs {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// attrRel returns a relationships-namespace attribute such as r:id or r:embed.
func (n *xnode) attrRel(local string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.attrs {
		if a.Name.Local == local && strings.Contains(a.Name.Space, "relationships") {
			return a.Value
		}
	}
	return ""
}

func (n *xnode) child(name string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name == name {
			return k
		}
	}
	return nil
}

// find returns the first descendant with this name (depth-first).
func (n *xnode) find(name string) *xnode {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name == name {
			return k
		}
		if f := k.find(name); f != nil {
			return f
		}
	}
	return nil
}

func (n *xnode) findAll(name string) []*xnode {
	var out []*xnode
	if n == nil {
		return out
	}
	for _, k := range n.kids {
		if k.name == name {
			out = append(out, k)
		}
		out = append(out, k.findAll(name)...)
	}
	return out
}

// onOff reads an OOXML boolean property element like <w:b/> or <w:b w:val="0"/>.
func onOff(n *xnode) bool {
	if n == nil {
		return false
	}
	switch n.attr("val") {
	case "0", "false", "off", "none":
		return false
	}
	return true
}

// markdownTable renders rows as a Markdown table, using the first row as the
// header and padding short rows.
func markdownTable(rows [][]string) string {
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	if width == 0 {
		return ""
	}
	var b strings.Builder
	for i, r := range rows {
		b.WriteString("|")
		for c := range width {
			cell := ""
			if c < len(r) {
				cell = tableCell(r[c])
			}
			if cell == "" {
				b.WriteString(" |")
			} else {
				b.WriteString(" " + cell + " |")
			}
		}
		b.WriteString("\n")
		if i == 0 {
			b.WriteString("|" + strings.Repeat("---|", width) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func tableCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}
