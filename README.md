# tokensaver

**Deterministic-task APIs for LLM agents.**

When an agent needs to do something mechanical — extract text from a PDF, clean up a
scraped web page, resize an image, convert JSON to CSV, hash a string, zip some files —
the usual pattern is: the agent writes a script, runs it, debugs it when it fails, and
burns a pile of tokens doing so. That work is deterministic. It doesn't need a language
model at all.

tokensaver is a POC: a single Spring Boot service that exposes this work both as plain
REST endpoints and as native MCP tools over HTTP — so an MCP-capable agent (Claude,
etc.) can add it as a remote connector and call the tools directly, the same way a
Google Drive/Gmail connector works. Nothing to download or run locally.

**Hard constraint:** nothing in this service calls an LLM or any paid AI API. Every
endpoint is implemented with deterministic code and open-source libraries (Jsoup,
PDFBox, POI, Jackson, java.awt/ImageIO, java.util.zip, java.security). The whole point
is to save tokens — routing the work through another model would just burn them
somewhere else.

This is a proof-of-concept stage: no auth, no plans/billing, no persistence. It's meant
to answer "does this actually save tokens / reduce friction for an agent" before any of
that is built.

## Running it

```bash
mvn -pl tokensaver-api -am -DskipTests package
java -jar tokensaver-api/target/tokensaver-api-0.1.0-POC.jar
```

Server starts on `http://localhost:8080`. REST endpoints live under `/api/...`; the
MCP endpoint is `/mcp`.

`/mcp` speaks the MCP protocol (JSON-RPC over HTTP, with session headers) — it's not
meant to be opened in a browser or curled plainly; a client like Claude Code handles
that framing for you. To just see what tools exist without any of that, use:

```bash
curl http://localhost:8080/api/mcp-tools
```

which returns `{ "tools": [...], "fileOperations": { "note": "...", "endpoints": [...] } }` —
the `tools` array is read straight off the live MCP server, so it can't drift out of
sync with what `/mcp` actually serves; `fileOperations` documents the 5 file-based REST
endpoints with a ready-to-run curl command for each, since those aren't MCP tools and
wouldn't otherwise show up here at all.

## Connecting an agent via MCP

Add the deployed URL + `/mcp` as a remote MCP connector — for example in Claude Code:

```bash
claude mcp add --transport http tokensaver http://localhost:8080/mcp
```

(swap the host for wherever tokensaver-api is actually deployed). Once added, the
agent sees 5 tools — `web_extract`, `convert_data`, `diff_data`, `hash_text`, `base64` —
and can call them directly instead of writing a script for the same job.

**File-based operations are deliberately not exposed as MCP tools** — extracting text
from documents, image conversion, OCR, and zip/unzip are REST-only, called via curl.
MCP tool arguments are JSON, which has no binary type, so the only way to pass file
content through a tool call is base64 — and that forces the model to *generate* the
entire file as output tokens just to make the call, which is more expensive than the
script this API exists to replace. An earlier version exposed these as tools anyway
(with warnings and a size cap), but an agent would still sometimes read a file and
base64-encode it into a tool call rather than reaching for curl. Rather than rely on
an agent noticing and heeding a warning, the tools simply don't exist — the MCP
server's `instructions` (surfaced automatically to the agent on connect) tell it to
use curl for these operations, and there's no tool to reach for instead:

```
Extract the text from /path/to/file.pdf using tokensaver
```
should make the agent run
```bash
curl -F "file=@/path/to/file.pdf" http://localhost:8080/api/files/extract-text
```
on its own.

## Measuring whether this actually saves tokens

