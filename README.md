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

## Prerequisites

- Java 21
- Maven
- Optional system dependencies, each needed only for its one endpoint — everything
  else has no dependency beyond the JVM:
  - `tesseract` — `/api/files/ocr` (`brew install tesseract` / `apt install tesseract-ocr`)
  - `whisper` (+ `ffmpeg`) — `/api/audio/transcribe` (`pip install openai-whisper`)
  - `wkhtmltopdf` / `wkhtmltoimage` — `/api/render/html` (`brew install wkhtmltopdf` /
    the `wkhtmltopdf` package on Debian/Ubuntu)

## Running it

```bash
mvn -pl tokensaver-api -am -DskipTests package
java -jar tokensaver-api/target/tokensaver-api-0.1.0-POC.jar
```

Server starts on `http://localhost:8080` by default. Override with
`-Dserver.port=<port>` if that's taken. REST endpoints live under `/api/...`; the
MCP endpoint is `/mcp`.

Confirm it's up:

```bash
curl http://localhost:8080/api/mcp-tools
```

`/mcp` speaks the MCP protocol (JSON-RPC over HTTP, with session headers) — it's not
meant to be opened in a browser or curled plainly; a client like Claude Code handles
that framing for you. `/api/mcp-tools` is the plain-JSON way to see what tools exist
without any of that — it returns
`{ "tools": [...], "fileOperations": { "note": "...", "endpoints": [...] } }` —
the `tools` array is read straight off the live MCP server, so it can't drift out of
sync with what `/mcp` actually serves; `fileOperations` documents the file-based REST
endpoints with a ready-to-run curl command for each, since those aren't MCP tools and
wouldn't otherwise show up here at all.

If you're not deploying at `http://localhost:<port>` (e.g. behind a reverse proxy or a
public host), set `tokensaver.public-base-url` so the MCP server's `instructions` tell
agents the correct address for file-operation curl commands instead of guessing:

```bash
java -jar tokensaver-api/target/tokensaver-api-0.1.0-POC.jar \
  --tokensaver.public-base-url=https://tokensaver.example.com
```

## Connecting an agent via MCP

Add the deployed URL + `/mcp` as a remote MCP connector — for example in Claude Code:

```bash
claude mcp add --transport http tokensaver http://localhost:8080/mcp
```

(swap the host for wherever tokensaver-api is actually deployed). Once added, the
agent sees 6 tools — `web_extract`, `convert_data`, `diff_data`, `hash_text`, `base64`,
`evaluate_formula` — and can call them directly instead of writing a script for the
same job.

**File-based operations are deliberately not exposed as MCP tools** — extracting text
from documents, image conversion, OCR, zip/unzip, audio transcription, HTML/Markdown
rendering, barcode generate/decode, and PDF merge/split/rotate/watermark/fill-form are
all REST-only, called via curl. MCP tool arguments are JSON, which has no binary type,
so the only way to pass file content through a tool call is base64 — and that forces
the model to *generate* the entire file as output tokens just to make the call, which
is more expensive than the script this API exists to replace. An earlier version
exposed some of these as tools anyway (with warnings and a size cap), but an agent
would still sometimes read a file and base64-encode it into a tool call rather than
reaching for curl. Rather than rely on an agent noticing and heeding a warning, the
tools simply don't exist — the MCP server's `instructions` (surfaced automatically to
the agent on connect) tell it to use curl for these operations, and there's no tool to
reach for instead:

```
Extract the text from /path/to/file.pdf using tokensaver
```
should make the agent run
```bash
curl -F "file=@/path/to/file.pdf" http://localhost:8080/api/files/extract-text
```
on its own.

## Testing

### 1. REST smoke test (no agent involved)

With the server running, exercise each endpoint directly:

