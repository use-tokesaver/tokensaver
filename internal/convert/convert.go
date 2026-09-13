// Package convert turns loaded documents (HTML, PDF, DOCX, XLSX, PPTX, text)
// into compact Markdown.
package convert

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/use-tokesaver/tokensaver/internal/source"
)

// Doc is a converted document.
type Doc struct {
	Markdown string
	Title    string
	Note     string // something the LLM should know, e.g. that JS rendering was unavailable
	Rendered bool   // the page was rendered in a headless browser
}

type Options struct {
	JS bool // force rendering web pages in a headless browser
}

// Convert converts src, already identified as kind, to Markdown.
func Convert(ctx context.Context, src *source.Source, kind source.Kind, opts Options) (*Doc, error) {
	switch kind {
	case source.HTML:
		return convertHTML(ctx, src, opts)
	case source.PDF:
		return convertPDF(ctx, src.Data)
	case source.DOCX:
		return convertDOCX(src.Data)
	case source.XLSX:
		return convertXLSX(src.Data)
	case source.PPTX:
		return convertPPTX(src.Data)
	case source.Diff:
		return convertDiff(src.Data)
	case source.Text, source.JSON:
		return &Doc{Markdown: cleanText(src.UTF8())}, nil
	}
	return nil, fmt.Errorf("unsupported file type (supported: web pages/HTML, PDF, DOCX, XLSX, PPTX, JSON, diff/patch, text)")
}

var (
	trailingSpaceRe = regexp.MustCompile(`(?m)[ \t]+$`)
	manyBlankRe     = regexp.MustCompile(`\n{3,}`)
)

// cleanText normalizes line endings, trims trailing whitespace and collapses
// runs of blank lines.
func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = trailingSpaceRe.ReplaceAllString(s, "")
	return strings.TrimSpace(manyBlankRe.ReplaceAllString(s, "\n\n"))
}

// escapeHeading stops plain text that happens to start with "#" from being read
// as a Markdown heading (which would also confuse the outline).
func escapeHeading(line string) string {
	if t := strings.TrimLeft(line, " "); strings.HasPrefix(t, "#") {
		return strings.Replace(line, "#", `\#`, 1)
	}
	return line
}
