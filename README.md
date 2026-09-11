# tokensaver

**Local MCP server that turns web pages, PDFs, Office files and JSON into compact,
LLM-ready text — so your coding agent spends its context on content, not markup.**

Ask your agent *"read https://go.dev/doc/effective_go with tokensaver"* or *"fetch
this JSON via tokensaver and drop the large lists"*. tokensaver fetches or opens the
thing, strips everything an LLM doesn't need (navigation, scripts, styling, ads,
citation marks, running page headers, null fields…) and returns clean Markdown or
shrunk JSON — paged, with an outline so the agent can read just the part it needs.

- **Local and offline-first.** One Go binary. Runs on your machine as a stdio MCP
  server; nothing is sent anywhere except the requests to URLs you ask for. No
  telemetry, no API keys, and it never calls an LLM itself.
- **No runtime dependencies.** PDF extraction uses PDFium compiled to WebAssembly,
  running inside the binary. Chrome/Chromium is used *only if installed*, for pages
  that need JavaScript.
- **Inspired by [Microsoft MarkItDown](https://github.com/microsoft/markitdown)**,
  rebuilt in Go around one goal: fewer tokens.

## How much it saves

Measured on real pages (characters; ~4 characters per token):

| Source | Raw | tokensaver | Saved |
|---|---:|---:|---:|
| GitHub repo page (microsoft/markitdown) | 399,441 | 16,568 | 96% |
| MDN docs page (HTTP 404) | 210,385 | 2,646 | 99% |
| Wikipedia article (Markdown) | 311,702 | 30,508 | 90% |
| …only its "History" section via `outline` → `section` | 311,702 | 1,775 | 99.4% |
| pkg.go.dev (encoding/json), whole page | 207,129 | 58,607 | 72% |
| …only `section="func Unmarshal"` | 207,129 | 4,689 | 98% |
| GitHub search API, 30 repos (JSON), with `select` + `table` | 190,897 | 552 | 99.7% |

The outline is where large savings come from: a 50-page PDF or a long docs page
costs a few hundred tokens to outline, then the agent reads only the section it
needs.

## Install

```bash
go install github.com/MilanBehnam/tokensaver/cmd/tokensaver@latest
```

(Go 1.26+. The binary lands in `$(go env GOPATH)/bin` — make sure that is on your
`PATH`.) Or build from a clone: `go build -o tokensaver ./cmd/tokensaver`.

Optional: Chrome, Chromium, Brave or Edge installed in the usual place (or
`TOKENSAVER_CHROME=/path/to/chrome`) lets tokensaver render JavaScript-only pages.

## Connect it to your agent

**Claude Code**

```bash
claude mcp add --scope user tokensaver -- tokensaver
```

**Cursor** (`~/.cursor/mcp.json`), **Claude Desktop**
(`claude_desktop_config.json`), and other MCP clients:

```json
{
  "mcpServers": {
    "tokensaver": { "command": "tokensaver" }
  }
}
```

If the client can't find it, use the absolute path, e.g.
`"command": "/Users/you/go/bin/tokensaver"`.

## Tools

The tool definitions cost ~650 tokens per request in total — they are kept short
on purpose.

### `read` — web pages and documents → Markdown

| Parameter | |
|---|---|
| `source` | URL, or local file path (`~/`, `file://`, and bare `localhost:3000/…` work) |
| `outline` | Only list the sections: id, heading, estimated size |
| `section` | Only return one section — an id from the outline, or heading text |
| `page` | Page of the output (long output is split at paragraph boundaries) |
| `max_chars` | Page size, default 20,000 characters (~5k tokens) |
| `js` | Render in headless Chrome first; happens automatically when a page looks empty |

Supported: HTML/web pages, PDF, DOCX, XLSX, PPTX, JSON, and any text file
(Markdown, CSV, code, logs…).

What it does per format:

- **Web pages** — main-content extraction (Mozilla Readability), then Markdown.
  Scripts, styles, images, forms, nav/header/footer chrome, heading permalinks and
  `[12]` citation marks are dropped; same-site links become short root-relative
  paths (`/docs/x`), tracking parameters (`utm_*`) are removed; tables and code
  blocks (with their language) are kept.
- **PDF** — text per page under `## Page N` headings. Running headers/footers and
  page numbers that repeat on most pages are removed.
- **DOCX** — headings (from styles), bullet/numbered lists, bold/italic, links,
  tables. Deleted tracked changes are skipped.
- **XLSX** — each sheet as a Markdown table of displayed values; empty rows and
  columns dropped; hidden sheets marked.
- **PPTX** — `## Slide N: Title`, bullets with nesting, tables, speaker notes;
  slide numbers/footers skipped.

Paged output ends with a hint such as `[page 1 of 4 · next: page=2 · outline=true
lists sections]`. When a table or code block is split across pages, the table
header is repeated and the code fence is closed and reopened, so every page stands
on its own.

### `read_json` — JSON APIs and files, shrunk

| Parameter | |
|---|---|
| `source` | URL (HTTP GET) or local file path |
| `outline` | Show the structure — keys, types, array sizes, one example each — instead of the data |
| `select` | Keep only these fields (see below) |
| `drop_keys` | Keys to remove everywhere; names or globs: `["*_url", "node_id"]` |
| `max_items` | Keep only the first N items of every array (`… 480 more items`) |
| `drop_lists_over` | Replace arrays longer than N with `"[480 items omitted]"` |
| `max_str` | Cut strings longer than N characters |
| `table` | Render arrays of objects as Markdown tables |
| `keep_empty` | Keep `null`, `""`, `[]`, `{}` — by default they are dropped |
| `page`, `max_chars` | As for `read` |

`select` syntax — a small, forgiving path language:

```text
total_count                     a field
items.name                      a field of every element (arrays map implicitly)
items[0]   items[-1]   items[:5]  index / slice
items.{name, owner.login}       pick fields → {"name":…, "owner.login":…}
items.{repo: name, stars: stargazers_count}   pick with aliases
total_count, items.id           several fields at once
items[].tags[0]                 explicit [] maps everything after it (first tag of each item)
```

A typical exchange on an unfamiliar API:

```text
read_json source=https://api.github.com/search/repositories?q=mcp outline=true
→ {total_count: number (12340), items: [30] {id: number, name: string ("github-mcp-server"), owner: {…}, …}}

read_json source=… select="items.{full_name, stargazers_count, description}" max_items=5 table=true
→ | full_name | stargazers_count | description |
  |---|---|---|
  | github/github-mcp-server | 32860 | GitHub's official MCP Server |
  …
```

JSON output is compact: containers that fit on a line stay on one line; bigger ones
put one element per line with one-space indentation — close to minified JSON in
tokens, but still readable and pageable. API error responses (4xx/5xx) are returned
shrunk, so the agent sees the error message itself.

## Configuration

| Environment variable | |
|---|---|
| `TOKENSAVER_MAX_CHARS` | Default page size (default `20000`) |
| `TOKENSAVER_CHROME` | Path to a Chrome/Chromium-family browser |
| `TOKENSAVER_LOG` | `debug`, `info` (default) or `warn`; logs go to stderr — one line per tool call with sizes and timings |

Freshness: a plain `read`/`read_json` always fetches the URL again (the page or API
you are developing may have just changed). Follow-up calls — `page=2`, `section=`,
`outline=` — reuse the result for up to 10 minutes, in memory only, so page numbers
stay stable and big PDFs aren't re-parsed. Local files are re-read whenever they
change.

## Security notes

tokensaver reads what your agent asks it to read, with your permissions: any local
file you can read, and any URL, including `localhost` and your private network
(useful for dev servers, but keep it in mind if your agent processes untrusted
content). It only performs HTTP `GET` requests and never writes files.

## Development

```bash
go test ./...                       # unit + end-to-end tests (spawns the real binary over stdio)
go test -short ./...                # skip the binary build and Chrome tests

go run ./cmd/tsdev read https://go.dev/doc/effective_go outline=true
go run ./cmd/tsdev read_json ./data.json select='items.{id,name}' table=true
go run ./cmd/tsdev tools            # the tool schemas and what they cost per request
```

`tsdev` calls the tools in-process through a real MCP client and prints the result
with its size; it is a development aid, not part of the installed product.

```text
cmd/tokensaver     the stdio MCP server binary
cmd/tsdev          development harness
internal/server    MCP tools, caching, request handling
internal/source    loading URLs/files, type detection, charset handling
internal/convert   HTML, PDF, DOCX, XLSX, PPTX → Markdown
internal/browser   optional headless Chrome rendering
internal/jsonshrink  order-preserving JSON, select, shrinking, tables, shape
internal/view      outline, sections, paging
```

`.claude/agents/` holds Claude Code subagents that each own one module (web, PDF,
Office, JSON, MCP server) and know its tests and pitfalls — ask Claude Code to use
the matching agent when something in that area breaks.

## Roadmap

- Headings detected in PDFs (from font sizes) for a real outline instead of pages
- OCR for scanned PDFs and images (optional `tesseract`)
- Audio transcription (optional `whisper.cpp`)
- EPUB, ZIP archives, RSS/Atom feeds
- Optional per-host auth headers for private APIs (from a local config file, never
  passed through the model)

## License

MIT — see [LICENSE](LICENSE). The binary embeds PDFium (BSD-3-Clause / Apache-2.0)
via [go-pdfium](https://github.com/klippa-app/go-pdfium); other dependencies are
listed in `go.mod` and carry their own permissive licenses.
