package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect starts the server and a client over in-memory transports: the same
// MCP round trip an agent makes, minus the process boundary.
func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := New("test", nil).Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s %v: %v", tool, args, err)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	if res.StructuredContent != nil {
		t.Errorf("%s returned structured content too; that doubles the tokens", tool)
	}
	return b.String(), res.IsError
}

func testSite(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var apiHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		var b strings.Builder
		b.WriteString("<html><head><title>Docs</title></head><body><nav>Menu</nav><article><h1>Docs</h1>")
		for i := 1; i <= 6; i++ {
			fmt.Fprintf(&b, "<h2>Chapter %d</h2>", i)
			for j := range 8 {
				fmt.Fprintf(&b, "<p>Chapter %d paragraph %d explains an important idea in enough words to matter for paging.</p>", i, j)
			}
		}
		b.WriteString("</article><footer>Footer links</footer></body></html>")
		fmt.Fprint(w, b.String())
	})
	mux.HandleFunc("/api/repos", func(w http.ResponseWriter, r *http.Request) {
		apiHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		var items []string
		for i := range 40 {
			items = append(items, fmt.Sprintf(`{"id":%d,"name":"repo-%d","node_id":"N%d","owner":{"login":"u%d","avatar_url":"https://a/%d"},"description":null}`, i, i, i, i, i))
		}
		fmt.Fprintf(w, `{"total_count":40,"items":[%s]}`, strings.Join(items, ","))
	})
	mux.HandleFunc("/api/missing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found","documentation_url":"https://docs.example/404"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &apiHits
}

func TestListTools(t *testing.T) {
	cs := connect(t)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s should be marked read-only", tool.Name)
		}
		if tool.OutputSchema != nil {
			t.Errorf("%s has an output schema; results should be plain text", tool.Name)
		}
	}
	if strings.Join(names, ",") != "read,read_json" {
		t.Fatalf("tools = %v", names)
	}
}

func TestReadWebPageOutlineSectionAndPages(t *testing.T) {
	srv, _ := testSite(t)
	cs := connect(t)
	url := srv.URL + "/docs"

	out, isErr := call(t, cs, "read", map[string]any{"source": url, "max_chars": 1500})
	if isErr || !strings.HasPrefix(out, "# Docs") || strings.Contains(out, "Menu") || strings.Contains(out, "Footer links") {
		t.Fatalf("page 1:\n%s", out)
	}
	if !strings.Contains(out, "[page 1 of ") || !strings.Contains(out, "next: page=2") || !strings.Contains(out, "outline=true") {
		t.Fatalf("missing paging footer:\n%s", out)
	}

	out, _ = call(t, cs, "read", map[string]any{"source": url, "outline": true})
	if !strings.Contains(out, "7 sections") || !strings.Contains(out, "  4. Chapter 3 (~") {
		t.Fatalf("outline:\n%s", out)
	}

	out, _ = call(t, cs, "read", map[string]any{"source": url, "section": "4"})
	if !strings.HasPrefix(out, "## Chapter 3") || strings.Contains(out, "Chapter 4") {
		t.Fatalf("section:\n%s", out)
	}
	out, _ = call(t, cs, "read", map[string]any{"source": url, "section": "chapter 6"})
	if !strings.HasPrefix(out, "## Chapter 6") {
		t.Fatalf("section by title:\n%s", out)
	}

	out, isErr = call(t, cs, "read", map[string]any{"source": url, "page": 99})
	if !isErr || !strings.Contains(out, "out of range") {
		t.Fatalf("expected out-of-range error, got %v %q", isErr, out)
	}
}

