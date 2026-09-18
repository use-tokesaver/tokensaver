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

// Converter turns a detected source into Markdown. Implement this interface to add
// support for a new file format:
//  1. define a struct type and give it a Convert method matching this interface
//  2. add an instance to the converters map below, keyed by your source.Kind
//  3. add detection for your Kind in internal/source/detect.go
type Converter interface {
	Convert(ctx context.Context, src *source.Source, opts Options) (*Doc, error)
}

var converters = map[source.Kind]Converter{
	source.HTML:    htmlConverter{},
	source.PDF:     pdfConverter{},
	source.DOCX:    docxConverter{},
	source.XLSX:    xlsxConverter{},
	source.PPTX:    pptxConverter{},
	source.Diff:    diffConverter{},
	source.LogFile: logConverter{},
	source.ZIP:     zipConverter{},
	source.Text:    textConverter{},
	source.JSON:    textConverter{},
}

// Convert converts src, already identified as kind, to Markdown.
func Convert(ctx context.Context, src *source.Source, kind source.Kind, opts Options) (*Doc, error) {
	c, ok := converters[kind]
	if !ok {
		return nil, fmt.Errorf("unsupported file type (supported: web pages/HTML, PDF, DOCX, XLSX, PPTX, JSON, diff/patch, logs, ZIP archives, text)")
	}
	return c.Convert(ctx, src, opts)
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

var (
	errorPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(FAIL|ERROR|FATAL|PANIC|EXCEPTION|TRACEBACK)\b`),
		regexp.MustCompile(`^\s*at\s+\S+\s*\(`),                     // stack trace line
		regexp.MustCompile(`^(FAILED|PASSED)\s+`),                   // pytest/go test
		regexp.MustCompile(`^---\s+FAIL:`),                          // go test format
		regexp.MustCompile(`●\s*(test|✕)`),                          // jest format
		regexp.MustCompile(`^\s*\|\s*(AssertionError|Error:|failed)`), // assertion failures
	}
	contextLines = 2 // lines before/after each error match
)

// summarizeLog extracts errors and failures from a log file with surrounding context.
func summarizeLog(s string) *Doc {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	interesting := make([]bool, len(lines))

	// Mark interesting lines
	for i, line := range lines {
		for _, pat := range errorPatterns {
			if pat.MatchString(line) {
				interesting[i] = true
				break
			}
		}
	}

	// Expand context windows
	for i := 0; i < len(interesting); i++ {
		if interesting[i] {
			for j := max(0, i-contextLines); j <= min(len(interesting)-1, i+contextLines); j++ {
				interesting[j] = true
			}
		}
	}

	// Build output, collapsing uninteresting runs
	var result []string
	skipStart := -1
	for i := 0; i < len(lines); i++ {
		if interesting[i] {
			if skipStart >= 0 {
				skipCount := i - skipStart
				if skipCount > 1 {
					result = append(result, fmt.Sprintf("... %d lines omitted ...", skipCount))
				} else if skipCount == 1 && skipStart < len(lines) {
					result = append(result, lines[skipStart])
				}
				skipStart = -1
			}
			result = append(result, lines[i])
		} else if skipStart < 0 {
			skipStart = i
		}
	}

	// Handle trailing skip
	if skipStart >= 0 && skipStart < len(lines) {
		skipCount := len(lines) - skipStart
		if skipCount > 1 {
			result = append(result, fmt.Sprintf("... %d lines omitted ...", skipCount))
		} else {
			result = append(result, lines[skipStart])
		}
	}

	markdown := strings.Join(result, "\n")
	return &Doc{Markdown: cleanText(markdown)}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Converter implementations for formats without dedicated files.

type logConverter struct{}

func (logConverter) Convert(ctx context.Context, src *source.Source, opts Options) (*Doc, error) {
	return summarizeLog(src.UTF8()), nil
}

type textConverter struct{}

func (textConverter) Convert(ctx context.Context, src *source.Source, opts Options) (*Doc, error) {
	return &Doc{Markdown: cleanText(src.UTF8())}, nil
}
