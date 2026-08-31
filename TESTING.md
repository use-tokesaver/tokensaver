# tokensaver — test scenarios

Full test scenarios for every endpoint: happy paths, edge cases, and bad-input cases.
Run these against a live instance (`java -jar tokensaver-api/target/tokensaver-api-0.1.0-POC.jar`,
default `http://localhost:8080`). Swap the host/port if you're testing a different
deployment. Where a scenario needs a sample file, any small file of that type works —
see the "sample files" note at the bottom.

For each scenario, the expected outcome is either a `200` with the described shape, or
a `4xx` with `{ "error": "..." }` — never a raw `500`/stack trace. If you get a `500`,
that's a bug: capture the response body and server log line and route it to the
matching agent in `.claude/agents/` (see `README.md` → "Fixing a broken API").

## Web extraction — `/api/web/extract`

| # | Scenario | Request | Expect |
|---|---|---|---|
| 1 | Happy path | `{"url":"https://example.com"}` | `200`, `{url,title,text,markdown}` all non-empty |
| 2 | Missing `url` | `{}` | `400` |
| 3 | Malformed URL | `{"url":"not a url"}` | `400` |
| 4 | Unreachable host | `{"url":"https://this-does-not-exist.invalid"}` | `400` (not 500) |
| 5 | SSRF: localhost | `{"url":"http://localhost:8080/api/mcp-tools"}` | `400`, rejected as a private/loopback target |
| 6 | SSRF: private IP | `{"url":"http://192.168.1.1"}` | `400` |
| 7 | SSRF: link-local | `{"url":"http://169.254.169.254"}` | `400` (cloud metadata endpoint — must be blocked) |
| 8 | Non-HTML content | `{"url":"https://example.com/some.pdf"}` | `200` with empty/minimal text, or a clear `400` — should not throw |

## File → text extraction — `/api/files/extract-text`

| # | Scenario | Expect |
|---|---|---|
| 1 | `.pdf` happy path | `200`, `{text}` contains the PDF's real text |
| 2 | `.docx` happy path | `200` |
| 3 | `.xlsx` happy path | `200` |
| 4 | `.txt` / `.md` / `.csv` happy path | `200` |
| 5 | Missing `file` part | `400`, "Missing required part: file" |
| 6 | Empty file (0 bytes) | `400` |
| 7 | Unsupported extension (e.g. `.exe`) | `400` |
| 8 | Corrupted PDF bytes (truncate a real PDF) | `400`, not `500` |
| 9 | Extension/content mismatch (a `.txt` renamed to `.pdf`) | `400` or best-effort `200` — should not crash |

## Image conversion — `/api/files/image/convert`

| # | Scenario | Expect |
|---|---|---|
| 1 | PNG → JPG, with width/height | `200`, image bytes decode as a JPG of the requested size |
| 2 | No width/height (format-only conversion) | `200`, original dimensions preserved |
| 3 | Missing `file` | `400` |
| 4 | Invalid `format` value (e.g. `format=exe`) | `400` |
| 5 | Non-image file uploaded | `400`, not `500` |
| 6 | Width/height = 0 or negative | `400` |

## OCR — `/api/files/ocr`

| # | Scenario | Expect |
|---|---|---|
| 1 | Clear image of printed text | `200`, `{text}` roughly matches the source text |
| 2 | Scanned (image-only) PDF | `200`, text extracted per page |
| 3 | Missing `file` | `400` |
| 4 | `tesseract` not installed | `400`, message names the exact fix (`brew install tesseract`) |
| 5 | Blank/noise image | `200` with empty or near-empty text, not an error |
| 6 | Non-image, non-PDF file | `400` |

## Data conversion — `/api/data/convert`

| # | Scenario | Expect |
|---|---|---|
| 1 | JSON → YAML | `200` |
| 2 | YAML → JSON | `200` |
| 3 | JSON → CSV (flat array of objects) | `200` |
| 4 | CSV → JSON | `200` |
| 5 | JSON → CSV with a nested object (not flat) | `400`, clear message (CSV needs flat objects) |
| 6 | Invalid `from`/`to` value | `400` |
| 7 | Malformed input for the stated format | `400`, not `500` |
| 8 | `from` == `to` | `200`, input echoed/normalized |

