package view

import (
	"fmt"
	"strings"
	"testing"
)

const doc = `# Guide

Intro text.

## Install

Run it.

` + "```sh\n# not a heading\ngo install\n```" + `

### macOS

brew.

## Usage [docs](https://x.dev/usage)

Use it.

## func (\*Decoder) Decode

Decodes.
`

func TestOutline(t *testing.T) {
	hs := Outline(doc)
	var got []string
	for _, h := range hs {
		got = append(got, fmt.Sprintf("%d:%d:%s", h.ID, h.Level, h.Title))
	}
	want := []string{"1:1:Guide", "2:2:Install", "3:3:macOS", "4:2:Usage docs", "5:2:func (*Decoder) Decode"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("outline = %v, want %v", got, want)
	}
	out := FormatOutline(doc)
	if !strings.Contains(out, "5 sections") || !strings.Contains(out, "\n    3. macOS") {
		t.Fatalf("FormatOutline:\n%s", out)
	}
	if s, err := Section(doc, "func (*Decoder) Decode"); err != nil || !strings.HasSuffix(s, "Decodes.") {
		t.Fatalf("unescaped title lookup = %q, %v", s, err)
	}
}

func TestSection(t *testing.T) {
	for _, key := range []string{"2", "install", "## Install", "inst"} {
		s, err := Section(doc, key)
		if err != nil {
			t.Fatalf("Section(%q): %v", key, err)
		}
		if !strings.HasPrefix(s, "## Install") || !strings.Contains(s, "brew.") || strings.Contains(s, "Use it.") {
			t.Fatalf("Section(%q) = %q", key, s)
		}
	}
	if s, _ := Section(doc, "usage docs"); !strings.HasSuffix(s, "Use it.") {
		t.Fatalf("last section = %q", s)
	}
	if _, err := Section(doc, "nope"); err == nil || !strings.Contains(err.Error(), "outline=true") {
		t.Fatalf("missing section error = %v", err)
	}
}

func TestPaginateFitsAndCoversEverything(t *testing.T) {
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "Paragraph %d has some words in it to fill the page up.\n\n", i)
	}
	text := strings.TrimSpace(b.String())
	pages := Paginate(text, 500)
	if len(pages) < 10 {
		t.Fatalf("expected many pages, got %d", len(pages))
	}
	for i, p := range pages {
		if n := runeLen(p); n > 500 {
			t.Fatalf("page %d has %d chars", i+1, n)
		}
	}
	if strings.Join(pages, "\n\n") != text {
		t.Fatal("pages do not reassemble to the original text")
	}
}

func TestPaginateRepeatsTableHeader(t *testing.T) {
	var b strings.Builder
	b.WriteString("| name | value |\n| --- | --- |\n")
	for i := range 100 {
		fmt.Fprintf(&b, "| row%03d | %d |\n", i, i*i)
	}
	pages := Paginate(b.String(), 300)
	if len(pages) < 3 {
		t.Fatalf("expected the table to span pages, got %d", len(pages))
	}
	rows := 0
	for i, p := range pages {
		if !strings.HasPrefix(p, "| name | value |\n| --- | --- |\n") {
			t.Fatalf("page %d does not start with the header:\n%s", i+1, p)
		}
		if runeLen(p) > 300 {
			t.Fatalf("page %d too long", i+1)
		}
		rows += strings.Count(p, "| row")
	}
	if rows != 100 {
		t.Fatalf("rows across pages = %d, want 100", rows)
	}
}

func TestPaginateKeepsFencesClosed(t *testing.T) {
	var b strings.Builder
	b.WriteString("Before.\n\n```go\n")
	for i := range 80 {
		fmt.Fprintf(&b, "fmt.Println(%d) // line\n", i)
	}
	b.WriteString("```\n\nAfter.")
	pages := Paginate(b.String(), 400)
	for i, p := range pages {
		if strings.Count(p, "```")%2 != 0 {
			t.Fatalf("page %d has an unbalanced fence:\n%s", i+1, p)
		}
	}
	all := strings.Join(pages, "\n")
	for i := range 80 {
		if !strings.Contains(all, fmt.Sprintf("fmt.Println(%d) // line", i)) {
			t.Fatalf("line %d lost", i)
		}
	}
}

func TestPaginateCutsGiantLines(t *testing.T) {
	line := strings.Repeat("word ", 1000)
	pages := Paginate(line, 700)
	for i, p := range pages {
		if runeLen(p) > 700 {
			t.Fatalf("page %d is %d chars", i+1, runeLen(p))
		}
	}
	if got := strings.Count(strings.Join(pages, " "), "word"); got != 1000 {
		t.Fatalf("words across pages = %d, want 1000", got)
	}
}

func TestPageFooter(t *testing.T) {
	text := strings.Repeat("Some paragraph text here.\n\n", 50)
	p, err := Page(text, 200, 1, "outline=true lists sections")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "[page 1 of ") || !strings.Contains(p, "next: page=2") || !strings.Contains(p, "outline=true") {
		t.Fatalf("footer missing: %q", p[len(p)-80:])
	}
	if _, err := Page(text, 200, 999, ""); err == nil {
		t.Fatal("expected out-of-range error")
	}
	if p, _ := Page("short", 200, 1, "x"); p != "short" {
		t.Fatalf("single page should have no footer, got %q", p)
	}
}