`/cost` in Claude Code (or the Anthropic API's `usage` field) is the only reliable way
to see real token numbers — the consumer chat UI doesn't expose this. Compare the same
task run twice in fresh sessions: once with the tokensaver MCP connector added, once
without (forcing a written script). Repeat a few times per task — script-writing has
variance (sometimes it one-shots, sometimes it debugs an import error) — and check the
output is actually correct in both runs, not just cheaper.

## REST endpoints

### Web extraction
`POST /api/web/extract`
```json
{ "url": "https://example.com/article" }
```
Fetches the page, strips nav/scripts/ads/boilerplate, returns `{ url, title, text, markdown }`.
Refuses localhost/private/link-local targets (SSRF guard).

### File → text extraction
`POST /api/files/extract-text` (multipart `file`: `.pdf`, `.docx`, `.xlsx`, `.txt`, `.md`, `.csv`)
Returns `{ "text": "..." }`.

### Image conversion
`POST /api/files/image/convert?format=jpg&width=200&height=200` (multipart `file`)
Returns the converted image bytes directly.

### Data format conversion
`POST /api/data/convert`
```json
{ "input": "{\"a\":1}", "from": "json", "to": "yaml" }
```
Supports `json`, `yaml`, `csv` in any direction (CSV requires an array of flat objects).

### Hashing
`POST /api/util/hash`
```json
{ "text": "hello", "algorithm": "SHA-256" }
```
Algorithms: `MD5`, `SHA-1`, `SHA-256`, `SHA-512`.

### Base64
`POST /api/util/base64/encode` / `POST /api/util/base64/decode`
```json
{ "text": "hello world" }
```

### Zip / unzip
`POST /api/util/zip` (multipart `files`, repeatable) → returns a `.zip`
`POST /api/util/unzip` (multipart `file`) → `[{ name, size, textPreview }]`

### OCR
`POST /api/files/ocr` (multipart `file`: an image, or a scanned `.pdf`)
Returns `{ "text": "..." }` via the locally installed Tesseract CLI. **Requires
`tesseract` on PATH** (`brew install tesseract` on macOS, `apt install tesseract-ocr`
on Debian/Ubuntu) — this is the one endpoint with a system dependency beyond the JVM.

### Diff
`POST /api/data/diff`
```json
{ "left": "...", "right": "...", "format": "text" }
```
`format: text` returns a unified diff string. `format: json` or `yaml` parses both
sides and returns a list of `{ path, type, before, after }` changes (`type` is
`added`, `removed`, or `changed`).

## Design notes

- Every module (`web`, `files`, `data`, `util`) is a self-contained package: a
  `*Service` with the actual logic and a thin `*Controller`. Adding a new REST API
  means adding a new package in this shape — no shared framework beyond
  `common/ApiException` + `common/GlobalExceptionHandler`.
- `mcp/McpToolsConfiguration` registers MCP tools on a servlet mounted at `/mcp`
  (Streamable HTTP transport). Each tool handler forwards to the matching REST
  endpoint via `mcp/LoopbackApiClient` (a plain `java.net.http.HttpClient` call to
  `http://localhost:<port>/api/...`) rather than calling service beans directly — the
  REST controllers stay the one real implementation, MCP is just a protocol adapter in
  front of them. Adding a tool here means adding one method that builds a
  `McpSchema.Tool` and calls the matching REST endpoint through the client — but only
  for endpoints with no file content; see above for why file-based endpoints stay
  REST-only.
- The MCP SDK's internal JSON handling is Jackson 3 ("tools.jackson"), which needs a
  newer `jackson-annotations` than Spring Boot manages by default — pinned explicitly
  in `pom.xml` to avoid a `NoSuchFieldError` at startup.
- Errors from bad input (unreachable URL, unsupported format, malformed file) return
  `400` with `{ "error": "..." }` from the REST API, and surface as an `isError` tool
  result on the MCP side — agents can branch on that instead of parsing stack traces.

## Ideas for the next batch of APIs (not yet built)

- Local/offline audio transcription (Vosk or whisper.cpp subprocess — deterministic
  from the caller's point of view, no per-token billing)
- EXIF read/strip, audio/video metadata (duration, codec)
- Render Markdown/HTML → PDF or PNG (headless)
- JSON Schema validation, JSONPath/XPath query
- Unit conversion, timezone conversion, cron expression parsing
- Checksum verification, archive formats beyond zip (tar.gz)

## Explicitly out of scope for this stage

- Authentication, rate limiting, usage plans/billing
- Persistence / job queues for long-running work
- Any endpoint whose implementation calls an LLM
