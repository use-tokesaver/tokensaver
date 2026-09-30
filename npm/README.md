# tokensaver-mcp

Runs [tokensaver](https://github.com/use-tokesaver/tokensaver) — a local MCP
server that turns web pages, PDFs, Office files and JSON into compact,
LLM-ready text — without installing Go or Homebrew.

```bash
npx tokensaver-mcp
```

The first run downloads the matching prebuilt binary from the
[GitHub release](https://github.com/use-tokesaver/tokensaver/releases) for your
platform and caches it under `~/.cache/tokensaver-npm/`; later runs use the
cache directly. This package is a thin launcher — the binary itself has no
runtime dependencies (pure Go, PDF extraction via WebAssembly).

Point your MCP client at it directly:

```json
{
  "mcpServers": {
    "tokensaver": { "command": "npx", "args": ["-y", "tokensaver-mcp"] }
  }
}
```

Or, for Claude Code:

```bash
claude mcp add --scope user tokensaver -- npx -y tokensaver-mcp
```

See the [main repo](https://github.com/use-tokesaver/tokensaver) for what it
does, supported formats, and configuration.
