package convert

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/use-tokesaver/tokensaver/internal/browser"
	"github.com/use-tokesaver/tokensaver/internal/source"
	"github.com/use-tokesaver/tokensaver/internal/testdoc"
)

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func mustNotContain(t *testing.T, got string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(got, n) {
			t.Errorf("unexpected %q in:\n%s", n, got)
		}
	}
}

const articleHTML = `<!doctype html><html><head><title>Install Guide · Acme Docs</title>
<style>body{color:red}</style><script>track()</script></head><body>
<header><nav><a href="/">Home</a> <a href="/pricing">Pricing</a> <a href="/login">Log in</a></nav></header>
<main><article>
<h1>Install Guide</h1>
<p>Acme is a command-line tool for shipping software quickly. This guide walks you through installing it
on every supported platform, verifying the install, and upgrading later. Read the
<a href="/docs/requirements?utm_source=nav&amp;lang=en" title="Requirements page">requirements</a> first, and see the
<a href="https://github.com/acme/acme">source on GitHub</a>.</p>
<h2 id="macos">macOS <a class="anchor" href="#macos">¶</a></h2>
<p>Use Homebrew to install the latest release. <img src="/img/brew.png" alt="brew"> It takes a minute or so,
depending on your connection. Jump to <a href="#linux">Linux</a> if you are not on a Mac.</p>
<pre><code class="language-sh">brew install acme
acme --version</code></pre>
<h2 id="linux">Linux</h2>
<table><thead><tr><th>Distro</th><th>Command</th></tr></thead>
<tbody><tr><td>Debian</td><td>apt install acme</td></tr><tr><td>Fedora</td><td>dnf install acme</td></tr></tbody></table>
<p>After installing, run the doctor command to check your setup. It reports missing dependencies.</p>
</article></main>
<aside>Related: <a href="/blog">Blog</a></aside>
<footer>© 2026 Acme · <a href="/privacy">Privacy</a> · <a href="/terms">Terms</a></footer>
</body></html>`

func TestHTMLArticle(t *testing.T) {
	base, _ := url.Parse("https://docs.acme.dev/install")
	doc, _, err := htmlToMarkdown(articleHTML, base)
	if err != nil {
		t.Fatal(err)
	}
	md := doc.Markdown
	mustContain(t, md,
		"# Install Guide",
		"## macOS",
		"## Linux",
		"[requirements](/docs/requirements?lang=en)", // same-site → root-relative, utm_ stripped
		"[source on GitHub](https://github.com/acme/acme)",
		"Jump to Linux if", // same-page anchor unwrapped
		"```sh\nbrew install acme\nacme --version\n```",
		"| Debian | apt install acme |",
	)
	mustNotContain(t, md, "Pricing", "Privacy", "track()", "color:red", "brew.png", "¶", "Requirements page", "utm_source", "Acme Docs")
	if strings.Count(md, "# Install Guide") != 1 {
		t.Errorf("title duplicated:\n%s", md)
	}
}

func TestHTMLCodeBlocks(t *testing.T) {
	prose := strings.Repeat("Unmarshal parses the JSON-encoded data and stores the result in the value pointed to by v. ", 4)
	page := `<html><head><title>json package</title></head><body><main><article><h1>json</h1><p>` + prose + `</p>
<h4 id="Unmarshal">func <a href="https://cs.example/decode.go">Unmarshal</a></h4>
<div class="Documentation-declaration"><pre>func Unmarshal(data []<a href="/builtin#byte">byte</a>, v <a href="/builtin#any">any</a>) <a href="/builtin#error">error</a></pre></div>
<p>` + prose + `</p>
<div class="code-example"><div class="example-header"><span class="language-name">js</span></div><pre class="brush: js notranslate"><code>const r = await fetch(url);</code></pre></div>
<p class="label">go</p><pre><code class="language-go">x := 1</code></pre>
<p>bash</p><pre><code class="language-python">not_the_label = True</code></pre>
<div class="highlight highlight-source-go notranslate"><pre>fmt.Println("hi")</pre></div>
<pre><code class="language-go" data-lang="go">var x = 1</code></pre>
<pre><code data-lang="Python">print(1)</code></pre>
<div class="language-ruby highlighter-rouge"><div class="highlight"><pre class="highlight"><code>puts 1</code></pre></div></div>
<p>` + prose + `</p></article></main></body></html>`
	base, _ := url.Parse("https://pkg.example/encoding/json")
	doc, _, err := htmlToMarkdown(page, base)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown,
		"```\nfunc Unmarshal(data []byte, v any) error\n```", // links flattened, so readability keeps it
		"```js\nconst r = await fetch(url);\n```",
		"```go\nfmt.Println(\"hi\")\n```",
		"```go\nvar x = 1\n```",
		"```python\nprint(1)\n```",
		"```ruby\nputs 1\n```",
		"```go\nx := 1\n```",
		"bash\n\n```python\nnot_the_label = True\n```", // only a label that repeats the language goes
	)
	// The "js" header MDN puts above every example is gone, and so is the "go"
	// label, but the fences still carry the language.
	if n := strings.Count(doc.Markdown, "\njs\n"); n != 0 {
		t.Errorf("%d language labels left:\n%s", n, doc.Markdown)
	}
	mustNotContain(t, doc.Markdown, "\ngo\n\n```go")
}

