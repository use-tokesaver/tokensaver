package convert

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/use-tokesaver/tokensaver/internal/source"
)

type diffConverter struct{}

func (diffConverter) Convert(ctx context.Context, src *source.Source, opts Options) (*Doc, error) {
	return convertDiff(src.Data)
}

// convertDiff turns a unified diff (git format or a plain `diff -u` patch) into
// Markdown: one heading per file so outline/section/paging work for free, with
// the hunks underneath in a fenced code block. Hunks that only change whitespace
// are dropped with a note, same as jsonshrink's array-truncation notes.
func convertDiff(data []byte) (*Doc, error) {
	text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	chunks := splitDiff(text)
	if len(chunks) == 0 {
		return nil, fmt.Errorf("not a recognizable diff/patch")
	}
	files := make([]diffFile, len(chunks))
	totalAdd, totalDel := 0, 0
	for i, c := range chunks {
		files[i] = parseFileDiff(c)
		totalAdd += files[i].adds
		totalDel += files[i].dels
	}
	fence := fenceFor(text)
	var b strings.Builder
	fmt.Fprintf(&b, "**%d file%s changed** (+%d -%d)\n\n", len(files), plural(len(files)), totalAdd, totalDel)
	for _, f := range files {
		writeFileSection(&b, f, fence)
	}
	return &Doc{Markdown: strings.TrimSpace(b.String())}, nil
}

type diffFile struct {
	oldPath, newPath string
	status           string // "new file", "deleted", "renamed", "renamed, modified", "mode changed", "binary file changed", or "" (modified)
	adds, dels       int
	body             string // fenced hunk content; "" when there's nothing to show
	whitespaceOnly   int    // hunks dropped for changing only whitespace
}

var gitFileHeaderRe = regexp.MustCompile(`^diff --git a/(.+) b/(.+)$`)

// splitDiff breaks a (possibly multi-file) diff into one chunk per file.
func splitDiff(text string) []string {
	lines := strings.Split(text, "\n")
	marker := "diff --git "
	var starts []int
	for i, l := range lines {
		if strings.HasPrefix(l, marker) {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		// No git headers (a plain `diff -u` patch): fall back to splitting on
		// "--- " file headers instead.
		marker = "--- "
		for i, l := range lines {
			if strings.HasPrefix(l, marker) {
				starts = append(starts, i)
			}
		}
	}
	if len(starts) == 0 {
		return nil
	}
	chunks := make([]string, len(starts))
	for i, s := range starts {
		end := len(lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		chunks[i] = strings.Join(lines[s:end], "\n")
	}
	return chunks
}

func parseFileDiff(chunk string) diffFile {
	lines := strings.Split(chunk, "\n")
	hunkStart := len(lines)
	for i, l := range lines {
		if strings.HasPrefix(l, "@@ ") {
			hunkStart = i
			break
		}
	}

	var f diffFile
	var renameFrom, renameTo string
	var newFile, deleted, binary, modeOnly bool
	for _, l := range lines[:hunkStart] {
		switch {
		case strings.HasPrefix(l, "diff --git "):
			if m := gitFileHeaderRe.FindStringSubmatch(l); m != nil {
				f.oldPath, f.newPath = m[1], m[2]
			}
		case strings.HasPrefix(l, "new file mode"):
			newFile = true
		case strings.HasPrefix(l, "deleted file mode"):
			deleted = true
		case strings.HasPrefix(l, "rename from "):
			renameFrom = strings.TrimPrefix(l, "rename from ")
		case strings.HasPrefix(l, "rename to "):
			renameTo = strings.TrimPrefix(l, "rename to ")
		case strings.HasPrefix(l, "old mode ") || strings.HasPrefix(l, "new mode "):
			modeOnly = true
		case strings.HasPrefix(l, "Binary files ") && strings.HasSuffix(l, " differ"):
			binary = true
		case strings.HasPrefix(l, "--- "):
			if p := headerPath(l, "--- "); p != "" {
				f.oldPath = p
			}
		case strings.HasPrefix(l, "+++ "):
			if p := headerPath(l, "+++ "); p != "" {
				f.newPath = p
			}
		}
	}
	if renameFrom != "" {
		f.oldPath = renameFrom
	}
	if renameTo != "" {
		f.newPath = renameTo
	}
	if f.newPath == "" {
		f.newPath = f.oldPath
	}
	if f.oldPath == "" {
		f.oldPath = f.newPath
	}

	hunks, adds, dels, wsSkipped := parseHunks(lines[hunkStart:])
	f.adds, f.dels, f.whitespaceOnly = adds, dels, wsSkipped
	f.body = strings.Join(hunks, "\n")

	switch {
	case binary:
		f.status = "binary file changed"
	case newFile:
		f.status = "new file"
	case deleted:
		f.status = "deleted"
	case renameFrom != "" && len(hunks) == 0 && wsSkipped == 0:
		f.status = "renamed"
	case renameFrom != "":
		f.status = "renamed, modified"
	case modeOnly && len(hunks) == 0 && wsSkipped == 0:
		f.status = "mode changed"
	}
	return f
}

// headerPath extracts the path from a "--- " / "+++ " file header line: strips
// the marker, a trailing tab-separated timestamp, and a leading "a/"/"b/". Empty
// for /dev/null (the added/removed side of a new/deleted file).
func headerPath(line, marker string) string {
	rest := strings.TrimPrefix(line, marker)
	if i := strings.IndexByte(rest, '\t'); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimSpace(rest)
	if rest == "/dev/null" {
		return ""
	}
	if p, ok := strings.CutPrefix(rest, "a/"); ok {
		return p
	}
	if p, ok := strings.CutPrefix(rest, "b/"); ok {
		return p
	}
	return rest
}

// parseHunks splits a file's hunk region (everything after its --- /+++ header)
// into individual "@@ ... @@" hunks, dropping ones that only change whitespace.
func parseHunks(lines []string) (hunks []string, adds, dels, wsSkipped int) {
	i := 0
	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "@@ ") {
			i++
			continue
		}
		header := lines[i]
		j := i + 1
		for j < len(lines) && !strings.HasPrefix(lines[j], "@@ ") {
			j++
		}
		body := lines[i+1 : j]
		a, d := countChanges(body)
		adds += a
		dels += d
		if isWhitespaceOnly(body) {
			wsSkipped++
		} else {
			hunks = append(hunks, strings.TrimRight(strings.Join(append([]string{header}, body...), "\n"), "\n"))
		}
		i = j
	}
	return hunks, adds, dels, wsSkipped
}

