package convert

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/use-tokesaver/tokensaver/internal/source"
)

type xlsxConverter struct{}

func (xlsxConverter) Convert(ctx context.Context, src *source.Source, opts Options) (*Doc, error) {
	return convertXLSX(src.Data)
}

// convertXLSX renders every sheet as a Markdown table of formatted cell values
// (what a user sees, including computed formula results). Empty rows and
// columns are dropped.
func convertXLSX(data []byte) (*Doc, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("read XLSX: %w", err)
	}
	defer f.Close()
	var b strings.Builder
	for _, name := range f.GetSheetList() {
		rows, err := f.GetRows(name)
		if err != nil {
			continue
		}
		rows = trimGrid(rows)
		fmt.Fprintf(&b, "## Sheet: %s", name)
		if visible, err := f.GetSheetVisible(name); err == nil && !visible {
			b.WriteString(" (hidden)")
		}
		if len(rows) == 0 {
			b.WriteString("\n\n(empty)\n\n")
			continue
		}
		fmt.Fprintf(&b, " (%d rows × %d cols)\n\n%s\n\n", len(rows), len(rows[0]), markdownTable(rows))
	}
	return &Doc{Markdown: cleanText(b.String())}, nil
}

// trimGrid pads rows to a rectangle and removes rows and columns that are
// entirely empty.
func trimGrid(rows [][]string) [][]string {
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	used := make([]bool, width)
	var kept [][]string
	for _, r := range rows {
		empty := true
		for c, v := range r {
			if strings.TrimSpace(v) != "" {
				used[c] = true
				empty = false
			}
		}
		if !empty {
			kept = append(kept, r)
		}
	}
	out := make([][]string, len(kept))
	for i, r := range kept {
		for c := range width {
			if !used[c] {
				continue
			}
			v := ""
			if c < len(r) {
				v = r[c]
			}
			out[i] = append(out[i], v)
		}
	}
	return out
}
