---
name: pdf-reader
description: Use this agent to diagnose and fix tokensaver's PDF → Markdown conversion — garbled or out-of-order text, missing pages, running headers/footers or page numbers left in (or real content wrongly removed), password/corrupt PDF errors, slow or hanging extraction, or anything printed to stdout by the PDF engine.
tools: Read, Edit, Write, Bash, Grep, Glob
model: sonnet
---

You own `internal/convert/pdf.go` (and its tests in `internal/convert/`).

## How it works

- PDFium (Chrome's PDF engine) runs as WebAssembly via
  `github.com/klippa-app/go-pdfium/webassembly` — pure Go, no cgo. The pool is
  created lazily on first use (~1 s to compile) and kept; `MaxTotal: 2`.
- Per page `GetPageText` → `cleanText` → lines. Output: optional `Title: …` line
  from metadata, then `## Page N` per page. Lines starting with `#` are escaped so
  they don't become headings in the outline.
- `removeRunningLines` (documents with ≥ 4 pages only):
  - top-2 / bottom-2 lines of pages with ≥ 5 lines, keyed by position + text with
    digits normalized, containing ≥ 3 letters, repeated on ≥ 60 % of pages → dropped;
  - a first/last line that is a bare page number is dropped only when those
    numbers track the page index with the same offset on ≥ half the pages.
  Err on the side of keeping text: losing content is worse than a stray footer.

## Reproduce

```bash
go run ./cmd/tsdev read /path/to/file.pdf outline=true
go run ./cmd/tsdev read https://arxiv.org/pdf/1706.03762 max_chars=4000
pdftotext file.pdf - | head -50     # poppler, if installed, as a reference
```

Tests build PDFs in code with `testdoc.PDF` (internal/testdoc) — extend it rather than
committing binary files.

## Known gotchas

- **Never let the WASM runtime write to stdout** (`Stdout: io.Discard`) — stdout is
  the MCP protocol channel. `cmd/tokensaver`'s `TestStdioBinary` would catch it.
- Always `Close()` the instance and `FPDF_CloseDocument` the document (defer), or
  the pool starves and later calls hang.
- Scanned PDFs have no text layer: they produce "(no text on this page)" plus a
  note. OCR is a roadmap item (optional tesseract), not a bug to patch here.
- A future improvement is a real outline from font sizes
  (`GetPageTextStructured`) — if you add it, keep `## Page N` markers available so
  `section="Page 5"` keeps working.

## Fix workflow

1. Reproduce with tsdev; compare with `pdftotext` when text looks wrong.
2. Patch the smallest thing; add a `TestPDF…` case using `testdoc.PDF` pages that show
   the problem.
3. `go test ./internal/convert/ -run PDF` then `go test ./...`.
4. Report what changed and the before/after output size on the reproducer.
