package e2e

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/use-tokesaver/tokensaver/internal/browser"
	"github.com/use-tokesaver/tokensaver/internal/testdoc"
)

func TestToolsAreCheap(t *testing.T) {
	s := start(t)
	res, err := s.cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tool.Name)
		}
		if tool.OutputSchema != nil {
			t.Errorf("%s has an output schema; results are plain text", tool.Name)
		}
	}
	if strings.Join(names, ",") != "read,read_json" {
		t.Fatalf("tools = %v", names)
	}
	// Tool definitions and instructions are sent with every request of every
	// conversation that loads tokensaver; keep them from creeping up.
	defs, _ := json.Marshal(res.Tools)
	total := len(defs) + len(s.cs.InitializeResult().Instructions)
	t.Logf("tool definitions + instructions: %d chars (~%d tokens)", total, total/4)
	if total > 3400 {
		t.Errorf("tool definitions + instructions are %d chars (~%d tokens per request); budget is 3400", total, total/4)
	}
}

func TestDocsPage(t *testing.T) {
	site := newSite(t)
	s := start(t)
	url := site.URL + "/docs/install"
	out := s.ok("read", map[string]any{"source": url})
	contains(t, out,
		"# Installing Acme CLI\n\nAcme CLI ships your code",
		"## Requirements\n",
		"[configuration reference](/docs/config?lang=en)", // same-site link made root-relative, tracking removed
		"| Platform | Architectures | Packages |",
		"| macOS 13+ | x86_64, arm64 | `brew`, `.pkg` |",
		"## Install on macOS\n",
		"```bash\nbrew install acme/tap/acme\nacme --version\n```",
		"**Note:** Apple silicon Macs need Rosetta",
		"### Configuration file",
		"```yaml\nregion: eu-west-1\ntelemetry: false\n```",
		"1. Open a new terminal.",
		"Run `acme doctor`.",
		"[Troubleshooting](/docs/troubleshooting#install)",
		"[GitHub Discussions](https://github.com/acme/cli/discussions)",
		"[GitHub releases](https://github.com/acme/cli/releases)",
	)
	lacks(t, out,
		"cookies", "Accept all", "Search docs", "Pricing", "Group topic", "Getting started",
		"Was this page helpful", "On this page", "Newsletter", "Join our newsletter", "Tracking fallback",
		"Footer link", "© 2026", "utm_", "__next", "function", "display:flex", "og:tag", "terminal.png", "[page ")
	raw := docsPage(strings.TrimPrefix(site.URL, "http://"))
	saves(t, "docs page (framework site)", "HTML", raw, out, 0.05)

	// A redirect lands on the same page.
	if again := s.ok("read", map[string]any{"source": site.URL + "/old-install"}); again != out {
		t.Errorf("redirected read differs from direct read")
	}
	// Schemeless localhost URLs work too (agents often drop the scheme).
	local := s.ok("read", map[string]any{"source": strings.TrimPrefix(url, "http://")})
	if local != out {
		t.Errorf("schemeless localhost read differs from direct read")
	}
}

var markerRe = regexp.MustCompile(`marker-\d+-\d+`)

