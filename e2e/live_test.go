package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/use-tokesaver/tokensaver/internal/source"
)

// Live checks run against real websites, a real API and a real PDF. They need
// the network and the sites change over time, so they only run on request:
//
//	TOKENSAVER_LIVE=1 go test ./e2e/ -run Live -v
//
// Each case fetches the raw source (what an agent gets from curl or a plain
// fetch), reads it through tokensaver, checks that the facts a reader would
// look for survived, and records both sizes in the savings table.

type liveCase struct {
	name, source string
	tool         string         // "read" unless set
	args         map[string]any // extra tool arguments (section, select, …)
	facts        []string       // must appear in the output (all pages)
	factsFrom    func(raw []byte) []string
	maxRatio     float64 // output/raw; 0 for binary sources where the ratio means nothing
}

const (
	wikiGo    = "https://en.wikipedia.org/wiki/Go_(programming_language)"
	mdnFetch  = "https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch"
	ghRepo    = "https://github.com/modelcontextprotocol/go-sdk"
	goDocJSON = "https://pkg.go.dev/encoding/json"
	ghSearch  = "https://api.github.com/search/repositories?q=language:go&sort=stars&order=desc&per_page=50"
	arxivPDF  = "https://arxiv.org/pdf/1706.03762"
)

var liveCases = []liveCase{
	{name: "Wikipedia article", source: wikiGo, maxRatio: 0.20,
		facts: []string{"Robert Griesemer", "Rob Pike", "Ken Thompson", "2009", "goroutine"}},
	{name: "Wikipedia, outline", source: wikiGo, args: map[string]any{"outline": true}, maxRatio: 0.01,
		facts: []string{"History", "Design"}},
	{name: "Wikipedia, History section", source: wikiGo, args: map[string]any{"section": "History"}, maxRatio: 0.03,
		facts: []string{"2007"}},
	{name: "MDN guide", source: mdnFetch, maxRatio: 0.15,
		facts: []string{"fetch(", "```js", "Response", "POST"}},
	{name: "GitHub repo page", source: ghRepo, maxRatio: 0.10,
		facts: []string{"github.com/modelcontextprotocol/go-sdk"}},
	{name: "pkg.go.dev, one function", source: goDocJSON, args: map[string]any{"section": "func Unmarshal"}, maxRatio: 0.05,
		facts: []string{"func Unmarshal(data []byte, v any) error", "float64"}},
	{name: "GitHub API, select + table", source: ghSearch, tool: "read_json", maxRatio: 0.02,
		args:      map[string]any{"select": "items.{full_name, stargazers_count}", "table": true},
		factsFrom: topRepos},
	{name: "GitHub API, outline", source: ghSearch, tool: "read_json", args: map[string]any{"outline": true}, maxRatio: 0.03,
		facts: []string{"items: [50] {", "full_name: string"}},
	{name: "arXiv PDF (15 pages)", source: arxivPDF,
		facts: []string{"Attention Is All You Need", "Multi-Head Attention", "28.4"}},
}

func topRepos(raw []byte) []string {
	var r struct {
		Items []struct {
			FullName string `json:"full_name"`
		} `json:"items"`
	}
	if json.Unmarshal(raw, &r) != nil || len(r.Items) < 3 {
		return []string{"(could not parse the raw response)"}
	}
	return []string{"| " + r.Items[0].FullName + " |", "| " + r.Items[2].FullName + " |"}
}

func TestLive(t *testing.T) {
	if os.Getenv("TOKENSAVER_LIVE") == "" {
		t.Skip("set TOKENSAVER_LIVE=1 to run against real websites")
	}
	s := start(t)
	rawCache := map[string][]byte{}
	for _, c := range liveCases {
		t.Run(c.name, func(t *testing.T) {
			raw, ok := rawCache[c.source]
			if !ok {
				var err error
				if raw, err = fetchRaw(c.source); err != nil {
					t.Skipf("cannot fetch the raw source: %v", err)
				}
				rawCache[c.source] = raw
			}
			tool := c.tool
			if tool == "" {
				tool = "read"
			}
			args := map[string]any{"source": c.source}
			for k, v := range c.args {
				args[k] = v
			}
			start := time.Now()
			out, pages := readAll(t, s, tool, args)
			facts := c.facts
			if c.factsFrom != nil {
				facts = c.factsFrom(raw)
			}
			contains(t, out, facts...)
			t.Logf("%s: %d raw bytes → %d chars in %d page(s), %v", c.source, len(raw), len(out), pages, time.Since(start).Round(time.Millisecond))
			if c.maxRatio == 0 {
				ledger.Lock()
				ledger.rows = append(ledger.rows, ledgerRow{name: "live: " + c.name, raw: "PDF (binary)", outN: len(out) / 4})
				ledger.Unlock()
				return
			}
			kind := "HTML"
			if tool == "read_json" {
				kind = "JSON"
			}
			saves(t, "live: "+c.name, kind, string(raw), out, c.maxRatio)
		})
	}
}

var footerRe = regexp.MustCompile(`\[page 1 of (\d+)`)

// readAll reads every page of a tool's output and joins them.
func readAll(t *testing.T, s *session, tool string, args map[string]any) (string, int) {
	t.Helper()
	first := s.ok(tool, args)
	m := footerRe.FindStringSubmatch(first)
	if m == nil {
		return first, 1
	}
	n, _ := strconv.Atoi(m[1])
	parts := []string{first}
	for p := 2; p <= n; p++ {
		next := map[string]any{}
		for k, v := range args {
			next[k] = v
		}
		next["page"] = p
		parts = append(parts, s.ok(tool, next))
	}
	return strings.Join(parts, "\n"), n
}

// fetchRaw downloads a source the way a plain HTTP fetch would.
func fetchRaw(u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", source.UserAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 50<<20))
}
