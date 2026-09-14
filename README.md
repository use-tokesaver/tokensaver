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

Measured on real sources by the live benchmark on 2026-09-11 (tokens estimated as
characters / 4; reproduce with `TOKENSAVER_LIVE=1 go test ./e2e/ -run Live -v`):

| Source | Raw tokens | tokensaver | Saved |
|---|---:|---:|---:|
| Wikipedia article (Go), whole article | 183,202 | 19,383 | 89% |
| …its outline | | 248 | 99.9% |
| …only the "History" section | | 1,274 | 99.3% |
| GitHub repo page (modelcontextprotocol/go-sdk) | 87,100 | 1,433 | 98% |
| MDN guide (Using Fetch), whole page | 45,954 | 6,270 | 86% |
| pkg.go.dev (encoding/json), only `section="func Unmarshal"` | 51,758 | 1,184 | 98% |
| GitHub search API, 50 repos, `select` + `table` | 78,745 | 379 | 99.5% |
| …its outline | | 1,048 | 98.7% |
| arXiv paper (PDF, 15 pages) | 2.2 MB binary | 10,019 | — |

Each row also checks that the facts a reader looks for (names, dates, code
signatures, the top repositories) survive in the output.

"Raw" is what a plain fetch puts in the context. Agents don't always see raw
pages: Claude Code's WebFetch, for one, has a small model summarize the page
first. To compare against what an agent really spends, run the A/B harness
(see [Testing](#testing)).

The outline is where large savings come from: a 50-page PDF or a long docs page
costs a few hundred tokens to outline, then the agent reads only the section it
needs.

## Install

```bash
brew tap use-tokesaver/tokensaver
brew install tokensaver
```

macOS and Linux, Intel and ARM — a prebuilt binary, no Go toolchain needed.
Homebrew 7 refuses to load non-official taps until you trust them, so if it
complains, run `brew trust use-tokesaver/tokensaver` and install again.

On Windows, or not using Homebrew? Download an archive from
[Releases](https://github.com/use-tokesaver/tokensaver/releases) and put the
binary on your `PATH`.

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
| `source` | URL, local file path, or local directory path (`~/`, `file://`, and bare `localhost:3000/…` work) |
| `outline` | Only list the sections: id, heading, estimated size |
| `section` | Only return one section — an id from the outline, or heading text |
| `page` | Page of the output (long output is split at paragraph boundaries) |
| `max_chars` | Page size, default 20,000 characters (~5k tokens) |
| `js` | Render in headless Chrome first; happens automatically when a page looks empty |

Supported: HTML/web pages, PDF, DOCX, XLSX, PPTX, JSON, diff/patch (including
GitHub PR and commit URLs), local directories (as a tree), and any text file
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
- **Diffs/PRs** — a GitHub PR or commit URL (`.../pull/42`, `.../commit/<sha>`) is
  fetched as its underlying diff automatically; local `.diff`/`.patch` files work
  the same way. One `## path (+N -M)` heading per changed file; hunks that only
  change whitespace are dropped with a note instead of shown.
- **Directories** — a token-aware tree: the root `.gitignore` is respected, `.git`
  is always skipped, dependency directories (`node_modules`, `vendor`, `dist`,
  `build`…) and any nested git repo/worktree are collapsed to one line instead of
  walked, and any other directory with over 100 direct entries collapses to an
  item count. Files show their size.

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

## Testing

Three layers, from free and deterministic to real and billed:

```bash
go test ./...                                   # unit tests + the e2e suite
TOKENSAVER_LIVE=1 go test ./e2e/ -run Live -v   # real websites, an API and a PDF
go run ./cmd/tsab                               # real Claude Code sessions, with vs. without tokensaver
```

- **e2e suite** (`e2e/`, part of `go test ./...`): builds the binary and talks to
  it over stdio exactly as an agent does. The sources are a local test site (a
  docs page wrapped in the usual framework payload, a Wikipedia-style article, a
  GitHub-style JSON API, a JavaScript app, redirects, legacy charsets, error
  pages) and generated PDF, DOCX, XLSX and PPTX files. It checks that content
  survives, clutter is gone, paging never loses or repeats a paragraph, errors
  are clear, concurrent calls agree, and the tool definitions stay within their
  token budget. With `-v` it prints a savings table.
- **Live benchmark**: the same checks against real sources (the table above).
  It needs the network, and the sites change over time.
- **A/B harness** (`cmd/tsab`): asks Claude Code the same nine questions twice.
  The questions cover Wikipedia, MDN, pkg.go.dev, a GitHub README, the GitHub
  API, an arXiv PDF, a Word report and a 400-row spreadsheet. One arm has only
  Claude Code's built-in tools (WebFetch, Read, Bash); the other has the same
  tools plus tokensaver. The harness reports Claude Code's own token counts
  and cost for every session, and the fixed per-request cost of tokensaver's
  tool definitions. It grades answers by string match and saves every
  transcript under `ab-results/`. Each session is a real `claude -p` run billed
  to your account: a full run is roughly $2–5 on Sonnet. `-only`, `-runs` and
  `-budget` control the spend. It needs a signed-in CLI (`claude auth login`).

## Development

```bash
go test -short ./...                # skip the binary builds and Chrome tests
go run ./cmd/tsdev read https://go.dev/doc/effective_go outline=true
go run ./cmd/tsdev read_json ./data.json select='items.{id,name}' table=true
go run ./cmd/tsdev tools            # the tool schemas and what they cost per request
```

`tsdev` calls the tools in-process through a real MCP client and prints the result
with its size; it is a development aid, not part of the installed product.

```text
cmd/tokensaver     the stdio MCP server binary
cmd/tsdev          development harness
cmd/tsab           A/B harness (real Claude Code sessions)
e2e/               end-to-end suite and live benchmark
internal/server    MCP tools, caching, request handling
internal/source    loading URLs/files, type detection, charset handling
internal/convert   HTML, PDF, DOCX, XLSX, PPTX → Markdown
internal/browser   optional headless Chrome rendering
internal/jsonshrink  order-preserving JSON, select, shrinking, tables, shape
internal/view      outline, sections, paging
internal/testdoc   documents generated in code for tests and benchmarks
```

`.claude/agents/` holds Claude Code subagents that each own one module (web, PDF,
Office, JSON, MCP server) and know its tests and pitfalls — ask Claude Code to use
the matching agent when something in that area breaks.

### Contributing and releasing

`main` takes pull requests only. Branch, open a PR, and
[.github/workflows/ci.yml](.github/workflows/ci.yml) (gofmt, `go vet`, `go test ./...`)
has to pass before it can merge.

Releases are cut automatically from commit messages — no manual tagging. Subjects
follow [Conventional Commits](https://www.conventionalcommits.org): `feat:` bumps the
minor version, `fix:` the patch, `feat!:` (or a `BREAKING CHANGE:` footer) the major,
and `docs:`/`chore:`/`test:`/`refactor:`/`ci:` release nothing.

When a PR merges, [.github/workflows/release.yml](.github/workflows/release.yml) asks
[`svu`](https://github.com/caarlos0/svu) for the next version; if nothing releasable
landed it stops there, otherwise it tags and hands over to
[GoReleaser](https://goreleaser.com) ([.goreleaser.yaml](.goreleaser.yaml)), which
cross-compiles `cmd/tokensaver` for macOS and Linux (amd64 + arm64), publishes a GitHub
Release with archives and checksums, and updates
[use-tokesaver/homebrew-tokensaver](https://github.com/use-tokesaver/homebrew-tokensaver).

Pushing a `v*` tag by hand still works as an escape hatch. To dry-run the build locally
without publishing: `goreleaser release --snapshot --clean --skip=publish`.

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
