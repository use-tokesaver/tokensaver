package main

import (
	"encoding/json"
	"fmt"

	"github.com/use-tokesaver/tokensaver/internal/testdoc"
)

// A task is one question about one source, asked in both arms. Answers are
// graded by string match, never by a model: every group in expect must have
// one of its alternatives in the answer.
type task struct {
	name   string
	kind   string // web, api, pdf, docx, xlsx
	prompt string // for local files, %s is the file's absolute path
	file   string // fixture in the work dir, for local-file tasks
	// allow are the permission rules the built-in arm may use for this task;
	// the tokensaver arm gets the same rules plus the tokensaver tools, so
	// the only difference between the arms is tokensaver being available.
	// Web tasks get no shell: they read third-party pages.
	allow  []string
	expect [][]string
	// expectFrom computes expect from the live source (API data changes).
	expectFrom func() ([][]string, error)
}

// Local-file tasks give the built-in arm a real shell, as a developer would by
// approving textutil, unzip or a python snippet. Per-command rules are not
// enough: they refuse the compound and multi-line commands Claude actually
// writes ("cd /tmp && textutil …"), which would handicap the baseline. The
// files are generated locally, so no third-party text reaches the shell.
var docShell = []string{"Read", "Bash"}

// The API task may fetch with curl as well as WebFetch, which is what a
// developer working against an API would allow.
var apiTools = []string{"WebFetch(domain:api.github.com)", "Bash(curl:*)", "Bash(jq:*)", "Bash(python3:*)"}

const searchURL = "https://api.github.com/search/repositories?q=language:go&sort=stars&order=desc&per_page=30"

var tasks = []task{
	{
		name: "wiki-facts", kind: "web",
		prompt: "Using the page https://en.wikipedia.org/wiki/Go_(programming_language), answer: who designed Go, and in which year was it publicly announced? Answer in one or two sentences.",
		allow:  []string{"WebFetch(domain:en.wikipedia.org)"},
		expect: [][]string{{"Griesemer"}, {"Pike"}, {"Thompson"}, {"2009"}},
	},
	{
		name: "wiki-three-sections", kind: "web",
		prompt: "Using https://en.wikipedia.org/wiki/Go_(programming_language), answer briefly: (1) which Go version added generics, (2) what Go's mascot is, (3) which company created Go.",
		allow:  []string{"WebFetch(domain:en.wikipedia.org)"},
		expect: [][]string{{"1.18"}, {"gopher"}, {"Google"}},
	},
	{
		name: "mdn-code", kind: "web",
		prompt: "From https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API/Using_Fetch, show the example code that sends a POST request with a JSON body and sets the Content-Type header, exactly as the page shows it.",
		allow:  []string{"WebFetch(domain:developer.mozilla.org)"},
		expect: [][]string{{`method: "POST"`}, {"JSON.stringify"}, {"application/json"}},
	},
	{
		name: "godoc-signature", kind: "web",
		prompt: "According to https://pkg.go.dev/encoding/json, what is the exact signature of json.Unmarshal, and which Go types does it store in an interface value for JSON numbers, arrays and objects?",
		allow:  []string{"WebFetch(domain:pkg.go.dev)"},
		expect: [][]string{{"func Unmarshal(data []byte, v any) error"}, {"float64"}, {"[]interface{}", "[]any"}, {"map[string]interface{}", "map[string]any"}},
	},
	{
		name: "github-readme", kind: "web",
		prompt: "Which Go import path does the README at https://github.com/modelcontextprotocol/go-sdk tell you to use for building MCP servers and clients?",
		allow:  []string{"WebFetch(domain:github.com)"},
		expect: [][]string{{"github.com/modelcontextprotocol/go-sdk/mcp"}},
	},
	{
		name: "api-top-repos", kind: "api",
		prompt: "Using the GitHub API endpoint " + searchURL + " list the 5 most-starred Go repositories (owner/name) with their star counts.",
		allow:  apiTools,
		expectFrom: func() ([][]string, error) {
			raw, err := fetch(searchURL)
			if err != nil {
				return nil, err
			}
			var r struct {
				Items []struct {
					FullName string `json:"full_name"`
				} `json:"items"`
			}
			if err := json.Unmarshal(raw, &r); err != nil || len(r.Items) < 5 {
				return nil, fmt.Errorf("unexpected search response: %v", err)
			}
			var want [][]string
			for _, it := range r.Items[:5] {
				want = append(want, []string{it.FullName})
			}
			return want, nil
		},
	},
	{
		name: "pdf-paper", kind: "pdf", file: "attention.pdf",
		prompt: "Read the PDF at %s. What BLEU score does the big Transformer model get on WMT 2014 English-to-German, and how many attention heads does the base model use?",
		allow:  docShell,
		expect: [][]string{{"28.4"}, {"8 heads", "eight heads", "h = 8", "h=8", "8 attention heads", "8 parallel attention", "eight parallel", "eight attention heads"}},
	},
	{
		name: "docx-report", kind: "docx", file: "annual-report.docx",
		prompt: "Read the Word document at %s. What was EMEA revenue in 2025, and who approved the 2026 budget, and on what date?",
		allow:  docShell,
		expect: [][]string{{"48.2"}, {testdoc.ReportApprover}, {testdoc.ReportApprovalDate, "March 14, 2025", "14 Mar 2025", "2025-03-14"}},
	},
	{
		name: "xlsx-lookup", kind: "xlsx", file: "inventory.xlsx",
		prompt: "Using the spreadsheet at %s, what are the product name, stock level, unit price and supplier of SKU-0217?",
		allow:  docShell,
		expect: func() [][]string {
			it := testdoc.InventoryItem(217)
			return [][]string{{it.Product}, {fmt.Sprint(it.Stock)}, {it.UnitPrice}, {it.Supplier}}
		}(),
	},
}
