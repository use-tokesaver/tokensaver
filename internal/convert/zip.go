package convert

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/use-tokesaver/tokensaver/internal/source"
)

const (
	maxZipMembers   = 200
	maxInlineMember = 5 << 20 // 5 MB
)

type zipConverter struct{}

func (zipConverter) Convert(ctx context.Context, src *source.Source, opts Options) (*Doc, error) {
	return convertZIP(ctx, src.Data)
}

func convertZIP(ctx context.Context, data []byte) (*Doc, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	// Collect members (skip directories)
	var members []*zip.File
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		members = append(members, f)
	}

	// Sort by name for consistent order
	sort.Slice(members, func(i, j int) bool {
		return members[i].Name < members[j].Name
	})

	// Cap and convert
	var result strings.Builder
	for i, f := range members {
		if i >= maxZipMembers {
			fmt.Fprintf(&result, "\n\n... %d more members omitted ...", len(members)-i)
			break
		}

		// Read member data
		rc, err := f.Open()
		if err != nil {
			fmt.Fprintf(&result, "## %s\n\n(error reading: %v)\n\n", f.Name, err)
			continue
		}
		memberData, err := io.ReadAll(io.LimitReader(rc, maxInlineMember+1))
		rc.Close()
		if err != nil {
			fmt.Fprintf(&result, "## %s\n\n(error reading: %v)\n\n", f.Name, err)
			continue
		}

		// Detect and convert
		src := &source.Source{Path: f.Name, Data: memberData}
		kind := source.Detect(src)

		// Size info for heading
		sizeStr := humanSize(int64(f.UncompressedSize64))
		heading := fmt.Sprintf("## %s (%s", f.Name, sizeStr)
		if kind != source.Unknown && kind != source.Text {
			heading += fmt.Sprintf(", %s", kind)
		}
		heading += ")"

		if len(memberData) > maxInlineMember {
			// Too large to decompress
			fmt.Fprintf(&result, "%s\n\n(file too large, %s uncompressed — not converted)\n\n", heading, humanSize(int64(f.UncompressedSize64)))
		} else if kind == source.ZIP || kind == source.Unknown {
			// Don't recurse into nested archives; binary files get metadata only
			if kind == source.ZIP {
				fmt.Fprintf(&result, "%s\n\n(nested archive, not expanded)\n\n", heading)
			} else {
				fmt.Fprintf(&result, "%s\n\n(binary, %s, not converted)\n\n", heading, humanSize(int64(len(memberData))))
			}
		} else {
			// Recursively convert this member
			doc, err := Convert(ctx, src, kind, Options{})
			if err != nil {
				fmt.Fprintf(&result, "%s\n\n(error converting: %v)\n\n", heading, err)
				continue
			}
			fmt.Fprintf(&result, "%s\n\n%s\n\n", heading, doc.Markdown)
		}
	}

	return &Doc{Markdown: cleanText(strings.TrimSpace(result.String()))}, nil
}

func humanSize(b int64) string {
	const (
		unit = 1024
	)
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %c", float64(b)/float64(div), "KMGTPE"[exp])
}
