package testdoc

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const wordNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`

var xmlEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Word body builders. Styles available to WPara: Title, Heading1, Heading2,
// Heading3 and CustomHeading (a custom style based on Heading2). Numbering for
// WItem: "1" is a bulleted list, "2" a numbered one. The markup carries the
// revision ids, fonts and spacing Word writes, so the XML is as verbose as a
// real document's.

const (
	wParaAttrs = ` w:rsidR="00A81C3E" w:rsidRDefault="00A81C3E" w:rsidP="004F2B7D"`
	wSpacing   = `<w:spacing w:after="160" w:line="259" w:lineRule="auto"/>`
	wRunProps  = `<w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="22"/><w:szCs w:val="22"/><w:lang w:val="en-US"/>`
)

func WPara(style string, runs ...string) string {
	ppr := `<w:pPr>` + wSpacing + `</w:pPr>`
	if style != "" {
		ppr = `<w:pPr><w:pStyle w:val="` + style + `"/>` + wSpacing + `</w:pPr>`
	}
	return `<w:p` + wParaAttrs + `>` + ppr + strings.Join(runs, "") + `</w:p>`
}

// WRun is a run of text; props is extra run-property XML such as "<w:b/>".
func WRun(text, props string) string {
	return `<w:r w:rsidRPr="00C5094E"><w:rPr>` + wRunProps + props + `</w:rPr><w:t xml:space="preserve">` + xmlEsc.Replace(text) + `</w:t></w:r>`
}

func WItem(numID string, ilvl int, text string) string {
	return fmt.Sprintf(`<w:p><w:pPr><w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%s"/></w:numPr></w:pPr>%s</w:p>`, ilvl, numID, WRun(text, ""))
}

// WLink wraps runs in a hyperlink whose target is the relationship relID
// (see the links argument of DOCX).
func WLink(relID string, runs ...string) string {
	return `<w:hyperlink r:id="` + relID + `">` + strings.Join(runs, "") + `</w:hyperlink>`
}

func WCell(text string, span int) string {
	tcpr := ""
	if span > 1 {
		tcpr = fmt.Sprintf(`<w:tcPr><w:gridSpan w:val="%d"/></w:tcPr>`, span)
	}
	return `<w:tc>` + tcpr + `<w:p>` + WRun(text, "") + `</w:p></w:tc>`
}

func WRow(cells ...string) string { return `<w:tr>` + strings.Join(cells, "") + `</w:tr>` }

func WTable(rows ...string) string { return `<w:tbl>` + strings.Join(rows, "") + `</w:tbl>` }

// WTextTable is a table of plain single-span cells.
func WTextTable(rows [][]string) string {
	var b strings.Builder
	for _, r := range rows {
		var cells []string
		for _, c := range r {
			cells = append(cells, WCell(c, 1))
		}
		b.WriteString(WRow(cells...))
	}
	return WTable(b.String())
}

// DOCX packages a document body with the styles and numbering described above.
// links maps hyperlink relationship ids to their URLs.
func DOCX(body string, links map[string]string) []byte {
	var rels strings.Builder
	for id, target := range links {
		fmt.Fprintf(&rels, `<Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`, id, target)
	}
	return Zip(map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`,
		"word/document.xml":   `<?xml version="1.0"?><w:document ` + wordNS + `><w:body>` + body + `<w:sectPr/></w:body></w:document>`,
		"word/styles.xml": `<?xml version="1.0"?><w:styles ` + wordNS + `>
			<w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/></w:style>
			<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/></w:style>
			<w:style w:type="paragraph" w:styleId="CustomHeading"><w:name w:val="My Heading"/><w:basedOn w:val="Heading2"/></w:style>
			<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/></w:style>
			<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/></w:style>
		</w:styles>`,
		"word/numbering.xml": `<?xml version="1.0"?><w:numbering ` + wordNS + `>
			<w:abstractNum w:abstractNumId="10"><w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl><w:lvl w:ilvl="1"><w:numFmt w:val="bullet"/></w:lvl></w:abstractNum>
			<w:abstractNum w:abstractNumId="20"><w:lvl w:ilvl="0"><w:numFmt w:val="decimal"/></w:lvl></w:abstractNum>
			<w:num w:numId="1"><w:abstractNumId w:val="10"/></w:num>
			<w:num w:numId="2"><w:abstractNumId w:val="20"/></w:num>
		</w:numbering>`,
		"word/_rels/document.xml.rels": `<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + rels.String() + `</Relationships>`,
	})
}

