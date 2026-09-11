# tokensaver

Local stdio MCP server in Go that converts web pages, PDFs, Office files and JSON
into compact text for LLMs. See README.md for the user-facing picture.

## Rules that matter

- **The point is fewer tokens.** Every change to output formats should make output
  smaller or clearer, never noisier. Check the size impact with `go run ./cmd/tsdev`.
- **stdout is the MCP channel.** Nothing may print to stdout in the server process
  — no `fmt.Print`, no library that writes there (PDFium's WASM runtime is given
  `io.Discard`). Log with the `slog` logger (stderr). `TestStdioBinary` guards this.
- **Tool descriptions and schemas are sent on every request.** Keep them short; run
  `go run ./cmd/tsdev tools` after touching them (currently ~650 tokens total).
- **Tool results are plain text only** — no structured content / output schemas
  (they would duplicate the payload).
- **Never call an LLM or a paid API.** Everything is deterministic and local.
- **No required system dependencies.** Pure Go (no cgo). Optional tools (Chrome
  today) must degrade gracefully with a clear note when missing.
- Test fixtures are generated in code (see `internal/convert/fixtures_test.go`), not
  committed as binaries.

## Layout

- `cmd/tokensaver` — the binary; `cmd/tsdev` — dev harness (calls tools in-process)
- `internal/server` — MCP tools `read` / `read_json`, cache, freshness rules
- `internal/source` — load URL/file, detect kind, charset → UTF-8
- `internal/convert` — html.go, pdf.go, docx.go, pptx.go, xlsx.go, ooxml.go
- `internal/browser` — optional headless Chrome (chromedp)
- `internal/jsonshrink` — ordered JSON tree, select language, shrink, table, shape
- `internal/view` — outline, section, paging

## Commands

```bash
go test ./...          # everything, incl. real-binary stdio test and Chrome test if installed
go test -short ./...   # fast
go vet ./... && gofmt -l .
go run ./cmd/tsdev read <url-or-file> [outline=true section=… page=… max_chars=…]
go run ./cmd/tsdev read_json <url-or-file> [select=… drop_keys=a,b max_items=… table=true …]
```

## Subagents

`.claude/agents/` has one fixer per area: `web-reader`, `pdf-reader`,
`office-reader`, `json-shrinker`, `mcp-server`. Use the one that owns the failing
area.
