package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/use-tokesaver/tokensaver/internal/source"
)

// TestDisabledByDefault confirms that with no configuration (the default),
// Record and Close never make an HTTP call — the collector below fails the
// test if it is ever hit.
func TestDisabledByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("collector was contacted while telemetry is disabled")
	}))
	defer srv.Close()

	r := New(Config{}, "test", nil)
	for range 5 {
		r.Record(Event{Tool: "read", Kind: "html", Success: true})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.Close(ctx)

	// Also verify the env parser treats an unset/empty env as disabled.
	t.Setenv("TOKENSAVER_TELEMETRY", "")
	t.Setenv("TOKENSAVER_TELEMETRY_ENDPOINT", "")
	cfg := ConfigFromEnv()
	if cfg.Enabled {
		t.Fatalf("ConfigFromEnv() with no env set: Enabled = true")
	}

	// Enabled but with no endpoint configured must still no-op silently.
	r2 := New(Config{Enabled: true}, "test", nil)
	r2.Record(Event{Tool: "read", Success: true})
	r2.Close(ctx)
}

// TestEnabledSendsEvents checks that once opted in with an endpoint, events
// reach the collector as a JSON batch containing only the expected
// non-identifying fields, and never a literal source URL/path.
func TestEnabledSendsEvents(t *testing.T) {
	const secretURL = "https://user:token@internal.example.com/very/secret/path"

	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	setUserConfigDir(t, t.TempDir())

	r := New(Config{Enabled: true, Endpoint: srv.URL}, "v1.2.3", nil)

	// Simulate the error path a real tool call takes: a StatusError whose
	// body/URL must never leak into telemetry, only its category.
	u, _ := url.Parse(secretURL)
	statusErr := &source.StatusError{Code: 404, Status: "404 Not Found", Source: &source.Source{URL: u, Data: []byte(secretURL)}}
	wrapped := fmt.Errorf("HTTP 404: %w", statusErr)

	r.Record(Event{Tool: "read", Kind: "html", Success: true, OutChars: 1234, DurationMS: 42})
	r.Record(Event{Tool: "read", Success: false, ErrorCategory: Categorize(wrapped)})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r.Close(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("collector received no requests")
	}
	var all []Event
	for _, b := range bodies {
		if strings.Contains(string(b), "internal.example.com") || strings.Contains(string(b), "secret") || strings.Contains(string(b), "token") {
			t.Fatalf("payload leaked the source URL: %s", b)
		}
		var batch []Event
		if err := json.Unmarshal(b, &batch); err != nil {
			t.Fatalf("bad JSON batch: %v", err)
		}
		all = append(all, batch...)
	}
	if len(all) != 2 {
		t.Fatalf("got %d events, want 2", len(all))
	}
	for _, e := range all {
		if e.Version != "v1.2.3" {
			t.Errorf("Version = %q", e.Version)
		}
		if e.InstallID == "" {
			t.Error("InstallID is empty")
		}
		if e.Timestamp.IsZero() {
			t.Error("Timestamp is zero")
		}
	}
	if all[1].ErrorCategory != CategoryNotFound {
		t.Errorf("ErrorCategory = %q, want %q", all[1].ErrorCategory, CategoryNotFound)
	}
}

// TestNonBlocking makes sure a slow/unresponsive collector never delays the
// caller of Record, and that Close still returns within its own deadline
// even while the background sender is stuck talking to that collector.
func TestNonBlocking(t *testing.T) {
	block := make(chan struct{})
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-block // hang until the test unblocks it
	}))
	defer func() {
		close(block)
		srv.Close()
	}()
	setUserConfigDir(t, t.TempDir())

	r := New(Config{Enabled: true, Endpoint: srv.URL}, "test", nil)

	start := time.Now()
	for range 400 { // well over the internal buffer size
		r.Record(Event{Tool: "read", Success: true})
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("Record() took %v for 400 calls against a hung collector; must be non-blocking", elapsed)
	}

	closeStart := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	r.Close(ctx)
	if elapsed := time.Since(closeStart); elapsed > time.Second {
		t.Fatalf("Close() took %v; must respect its context deadline", elapsed)
	}
}