```bash
curl -X POST http://localhost:8080/api/util/hash \
  -H "Content-Type: application/json" -d '{"text":"hello","algorithm":"SHA-256"}'

curl -X POST http://localhost:8080/api/data/convert \
  -H "Content-Type: application/json" -d '{"input":"{\"a\":1}","from":"json","to":"yaml"}'

curl -X POST http://localhost:8080/api/data/diff \
  -H "Content-Type: application/json" \
  -d '{"left":"{\"a\":1}","right":"{\"a\":2}","format":"json"}'

curl -X POST http://localhost:8080/api/web/extract \
  -H "Content-Type: application/json" -d '{"url":"https://example.com"}'

curl -F "file=@/path/to/file.pdf" http://localhost:8080/api/files/extract-text

curl -F "file=@scan.png" http://localhost:8080/api/files/ocr

curl -F "file=@image.png" "http://localhost:8080/api/files/image/convert?format=jpg&width=200" -o out.jpg

curl -F "files=@a.txt" -F "files=@b.txt" http://localhost:8080/api/util/zip -o bundle.zip
curl -F "file=@bundle.zip" http://localhost:8080/api/util/unzip

curl -X POST http://localhost:8080/api/sheet/evaluate \
  -H "Content-Type: application/json" -d '{"cells":{"A1":"5","A2":"10"},"formula":"=A1+A2"}'

curl -F "file=@audio.mp3" "http://localhost:8080/api/audio/transcribe?model=base"

curl -X POST http://localhost:8080/api/render/html -H "Content-Type: application/json" \
  -d '{"content":"# Hello","sourceType":"markdown","format":"pdf"}' -o out.pdf

curl -X POST http://localhost:8080/api/barcode/generate -H "Content-Type: application/json" \
  -d '{"text":"hello","format":"QR_CODE"}' -o qr.png
curl -F "file=@qr.png" http://localhost:8080/api/barcode/decode

curl -F "files=@a.pdf" -F "files=@b.pdf" http://localhost:8080/api/pdf/merge -o merged.pdf
curl -F "file=@merged.pdf" "http://localhost:8080/api/pdf/split?pagesPerFile=1" -o split.zip
curl -F "file=@merged.pdf" "http://localhost:8080/api/pdf/rotate?degrees=90" -o rotated.pdf
curl -F "file=@merged.pdf" "http://localhost:8080/api/pdf/watermark?text=DRAFT" -o watermarked.pdf
```

Each should return `200` with the expected JSON/bytes. Try a bad input too (missing
file part, malformed JSON, unreachable URL) and confirm you get a `4xx` with
`{ "error": "..." }` rather than a raw `500`.

### 2. MCP tool test (through an actual agent)

Register the connector once per project you'll test from:

```bash
claude mcp add --transport http tokensaver http://localhost:8080/mcp
```

