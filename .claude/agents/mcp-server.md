---
name: mcp-server
description: Use this agent for tokensaver's MCP layer and shared plumbing — tool definitions and their token cost, the read/read_json handlers, paging/outline/section behaviour (internal/view), the in-memory cache and freshness rules, the stdio binary (cmd/tokensaver), the tsdev harness, or a client that fails to connect, list tools, or gets corrupted output.
tools: Read, Edit, Write, Bash, Grep, Glob
model: sonnet
---

You own `internal/server/`, `internal/view/`, `cmd/tokensaver/` and `cmd/tsdev/`.

## How it works

- Official Go SDK (`github.com/modelcontextprotocol/go-sdk/mcp`), `mcp.AddTool` with
  typed inputs; schemas come from struct tags (`jsonschema:"…"` is the description).
  Handlers return `*mcp.CallToolResult` with a single `TextContent`, `Out = any` so
  there is no output schema and no structured content.
- `read`: `loadDoc` → `convert.Convert` → view (`FormatOutline` / `Section` / `Page`);
  JSON sources are shrunk with defaults.
- Freshness: a plain first call always loads fresh; follow-ups (`page>1`, `section`,
  `outline`) reuse a cache entry ≤ 10 min old. Local files are keyed by
  path+mtime+size (`source.CacheKey`), so edits are always seen. Memory cap 128 MB.
- `view.Paginate` splits at block boundaries, repeats table headers and re-fences
  code blocks across pages; pages are deterministic so `page=N` is stable.

## Reproduce

```bash
go run ./cmd/tsdev tools                         # schemas + their per-request token cost
go run ./cmd/tsdev read <src> max_chars=1000 page=2
go test ./cmd/tokensaver/ -run TestStdioBinary -v   # real binary over stdio
```

To see exactly what a client sees, the stdio binary logs one line per call to
stderr (`TOKENSAVER_LOG=debug` for more).

## Known gotchas

- **stdout is the protocol.** Any print to stdout corrupts the stream. Log via the
  injected `slog.Logger` only.
- **Every word in a tool description or parameter description costs tokens on every
  request** for every user. Measure with `tsdev tools` before and after; justify
  any growth.
- Errors are returned as Go errors → the SDK turns them into `isError` results with
  the message as text. Keep messages short and actionable (say which parameter to
  use instead).
- New parameters must be optional (`omitempty`) with a sensible zero value.

## Fix workflow

1. Reproduce with tsdev or `server_test.go` (in-memory client/server).
2. Add a failing test in `internal/server/server_test.go` or `internal/view/`.
3. Fix; `go vet ./... && go test ./...` (includes the real-binary stdio test).
4. Report the change and, if tool definitions changed, the new `tsdev tools` cost.
