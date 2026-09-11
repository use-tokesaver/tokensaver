package convert

import (
	"archive/zip"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// Test fixtures are generated in code so the repo carries no opaque binaries and
// each test shows exactly what its input contains.

// makePDF builds a minimal PDF (Helvetica text, one line per "\n") with a Title
// in its info dictionary.
func makePDF(title string, pages []string) []byte {
	var b bytes.Buffer
	var offsets []int
	obj := func(body string) int {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", len(offsets), body)
		return len(offsets)
	}
	esc := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`)
	b.WriteString("%PDF-1.4\n")
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	var kids []string
	for i := range pages {
		kids = append(kids, fmt.Sprintf("%d 0 R", 4+2*i))
	}
	obj(fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(pages)))
	obj("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	for i, text := range pages {
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", 5+2*i))
		var cs strings.Builder
		cs.WriteString("BT /F1 11 Tf 72 740 Td 16 TL\n")
		for _, line := range strings.Split(text, "\n") {
			fmt.Fprintf(&cs, "(%s) Tj T*\n", esc.Replace(line))
		}
		cs.WriteString("ET")
		obj(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", cs.Len(), cs.String()))
	}
	info := obj(fmt.Sprintf("<< /Title (%s) >>", esc.Replace(title)))
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, info, xref)
	return b.Bytes()
}

func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const wordNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

func makeDOCX(t *testing.T) []byte {
	para := func(style, inner string) string {
		ppr := ""
		if style != "" {
			ppr = `<w:pPr><w:pStyle w:val="` + style + `"/></w:pPr>`
		}
		return `<w:p>` + ppr + inner + `</w:p>`
	}
	run := func(text string, props string) string {
		return `<w:r><w:rPr>` + props + `</w:rPr><w:t xml:space="preserve">` + text + `</w:t></w:r>`
	}
	listItem := func(numID, ilvl, text string) string {
		return `<w:p><w:pPr><w:numPr><w:ilvl w:val="` + ilvl + `"/><w:numId w:val="` + numID + `"/></w:numPr></w:pPr>` + run(text, "") + `</w:p>`
	}
	cell := func(text string, span int) string {
		tcpr := ""
		if span > 1 {
			tcpr = fmt.Sprintf(`<w:tcPr><w:gridSpan w:val="%d"/></w:tcPr>`, span)
		}
		return `<w:tc>` + tcpr + `<w:p>` + run(text, "") + `</w:p></w:tc>`
	}
	body := strings.Join([]string{
		para("Title", run("Project Plan", "")),
		para("", run("This plan is ", "")+run("very", "<w:b/>")+run(" important", "<w:b/>")+run(" and ", "")+run("urgent", "<w:i/>")+run(".", "")),
		para("Heading1", run("Goals", "")),
		listItem("1", "0", "Ship v1"),
		listItem("1", "1", "Write docs"),
		listItem("2", "0", "First step"),
		listItem("2", "0", "Second step"),
		para("CustomHeading", run("Budget", "")),
		para("", `<w:hyperlink r:id="rId9">`+run("the site", "")+`</w:hyperlink>`+run(" has details", "")+`<w:del><w:r><w:delText>removed text</w:delText></w:r></w:del>`),
		`<w:tbl><w:tr>` + cell("Item", 1) + cell("Cost", 1) + cell("Note", 1) + `</w:tr><w:tr>` + cell("Servers", 1) + cell("100", 1) + cell("a|b", 1) + `</w:tr><w:tr>` + cell("Total spanning", 2) + cell("x", 1) + `</w:tr></w:tbl>`,
		para("", run("# not a heading", "")),
	}, "")
	return makeZip(t, map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document ` + wordNS + `><w:body>` + body + `<w:sectPr/></w:body></w:document>`,
		"word/styles.xml": `<?xml version="1.0"?><w:styles ` + wordNS + `>
			<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/></w:style>
			<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/></w:style>
			<w:style w:type="paragraph" w:styleId="CustomHeading"><w:name w:val="My Heading"/><w:basedOn w:val="Heading2"/></w:style>
			<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/></w:style>
		</w:styles>`,
		"word/numbering.xml": `<?xml version="1.0"?><w:numbering ` + wordNS + `>
			<w:abstractNum w:abstractNumId="10"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="bullet"/></w:lvl></w:abstractNum>
			<w:abstractNum w:abstractNumId="20"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>
			<w:num w:numId="1"><w:abstractNumId w:val="10"/></w:num>
			<w:num w:numId="2"><w:abstractNumId w:val="20"/></w:num>
		</w:numbering>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
			<Relationship Id="rId9" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/plan" TargetMode="External"/>
		</Relationships>`,
	})
}

