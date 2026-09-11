package source

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		src  Source
		want Kind
	}{
		{"pdf magic beats header", Source{ContentType: "application/octet-stream", Data: []byte("%PDF-1.7\n...")}, PDF},
		{"html header", Source{ContentType: "text/html", Data: []byte("whatever")}, HTML},
		{"json header", Source{ContentType: "application/vnd.api+json", Data: []byte(`{}`)}, JSON},
		{"json by extension on text/plain", Source{ContentType: "text/plain", Path: "x/data.json", Data: []byte(`{"a":1}`)}, JSON},
		{"html sniffed", Source{Path: "page", Data: []byte("\n  <!DOCTYPE html><html><body>hi</body></html>")}, HTML},
		{"json sniffed", Source{Path: "response", Data: []byte(` [1, 2, 3] `)}, JSON},
		{"markdown is text", Source{Path: "README.md", Data: []byte("# Title\n\nbody")}, Text},
		{"csv is text", Source{Path: "a.csv", Data: []byte("a,b\n1,2\n")}, Text},
		{"binary is unknown", Source{Path: "a.bin", Data: []byte{0x00, 0x01, 0x02, 0xff}}, Unknown},
		{"zip that is not office", Source{Path: "a.zip", Data: []byte("PK\x03\x04junk")}, Unknown},
	}
	for _, c := range cases {
		if got := Detect(&c.src); got != c.want {
			t.Errorf("%s: Detect = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestUTF8ConvertsLegacyCharsets(t *testing.T) {
	s := &Source{ContentType: "text/html", Charset: "windows-1252", Data: []byte("caf\xe9 \x93quoted\x94")}
	if got := s.UTF8(); got != "café “quoted”" {
		t.Fatalf("UTF8 = %q", got)
	}
	bom := &Source{Data: []byte("\xef\xbb\xbfhello")}
	if got := bom.UTF8(); got != "hello" {
		t.Fatalf("BOM not stripped: %q", got)
	}
}

func TestLoadFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	os.WriteFile(p, []byte("hello"), 0o644)
	for _, in := range []string{p, "file://" + p} {
		s, err := Load(context.Background(), in, "")
		if err != nil || string(s.Data) != "hello" || s.URL != nil {
			t.Fatalf("Load(%q) = %v, %v", in, s, err)
		}
	}
	if _, err := Load(context.Background(), filepath.Join(dir, "missing.txt"), ""); err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing file error = %v", err)
	}
	k1, remote := CacheKey(p)
	if remote || !strings.HasPrefix(k1, "file:") {
		t.Fatalf("CacheKey(file) = %q, %v", k1, remote)
	}
	os.WriteFile(p, []byte("hello, changed"), 0o644)
	if k2, _ := CacheKey(p); k2 == k1 {
		t.Fatal("cache key did not change when the file changed")
	}
}

func TestLoadHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/old":
			http.Redirect(w, r, "/new", http.StatusMovedPermanently)
		case "/new":
			w.Header().Set("Content-Type", "text/html; charset=ISO-8859-1")
			w.Write([]byte("<p>ok</p>"))
		default:
			http.Error(w, "gone", http.StatusGone)
		}
	}))
	defer srv.Close()

	s, err := Load(context.Background(), srv.URL+"/old", "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if s.URL.Path != "/new" || s.ContentType != "text/html" || !strings.EqualFold(s.Charset, "iso-8859-1") {
		t.Fatalf("got URL %s, type %q, charset %q", s.URL, s.ContentType, s.Charset)
	}
	_, err = Load(context.Background(), srv.URL+"/x", "")
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusGone || !strings.Contains(string(se.Source.Data), "gone") {
		t.Fatalf("status error = %v", err)
	}
	if _, remote := CacheKey(srv.URL + "/x"); !remote {
		t.Fatal("URL should be remote")
	}
}

func TestBareHostLooksLikeURL(t *testing.T) {
	for in, want := range map[string]bool{
		"example.com":          true,
		"docs.acme.dev/guide":  true,
		"notes.md":             true, // matches the pattern; only tried when no such file exists
		"./docs/readme.txt":    false,
		"/abs/path/report.pdf": false,
	} {
		if got := bareHostRe.MatchString(in); got != want {
			t.Errorf("bareHostRe(%q) = %v, want %v", in, got, want)
		}
	}
	for in, want := range map[string]bool{"localhost:3000/api": true, "localhost": true, "127.0.0.1:8080": true, "localhost.txt": false} {
		if got := localhostRe.MatchString(in); got != want {
			t.Errorf("localhostRe(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLoadSchemelessLocalhost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	hostPort := strings.TrimPrefix(srv.URL, "http://127.0.0.1")
	s, err := Load(context.Background(), "localhost"+hostPort+"/x", "")
	if err != nil || string(s.Data) != "ok" {
		t.Fatalf("Load(localhost…) = %v, %v", s, err)
	}
}
