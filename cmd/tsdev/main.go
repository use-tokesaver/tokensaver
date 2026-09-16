// Command tsdev is a development harness: it calls tokensaver's MCP tools
// in-process, exactly as an agent would, and prints the result with its size.
// It is not part of the installed product (users install cmd/tokensaver).
//
//	go run ./cmd/tsdev read https://go.dev/doc/effective_go outline=true
//	go run ./cmd/tsdev read_json https://api.github.com/repos/golang/go select=name,stargazers_count
//	go run ./cmd/tsdev read_json data.json drop_keys='*_url,node_id' max_items=3 table=true
//	go run ./cmd/tsdev tools            # tool schemas and what they cost per turn
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/use-tokesaver/tokensaver/internal/server"
)

func main() {
	if len(os.Args) < 2 || (os.Args[1] != "tools" && len(os.Args) < 3) {
		fmt.Fprintln(os.Stderr, "usage: tsdev <read|read_json> <source> [key=value ...]\n       tsdev tools")
		os.Exit(2)
	}
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	serverT, clientT := mcp.NewInMemoryTransports()
	srv := server.New("dev", logger)
	defer srv.Close(ctx) // flush telemetry, if an opted-in developer is running this
	if _, err := srv.Connect(ctx, serverT, nil); err != nil {
		fail(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "tsdev"}, nil).Connect(ctx, clientT, nil)
	if err != nil {
		fail(err)
	}
	defer cs.Close()

	if os.Args[1] == "tools" {
		res, err := cs.ListTools(ctx, nil)
		if err != nil {
			fail(err)
		}
		out, _ := json.MarshalIndent(res.Tools, "", "  ")
		fmt.Println(string(out))
		compact, _ := json.Marshal(res.Tools)
		fmt.Fprintf(os.Stderr, "\n--- tool definitions: %d chars, ~%d tokens (sent with every request)\n",
			utf8.RuneCount(compact), (utf8.RuneCount(compact)+3)/4)
		return
	}

	args := map[string]any{"source": os.Args[2]}
	for _, kv := range os.Args[3:] {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			fail(fmt.Errorf("argument %q is not key=value", kv))
		}
		args[k] = parseValue(k, v)
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: os.Args[1], Arguments: args})
	if err != nil {
		fail(err)
	}
	var text strings.Builder
	for _, c := range res.Content {
		if t, ok := c.(*mcp.TextContent); ok {
			text.WriteString(t.Text)
		}
	}
	fmt.Println(text.String())
	n := utf8.RuneCountInString(text.String())
	fmt.Fprintf(os.Stderr, "\n--- %d chars, ~%d tokens\n", n, (n+3)/4)
	if res.IsError {
		os.Exit(1)
	}
}

func parseValue(key, v string) any {
	switch key {
	case "drop_keys":
		return strings.Split(v, ",")
	case "section", "select", "source":
		return v
	}
	if b, err := strconv.ParseBool(v); err == nil && (v == "true" || v == "false") {
		return b
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return v
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tsdev:", err)
	os.Exit(1)
}