## Diff — `/api/data/diff`

| # | Scenario | Expect |
|---|---|---|
| 1 | `format: text`, differing inputs | `200`, unified diff string |
| 2 | `format: json`, added/removed/changed keys | `200`, list of `{path,type,before,after}` |
| 3 | `format: yaml` | `200`, same shape as json |
| 4 | Identical left/right | `200`, `"(no differences)"` |
| 5 | Invalid `format` value | `400` |
| 6 | Malformed JSON/YAML on either side | `400` |
| 7 | Deeply nested structural change | `200`, path correctly dotted/indexed |

## Hashing — `/api/util/hash`

| # | Scenario | Expect |
|---|---|---|
| 1 | Each of `MD5`, `SHA-1`, `SHA-256`, `SHA-512` | `200`, correct known hash for a fixed input |
| 2 | Omit `algorithm` | `200`, defaults to `SHA-256` |
| 3 | Invalid algorithm name | `400` |
| 4 | Empty `text` | `400` or `200` for the empty-string hash — pick one and keep it consistent |

## Base64 — `/api/util/base64/encode` / `/decode`

| # | Scenario | Expect |
|---|---|---|
| 1 | Encode → decode round-trip | Output equals original input |
| 2 | Decode invalid base64 | `400`, not `500` |
| 3 | Encode empty string | `200`, empty result |
| 4 | Unicode input | Round-trips correctly (UTF-8) |

## Zip / unzip — `/api/util/zip` / `/api/util/unzip`

| # | Scenario | Expect |
|---|---|---|
| 1 | Zip 2+ files | `200`, valid zip containing all files |
| 2 | Zip a single file | `200` |
| 3 | Zip with no files | `400` |
| 4 | Unzip a valid archive | `200`, `[{name,size,textPreview}]` per entry |
| 5 | Unzip a non-zip file | `400`, not `500` |
| 6 | Unzip an archive with a binary entry | `200`, `textPreview` shows `<binary content, N bytes>` |
| 7 | Unzip an archive with nested directories | `200`, directory entries skipped, file entries listed |

## Spreadsheet formula evaluation — `/api/sheet/evaluate`

| # | Scenario | Expect |
|---|---|---|
| 1 | `=A1+A2` with numeric cells | `200`, correct sum |
| 2 | `=SUM(A1:A3)` | `200` |
| 3 | `=A1&"-"&A2` (string concat) | `200`, string result |
| 4 | Reference to an unset cell | `200`, treated as 0/blank, not an error |
| 5 | `=A1/0` | `400` or a clear "evaluated to an error: #DIV/0!" message |
| 6 | Malformed formula syntax (e.g. `=A1+`) | `400` |
| 7 | Invalid cell reference key (e.g. `"cells": {"1A": "5"}`) | `400` |
| 8 | Missing `formula` | `400` |

## Audio transcription — `/api/audio/transcribe`

| # | Scenario | Expect |
|---|---|---|
| 1 | Clear short speech clip, default model | `200`, `{text}` roughly matches spoken content |
| 2 | `model=tiny` vs `model=large` | Both `200`; larger model takes longer but is more accurate |
| 3 | Missing `file` | `400` |
| 4 | `whisper` not installed | `400`, message names the exact fix (`pip install openai-whisper`) |
| 5 | Corrupted/non-audio file | `400`, not `500` |
| 6 | Silent audio clip | `200`, empty or near-empty transcript |
| 7 | Very long audio (multi-minute) | Either completes within the timeout or fails with a clear timeout message, not a hang |

## Markdown/HTML rendering — `/api/render/html`

| # | Scenario | Expect |
|---|---|---|
| 1 | Markdown → PDF | `200`, valid PDF bytes |
| 2 | Markdown → PNG | `200`, valid PNG bytes |
| 3 | Raw HTML → PDF | `200` |
| 4 | Missing `content` | `400` |
| 5 | Invalid `sourceType` | `400` |
| 6 | Invalid `format` | `400` |
| 7 | `wkhtmltopdf`/`wkhtmltoimage` not installed | `400`, message names the exact fix |
| 8 | Malformed Markdown/broken HTML tags | `200` (renderer should be tolerant) or a clear `400`, not `500` |

