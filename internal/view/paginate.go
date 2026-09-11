package view

import (
	"strings"
	"unicode/utf8"
)

type blockKind int

const (
	plainBlock blockKind = iota
	tableBlock
	fenceBlock
)

type block struct {
	kind  blockKind
	lines []string
}

// Paginate splits text into pages of at most maxChars characters. It breaks
// between paragraphs where it can, and when a table or code block has to be split
// it repeats the table header / reopens the fence so every page stands on its own.
// The split is deterministic, so page n is stable across calls.
func Paginate(text string, maxChars int) []string {
	text = strings.TrimSpace(text)
	if maxChars <= 0 || runeLen(text) <= maxChars {
		return []string{text}
	}
	var pages []string
	var cur []string // chunks of the page being built, joined by blank lines
	curLen := 0
	flush := func() {
		if len(cur) > 0 {
			pages = append(pages, strings.Join(cur, "\n\n"))
			cur, curLen = nil, 0
		}
	}
	add := func(s string) {
		if len(cur) > 0 {
			curLen += 2
		}
		cur = append(cur, s)
		curLen += runeLen(s)
	}
	for _, b := range parseBlocks(text) {
		s := strings.Join(b.lines, "\n")
		n := runeLen(s)
		sep := 0
		if len(cur) > 0 {
			sep = 2
		}
		if curLen+sep+n <= maxChars {
			add(s)
			continue
		}
		if n <= maxChars {
			flush()
			add(s)
			continue
		}
		// The block is bigger than a whole page. Top up the current page with its
		// start if a meaningful amount of room is left, otherwise begin fresh.
		room := maxChars - curLen - sep
		if len(cur) > 0 && room < maxChars/4 {
			flush()
			room = maxChars
		}
		for i, c := range splitBlock(b, room, maxChars) {
			if i > 0 || c == "" {
				flush()
			}
			if c != "" {
				add(c)
			}
		}
	}
	flush()
	return pages
}

func parseBlocks(text string) []block {
	var blocks []block
	var cur *block
	fence := ""
	flush := func() {
		if cur != nil && len(cur.lines) > 0 {
			blocks = append(blocks, *cur)
		}
		cur = nil
	}
	for _, l := range strings.Split(text, "\n") {
		if fence != "" {
			cur.lines = append(cur.lines, l)
			if f := fenceMarker(l); f != "" && f[0] == fence[0] && len(f) >= len(fence) && strings.TrimSpace(l) == f {
				fence = ""
				flush()
			}
			continue
		}
		if f := fenceMarker(l); f != "" {
			flush()
			fence = f
			cur = &block{kind: fenceBlock, lines: []string{l}}
			continue
		}
		if strings.TrimSpace(l) == "" {
			flush()
			continue
		}
		kind := plainBlock
		if strings.HasPrefix(strings.TrimLeft(l, " "), "|") {
			kind = tableBlock
		}
		if cur != nil && cur.kind != kind {
			flush()
		}
		if cur == nil {
			cur = &block{kind: kind}
		}
		cur.lines = append(cur.lines, l)
	}
	flush()
	return blocks
}

// splitBlock splits one oversized block into chunks: the first at most firstCap
// characters (or "" if nothing fits there), the rest at most maxChars. Every chunk
// of a table starts with its header; every chunk of a code block is wrapped in
// the fence. Lines longer than a page are cut.
func splitBlock(b block, firstCap, maxChars int) []string {
	head, tail, lines := blockFrame(b, maxChars)
	headLen := runeLen(strings.Join(head, "\n"))
	reserve := 0
	if tail != "" {
		reserve = runeLen(tail) + 1
	}

	var chunks, cur []string
	curLen := 0
	capNow := firstCap
	start := func() {
		cur = append([]string(nil), head...)
		curLen = headLen
	}
	end := func() {
		if tail != "" {
			cur = append(cur, tail)
		}
		chunks = append(chunks, strings.Join(cur, "\n"))
		capNow = maxChars
	}
	start()
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		n := runeLen(l)
		sep := 0
		if len(cur) > 0 {
			sep = 1
		}
		if curLen+sep+n+reserve <= capNow {
			cur = append(cur, l)
			curLen += sep + n
			continue
		}
		if len(cur) > len(head) {
			end()
			start()
			i--
			continue
		}
		if capNow < maxChars {
			// Not even one line fits in the leftover room: signal "start a new page".
			chunks = append(chunks, "")
			capNow = maxChars
			i--
			continue
		}
		first, rest := cutRunes(l, max(capNow-curLen-sep-reserve, 1))
		cur = append(cur, first)
		end()
		start()
		if rest != "" {
			lines[i] = rest
			i--
		}
	}
	if len(cur) > len(head) {
		end()
	}
	return chunks
}

// blockFrame separates a block into the lines repeated at the top of every chunk
// (table header, opening fence), the line closing every chunk (closing fence),
// and the body lines to distribute.
func blockFrame(b block, maxChars int) (head []string, tail string, lines []string) {
	lines = append([]string(nil), b.lines...)
	switch b.kind {
	case tableBlock:
		if len(lines) >= 2 && isTableSeparator(lines[1]) {
			head, lines = lines[:2:2], lines[2:]
		}
	case fenceBlock:
		tail = strings.TrimSpace(fenceMarker(lines[0]))
		head, lines = lines[:1:1], lines[1:]
		if n := len(lines); n > 0 && strings.TrimSpace(lines[n-1]) == tail {
			lines = lines[:n-1]
		}
	}
	if runeLen(strings.Join(head, "\n")) > maxChars/2 {
		return nil, "", append([]string(nil), b.lines...)
	}
	return head, tail, lines
}

func isTableSeparator(l string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, "|") && strings.Trim(t, "|-: ") == ""
}

// cutRunes splits s after at most n runes, preferring the last space in the
// final fifth of that window so words are not broken.
func cutRunes(s string, n int) (string, string) {
	i, count := 0, 0
	for i < len(s) && count < n {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		count++
	}
	if i >= len(s) {
		return s, ""
	}
	if sp := strings.LastIndexByte(s[:i], ' '); sp > 0 && sp > i-i/5 {
		return s[:sp], s[sp+1:]
	}
	return s[:i], s[i:]
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }
