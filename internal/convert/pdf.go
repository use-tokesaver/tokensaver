package convert

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/webassembly"
)

// PDFium (Chrome's PDF engine) runs as WebAssembly inside the Go process, so PDF
// support needs no cgo and no system library. Compiling the module takes about a
// second, so the pool is created on first use and then kept.
var (
	pdfOnce sync.Once
	pdfPool pdfium.Pool
	pdfErr  error
)

func pdfInstance(ctx context.Context) (pdfium.Pdfium, error) {
	pdfOnce.Do(func() {
		pdfPool, pdfErr = webassembly.Init(webassembly.Config{
			MinIdle: 1, MaxIdle: 1, MaxTotal: 2,
			// Never let the WASM runtime write to stdout: that is the MCP channel.
			Stdout: io.Discard, Stderr: io.Discard,
		})
	})
	if pdfErr != nil {
		return nil, fmt.Errorf("start PDF engine: %w", pdfErr)
	}
	return pdfPool.GetInstanceWithContext(ctx)
}

func convertPDF(ctx context.Context, data []byte) (*Doc, error) {
	inst, err := pdfInstance(ctx)
	if err != nil {
		return nil, err
	}
	defer inst.Close()

	opened, err := inst.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "password") {
			return nil, errors.New("the PDF is password-protected")
		}
		return nil, fmt.Errorf("open PDF: %w", err)
	}
	doc := opened.Document
	defer inst.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: doc})

	count, err := inst.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc})
	if err != nil {
		return nil, fmt.Errorf("read PDF: %w", err)
	}
	pages := make([][]string, count.PageCount)
	deadline := time.Now().Add(2 * time.Minute)
	for i := range pages {
		if ctx.Err() != nil || time.Now().After(deadline) {
			return nil, errors.New("PDF text extraction timed out")
		}
		txt, err := inst.GetPageText(&requests.GetPageText{
			Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: i}},
		})
		if err != nil {
			continue
		}
		pages[i] = strings.Split(cleanText(txt.Text), "\n")
	}
	removeRunningLines(pages)

	var b strings.Builder
	title := ""
	if meta, err := inst.FPDF_GetMetaText(&requests.FPDF_GetMetaText{Document: doc, Tag: "Title"}); err == nil {
		title = strings.TrimSpace(meta.Value)
	}
	if title != "" {
		fmt.Fprintf(&b, "Title: %s\n\n", title)
	}
	empty := 0
	for i, lines := range pages {
		fmt.Fprintf(&b, "## Page %d\n\n", i+1)
		text := strings.TrimSpace(strings.Join(lines, "\n"))
		if text == "" {
			empty++
			b.WriteString("(no text on this page)\n\n")
			continue
		}
		for _, l := range lines {
			b.WriteString(escapeHeading(l))
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	d := &Doc{Markdown: cleanText(b.String()), Title: title}
	if len(pages) > 0 && empty == len(pages) {
		d.Note = "this PDF has no text layer (probably scanned images); OCR is not supported yet"
	} else if empty > 0 {
		d.Note = fmt.Sprintf("%d of %d pages have no text layer (scanned?); OCR is not supported yet", empty, len(pages))
	}
	return d, nil
}

var (
	digitsRe  = regexp.MustCompile(`\d+`)
	lettersRe = regexp.MustCompile(`\pL.*\pL.*\pL`)
	pageNumRe = regexp.MustCompile(`(?i)^[\s\-–—]*(?:page\s*)?(\d+)(?:\s*(?:of|/)\s*\d+)?[\s\-–—]*$`)
)

const (
	edgeWindow   = 2 // lines at the top and at the bottom checked for headers/footers
	minPageLines = 5 // shorter pages are left alone: too little body to tell furniture apart
)

// removeRunningLines drops page furniture from documents of 4+ pages:
//   - lines among the top (or bottom) two of a page that repeat at the top (or
//     bottom), ignoring digits, on most pages — running headers and footers like
//     "ACME Report — Page 3 of 40";
//   - a first/last line that is only a page number, when those numbers track the
//     page index (same offset on at least half the pages), so a table value that
//     happens to sit at the edge of a page is left alone.
func removeRunningLines(pages [][]string) {
	if len(pages) < 4 {
		return
	}
	counts := map[string]int{}
	offsets := map[int]int{}
	for p, lines := range pages {
		seen := map[string]bool{}
		for _, e := range edgeLines(lines) {
			if key := e.key(lines); key != "" && !seen[key] {
				seen[key] = true
				counts[key]++
			}
		}
		for _, n := range pageNumbers(lines) {
			offsets[n.value-(p+1)]++
		}
	}
	bestOffset, bestCount := 0, 0
	for off, c := range offsets {
		if c > bestCount || c == bestCount && abs(off) < abs(bestOffset) {
			bestOffset, bestCount = off, c
		}
	}
	threshold := max(3, len(pages)*6/10)
	for p, lines := range pages {
		drop := map[int]bool{}
		for _, e := range edgeLines(lines) {
			if key := e.key(lines); key != "" && counts[key] >= threshold {
				drop[e.index] = true
			}
		}
		if bestCount >= max(2, len(pages)/2) {
			for _, n := range pageNumbers(lines) {
				if n.value-(p+1) == bestOffset {
					drop[n.line] = true
				}
			}
		}
		if len(drop) == 0 {
			continue
		}
		kept := lines[:0:0]
		for i, l := range lines {
			if !drop[i] {
				kept = append(kept, l)
			}
		}
		pages[p] = kept
	}
}

type pageNumber struct{ line, value int }

// pageNumbers returns the first and last non-blank lines that look like a bare
// page number ("12", "- 12 -", "Page 12", "12 of 40").
func pageNumbers(lines []string) []pageNumber {
	var out []pageNumber
	idx := nonBlank(lines)
	if len(idx) == 0 {
		return nil
	}
	for _, i := range []int{idx[0], idx[len(idx)-1]} {
		if m := pageNumRe.FindStringSubmatch(lines[i]); m != nil {
			var v int
			fmt.Sscan(m[1], &v)
			if len(out) == 0 || out[0].line != i {
				out = append(out, pageNumber{line: i, value: v})
			}
		}
	}
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

type edgeLine struct {
	index int
	top   bool
}

// key identifies a header/footer candidate: its position (top or bottom) plus
// its text with digits normalized. Lines without real words never qualify.
func (e edgeLine) key(lines []string) string {
	text := digitsRe.ReplaceAllString(strings.TrimSpace(lines[e.index]), "#")
	if !lettersRe.MatchString(text) {
		return ""
	}
	if e.top {
		return "top:" + text
	}
	return "bottom:" + text
}

// edgeLines are the first and last few non-blank lines of a page with enough
// lines to have a body between them.
func edgeLines(lines []string) []edgeLine {
	idx := nonBlank(lines)
	if len(idx) < minPageLines {
		return nil
	}
	var out []edgeLine
	for _, i := range idx[:edgeWindow] {
		out = append(out, edgeLine{index: i, top: true})
	}
	for _, i := range idx[len(idx)-edgeWindow:] {
		out = append(out, edgeLine{index: i})
	}
	return out
}

func nonBlank(lines []string) []int {
	var idx []int
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			idx = append(idx, i)
		}
	}
	return idx
}