func TestLongArticleOutlineSectionsAndPaging(t *testing.T) {
	site := newSite(t)
	s := start(t)
	url := site.URL + "/wiki/Gopher_language"

	var want []string
	for sec, spec := range wikiSections {
		for p := range spec.paras {
			want = append(want, fmt.Sprintf("marker-%d-%d", sec, p))
		}
	}

	// The whole article, page by page: every paragraph exactly once, in order,
	// every page within the budget, and a footer that says how to continue.
	const pageSize = 3000
	var all strings.Builder
	var pages int
	for page := 1; ; page++ {
		out := s.ok("read", map[string]any{"source": url, "max_chars": pageSize, "page": page})
		body, footer, ok := strings.Cut(out, "\n\n[page ")
		if !ok {
			t.Fatalf("page %d has no footer:\n%s", page, clip(out))
		}
		if len(body) > pageSize {
			t.Errorf("page %d body is %d chars, budget %d", page, len(body), pageSize)
		}
		all.WriteString(body + "\n")
		pages = page
		if !strings.Contains(footer, "next: page=") {
			if !strings.HasPrefix(footer, fmt.Sprintf("%d of %d", page, page)) {
				t.Errorf("last footer = %q", footer)
			}
			break
		}
		if !strings.Contains(footer, "outline=true lists sections") {
			t.Errorf("footer should point at the outline: %q", footer)
		}
		if page > 50 {
			t.Fatal("paging does not terminate")
		}
	}
	got := markerRe.FindAllString(all.String(), -1)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("paragraphs across %d pages are lost, duplicated or out of order:\n got %v\nwant %v", pages, got, want)
	}
	text := all.String()
	contains(t, text, "# Gopher language", "## History", "### Concurrency", "## Etymology",
		"**Gopher** is a statically typed programming language. It is known for fast compilation.")
	lacks(t, text, "Jump to content", "Portal 3", "[edit]", "[1]", "[citation needed]", "RLCONF", "last edited", "Creative Commons")

	// The outline is tiny and gives ids to ask for.
	outline := s.ok("read", map[string]any{"source": url, "outline": true})
	contains(t, outline, "sections, ~", "Read one with section=<id>.", "History", "Origins", "Concurrency (~", "Etymology")
	id := regexp.MustCompile(`(?m)^\s*(\d+)\. Concurrency \(`).FindStringSubmatch(outline)
	if id == nil {
		t.Fatalf("no id for Concurrency in outline:\n%s", outline)
	}
	sec := s.ok("read", map[string]any{"source": url, "section": id[1]})
	if !strings.HasPrefix(sec, "### Concurrency") {
		t.Errorf("section %s:\n%s", id[1], clip(sec))
	}
	if got := markerRe.FindAllString(sec, -1); len(got) != wikiSections[6].paras || got[0] != "marker-6-0" {
		t.Errorf("Concurrency section has markers %v", got)
	}
	// By heading text, case-insensitively; a level-2 section includes its subsections.
	hist := s.ok("read", map[string]any{"source": url, "section": "history"})
	if n := len(markerRe.FindAllString(hist, -1)); n != 3+5+6 {
		t.Errorf("History with subsections has %d paragraphs, want 14", n)
	}
	s.fails("read", map[string]any{"source": url, "section": "Pricing"}, "outline=true")
	s.fails("read", map[string]any{"source": url, "page": 99}, "out of range")

	// This article is mostly text, so the whole of it isn't much smaller than
	// its HTML; reading only what's needed is where the savings are.
	raw := wikiPage(strings.TrimPrefix(site.URL, "http://"))
	saves(t, "wiki article, outline", "HTML", raw, outline, 0.03)
	saves(t, "wiki article, one section", "HTML", raw, sec, 0.10)
}