// TestCategorizeNeverLeaksText exercises the closed set of error categories
// and confirms the category is chosen by error type, never by embedding or
// echoing the original message (which can carry response bodies or file
// content, e.g. the errors excerpt() in internal/server builds).
func TestCategorizeNeverLeaksText(t *testing.T) {
	secretBody := "super secret response body that must never leave the machine"
	u, _ := url.Parse("https://example.com/api")

	tests := []struct {
		name string
		err  error
		want ErrorCategory
	}{
		{
			name: "404 status error, excerpt-wrapped",
			err: fmt.Errorf("HTTP 404 Not Found: %w: %s",
				&source.StatusError{Code: 404, Status: "404 Not Found", Source: &source.Source{URL: u}},
				secretBody),
			want: CategoryNotFound,
		},
		{
			name: "non-404 status error",
			err: fmt.Errorf("HTTP 500: %w: %s",
				&source.StatusError{Code: 500, Status: "500 Internal Server Error", Source: &source.Source{URL: u}},
				secretBody),
			want: CategoryNetworkError,
		},
		{
			name: "context deadline",
			err:  fmt.Errorf("fetch %s: %w", secretBody, context.DeadlineExceeded),
			want: CategoryTimeout,
		},
		{
			name: "invalid JSON",
			err:  fmt.Errorf("invalid JSON: %w", mustSyntaxErr(secretBody)),
			want: CategoryInvalidInput,
		},
		{
			name: "unrecognized error",
			err:  errors.New(secretBody),
			want: CategoryOther,
		},
		{
			name: "nil",
			err:  nil,
			want: CategoryNone,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Categorize(tt.err)
			if got != tt.want {
				t.Errorf("Categorize() = %q, want %q", got, tt.want)
			}
			if strings.Contains(string(got), secretBody) {
				t.Fatalf("category leaked message text: %q", got)
			}
			// The category itself must always be one of the closed set.
			data, err := json.Marshal(Event{ErrorCategory: got})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), secretBody) {
				t.Fatalf("serialized event leaked message text: %s", data)
			}
		})
	}
}

func mustSyntaxErr(prefix string) error {
	var v any
	err := json.Unmarshal([]byte(prefix+": not json {{{"), &v)
	if err == nil {
		panic("expected a JSON syntax error")
	}
	return err
}

// TestInstallIDPersists checks the id is written under a config dir and
// reused across a second Reporter pointed at the same directory.
func TestInstallIDPersists(t *testing.T) {
	dir := t.TempDir()
	setUserConfigDir(t, dir)

	id1 := loadOrCreateInstallID(nil)
	id2 := loadOrCreateInstallID(nil)
	if id1 == "" {
		t.Fatal("empty install id")
	}
	if id1 != id2 {
		t.Fatalf("install id changed across calls: %q vs %q", id1, id2)
	}
}

// TestInstallIDFallsBackInMemory checks that a config dir that can't be
// created (e.g. a file sitting where the directory should be) still yields a
// usable id rather than an error.
func TestInstallIDFallsBackInMemory(t *testing.T) {
	dir := t.TempDir()
	configDir := setUserConfigDir(t, dir)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Put a plain file where the "tokensaver" directory should go, so
	// os.MkdirAll fails and loadOrCreateInstallID must fall back.
	blocked := filepath.Join(configDir, "tokensaver")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	id := loadOrCreateInstallID(nil)
	if id == "" {
		t.Fatal("expected a fallback in-memory id, got empty string")
	}
}

// setUserConfigDir points os.UserConfigDir() at dir for the duration of the
// test (HOME covers darwin, XDG_CONFIG_HOME covers linux, APPDATA covers
// windows — each OS reads only the one it cares about) and returns the
// resolved config dir.
func setUserConfigDir(t *testing.T, dir string) string {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	got, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestNoStdoutWrites is a grep-level guard: nothing in this package's
// non-test source should write to stdout, since stdout carries the MCP
// protocol in the real binary.
func TestNoStdoutWrites(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"os.Stdout", "fmt.Print(", "fmt.Println(", "fmt.Printf("}
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, pat := range forbidden {
			if strings.Contains(string(data), pat) {
				t.Errorf("%s contains %q; stdout must never be written to", f, pat)
			}
		}
	}
}