func countChanges(body []string) (adds, dels int) {
	for _, l := range body {
		if l == "" {
			continue
		}
		switch l[0] {
		case '+':
			adds++
		case '-':
			dels++
		}
	}
	return adds, dels
}

var wsRe = regexp.MustCompile(`\s+`)

// isWhitespaceOnly reports whether every removed line has a matching added line,
// in order, that becomes identical once all whitespace is stripped (catches
// re-indentation and brace-spacing changes, not just trailing-space edits).
func isWhitespaceOnly(body []string) bool {
	var removed, added []string
	for _, l := range body {
		if l == "" {
			continue
		}
		switch l[0] {
		case '-':
			removed = append(removed, stripWS(l[1:]))
		case '+':
			added = append(added, stripWS(l[1:]))
		}
	}
	if len(removed) == 0 || len(removed) != len(added) {
		return false
	}
	for i := range removed {
		if removed[i] != added[i] {
			return false
		}
	}
	return true
}

func stripWS(s string) string {
	return wsRe.ReplaceAllString(s, "")
}

func writeFileSection(b *strings.Builder, f diffFile, fence string) {
	b.WriteString("## ")
	b.WriteString(displayPath(f))
	var parts []string
	if f.status != "" {
		parts = append(parts, f.status)
	}
	if f.adds > 0 || f.dels > 0 {
		parts = append(parts, fmt.Sprintf("+%d -%d", f.adds, f.dels))
	}
	if len(parts) > 0 {
		fmt.Fprintf(b, " (%s)", strings.Join(parts, ", "))
	}
	b.WriteString("\n\n")
	if f.body != "" {
		b.WriteString(fence)
		b.WriteString("diff\n")
		b.WriteString(f.body)
		b.WriteString("\n")
		b.WriteString(fence)
		b.WriteString("\n")
	}
	if f.whitespaceOnly > 0 {
		fmt.Fprintf(b, "[%d whitespace-only hunk%s omitted]\n", f.whitespaceOnly, plural(f.whitespaceOnly))
	}
	b.WriteString("\n")
}

func displayPath(f diffFile) string {
	if f.oldPath != f.newPath && f.oldPath != "" && f.newPath != "" {
		return f.oldPath + " → " + f.newPath
	}
	if f.newPath != "" {
		return f.newPath
	}
	return f.oldPath
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// fenceFor picks a code-fence run longer than the longest backtick run
// anywhere in text, so a diffed file that itself contains fenced Markdown
// can't prematurely close the wrapper fence.
func fenceFor(text string) string {
	longest, run := 0, 0
	for _, r := range text {
		if r == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return strings.Repeat("`", max(4, longest+1))
}
