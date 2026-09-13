package convert

import (
	"testing"

	"github.com/use-tokesaver/tokensaver/internal/view"
)

const twoFileDiff = `diff --git a/internal/greet.go b/internal/greet.go
index e69de29..4b825dc 100644
--- a/internal/greet.go
+++ b/internal/greet.go
@@ -1,5 +1,5 @@
 package greet

 func Hello() string {
-	return "hi"
+	return "hello"
 }
diff --git a/README.md b/README.md
index 1234567..89abcde 100644
--- a/README.md
+++ b/README.md
@@ -1,3 +1,4 @@
 # demo

 A tiny demo.
+More text.
`

func TestDiffTwoFiles(t *testing.T) {
	doc, err := convertDiff([]byte(twoFileDiff))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown,
		"**2 files changed** (+2 -1)",
		"## internal/greet.go (+1 -1)",
		"## README.md (+1 -0)",
		`return "hi"`,
		`return "hello"`,
		"+More text.",
	)
}

const newFileDiff = `diff --git a/pkg/new.go b/pkg/new.go
new file mode 100644
index 0000000..1234567
--- /dev/null
+++ b/pkg/new.go
@@ -0,0 +1,2 @@
+package pkg
+// new
`

func TestDiffNewFile(t *testing.T) {
	doc, err := convertDiff([]byte(newFileDiff))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "## pkg/new.go (new file, +2 -0)")
}

const deletedFileDiff = `diff --git a/pkg/old.go b/pkg/old.go
deleted file mode 100644
index 1234567..0000000
--- a/pkg/old.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package pkg
-// old
`

func TestDiffDeletedFile(t *testing.T) {
	doc, err := convertDiff([]byte(deletedFileDiff))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "## pkg/old.go (deleted, +0 -2)")
}

const pureRenameDiff = `diff --git a/old/path.go b/new/path.go
similarity index 100%
rename from old/path.go
rename to new/path.go
`

func TestDiffPureRename(t *testing.T) {
	doc, err := convertDiff([]byte(pureRenameDiff))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "## old/path.go → new/path.go (renamed)")
	mustNotContain(t, doc.Markdown, "```diff")
}

const renameWithChangeDiff = `diff --git a/old/path.go b/new/path.go
similarity index 90%
rename from old/path.go
rename to new/path.go
index 1234567..89abcde 100644
--- a/old/path.go
+++ b/new/path.go
@@ -1,2 +1,2 @@
 package path
-var X = 1
+var X = 2
`

func TestDiffRenamedModified(t *testing.T) {
	doc, err := convertDiff([]byte(renameWithChangeDiff))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "## old/path.go → new/path.go (renamed, modified, +1 -1)")
}

const binaryDiff = `diff --git a/img/logo.png b/img/logo.png
index 1234567..89abcde 100644
Binary files a/img/logo.png and b/img/logo.png differ
`

func TestDiffBinary(t *testing.T) {
	doc, err := convertDiff([]byte(binaryDiff))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "## img/logo.png (binary file changed)")
	mustNotContain(t, doc.Markdown, "```diff")
}

const whitespaceOnlyDiff = `diff --git a/x.go b/x.go
index 1111111..2222222 100644
--- a/x.go
+++ b/x.go
@@ -1,3 +1,3 @@
 package x

-func Foo(){return 1}
+func Foo() { return 1 }
`

func TestDiffWhitespaceOnlyHunkOmitted(t *testing.T) {
	doc, err := convertDiff([]byte(whitespaceOnlyDiff))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "## x.go (+1 -1)", "[1 whitespace-only hunk omitted]")
	mustNotContain(t, doc.Markdown, "```diff", "func Foo")
}

const mixedWhitespaceAndRealDiff = `diff --git a/y.go b/y.go
index 1111111..2222222 100644
--- a/y.go
+++ b/y.go
@@ -1,3 +1,3 @@
 package y

-func Foo(){return 1}
+func Foo() { return 1 }
@@ -10,3 +10,3 @@ func Bar() {
 	x := 1
-	return x
+	return x + 1
 }
`

func TestDiffMixedWhitespaceAndRealHunks(t *testing.T) {
	doc, err := convertDiff([]byte(mixedWhitespaceAndRealDiff))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "## y.go (+2 -2)", "[1 whitespace-only hunk omitted]", "return x + 1")
	mustNotContain(t, doc.Markdown, "func Foo")
}

// A plain `diff -u` patch has no "diff --git" header at all.
const plainPatch = `--- a/notes.txt	2026-01-01 10:00:00
+++ b/notes.txt	2026-01-01 10:05:00
@@ -1,2 +1,2 @@
-old note
+new note
 kept line
`

func TestDiffPlainPatchNoGitHeader(t *testing.T) {
	doc, err := convertDiff([]byte(plainPatch))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "## notes.txt (+1 -1)", "-old note", "+new note")
}

func TestDiffNotRecognizable(t *testing.T) {
	if _, err := convertDiff([]byte("just some plain text, not a diff at all")); err == nil {
		t.Fatal("expected an error for non-diff input")
	}
}

// A diffed Markdown file that itself contains a fenced code block must not
// prematurely close tokensaver's own wrapping fence.
const nestedFenceDiff = "diff --git a/README.md b/README.md\n" +
	"index 1111111..2222222 100644\n" +
	"--- a/README.md\n" +
	"+++ b/README.md\n" +
	"@@ -1,3 +1,4 @@\n" +
	" # demo\n" +
	" ```go\n" +
	" fmt.Println(\"hi\")\n" +
	"+// more\n" +
	" ```\n"

func TestDiffNestedFenceDoesNotBreakOutput(t *testing.T) {
	doc, err := convertDiff([]byte(nestedFenceDiff))
	if err != nil {
		t.Fatal(err)
	}
	if got := len(view.Outline(doc.Markdown)); got != 1 {
		t.Fatalf("expected 1 heading in outline, got %d:\n%s", got, doc.Markdown)
	}
	mustContain(t, doc.Markdown, "````diff", "+// more")
}
