// Package view turns a converted Markdown document into what actually reaches the
// LLM: a heading outline, a single section, or one page of a size-capped split.
package view

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Heading is one entry of a document outline. Start/End are byte offsets of the
// section it opens: from the heading line up to the next heading of the same or a
// higher level (so a section includes its subsections).
type Heading struct {
	ID    int
	Level int
	Title string
	Start int
	End   int
}

var (
	headingRe  = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t#]*$`)
	mdLinkRe   = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	mdEscapeRe = regexp.MustCompile("\\\\([\\\\`*_{}\\[\\]()#+\\-.!|<>])")
)

// Outline lists the ATX headings of md, ignoring anything inside fenced code blocks.
func Outline(md string) []Heading {
	var hs []Heading
	fence := ""
	off := 0
	for _, line := range strings.SplitAfter(md, "\n") {
		start := off
		off += len(line)
		l := strings.TrimRight(line, "\r\n")
		if f := fenceMarker(l); f != "" {
			if fence == "" {
				fence = f
			} else if strings.HasPrefix(f, fence[:1]) && len(f) >= len(fence) && strings.TrimSpace(l) == f {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		m := headingRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		title := strings.TrimSpace(mdEscapeRe.ReplaceAllString(mdLinkRe.ReplaceAllString(m[2], "$1"), "$1"))
		if title == "" {
			continue
		}
		hs = append(hs, Heading{ID: len(hs) + 1, Level: len(m[1]), Title: title, Start: start})
	}
	for i := range hs {
		hs[i].End = len(md)
		for j := i + 1; j < len(hs); j++ {
			if hs[j].Level <= hs[i].Level {
				hs[i].End = hs[j].Start
				break
			}
		}
	}
	return hs
}

// fenceMarker returns the ``` or ~~~ run that opens/closes a code fence on this
// line, or "" if the line is not a fence line.
func fenceMarker(l string) string {
	t := strings.TrimLeft(l, " ")
	if len(l)-len(t) > 3 || len(t) < 3 {
		return ""
	}
	c := t[0]
	if c != '`' && c != '~' {
		return ""
	}
	n := 0
	for n < len(t) && t[n] == c {
		n++
	}
	if n < 3 {
		return ""
	}
	return t[:n]
}

// FormatOutline renders the outline compactly: one line per heading, indented by
// relative level, with an estimated token size for each section.
func FormatOutline(md string) string {
	hs := Outline(md)
	total := EstTokens(md)
	if len(hs) == 0 {
		return fmt.Sprintf("No headings found (~%s tokens). Read it with page=1, page=2, ….", humanInt(total))
	}
	minLevel := 6
	for _, h := range hs {
		minLevel = min(minLevel, h.Level)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d sections, ~%s tokens. Read one with section=<id>.\n", len(hs), humanInt(total))
	for _, h := range hs {
		b.WriteString(strings.Repeat("  ", h.Level-minLevel))
		fmt.Fprintf(&b, "%d. %s (~%s)\n", h.ID, h.Title, humanInt(EstTokens(md[h.Start:h.End])))
	}
	return strings.TrimRight(b.String(), "\n")
}

// Section returns the section of md selected by key: an outline id ("3"), or a
// heading title (exact, then prefix, then substring match; case-insensitive).
func Section(md, key string) (string, error) {
	hs := Outline(md)
	key = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(key), "#"))
	if id, err := strconv.Atoi(key); err == nil && id >= 1 && id <= len(hs) {
		h := hs[id-1]
		return strings.TrimSpace(md[h.Start:h.End]), nil
	}
	want := strings.ToLower(key)
	for _, match := range []func(string) bool{
		func(t string) bool { return t == want },
		func(t string) bool { return strings.HasPrefix(t, want) },
		func(t string) bool { return strings.Contains(t, want) },
	} {
		for _, h := range hs {
			if match(strings.ToLower(h.Title)) {
				return strings.TrimSpace(md[h.Start:h.End]), nil
			}
		}
	}
	if len(hs) == 0 {
		return "", fmt.Errorf("no section %q: document has no headings; use page= instead", key)
	}
	return "", fmt.Errorf("no section %q; call with outline=true to list sections", key)
}

// Page returns page n (1-based) of text split into pages of at most maxChars
// characters. When there is more than one page a short footer tells the LLM how
// to continue; hint is appended to it (e.g. "outline=true lists sections").
func Page(text string, maxChars, n int, hint string) (string, error) {
	pages := Paginate(text, maxChars)
	if n < 1 || n > len(pages) {
		return "", fmt.Errorf("page %d out of range: there are %d page(s)", n, len(pages))
	}
	if len(pages) == 1 {
		return pages[0], nil
	}
	footer := fmt.Sprintf("[page %d of %d", n, len(pages))
	if n < len(pages) {
		footer += fmt.Sprintf(" · next: page=%d", n+1)
	}
	if hint != "" {
		footer += " · " + hint
	}
	return pages[n-1] + "\n\n" + footer + "]", nil
}

// EstTokens is a rough token estimate (~4 characters per token).
func EstTokens(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}

func humanInt(n int) string {
	if n < 1000 {
		return strconv.Itoa(n)
	}
	return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
}