func TestJSONAPI(t *testing.T) {
	site := newSite(t)
	s := start(t)
	url := site.URL + "/api/search/repositories"
	raw := string(searchJSON())

	outline := s.ok("read_json", map[string]any{"source": url, "outline": true})
	contains(t, outline, "JSON structure (~", "total_count: number (12345)", "items: [100] {", `full_name: string ("org0/project-000")`,
		"stargazers_count: number", "license: ", "|null", "topics: [1-")
	saves(t, "JSON API, outline", "JSON", raw, outline, 0.05)

	table := s.ok("read_json", map[string]any{"source": url, "select": "items.{full_name, stargazers_count, language}", "max_items": 5, "table": true})
	var want strings.Builder
	want.WriteString("| full_name | stargazers_count | language |\n|---|---|---|\n")
	for i := range 5 {
		r := searchRepo(i)
		fmt.Fprintf(&want, "| %s | %d | %s |\n", r.FullName, r.StargazersCount, r.Language)
	}
	want.WriteString("… 95 more items")
	if table != want.String() {
		t.Errorf("select table:\n%s\nwant:\n%s", table, want.String())
	}
	saves(t, "JSON API, select + table", "JSON", raw, table, 0.01)

	// With no options: nulls and empty strings go, key order and numbers stay.
	def := s.ok("read_json", map[string]any{"source": url})
	contains(t, def, "{\n \"total_count\":12345,\n \"incomplete_results\":false,\n \"items\":[", `"full_name":"org0/project-000"`, "[page 1 of ")
	lacks(t, def, "null", `""`)
	saves(t, "JSON API, defaults (page 1)", "JSON", raw, def, 0.25)

	dropped := s.ok("read_json", map[string]any{"source": url, "drop_keys": []string{"*_url", "node_id", "owner", "license"}, "max_items": 3})
	lacks(t, dropped, "_url", "node_id", "avatars.example.com", "MIT License")
	contains(t, dropped, `"full_name":"org2/project-002"`, "… 97 more items")

	if out := s.ok("read_json", map[string]any{"source": url, "drop_lists_over": 5}); out != `{"total_count":12345,"incomplete_results":false,"items":"[100 items omitted]"}` {
		t.Errorf("drop_lists_over: %s", out)
	}
	if out := s.ok("read_json", map[string]any{"source": url, "select": "items[-1].owner.login"}); out != `"`+searchRepo(99).Owner.Login+`"` {
		t.Errorf("select last owner: %s", out)
	}
	if out := s.ok("read_json", map[string]any{"source": site.URL + "/api/events", "table": true}); out != "| id | type | actor.login |\n|---|---|---|\n| 1 | deploy | dev1 |\n| 2 | deploy | dev2 |\n| 3 | deploy | dev3 |" {
		t.Errorf("NDJSON table:\n%s", out)
	}
	// read on JSON shrinks it too.
	contains(t, s.ok("read", map[string]any{"source": url, "outline": true}), "items: [100] {")

	s.fails("read_json", map[string]any{"source": site.URL + "/api/missing"}, `HTTP 404`)
	s.fails("read_json", map[string]any{"source": site.URL + "/api/missing"}, `"message":"Not Found"`)
	s.fails("read_json", map[string]any{"source": url, "select": "itemz"}, "top-level keys: total_count, incomplete_results, items")
	s.fails("read_json", map[string]any{"source": site.URL + "/docs/install"}, "use the read tool")
	s.fails("read", map[string]any{"source": url, "section": "items"}, "read_json with select=")
}

func TestPDF(t *testing.T) {
	site := newSite(t)
	s := start(t)
	path := s.file("board-pack.pdf", boardPackPDF())
	for _, src := range []string{path, site.URL + "/files/board-pack.pdf"} {
		out := s.ok("read", map[string]any{"source": src})
		contains(t, out, "Title: Board Pack Q3 2026", "## Page 1\n", "## Page 12\n",
			"Agenda item 1: Minutes", "Agenda item 12: Any other business", "Decision log entry 7-B was recorded.")
		lacks(t, out, "Confidential", "Northwind Board Pack Q3 2026", "Page 3 of 12")
	}
	outline := s.ok("read", map[string]any{"source": path, "outline": true})
	contains(t, outline, "12 sections", "Page 7")
	contains(t, s.ok("read", map[string]any{"source": path, "section": "Page 7"}), "Agenda item 7: Legal")
}

