// Package testdoc builds PDF, DOCX, PPTX and XLSX documents in code. Tests, the
// e2e suite and the A/B harness use it so the repo carries no opaque binary
// fixtures and every test shows exactly what its input contains.
package testdoc

import (
	"archive/zip"
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// PDF builds a minimal PDF (Helvetica text, one line per "\n") with a Title in
// its info dictionary.
func PDF(title string, pages []string) []byte {
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

// Zip packs files (name → content) into a zip archive, in name order so the
// output is deterministic.
func Zip(files map[string]string) []byte {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			panic(err) // writes to a bytes.Buffer cannot fail
		}
		w.Write([]byte(files[name]))
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return b.Bytes()
}