const pptNS = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

func makePPTX(t *testing.T) []byte {
	shape := func(phType string, paras ...string) string {
		ph := ""
		if phType != "-" {
			ph = `<p:ph type="` + phType + `"/>`
			if phType == "" {
				ph = `<p:ph idx="1"/>`
			}
		}
		var ps strings.Builder
		for _, p := range paras {
			lvl := ""
			if strings.HasPrefix(p, ">") {
				lvl, p = `<a:pPr lvl="1"/>`, p[1:]
			}
			ps.WriteString(`<a:p>` + lvl + `<a:r><a:t>` + p + `</a:t></a:r></a:p>`)
		}
		return `<p:sp><p:nvSpPr><p:cNvPr id="2" name="s"/><p:cNvSpPr/><p:nvPr>` + ph + `</p:nvPr></p:nvSpPr><p:txBody>` + ps.String() + `</p:txBody></p:sp>`
	}
	slide := func(shapes ...string) string {
		return `<?xml version="1.0"?><p:sld ` + pptNS + `><p:cSld><p:spTree>` + strings.Join(shapes, "") + `</p:spTree></p:cSld></p:sld>`
	}
	table := `<p:graphicFrame><a:graphic><a:graphicData><a:tbl>
		<a:tr><a:tc><a:txBody><a:p><a:r><a:t>Region</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>Sales</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
		<a:tr><a:tc><a:txBody><a:p><a:r><a:t>EU</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>42</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
	</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`
	rels := func(entries ...string) string {
		return `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + strings.Join(entries, "") + `</Relationships>`
	}
	return makeZip(t, map[string]string{
		"ppt/presentation.xml": `<?xml version="1.0"?><p:presentation ` + pptNS + `><p:sldIdLst><p:sldId id="256" r:id="rId2"/><p:sldId id="257" r:id="rId3"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": rels(
			`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/>`,
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>`),
		"ppt/slides/slide1.xml": slide(
			shape("ctrTitle", "Quarterly Review"),
			shape("", "Revenue up", ">EU strongest", "Costs flat"),
			shape("sldNum", "1")),
		"ppt/slides/_rels/slide1.xml.rels": rels(`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide" Target="../notesSlides/notesSlide1.xml"/>`),
		"ppt/notesSlides/notesSlide1.xml":  `<?xml version="1.0"?><p:notes ` + pptNS + `><p:cSld><p:spTree>` + shape("body", "Mention the EU deal.") + shape("sldNum", "1") + `</p:spTree></p:cSld></p:notes>`,
		"ppt/slides/slide2.xml":            slide(shape("-", "Free text box"), `<p:grpSp>`+table+`</p:grpSp>`),
	})
}

func makeXLSX(t *testing.T) []byte {
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "Sales")
	rows := [][]any{
		{"Region", nil, "Q1", "Q2"},
		{"EU", nil, 10, 20},
		{},
		{"US|CA", nil, 5, 7},
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		f.SetSheetRow("Sales", cell, &r)
	}
	f.SetCellFormula("Sales", "D5", "SUM(D2:D4)")
	f.SetCellValue("Sales", "A5", "Total")
	f.NewSheet("Empty")
	f.NewSheet("Secret")
	f.SetCellValue("Secret", "A1", "hidden value")
	f.SetSheetVisible("Secret", false)
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
