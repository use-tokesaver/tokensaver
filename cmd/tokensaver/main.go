// Command tokensaver is a local MCP server (stdio) that turns web pages, PDFs,
// Office files and JSON into compact, LLM-ready text.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/use-tokesaver/tokensaver/internal/server"
)

// version is set at build time with -ldflags "-X main.version=v1.2.3"; `go
// install ...@v1.2.3` builds report the module version instead.
var version = "dev"

const usage = `tokensaver — a local MCP server that turns web pages, PDFs, Office files
and JSON into compact, LLM-ready text. It speaks MCP over stdio: your agent
starts it, you don't run it by hand.

Add it to Claude Code:
  claude mcp add tokensaver -- tokensaver

Other clients (Cursor, Claude Desktop, …): add an MCP server with command
"tokensaver" and no arguments.

Environment:
  TOKENSAVER_MAX_CHARS  default page size in characters (default 20000)
  TOKENSAVER_CHROME     path to Chrome/Chromium for JavaScript-heavy pages
  TOKENSAVER_LOG        debug | info | warn (default info), logged to stderr

Flags:
`

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println(buildVersion())
		return
	}
	if st, err := os.Stdin.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintln(os.Stderr, "tokensaver is an MCP server and is waiting for an MCP client on stdin (see tokensaver -h).")
	}

	// stdout carries the MCP protocol, so all logging goes to stderr.
	level := slog.LevelInfo
	_ = level.UnmarshalText([]byte(os.Getenv("TOKENSAVER_LOG")))
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.New(buildVersion(), logger).Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		logger.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

func buildVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return version
}