## Barcode / QR — `/api/barcode/generate` / `/decode`

| # | Scenario | Expect |
|---|---|---|
| 1 | Generate `QR_CODE` (default format) | `200`, valid PNG |
| 2 | Generate `CODE_128`, `EAN_13`, `UPC_A` | `200` each (note: `EAN_13`/`UPC_A` require exact digit-length input — this is a legitimate `400` if violated, not a bug) |
| 3 | Missing `text` | `400` |
| 4 | Invalid `format` value | `400`, lists example valid formats |
| 5 | Custom `width`/`height` | `200`, PNG dimensions match |
| 6 | Decode a freshly generated code | `200`, `{text,format}` round-trips exactly |
| 7 | Decode an image with no code in it | `400`, "No barcode/QR code found" |
| 8 | Decode a non-image file | `400`, not `500` |

## PDF manipulation — `/api/pdf/*`

| # | Scenario | Expect |
|---|---|---|
| 1 | `merge`: 2+ valid PDFs | `200`, merged PDF contains all source pages in order |
| 2 | `merge`: only 1 file | `400`, "at least two files" |
| 3 | `merge`: a non-PDF file included | `400`, not `500` |
| 4 | `split`: `pagesPerFile=1` on an N-page PDF | `200`, zip with N parts |
| 5 | `split`: `pagesPerFile=2` on an odd page count | `200`, last chunk has the remainder |
| 6 | `split`: `pagesPerFile=0` or negative | `400` |
| 7 | `rotate`: `degrees=90/180/270/360` | `200` each |
| 8 | `rotate`: `degrees=45` (not a multiple of 90) | `400` |
| 9 | `watermark`: valid text | `200`, watermark visible on every page |
| 10 | `watermark`: missing `text` | `400` |
| 11 | `fill-form`: PDF with an AcroForm, valid field names | `200`, filled PDF |
| 12 | `fill-form`: PDF with no AcroForm | `400`, "PDF has no fillable form fields" |
| 13 | `fill-form`: unknown field name | `400`, lists the unknown field(s) |
| 14 | `fill-form`: malformed `fields` JSON | `400` |

## Cross-cutting scenarios

| # | Scenario | Expect |
|---|---|---|
| 1 | Any JSON endpoint hit with the wrong HTTP method (e.g. `GET /api/data/convert`) | `405`, not `500` (regression test for the `ErrorResponse` bug fixed in [GlobalExceptionHandler](tokensaver-api/src/main/java/com/tokensaver/common/GlobalExceptionHandler.java)) |
| 2 | Any multipart endpoint hit as plain JSON | `400`, "Expected a multipart/form-data request" |
| 3 | Upload exceeding the 50MB multipart limit | `413` |
| 4 | `GET /api/mcp-tools` | `200`, `tools` array has all 6 MCP tools, `fileOperations.endpoints` has all 14 file-based endpoints |
| 5 | `POST /api/mcp-tools` | `405` |
| 6 | Every request logs one line via `com.tokensaver.http` (check `RequestLoggingFilter` output) | Log line present with method, path, status, duration |

## MCP tool tests (through an actual agent — see README § Testing)

Run each in a **fresh** Claude Code session with the `tokensaver` connector added:

1. `hash_text`, `base64`, `convert_data`, `diff_data`, `web_extract`, `evaluate_formula`
   — each should trigger a native tool call, not a script or curl.
2. File-operation fallback — ask it to extract text from / OCR / transcribe / render /
   barcode-decode / merge a real file path. It should go straight to the matching
   `curl` command (see `/api/mcp-tools`' `fileOperations` list) without probing for a
   health endpoint or attempting to read+base64 the file itself.

## Sample files

- Any tiny `.txt`/`.pdf`/`.png`/`.zip` works for the structural tests above.
- For OCR: a screenshot of printed text, or a scanned PDF.
- For audio transcription: any short `.mp3`/`.wav` with clear speech.
- For barcode decode: generate one via `/api/barcode/generate` first, then feed it back in.
- For PDF form-fill: any PDF with an actual AcroForm (most government/legal PDF
  templates have one; a plain PDF from `wkhtmltopdf`/Word export usually doesn't).
