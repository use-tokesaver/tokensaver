# tokensaver

Local stdio MCP server in Go that converts web pages, PDFs, Office files and JSON
into compact text for LLMs. See README.md for the user-facing picture.

## Rules that matter

- **The point is fewer tokens.** Every change to output formats should make output
  smaller or clearer, never noisier. Check the size impact with `go run ./cmd/tsdev`
  and the savings table of `go test ./e2e/ -v`.
- **stdout is the MCP channel.** Nothing may print to stdout in the server process
  — no `fmt.Print`, no library that writes there (PDFium's WASM runtime is given
  `io.Discard`). Log with the `slog` logger (stderr). `TestStdioBinary` and the
  e2e suite guard this.
- **Tool descriptions and schemas are sent on every request.** Keep them short; run
  `go run ./cmd/tsdev tools` after touching them (currently ~650 tokens total;
  `TestToolsAreCheap` fails above 3400 characters with the instructions).
- **Tool results are plain text only** — no structured content / output schemas
  (they would duplicate the payload).
- **Never call an LLM or a paid API** from the server or from `go test`. The one
  exception is `cmd/tsab`, the opt-in A/B harness that runs real, billed Claude
  Code sessions; never wire it into tests.
- **No required system dependencies.** Pure Go (no cgo). Optional tools (Chrome
  today) must degrade gracefully with a clear note when missing.
- Test fixtures are generated in code (`internal/testdoc`), not committed as
  binaries.

## Layout

- `cmd/tokensaver` — the binary; `cmd/tsdev` — dev harness (calls tools in-process)
- `cmd/tsab` — A/B harness: same tasks in Claude Code with and without tokensaver
- `e2e/` — end-to-end suite: the real binary over stdio against a local test site
  and generated documents; `live_test.go` checks real websites (opt-in)
- `internal/server` — MCP tools `read` / `read_json`, cache, freshness rules
- `internal/source` — load URL/file, detect kind, charset → UTF-8
- `internal/convert` — html.go, pdf.go, docx.go, pptx.go, xlsx.go, ooxml.go
- `internal/browser` — optional headless Chrome (chromedp)
- `internal/jsonshrink` — ordered JSON tree, select language, shrink, table, shape
- `internal/view` — outline, section, paging
- `internal/testdoc` — PDF/DOCX/PPTX/XLSX builders and larger documents with known facts

## Commands

```bash
go test ./...          # everything, incl. the e2e suite and Chrome tests if installed
go test -short ./...   # fast: no binary builds
go test ./e2e/ -v      # e2e suite + estimated savings table
TOKENSAVER_LIVE=1 go test ./e2e/ -run Live -v   # real sites: facts survive, savings
go vet ./... && gofmt -l .
go run ./cmd/tsdev read <url-or-file> [outline=true section=… page=… max_chars=…]
go run ./cmd/tsdev read_json <url-or-file> [select=… drop_keys=a,b max_items=… table=true …]
go run ./cmd/tsab [-only task,…] [-runs N] [-model sonnet]   # billed: real Claude sessions
```

## Subagents

`.claude/agents/` has one fixer per area: `web-reader`, `pdf-reader`,
`office-reader`, `json-shrinker`, `mcp-server`. Use the one that owns the failing
area.
