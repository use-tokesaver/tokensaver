package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkfile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func mustNotContain(t *testing.T, got string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(got, n) {
			t.Errorf("unexpected %q in:\n%s", n, got)
		}
	}
}

func TestTreeBasic(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "README.md"), 100)
	mkfile(t, filepath.Join(dir, "src", "main.go"), 2000)
	mkfile(t, filepath.Join(dir, "src", "util.go"), 50)

	got, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, got, "**3 files, 1 directory**", "README.md (100 B)", "src/", "main.go (2.0 KB)", "util.go (50 B)")
}

func TestTreeRespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\nbuild/\n"), 0o644)
	mkfile(t, filepath.Join(dir, "keep.txt"), 10)
	mkfile(t, filepath.Join(dir, "debug.log"), 10)
	mkfile(t, filepath.Join(dir, "build", "out.bin"), 10)

	got, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, got, "keep.txt")
	mustNotContain(t, got, "debug.log", "build/", "out.bin")
}

func TestTreeAlwaysSkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, ".git", "HEAD"), 10)
	mkfile(t, filepath.Join(dir, "main.go"), 10)

	got, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, got, "main.go")
	mustNotContain(t, got, ".git")
}

func TestTreeCollapsesDependencyDirs(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "node_modules", "left-pad", "index.js"), 10)
	mkfile(t, filepath.Join(dir, "app.js"), 10)

	got, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, got, "node_modules/ (dependency directory, collapsed)", "app.js")
	mustNotContain(t, got, "left-pad", "index.js")
}

func TestTreeCollapsesLargeDirByCount(t *testing.T) {
	dir := t.TempDir()
	for i := range collapseEntries + 10 {
		mkfile(t, filepath.Join(dir, "many", fmt.Sprintf("f%03d.txt", i)), 1)
	}
	got, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, got, "items, collapsed")
	mustNotContain(t, got, "f000.txt")
}

func TestTreeCollapsesNestedGitRepo(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "vendored-repo", ".git"), 10) // worktree: a FILE, not a dir
	mkfile(t, filepath.Join(dir, "vendored-repo", "src", "lib.go"), 10)
	mkfile(t, filepath.Join(dir, "own.go"), 10)

	got, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, got, "vendored-repo/ (nested git repo/worktree, collapsed)", "own.go")
	mustNotContain(t, got, "lib.go")
}

func TestTreeSortsDirsBeforeFilesAlphabetically(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, filepath.Join(dir, "zzz.txt"), 1)
	mkfile(t, filepath.Join(dir, "aaa.txt"), 1)
	mkfile(t, filepath.Join(dir, "mid", "x.txt"), 1)

	got, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	dirIdx := strings.Index(got, "mid/")
	aIdx := strings.Index(got, "aaa.txt")
	zIdx := strings.Index(got, "zzz.txt")
	if dirIdx == -1 || aIdx == -1 || zIdx == -1 {
		t.Fatalf("missing expected entries:\n%s", got)
	}
	if !(dirIdx < aIdx && aIdx < zIdx) {
		t.Errorf("expected dir before aaa.txt before zzz.txt, got positions %d, %d, %d", dirIdx, aIdx, zIdx)
	}
}

func TestTreeNotADirectory(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	mkfile(t, f, 1)
	if _, err := Build(f); err == nil {
		t.Fatal("expected an error for a non-directory root")
	}
}