Then, in a **fresh** Claude Code session (`/clear` or a new session — an existing
session won't pick up a connector added after it started) in that project, run `/mcp`
to confirm `tokensaver` shows up with 6 tools, then try:

```
Use tokensaver to hash "hello world" with sha256
Fetch https://example.com with tokensaver and summarize the page
Convert this JSON to YAML using tokensaver: {"name": "Milan", "role": "developer"}
Base64 encode the string "tokensaver rocks" using tokensaver
Diff these two JSON objects using tokensaver: {"a":1,"b":2} and {"a":1,"b":3,"c":4}
Use tokensaver to evaluate =SUM(A1:A3)*2 with A1=2, A2=3, A3=4
```

These should show up as native tool calls (`web_extract`, `convert_data`, `diff_data`,
`hash_text`, `base64`, `evaluate_formula`), not a curl command or a written script.

### 3. File-operation fallback test (the important one)

File operations are deliberately *not* MCP tools (see below), so this checks that an
agent correctly falls back to curl instead of trying to read the file itself:

```
Extract the text from /path/to/some/file.pdf using tokensaver
```

Watch for: no attempt to read the file's bytes or base64-encode it, and no probing for
a health-check endpoint first — it should go straight to
`curl -F "file=@/path/to/some/file.pdf" http://localhost:8080/api/files/extract-text`.
If it hesitates or guesses at the base URL, check `tokensaver.public-base-url` is set
correctly for your deployment (see above).

### 4. Measuring whether this actually saves tokens

`/cost` in Claude Code (or the Anthropic API's `usage` field) is the only reliable way
to see real token numbers — the consumer chat UI doesn't expose this. Compare the same
task run twice in fresh sessions:

- **With tokensaver**: connector added, run one of the prompts above, then `/cost`.
- **Without tokensaver** (baseline): a session with no tokensaver connector, asking the
  same thing but forcing a script, e.g. `"Extract the text from X.pdf — write and run a
  script to do it."`, then `/cost`.

Repeat a few times per task — script-writing has variance (sometimes it one-shots,
sometimes it debugs an import error) — and check the output is actually correct in
both runs, not just cheaper.

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

### Spreadsheet formula evaluation
`POST /api/sheet/evaluate`
```json
{ "cells": { "A1": "5", "A2": "10" }, "formula": "=A1+A2" }
```
Evaluates the formula against the given cell values via Apache POI's formula engine.
Returns `{ "result": "15" }`.

### Audio transcription
`POST /api/audio/transcribe?model=base` (multipart `file`: any audio format ffmpeg reads)
Returns `{ "text": "..." }` via the locally installed Whisper CLI. **Requires
`whisper` on PATH** (`pip install openai-whisper`, plus `ffmpeg`). `model` is one of
`tiny`/`base`/`small`/`medium`/`large` (default `base`) — larger models are slower but
more accurate, and are downloaded once on first use.

### Markdown/HTML rendering
`POST /api/render/html`
```json
{ "content": "# Hello", "sourceType": "markdown", "format": "pdf" }
```
Renders Markdown or HTML headlessly and returns the file's bytes (`format`: `pdf` or
`png`). **Requires `wkhtmltopdf`/`wkhtmltoimage` on PATH**.

### Barcode / QR generate & decode
`POST /api/barcode/generate`
```json
{ "text": "hello", "format": "QR_CODE", "width": 300, "height": 300 }
```
Returns a PNG. `format` is any ZXing `BarcodeFormat` (`QR_CODE`, `CODE_128`, `EAN_13`,
`UPC_A`, `PDF_417`, ...), default `QR_CODE`.

`POST /api/barcode/decode` (multipart `file`: an image containing a code)
Returns `{ "text": "...", "format": "..." }`.

### PDF manipulation
- `POST /api/pdf/merge` (multipart `files`, repeatable, ≥2) → merged PDF bytes
- `POST /api/pdf/split?pagesPerFile=1` (multipart `file`) → a `.zip` of PDF chunks
- `POST /api/pdf/rotate?degrees=90` (multipart `file`) → rotated PDF bytes (`degrees`
  must be a multiple of 90)
- `POST /api/pdf/watermark?text=DRAFT` (multipart `file`) → PDF with a diagonal text
  watermark stamped on every page
- `POST /api/pdf/fill-form` (multipart `file`, plus a `fields` part with a JSON object
  like `{"name":"John"}`) → PDF with its AcroForm fields filled in

## Design notes

- Every module (`web`, `files`, `data`, `util`, `audio`, `render`, `barcode`, `sheet`)
  is a self-contained package: a `*Service` with the actual logic and a thin
  `*Controller`. Adding a new REST API means adding a new package in this shape — no
  shared framework beyond `common/ApiException` + `common/GlobalExceptionHandler`.
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

- EXIF read/strip, audio/video metadata (duration, codec)
- JSON Schema validation, JSONPath/XPath query
- Unit conversion, timezone conversion, cron expression parsing
- Checksum verification, archive formats beyond zip (tar.gz)

## Explicitly out of scope for this stage

- Authentication, rate limiting, usage plans/billing
- Persistence / job queues for long-running work
- Any endpoint whose implementation calls an LLM