func TestOfficeFiles(t *testing.T) {
	s := start(t)

	report := testdoc.ReportDOCX()
	path := s.file("annual-report.docx", report)
	out := s.ok("read", map[string]any{"source": path}) + s.ok("read", map[string]any{"source": path, "page": 2})
	contains(t, out,
		"# "+testdoc.ReportTitle,
		"# Executive summary", "- Revenue grew in every region", "  - The Rotterdam and Lyon sites",
		"| Region | 2024 | 2025 | Change |\n|---|---|---|---|\n| North America | 61.7 | 66.9 | +8.4% |",
		"## EMEA\n", "### EMEA sales", "EMEA revenue was "+testdoc.ReportEMEARevenue,
		"1. Currency swings",
		"approved by **"+testdoc.ReportApprover+"**, Chief Financial Officer, on "+testdoc.ReportApprovalDate,
		"[investor site](https://investors.northwind.example/2025)",
		"| Month | North America | EMEA | Latin America | Asia Pacific |",
		"[page 2 of 2 ")
	saves(t, "DOCX report", "unzipped XML", unzippedXML(t, report), out, 0.35)
	budget := s.ok("read", map[string]any{"source": path, "section": "budget 2026"})
	contains(t, budget, testdoc.ReportApprover)
	if len(budget) > 1500 {
		t.Errorf("one section should be small, got %d chars", len(budget))
	}

	inv := testdoc.InventoryXLSX(400)
	xlsx := s.file("inventory.xlsx", inv)
	page1 := s.ok("read", map[string]any{"source": xlsx})
	header := "| SKU | Product | Category | Warehouse | Stock | Unit price (USD) | Supplier |\n|---|---|---|---|---|---|---|"
	it := testdoc.InventoryItem(217)
	contains(t, page1, "## Sheet: Inventory (401 rows × 7 cols)", header,
		fmt.Sprintf("| %s | %s | %s | %s | %d | %s | %s |", it.SKU, it.Product, it.Category, it.Warehouse, it.Stock, it.UnitPrice, it.Supplier),
		"[page 1 of 2 · next: page=2")
	page2 := s.ok("read", map[string]any{"source": xlsx, "page": 2})
	if !strings.HasPrefix(page2, header) {
		t.Errorf("page 2 should repeat the table header:\n%s", clip(page2))
	}
	contains(t, page2, "| SKU-0400 |", "## Sheet: Warehouses", "| São Paulo | Brazil |", "## Sheet: Notes (hidden)")
	saves(t, "XLSX 400 rows", "unzipped XML", unzippedXML(t, inv), page1+page2, 0.60)

	deck := s.file("deck.pptx", testdoc.SamplePPTX())
	want := "## Slide 1: Quarterly Review\n\n- Revenue up\n  - EU strongest\n- Costs flat\n\nNotes: Mention the EU deal.\n\n## Slide 2\n\nFree text box\n\n| Region | Sales |\n|---|---|\n| EU | 42 |"
	// The same file by absolute path, file:// URL, ~ and a relative path.
	for _, src := range []string{deck, "file://" + deck, "~/deck.pptx", "deck.pptx"} {
		if got := s.ok("read", map[string]any{"source": src}); got != want {
			t.Errorf("read %s:\n%s\nwant:\n%s", src, got, want)
		}
	}

	sample := s.ok("read", map[string]any{"source": s.file("plan.docx", testdoc.SampleDOCX())})
	contains(t, sample, "# Project Plan", "This plan is **very important** and *urgent*.", "## Budget",
		"| Servers | 100 | a\\|b |", `\# not a heading`)
	lacks(t, sample, "removed text")
}

// unzippedXML is what an agent gets by unzipping an Office file to read it.
func unzippedXML(t *testing.T, data []byte) string {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".xml") || strings.Contains(f.Name, "theme") || strings.Contains(f.Name, "styles") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(&b, rc)
		rc.Close()
	}
	return b.String()
}