// SampleDOCX exercises the DOCX converter's special cases: title and heading
// styles (incl. a custom style based on a heading), nested and numbered lists,
// bold/italic runs, a hyperlink, deleted text, a spanning table cell and text
// that looks like Markdown.
func SampleDOCX() []byte {
	body := strings.Join([]string{
		WPara("Title", WRun("Project Plan", "")),
		WPara("", WRun("This plan is ", ""), WRun("very", "<w:b/>"), WRun(" important", "<w:b/>"), WRun(" and ", ""), WRun("urgent", "<w:i/>"), WRun(".", "")),
		WPara("Heading1", WRun("Goals", "")),
		WItem("1", 0, "Ship v1"),
		WItem("1", 1, "Write docs"),
		WItem("2", 0, "First step"),
		WItem("2", 0, "Second step"),
		WPara("CustomHeading", WRun("Budget", "")),
		WPara("", WLink("rId9", WRun("the site", "")), WRun(" has details", ""), `<w:del><w:r><w:delText>removed text</w:delText></w:r></w:del>`),
		WTable(
			WRow(WCell("Item", 1), WCell("Cost", 1), WCell("Note", 1)),
			WRow(WCell("Servers", 1), WCell("100", 1), WCell("a|b", 1)),
			WRow(WCell("Total spanning", 2), WCell("x", 1))),
		WPara("", WRun("# not a heading", "")),
	}, "")
	return DOCX(body, map[string]string{"rId9": "https://example.com/plan"})
}

const pptNS = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"`

// SamplePPTX has two slides, listed out of order in the relationships: a title
// with nested bullets, speaker notes and a slide number; then a free text box
// and a table inside a group.
func SamplePPTX() []byte {
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
	return Zip(map[string]string{
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

// XLSX builds a workbook; sheets are written in order, and a sheet whose name
// starts with "." is hidden (the dot is dropped).
func XLSX(sheets []Sheet) []byte {
	f := excelize.NewFile()
	defer f.Close()
	for i, s := range sheets {
		name, hidden := strings.CutPrefix(s.Name, ".")
		if i == 0 {
			f.SetSheetName("Sheet1", name)
		} else {
			f.NewSheet(name)
		}
		for r, row := range s.Rows {
			cell, _ := excelize.CoordinatesToCellName(1, r+1)
			f.SetSheetRow(name, cell, &row)
		}
		for cell, formula := range s.Formulas {
			f.SetCellFormula(name, cell, formula)
		}
		if hidden {
			f.SetSheetVisible(name, false)
		}
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		panic(err)
	}
	return b.Bytes()
}

type Sheet struct {
	Name     string
	Rows     [][]any
	Formulas map[string]string // cell → formula
}

// SampleXLSX has a sheet with a gap column, an empty row, a pipe in a cell and
// a formula; an empty sheet; and a hidden sheet.
func SampleXLSX() []byte {
	return XLSX([]Sheet{
		{Name: "Sales", Rows: [][]any{
			{"Region", nil, "Q1", "Q2"},
			{"EU", nil, 10, 20},
			{},
			{"US|CA", nil, 5, 7},
			{"Total"},
		}, Formulas: map[string]string{"D5": "SUM(D2:D4)"}},
		{Name: "Empty"},
		{Name: ".Secret", Rows: [][]any{{"hidden value"}}},
	})
}
