// Package e2e runs the real tokensaver binary over stdio, exactly as an agent
// does, and checks two things for every source: the content that matters
// survives, and the output is much smaller than the raw source.
//
//	go test ./e2e/ -v                          # offline suite + savings table
//	TOKENSAVER_LIVE=1 go test ./e2e/ -run Live -v   # real websites, APIs and a PDF
package e2e

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/use-tokesaver/tokensaver/internal/view"
)

var binPath string

func TestMain(m *testing.M) {
	flag.Parse()
	dir, err := os.MkdirTemp("", "tokensaver-e2e-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "tokensaver")
	if !testing.Short() {
		out, err := exec.Command("go", "build", "-o", binPath, "github.com/use-tokesaver/tokensaver/cmd/tokensaver").CombinedOutput()
		if err != nil {
			fmt.Fprintf(os.Stderr, "build tokensaver: %v\n%s", err, out)
			os.Exit(1)
		}
	}
	code := m.Run()
	if testing.Verbose() {
		printLedger()
	}
	os.RemoveAll(dir)
	os.Exit(code)
}

// session is one running tokensaver process with an MCP client attached. Its
// working directory and HOME are a fresh temp dir, so relative and ~ paths
// resolve there.
type session struct {
	t      *testing.T
	cs     *mcp.ClientSession
	dir    string
	stderr *syncBuffer
}

func start(t *testing.T, env ...string) *session {
	t.Helper()
	if testing.Short() {
		t.Skip("e2e: runs the real binary")
	}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, binPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+dir, "TOKENSAVER_LOG=debug")
	cmd.Env = append(cmd.Env, env...)
	stderr := &syncBuffer{}
	cmd.Stderr = stderr
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "e2e"}, nil).Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect: %v\nserver stderr:\n%s", err, stderr)
	}
	t.Cleanup(func() {
		cs.Close()
		cancel()
		if strings.Contains(stderr.String(), "panic:") {
			t.Errorf("server panicked:\n%s", stderr)
		}
	})
	return &session{t: t, cs: cs, dir: dir, stderr: stderr}
}

// call runs a tool and returns its text and whether it was a tool error.
func (s *session) call(tool string, args map[string]any) (string, bool) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := s.cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		s.t.Fatalf("%s %v: %v\nserver stderr:\n%s", tool, args, err, s.stderr)
	}
	if res.StructuredContent != nil {
		s.t.Errorf("%s returned structured content; that doubles the tokens", tool)
	}
	var b strings.Builder
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			s.t.Errorf("%s returned %T; results must be plain text", tool, c)
			continue
		}
		b.WriteString(tc.Text)
	}
	return b.String(), res.IsError
}

// ok calls a tool that must succeed.
func (s *session) ok(tool string, args map[string]any) string {
	s.t.Helper()
	out, isErr := s.call(tool, args)
	if isErr {
		s.t.Fatalf("%s %v failed: %s", tool, args, out)
	}
	return out
}

// fails calls a tool that must fail with a message containing want.
func (s *session) fails(tool string, args map[string]any, want string) {
	s.t.Helper()
	out, isErr := s.call(tool, args)
	if !isErr || !strings.Contains(out, want) {
		s.t.Errorf("%s %v: want error containing %q, got isError=%v:\n%s", tool, args, want, isErr, clip(out))
	}
}

// file writes data into the session's directory and returns its absolute path.
func (s *session) file(name string, data []byte) string {
	s.t.Helper()
	p := filepath.Join(s.dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		s.t.Fatal(err)
	}
	return p
}

func contains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, clip(got))
		}
	}
}

func lacks(t *testing.T, got string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if i := strings.Index(got, n); i >= 0 {
			t.Errorf("unexpected %q in output, near: %q", n, got[max(0, i-80):min(len(got), i+80)])
		}
	}
}

func clip(s string) string {
	if len(s) > 3000 {
		return s[:3000] + fmt.Sprintf("\n… (%d more chars)", len(s)-3000)
	}
	return s
}

// The ledger collects raw vs. output sizes for the savings table printed at the
// end of a verbose run.
var ledger struct {
	sync.Mutex
	rows []ledgerRow
}

type ledgerRow struct {
	name, raw  string
	rawN, outN int
}

// saves records a raw → output measurement and fails if the output is more
// than maxRatio of the raw size. rawKind says what "raw" is (the bytes an agent
// would otherwise put in its context).
func saves(t *testing.T, name, rawKind string, raw, out string, maxRatio float64) {
	t.Helper()
	ledger.Lock()
	ledger.rows = append(ledger.rows, ledgerRow{name, rawKind, view.EstTokens(raw), view.EstTokens(out)})
	ledger.Unlock()
	if ratio := float64(len(out)) / float64(len(raw)); ratio > maxRatio {
		t.Errorf("%s: output is %.1f%% of raw (%d → %d chars), want ≤ %.1f%%", name, 100*ratio, len(raw), len(out), 100*maxRatio)
	}
}

func printLedger() {
	ledger.Lock()
	defer ledger.Unlock()
	if len(ledger.rows) == 0 {
		return
	}
	sort.SliceStable(ledger.rows, func(i, j int) bool { return ledger.rows[i].name < ledger.rows[j].name })
	fmt.Println("\nToken savings (estimated as chars/4; offline rows use generated pages, live: rows real ones):")
	fmt.Println("| Case | Raw is | Raw tokens | Output tokens | Saved |")
	fmt.Println("|---|---|--:|--:|--:|")
	for _, r := range ledger.rows {
		if r.rawN == 0 {
			fmt.Printf("| %s | %s | — | %d | — |\n", r.name, r.raw, r.outN)
			continue
		}
		fmt.Printf("| %s | %s | %d | %d | %.1f%% |\n", r.name, r.raw, r.rawN, r.outN, 100*(1-float64(r.outN)/float64(r.rawN)))
	}
}

// syncBuffer is a bytes.Buffer safe for the process's stderr writer and the
// test reading it concurrently.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
