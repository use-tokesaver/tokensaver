// Package source loads what the LLM asked for — a URL or a local file — and works
// out what kind of document it is.
package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// MaxBytes caps how much is read from a URL or file.
const MaxBytes = 50 << 20

// UserAgent is a mainstream browser UA with a tokensaver suffix: many sites
// serve stripped-down or blocked pages to unknown clients.
const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 tokensaver/0.1"

// Source is loaded content plus what is known about where it came from.
type Source struct {
	Input       string   // as given by the caller
	URL         *url.URL // final URL after redirects; nil for local files
	Path        string   // local file path; "" for URLs
	ContentType string   // media type from the HTTP response, lowercased, no params
	Charset     string   // charset parameter from the HTTP response, if any
	Data        []byte
}

// Name is the file name or URL path used for extension-based detection.
func (s *Source) Name() string {
	if s.URL != nil {
		return s.URL.Path
	}
	return s.Path
}

// StatusError is returned for non-2xx HTTP responses. Source holds the error body,
// which is often informative for APIs.
type StatusError struct {
	Code   int
	Status string
	Source *Source
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("HTTP %s from %s", e.Status, e.Source.URL)
}

var client = &http.Client{Timeout: 30 * time.Second}

var (
	bareHostRe   = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}(:\d+)?(/|$)`)
	localhostRe  = regexp.MustCompile(`^(localhost|127\.0\.0\.1|\[::1\])(:\d+)?(/|$)`)
	githubDiffRe = regexp.MustCompile(`^https://github\.com/[\w.-]+/[\w.-]+/(?:pull/\d+|commit/[0-9a-fA-F]{7,40})$`)
)

// diffURL appends ".diff" to a GitHub pull request or commit URL, so an agent can
// pass one straight in instead of knowing about GitHub's .diff endpoint.
func diffURL(input string) string {
	if githubDiffRe.MatchString(strings.TrimSuffix(input, "/")) {
		return strings.TrimSuffix(input, "/") + ".diff"
	}
	return input
}

// Load fetches a URL (GET) or reads a local file. accept is sent as the HTTP
// Accept header. A missing scheme is tolerated: "localhost:3000/api" means
// http://, and something that looks like a host ("example.com/docs") means
// https:// when no such local file exists.
func Load(ctx context.Context, input, accept string) (*Source, error) {
	input = diffURL(strings.TrimSpace(input))
	if input == "" {
		return nil, errors.New("source is empty")
	}
	if localhostRe.MatchString(input) {
		return fetch(ctx, "http://"+input, accept)
	}
	if u, err := url.Parse(input); err == nil {
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			return fetch(ctx, input, accept)
		case "file":
			return readFile(input, u.Path)
		}
	}
	path := expandHome(input)
	if _, err := os.Stat(path); err != nil && bareHostRe.MatchString(input) {
		s, ferr := fetch(ctx, "https://"+input, accept)
		var se *StatusError
		if ferr == nil || errors.As(ferr, &se) {
			return s, ferr
		}
		// Not reachable as a host either (e.g. "notes.md"): report the file.
		return nil, fmt.Errorf("no such file: %s", path)
	}
	return readFile(input, path)
}

// CacheKey identifies what input refers to right now. Local files include their
// modification time and size, so an edited file never matches a stale entry;
// remote is reported so callers can decide how long a URL may be trusted.
func CacheKey(input string) (key string, remote bool) {
	input = strings.TrimSpace(input)
	path := input
	if localhostRe.MatchString(input) {
		return "url:http://" + input, true
	}
	if u, err := url.Parse(input); err == nil {
		switch strings.ToLower(u.Scheme) {
		case "http", "https":
			return "url:" + input, true
		case "file":
			path = u.Path
		}
	}
	path = expandHome(path)
	st, err := os.Stat(path)
	if err != nil {
		return "url:" + input, true // not a readable file: Load will treat it as a bare host
	}
	abs, _ := filepath.Abs(path)
	return fmt.Sprintf("file:%s:%d:%d", abs, st.ModTime().UnixNano(), st.Size()), false
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func readFile(input, path string) (*Source, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no such file: %s", path)
		}
		return nil, err
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.IsDir() {
		return nil, fmt.Errorf("%s is a directory, not a file", path)
	}
	data, err := readCapped(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Source{Input: input, Path: path, Data: data}, nil
}

func fetch(ctx context.Context, rawURL, accept string) (*Source, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("bad URL %q: %w", rawURL, err)
	}
	req.Header.Set("User-Agent", UserAgent)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, unwrapURLError(err))
	}
	defer resp.Body.Close()
	data, err := readCapped(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	s := &Source{Input: rawURL, URL: resp.Request.URL, Data: data}
	if mt, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil {
		s.ContentType = strings.ToLower(mt)
		s.Charset = params["charset"]
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &StatusError{Code: resp.StatusCode, Status: resp.Status, Source: s}
	}
	return s, nil
}

func unwrapURLError(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func readCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("larger than the %d MB limit", MaxBytes>>20)
	}
	return data, nil
}