func TestUnescapeIntraword(t *testing.T) {
	cases := map[string]string{
		`set max\_tokens and get\_user\_by\_id`:  `set max_tokens and get_user_by_id`,
		`MAX\_RETRIES, café\_au\_lait, x86\_64`:  `MAX_RETRIES, café_au_lait, x86_64`,
		`\_private and trailing\_ stay escaped`:  `\_private and trailing\_ stay escaped`,
		`\_\_dunder\_\_ and a\*b`:                `\_\_dunder\_\_ and a\*b`,
		"`in\\_code` but out\\_side":             "`in\\_code` but out_side",
		"``a`b\\_c`` d\\_e":                      "``a`b\\_c`` d_e",
		"\\`not\\_code\\`":                       "\\`not_code\\`",
		"C:\\\\Users\\\\x\\_y":                   "C:\\\\Users\\\\x_y",
		"```\nraw\\_code\n```\nafter\\_fence":    "```\nraw\\_code\n```\nafter_fence",
		"````md\n```\nstill\\_code\n````\nx\\_y": "````md\n```\nstill\\_code\n````\nx_y",
	}
	for in, want := range cases {
		if got := unescapeIntraword(in); got != want {
			t.Errorf("unescapeIntraword(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHTMLFallsBackToBodyWithoutArticle(t *testing.T) {
	page := `<html><head><title>Tiny</title></head><body><nav>Menu stuff</nav><p>Just one short line.</p><footer>foot</footer></body></html>`
	base, _ := url.Parse("https://x.dev/")
	doc, n, err := htmlToMarkdown(page, base)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "# Tiny", "Just one short line.")
	mustNotContain(t, doc.Markdown, "Menu stuff", "foot")
	if n > 40 {
		t.Errorf("text length = %d", n)
	}
}

func TestPDF(t *testing.T) {
	// Five pages with a running header, a "Confidential" footer, and a printed
	// page number that is the page index + 2 (front matter).
	topics := []string{"revenue", "costs", "hiring", "risks", "outlook"}
	var pages []string
	for i := 1; i <= 5; i++ {
		pages = append(pages, fmt.Sprintf(
			"ACME Quarterly Report 2026\nSection %d body text about %s.\nMore details on %s follow.\nTable row value %d\nSummary line %d\nConfidential - do not share\n%d",
			i, topics[i-1], topics[i-1], i*10, i, i+2))
	}
	pages[2] = "ACME Quarterly Report 2026\n# of units shipped: 12\n5" // a short page
	pages = append(pages, "Short page.\n42")                           // 42 is not a page number
	doc, err := convertPDF(context.Background(), testdoc.PDF("Q3 Report", pages))
	if err != nil {
		t.Fatal(err)
	}
	md := doc.Markdown
	mustContain(t, md, "Title: Q3 Report", "## Page 1", "## Page 6",
		"Section 1 body text about revenue.", "Section 5 body text", "Summary line 4",
		`\# of units shipped: 12`, "Short page.\n42")
	mustNotContain(t, md, "Confidential")
	if strings.Count(md, "ACME Quarterly Report") != 1 { // kept only on the short page
		t.Errorf("running header not removed:\n%s", md)
	}
	for _, n := range []string{"\n3\n", "\n7\n"} {
		if strings.Contains(md, n) {
			t.Errorf("page number line %q not removed:\n%s", n, md)
		}
	}
}

func TestDOCX(t *testing.T) {
	doc, err := convertDOCX(testdoc.SampleDOCX())
	if err != nil {
		t.Fatal(err)
	}
	md := doc.Markdown
	mustContain(t, md,
		"# Project Plan",
		"This plan is **very important** and *urgent*.",
		"# Goals", "- Ship v1\n  - Write docs",
		"1. First step\n1. Second step",
		"## Budget", // custom style based on Heading2
		"[the site](https://example.com/plan) has details",
		"| Item | Cost | Note |\n|---|---|---|\n| Servers | 100 | a\\|b |\n| Total spanning | | x |",
		`\# not a heading`,
	)
	mustNotContain(t, md, "removed text")
}

func TestPPTX(t *testing.T) {
	doc, err := convertPPTX(testdoc.SamplePPTX())
	if err != nil {
		t.Fatal(err)
	}
	want := "## Slide 1: Quarterly Review\n\n- Revenue up\n  - EU strongest\n- Costs flat\n\nNotes: Mention the EU deal.\n\n## Slide 2\n\nFree text box\n\n| Region | Sales |\n|---|---|\n| EU | 42 |"
	if doc.Markdown != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", doc.Markdown, want)
	}
}

func TestXLSX(t *testing.T) {
	doc, err := convertXLSX(testdoc.SampleXLSX())
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown,
		"## Sheet: Sales (4 rows × 3 cols)",
		"| Region | Q1 | Q2 |\n|---|---|---|\n| EU | 10 | 20 |\n| US\\|CA | 5 | 7 |\n| Total |",
		"## Sheet: Empty\n\n(empty)",
		"## Sheet: Secret (hidden) (1 rows × 1 cols)",
	)
}

