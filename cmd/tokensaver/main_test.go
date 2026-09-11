package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStdioBinary builds the real binary and talks to it over stdin/stdout the
// way an agent does. It guards the one rule a stdio server must never break:
// nothing but protocol messages on stdout (a stray print from a library would
// corrupt the stream and fail this handshake).
func TestStdioBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	bin := filepath.Join(t.TempDir(), "tokensaver")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.html")
	os.WriteFile(notes, []byte("<html><head><title>Hi</title></head><body><p>Hello from a local file.</p></body></html>"), 0o644)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = append(os.Environ(), "TOKENSAVER_LOG=debug") // logging must go to stderr only
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	if got := cs.InitializeResult().ServerInfo.Name; got != "tokensaver" {
		t.Fatalf("server name = %q", got)
	}
	if !strings.Contains(cs.InitializeResult().Instructions, "read_json") {
		t.Fatal("instructions missing")
	}
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "read", Arguments: map[string]any{"source": notes}})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError || text != "# Hi\n\nHello from a local file." {
		t.Fatalf("read = %v %q", res.IsError, text)
	}
}