func TestReadJSONShrinks(t *testing.T) {
	srv, hits := testSite(t)
	cs := connect(t)
	url := srv.URL + "/api/repos"

	out, _ := call(t, cs, "read_json", map[string]any{"source": url, "outline": true})
	for _, want := range []string{"JSON structure (~", "total_count: number (40)", "items: [40] {", `login: string ("u0")`, "description: null"} {
		if !strings.Contains(out, want) {
			t.Errorf("outline missing %q:\n%s", want, out)
		}
	}

	out, _ = call(t, cs, "read_json", map[string]any{
		"source": url, "select": "items.{name, owner.login}", "max_items": 2, "table": true,
	})
	want := "| name | owner.login |\n|---|---|\n| repo-0 | u0 |\n| repo-1 | u1 |\n… 38 more items"
	if out != want {
		t.Fatalf("table:\n%s\nwant:\n%s", out, want)
	}

	out, _ = call(t, cs, "read_json", map[string]any{"source": url, "drop_keys": []string{"*_url", "node_id"}, "drop_lists_over": 10})
	if out != `{"total_count":40,"items":"[40 items omitted]"}` {
		t.Fatalf("drop: %s", out)
	}

	// A plain read of JSON also shrinks (null description gone).
	out, _ = call(t, cs, "read", map[string]any{"source": url, "max_chars": 800})
	if strings.Contains(out, "description") || !strings.Contains(out, `"login":"u0"`) {
		t.Fatalf("read on JSON:\n%s", out)
	}
	if hits.Load() != 4 {
		t.Errorf("API hit %d times; first pages must always fetch fresh", hits.Load())
	}
	// Later pages reuse the response instead of re-fetching.
	call(t, cs, "read_json", map[string]any{"source": url, "max_chars": 800, "page": 2})
	if hits.Load() != 4 {
		t.Errorf("page 2 re-fetched the API")
	}
}

func TestReadJSONErrors(t *testing.T) {
	srv, _ := testSite(t)
	cs := connect(t)
	out, isErr := call(t, cs, "read_json", map[string]any{"source": srv.URL + "/api/missing"})
	if !isErr || !strings.Contains(out, "HTTP 404") || !strings.Contains(out, `"message":"Not Found"`) {
		t.Fatalf("404: %v %s", isErr, out)
	}
	out, isErr = call(t, cs, "read_json", map[string]any{"source": srv.URL + "/api/repos", "select": "itemz"})
	if !isErr || !strings.Contains(out, "top-level keys: total_count, items") {
		t.Fatalf("bad select: %v %s", isErr, out)
	}
	out, isErr = call(t, cs, "read_json", map[string]any{"source": srv.URL + "/docs"})
	if !isErr || !strings.Contains(out, "looks like html") {
		t.Fatalf("html as json: %v %s", isErr, out)
	}
}

func TestReadLocalFiles(t *testing.T) {
	dir := t.TempDir()
	notes := filepath.Join(dir, "notes.md")
	os.WriteFile(notes, []byte("# Notes\n\nfirst version\n"), 0o644)
	cs := connect(t)

	out, _ := call(t, cs, "read", map[string]any{"source": notes})
	if out != "# Notes\n\nfirst version" {
		t.Fatalf("text file: %q", out)
	}
	// An edited file is never served from the cache.
	os.WriteFile(notes, []byte("# Notes\n\nsecond version, longer\n"), 0o644)
	out, _ = call(t, cs, "read", map[string]any{"source": notes})
	if !strings.Contains(out, "second version") {
		t.Fatalf("stale file content: %q", out)
	}

	data := filepath.Join(dir, "data.json")
	os.WriteFile(data, []byte(`[{"a":1,"b":null},{"a":2,"b":"x"}]`), 0o644)
	out, _ = call(t, cs, "read_json", map[string]any{"source": "file://" + data, "table": true})
	if out != "| a | b |\n|---|---|\n| 1 | |\n| 2 | x |" {
		t.Fatalf("json file table: %q", out)
	}

	out, isErr := call(t, cs, "read", map[string]any{"source": filepath.Join(dir, "nope.pdf")})
	if !isErr || !strings.Contains(out, "no such file") {
		t.Fatalf("missing file: %v %q", isErr, out)
	}
	out, isErr = call(t, cs, "read", map[string]any{"source": dir})
	if !isErr || !strings.Contains(out, "is a directory") {
		t.Fatalf("directory: %v %q", isErr, out)
	}
}