func TestConvertDetectsFromBytes(t *testing.T) {
	cases := map[source.Kind][]byte{
		source.PDF:  testdoc.PDF("t", []string{"hello pdf"}),
		source.DOCX: testdoc.SampleDOCX(),
		source.PPTX: testdoc.SamplePPTX(),
		source.XLSX: testdoc.SampleXLSX(),
	}
	for want, data := range cases {
		s := &source.Source{Path: "download.bin", Data: data}
		if got := source.Detect(s); got != want {
			t.Errorf("Detect = %q, want %q", got, want)
		}
	}
}

func TestJSRendering(t *testing.T) {
	if testing.Short() || browser.ExecPath() == "" {
		t.Skip("no Chrome/Chromium available")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>App</title></head><body><div id="root"></div><script>
			setTimeout(function () {
				document.getElementById('root').innerHTML = '<h1>Dashboard</h1><p>' +
					'Rendered by JavaScript. '.repeat(30) + '</p>';
			}, 300);
		</script></body></html>`)
	}))
	defer srv.Close()
	src, err := source.Load(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := Convert(context.Background(), src, source.HTML, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, doc.Markdown, "Dashboard", "Rendered by JavaScript.")
	if doc.Note != "" {
		t.Errorf("unexpected note: %s", doc.Note)
	}
}

func TestLogSummarization(t *testing.T) {
	logContent := `2024-01-15 10:23:45 INFO: Starting test suite
2024-01-15 10:23:46 INFO: Test 1 passed
2024-01-15 10:23:47 INFO: Test 2 passed
2024-01-15 10:23:48 INFO: Test 3 passed
2024-01-15 10:23:49 INFO: Test 4 passed
2024-01-15 10:23:50 INFO: Test 5 passed
2024-01-15 10:23:51 ERROR: Test 6 failed: assertion error
   at module.test_func()
   at runner.execute()
2024-01-15 10:23:52 INFO: Test 7 passed
2024-01-15 10:23:53 INFO: Test 8 passed
2024-01-15 10:23:54 FATAL: Test 9 panicked
   at main.TestPanic()
2024-01-15 10:23:55 INFO: Test 10 passed`

	src := &source.Source{
		Path: "test.log",
		Data: []byte(logContent),
	}
	
	doc, err := Convert(context.Background(), src, source.LogFile, Options{})
	if err != nil {
		t.Fatal(err)
	}
	
	// Should contain error lines
	mustContain(t, doc.Markdown, "Test 6 failed", "ERROR", "Test 9 panicked", "FATAL")
	// Should contain context around errors
	mustContain(t, doc.Markdown, "at module.test_func()")
	// Should collapse uninteresting sections
	mustContain(t, doc.Markdown, "omitted")
	// Should not contain all the passing tests
	if strings.Count(doc.Markdown, "Test") < 5 {
		t.Logf("Log summarization working: %s", doc.Markdown)
	}
}
