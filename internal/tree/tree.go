// Package tree renders a directory as a token-aware Markdown tree: an agent's
// token-cheap alternative to `ls -R`. Respects the root .gitignore, and collapses
// dependency/build directories and unusually large ones to a one-line summary
// instead of listing every entry.
package tree

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// collapseEntries: a directory with more direct entries than this, after
// .gitignore filtering, is collapsed to a one-line "(N items)" summary instead
// of being listed or recursed into.
const collapseEntries = 100

// alwaysCollapse are dependency/build directories collapsed unconditionally
// (never recursed into), regardless of .gitignore — most repos already
// gitignore these, in which case they're invisible rather than collapsed; this
// is the fallback for the ones that aren't.
var alwaysCollapse = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true,
	"target": true, ".venv": true, "venv": true, "__pycache__": true,
	".next": true, ".nuxt": true, ".terraform": true,
}

// Build walks root and returns a rendered tree. root must be a directory.
func Build(root string) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", root)
	}
	ign := loadIgnore(root)

	var b strings.Builder
	fmt.Fprintf(&b, "%s/\n", filepath.Base(root))
	files, dirs := walk(&b, root, "", 1, ign)

	fence := "```"
	return fmt.Sprintf("**%d %s, %d %s**\n\n%s\n%s\n%s",
		files, plural(files, "file", "files"), dirs, plural(dirs, "directory", "directories"),
		fence, strings.TrimRight(b.String(), "\n"), fence), nil
}

func loadIgnore(root string) *ignore.GitIgnore {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return ignore.CompileIgnoreLines()
	}
	return ignore.CompileIgnoreLines(strings.Split(string(data), "\n")...)
}

// walk lists dir's entries (relPath is dir's path relative to root, using "/"),
// writing directories then files, alphabetically within each group, indented by
// depth. It returns the total files and directories shown (including collapsed
// ones, counted as one directory each).
func walk(b *strings.Builder, dir, relPath string, depth int, ign *ignore.GitIgnore) (files, dirs int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	var subdirs, regularFiles []os.DirEntry
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if ign.MatchesPath(entryRelPath(relPath, e)) {
			continue
		}
		if e.IsDir() {
			subdirs = append(subdirs, e)
		} else {
			regularFiles = append(regularFiles, e)
		}
	}
	sort.Slice(subdirs, func(i, j int) bool { return subdirs[i].Name() < subdirs[j].Name() })
	sort.Slice(regularFiles, func(i, j int) bool { return regularFiles[i].Name() < regularFiles[j].Name() })

	indent := strings.Repeat("  ", depth)
	for _, d := range subdirs {
		dirs++
		name := d.Name()
		childDir := filepath.Join(dir, name)
		childRel := path.Join(relPath, name)

		if alwaysCollapse[name] {
			fmt.Fprintf(b, "%s%s/ (dependency directory, collapsed)\n", indent, name)
			continue
		}
		if isNestedRepo(childDir) {
			fmt.Fprintf(b, "%s%s/ (nested git repo/worktree, collapsed)\n", indent, name)
			continue
		}
		childEntries, err := os.ReadDir(childDir)
		if err != nil {
			fmt.Fprintf(b, "%s%s/ (unreadable)\n", indent, name)
			continue
		}
		visible := 0
		for _, ce := range childEntries {
			if ce.Name() == ".git" {
				continue
			}
			if !ign.MatchesPath(entryRelPath(childRel, ce)) {
				visible++
			}
		}
		if visible > collapseEntries {
			fmt.Fprintf(b, "%s%s/ (%d items, collapsed)\n", indent, name, visible)
			continue
		}
		fmt.Fprintf(b, "%s%s/\n", indent, name)
		f, d := walk(b, childDir, childRel, depth+1, ign)
		files += f
		dirs += d
	}
	for _, fe := range regularFiles {
		files++
		size := int64(0)
		if info, err := fe.Info(); err == nil {
			size = info.Size()
		}
		fmt.Fprintf(b, "%s%s (%s)\n", indent, fe.Name(), humanBytes(size))
	}
	return files, dirs
}

// isNestedRepo reports whether dir is itself a git repo or worktree root (has
// its own .git file or directory) — a boundary tokensaver shouldn't cross, the
// same way it never descends into the outer repo's own .git.
func isNestedRepo(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

// entryRelPath is e's path relative to root, "/"-separated, with a trailing "/"
// for directories — the form go-gitignore expects to match directory-only
// patterns correctly.
func entryRelPath(parentRel string, e os.DirEntry) string {
	p := path.Join(parentRel, e.Name())
	if e.IsDir() {
		p += "/"
	}
	return p
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func humanBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
	}
}