func TestLocalFilesAndErrors(t *testing.T) {
	site := newSite(t)
	s := start(t)

	notes := s.file("notes.md", []byte("# Notes\n\nfirst draft\n"))
	if out := s.ok("read", map[string]any{"source": notes}); out != "# Notes\n\nfirst draft" {
		t.Errorf("notes: %q", out)
	}
	os.WriteFile(notes, []byte("# Notes\n\nsecond draft, now longer\n"), 0o644)
	if out := s.ok("read", map[string]any{"source": notes}); !strings.Contains(out, "second draft") {
		t.Errorf("an edited file was served stale: %q", out)
	}
	data := s.file("rows.json", []byte(`[{"id":1,"tag":null},{"id":2,"tag":"x"}]`))
	if out := s.ok("read_json", map[string]any{"source": data, "table": true}); out != "| id | tag |\n|---|---|\n| 1 | |\n| 2 | x |" {
		t.Errorf("json file: %q", out)
	}
	contains(t, s.ok("read", map[string]any{"source": site.URL + "/cafe"}), "# Café menu", "Crème brûlée and café au lait")

	s.fails("read", map[string]any{"source": filepath.Join(s.dir, "missing.pdf")}, "no such file")
	contains(t, s.ok("read", map[string]any{"source": s.dir}), "notes.md", "rows.json")
	s.fails("read", map[string]any{"source": s.file("blob.bin", []byte{0, 1, 2, 3, 0xff, 0xfe, 0, 0, 9, 8, 7})}, "unsupported file type")
	s.fails("read", map[string]any{"source": site.URL + "/gone"}, "HTTP 410")
	s.fails("read", map[string]any{"source": site.URL + "/gone"}, "This page was removed in 2024.")
	s.fails("read", map[string]any{"source": "http://127.0.0.1:1/"}, "127.0.0.1:1")

	// Every call is logged on stderr (stdout is the protocol).
	contains(t, s.stderr.String(), "msg=read ", "msg=read_json ", "msg=\"read failed\"")
}

func TestJavaScriptPages(t *testing.T) {
	site := newSite(t)
	if browser.ExecPath() != "" {
		out := start(t).ok("read", map[string]any{"source": site.URL + "/app"})
		contains(t, out, "# Status board", "All systems operational in every region today.")
		lacks(t, out, "[note:", "innerHTML")
	} else {
		t.Log("no Chrome/Chromium installed; only checking the fallback note")
	}
	// Without a working browser the page still comes back, with a note saying why
	// it may be incomplete.
	out := start(t, "TOKENSAVER_CHROME=/nonexistent/chrome").ok("read", map[string]any{"source": site.URL + "/app"})
	contains(t, out, "# Status board", "[note: JavaScript rendering failed")
}

func TestConcurrentCalls(t *testing.T) {
	site := newSite(t)
	s := start(t)
	pdf := s.file("board-pack.pdf", boardPackPDF())
	docx := s.file("annual-report.docx", testdoc.ReportDOCX())
	calls := []mcp.CallToolParams{
		{Name: "read", Arguments: map[string]any{"source": site.URL + "/docs/install"}},
		{Name: "read", Arguments: map[string]any{"source": site.URL + "/wiki/Gopher_language", "outline": true}},
		{Name: "read_json", Arguments: map[string]any{"source": site.URL + "/api/search/repositories", "select": "items.name", "max_items": 3}},
		{Name: "read", Arguments: map[string]any{"source": pdf}},
		{Name: "read", Arguments: map[string]any{"source": docx, "outline": true}},
		{Name: "read", Arguments: map[string]any{"source": site.URL + "/cafe"}},
	}
	// Each call alone first, then all of them at once, twice over: same answers.
	want := make([]string, len(calls))
	for i, c := range calls {
		want[i] = s.ok(c.Name, c.Arguments.(map[string]any))
	}
	var wg sync.WaitGroup
	for range 2 {
		for i, c := range calls {
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, err := s.cs.CallTool(t.Context(), &c)
				if err != nil || res.IsError {
					t.Errorf("concurrent %s: %v %v", c.Name, err, res)
					return
				}
				if got := res.Content[0].(*mcp.TextContent).Text; got != want[i] {
					t.Errorf("concurrent %s %v differs from the sequential result", c.Name, c.Arguments)
				}
			}()
		}
	}
	wg.Wait()
}
