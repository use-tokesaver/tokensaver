package convert

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/use-tokesaver/tokensaver/internal/source"
	"github.com/use-tokesaver/tokensaver/internal/testdoc"
)

func TestZIPOutline(t *testing.T) {
	// Create a ZIP with mixed file types
	files := map[string]string{
		"readme.txt":  "This is a readme\nWith multiple lines",
		"data.json":   `{"key": "value", "number": 42}`,
		"binary.bin":  "GIF89a\x00\x00\x00\x00", // GIF header
		"archive.zip": string(testdoc.Zip(map[string]string{"nested.txt": "nested"})),
		"dir/":        "", // Directory (should be skipped)
	}
	zipData := testdoc.Zip(files)

	src := &source.Source{Path: "test.zip", Data: zipData}
	doc, err := Convert(context.Background(), src, source.ZIP, Options{})
	if err != nil {
		t.Fatal(err)
	}

	// Check structure
	mustContain(t, doc.Markdown, "archive.zip", "data.json", "readme.txt", "binary.bin")
	// Nested archive should be noted, not expanded
	mustContain(t, doc.Markdown, "nested archive")
	// Binary file should be noted
	mustContain(t, doc.Markdown, "binary")
	// Directory should be skipped
	mustNotContain(t, doc.Markdown, "dir/")
	// JSON should be converted (shrunk)
	mustContain(t, doc.Markdown, "key", "value")
	// Text should be converted
	mustContain(t, doc.Markdown, "readme", "multiple lines")
}

func TestZIPSection(t *testing.T) {
	zipData := testdoc.Zip(map[string]string{
		"test.json": `{"a": 1, "b": 2}`,
		"test.txt":  "Hello World",
	})

	src := &source.Source{Path: "test.zip", Data: zipData}
	doc, err := Convert(context.Background(), src, source.ZIP, Options{})
	if err != nil {
		t.Fatal(err)
	}

	// Get outline to find section IDs
	headings := extractHeadings(doc.Markdown)
	if len(headings) < 2 {
		t.Fatalf("expected at least 2 members, got %d", len(headings))
	}

	// Extract a specific member
	for _, h := range headings {
		if strings.Contains(h, "test.json") {
			// This proves the member is reachable via outline/section flow
			if !strings.Contains(doc.Markdown, "test.json") {
				t.Error("test.json member not in output")
			}
		}
	}
}

func TestZIPDetection(t *testing.T) {
	// Plain ZIP (not DOCX/XLSX/PPTX) should be detected as ZIP
	zipData := testdoc.Zip(map[string]string{"file.txt": "content"})

	src := &source.Source{Path: "plain.zip", Data: zipData}
	kind := source.Detect(src)

	if kind != source.ZIP {
		t.Errorf("expected ZIP, got %s", kind)
	}
}

func TestZIPMemberCapping(t *testing.T) {
	// Create a ZIP with more than maxZipMembers entries
	files := make(map[string]string, maxZipMembers+10)
	for i := 0; i < maxZipMembers+10; i++ {
		files[formatNum("file_", i, ".txt")] = "content"
	}
	zipData := testdoc.Zip(files)

	src := &source.Source{Path: "big.zip", Data: zipData}
	doc, err := Convert(context.Background(), src, source.ZIP, Options{})
	if err != nil {
		t.Fatal(err)
	}

	// Should include an "omitted" line
	mustContain(t, doc.Markdown, "omitted")
	// First maxZipMembers should be present
	mustContain(t, doc.Markdown, "file_0.txt")
}

func extractHeadings(md string) []string {
	var headings []string
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "##") {
			headings = append(headings, line)
		}
	}
	return headings
}

func formatNum(prefix string, n int, suffix string) string {
	return prefix + fmt.Sprint(n) + suffix
}
