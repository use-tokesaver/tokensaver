package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MilanBehnam/tokensaver/internal/testdoc"
)

// The test site imitates what agents actually fetch: a docs page wrapped in the
// usual framework payload (CSS, JS bundles, hydration data, navigation, cookie
// banner, footer), a long Wikipedia-style article, a GitHub-style JSON API, a
// JavaScript-only app, and a few edge cases. Pages are generated per request
// so same-site links use the test server's host.

func newSite(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()
	html := func(path string, page func(host string) string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, page(r.Host))
		})
	}
	html("/docs/install", docsPage)
	html("/wiki/Gopher_language", wikiPage)
	html("/app", func(string) string { return spaPage })
	mux.Handle("/old-install", http.RedirectHandler("/docs/install", http.StatusMovedPermanently))
	mux.HandleFunc("/cafe", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=windows-1252")
		w.Write([]byte("<html><head><title>Caf\xe9 menu</title></head><body><p>Cr\xe8me br\xfbl\xe9e and caf\xe9 au lait, served all day.</p></body></html>"))
	})
	mux.HandleFunc("/gone", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusGone)
		fmt.Fprint(w, `<html><head><title>Gone</title><style>.x{}</style></head><body><nav>Home</nav><h1>Gone</h1><p>This page was removed in 2024.</p></body></html>`)
	})
	mux.HandleFunc("/api/search/repositories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(searchJSON())
	})
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		for i := 1; i <= 3; i++ {
			fmt.Fprintf(w, `{"id":%d,"type":"deploy","actor":{"login":"dev%d"},"payload":null}`+"\n", i, i)
		}
	})
	mux.HandleFunc("/api/missing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found","documentation_url":"https://docs.example/rest","status":"404"}`)
	})
	mux.HandleFunc("/files/board-pack.pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(boardPackPDF())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// docsArticle is the part of the docs page a reader cares about.
const docsArticle = `<h1>Installing Acme CLI</h1>
<p>Acme CLI ships your code to production from the terminal. This guide covers the supported platforms, installing on each of them, checking that the install worked, and upgrading later. It takes about five minutes.</p>
<h2 id="requirements">Requirements<a class="anchor" href="#requirements" aria-hidden="true">#</a></h2>
<p>You need a 64-bit operating system and about 120 MB of free disk space. See the <a href="http://HOST/docs/config?utm_source=docs&amp;utm_medium=web&amp;lang=en" title="Configuration">configuration reference</a> for proxy settings.</p>
<table><thead><tr><th>Platform</th><th>Architectures</th><th>Packages</th></tr></thead><tbody>
<tr><td>macOS 13+</td><td>x86_64, arm64</td><td><code>brew</code>, <code>.pkg</code></td></tr>
<tr><td>Linux</td><td>x86_64, arm64</td><td><code>.deb</code>, <code>.rpm</code>, tarball</td></tr>
<tr><td>Windows 10+</td><td>x86_64</td><td><code>winget</code>, <code>.msi</code></td></tr>
</tbody></table>
<h2 id="macos">Install on macOS<a class="anchor" href="#macos" aria-hidden="true">#</a></h2>
<p>Install the latest release with Homebrew:</p>
<pre><code class="language-bash">brew install acme/tap/acme
acme --version</code></pre>
<div class="admonition note"><p><strong>Note:</strong> Apple silicon Macs need Rosetta only for plugins built before 2024.</p></div>
<h2 id="linux">Install on Linux</h2>
<p>On Debian and Ubuntu, add the signing key and install the package:</p>
<pre><code class="language-bash">curl -fsSL https://get.acme.dev/key.gpg | sudo gpg --dearmor -o /usr/share/keyrings/acme.gpg
sudo apt install acme</code></pre>
<p>On Fedora, run <code>sudo dnf install acme</code> instead.</p>
<h3 id="linux-config">Configuration file</h3>
<p>Acme CLI reads its settings from <code>~/.config/acme/config.yaml</code>:</p>
<pre><code class="language-yaml">region: eu-west-1
telemetry: false</code></pre>
<h2 id="windows">Install on Windows</h2>
<p>Use winget: <code>winget install Acme.CLI</code>. The installer adds <code>acme</code> to your PATH.</p>
<h2 id="verify">Verify the install</h2>
<ol><li>Open a new terminal.</li><li>Run <code>acme doctor</code>.</li><li>Check that every line says <em>ok</em>.</li></ol>
<p>If something fails, see <a href="http://HOST/docs/troubleshooting#install">Troubleshooting</a> or ask on <a href="https://github.com/acme/cli/discussions">GitHub Discussions</a>.</p>
<h2 id="upgrade">Upgrade</h2>
<p>Run <code>acme upgrade</code> at any time. Release notes are on <a href="https://github.com/acme/cli/releases?utm_campaign=docs">GitHub releases</a>.</p>
<figure><img src="/img/terminal.png" alt="Terminal showing acme doctor output"></figure>`

func docsPage(host string) string {
	var b strings.Builder
	w := func(parts ...string) {
		for _, p := range parts {
			b.WriteString(p)
		}
	}
	w(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Installing Acme CLI · Acme Docs</title>`)
	for i := range 24 {
		fmt.Fprintf(&b, `<meta property="og:tag%d" content="acme docs install cli %d">`, i, i)
	}
	for i := range 12 {
		fmt.Fprintf(&b, `<link rel="preload" href="/_next/static/chunks/%x.js" as="script">`, i*7919)
	}
	w(`<style>`, fakeCSS(900), `</style>`)
	w(`<script type="application/ld+json">{"@context":"https://schema.org","@type":"TechArticle","headline":"Installing Acme CLI"}</script>`)
	w(`<script>`, fakeJS(1500), `</script></head><body>`)
	w(`<div id="cookie-banner"><p>We use cookies to improve your experience and for marketing.</p><button>Accept all</button><button>Reject</button></div>`)
	w(`<header class="site-header"><a href="/" class="logo">`, svgIcon(1), `Acme</a><nav aria-label="Main"><ul>`)
	for i, menu := range []string{"Products", "Solutions", "Pricing", "Customers", "Blog", "Company"} {
		fmt.Fprintf(&b, `<li><button aria-expanded="false">%s</button><ul class="dropdown">`, menu)
		for j := range 8 {
			fmt.Fprintf(&b, `<li><a href="/%s/item-%d">%s%s item %d</a></li>`, strings.ToLower(menu), j, svgIcon(i*8+j), menu, j)
		}
		w(`</ul></li>`)
	}
	w(`</ul></nav><form role="search"><input type="search" placeholder="Search docs"></form></header>`)
	w(`<div class="layout"><aside class="sidebar"><nav aria-label="Docs">`)
	for g := range 6 {
		fmt.Fprintf(&b, `<p class="group">Group %d</p><ul>`, g)
		for i := range 10 {
			fmt.Fprintf(&b, `<li><a href="/docs/g%d/t%d?utm_source=sidebar">Group topic %d.%d</a></li>`, g, i, g, i)
		}
		w(`</ul>`)
	}
	w(`</nav></aside><main><nav class="breadcrumbs"><a href="/docs">Docs</a> / <a href="/docs/start">Getting started</a></nav>`)
	w(`<article>`, strings.ReplaceAll(docsArticle, "HOST", host), `</article>`)
	w(`<div class="feedback">Was this page helpful? <button>Yes</button> <button>No</button></div>`)
	w(`<aside class="toc"><p>On this page</p><ul><li><a href="#requirements">Requirements</a></li><li><a href="#macos">macOS</a></li></ul></aside></main></div>`)
	w(`<footer><div class="cols">`)
	for c, col := range []string{"Product", "Developers", "Company", "Legal"} {
		fmt.Fprintf(&b, `<div><h4>%s</h4><ul>`, col)
		for i := range 10 {
			fmt.Fprintf(&b, `<li><a href="/%d/%d">Footer link %d.%d</a></li>`, c, i, c, i)
		}
		w(`</ul></div>`)
	}
	w(`</div><form><label>Newsletter</label><input type="email"><button>Subscribe</button></form>`)
	for i := range 6 {
		w(`<a href="https://social.example/`, fmt.Sprint(i), `">`, svgIcon(100+i), `</a>`)
	}
	w(`<p>© 2026 Acme Inc. All rights reserved.</p></footer>`)
	w(`<div class="modal" hidden><h2>Join our newsletter</h2><p>Monthly news, no spam.</p></div>`)
	w(`<div style="display:none">Tracking fallback</div><img src="/pixel.gif?id=123" width="1" height="1" alt="">`)
	for i := range 10 {
		fmt.Fprintf(&b, `<script src="/_next/static/chunks/%x.js" defer></script>`, i*104729)
	}
	w(`<script>self.__next_f.push([1,"`, strings.Repeat(`0:[\"$\",\"div\",null,{\"className\":\"docs-layout\"}]`, 400), `"])</script>`)
	w(`</body></html>`)
	return b.String()
}

func fakeCSS(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, ".c-%x{display:flex;margin:0 %dpx;color:#%06x;transition:all .%ds ease}", i*2654435761%1000003, i%24, i*9973%0xffffff, i%9)
	}
	return b.String()
}

func fakeJS(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "function f%x(a,b){return a*%d+(b||[]).length}", i*40503%1000003, i)
	}
	return b.String()
}

func svgIcon(seed int) string {
	var d strings.Builder
	for i := range 30 {
		fmt.Fprintf(&d, "M%d.%d %d.%dl%d %d ", (seed+i)%24, i%10, (seed*i)%24, (seed+3*i)%10, i%5, (seed+i)%7)
	}
	return `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="` + d.String() + `"/></svg>`
}

// wikiSections is the long article's structure: level, title, paragraphs.
var wikiSections = []struct {
	level int
	title string
	paras int
}{
	{2, "History", 3}, {3, "Origins", 5}, {3, "Releases", 6},
	{2, "Design", 2}, {3, "Syntax", 6}, {3, "Types", 7}, {3, "Concurrency", 8},
	{2, "Tools", 5}, {2, "Reception", 6}, {2, "Etymology", 3},
}

// wikiPara is paragraph p of section s; its marker lets tests find every
// paragraph exactly once across pages and sections.
func wikiPara(s, p int) string {
	return fmt.Sprintf("Paragraph marker-%d-%d of the %s section. The Gopher language was designed for large codebases, fast builds and simple concurrency, and this sentence pads the paragraph to a realistic length so paging has real work to do across sections.",
		s, p, wikiSections[s].title)
}

func wikiPage(host string) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head><title>Gopher language - Wikipedia</title><link rel="stylesheet" href="/w/load.php"><script>RLCONF={"wgPageName":"Gopher_language"};</script></head><body>`)
	b.WriteString(`<a class="mw-jump-link" href="#bodyContent">Jump to content</a><div id="mw-navigation"><div id="mw-panel"><ul>`)
	for i := range 40 {
		fmt.Fprintf(&b, `<li><a href="/wiki/Portal:%d">Portal %d</a></li>`, i, i)
	}
	b.WriteString(`</ul></div></div><div id="content"><h1 id="firstHeading">Gopher language</h1><div id="bodyContent"><div class="mw-parser-output">`)
	b.WriteString(`<table class="infobox"><tr><th>Designed by</th><td>Ada Park, Rui Tan</td></tr><tr><th>First appeared</th><td>2011</td></tr></table>`)
	b.WriteString(`<p><b>Gopher</b> is a statically typed programming language.<sup class="reference"><a href="#cite_note-1">[1]</a></sup> It is known for fast compilation.<sup class="noprint"><a href="#cn">[citation needed]</a></sup></p>`)
	b.WriteString(`<div id="toc" class="toc"><div class="toctitle"><h2>Contents</h2></div><ul><li><a href="#History">History</a></li><li><a href="#Design">Design</a></li></ul></div>`)
	for s, sec := range wikiSections {
		fmt.Fprintf(&b, `<div class="mw-heading mw-heading%d"><h%d id="%s">%s</h%d><span class="mw-editsection">[<a href="/w/index.php?action=edit&amp;section=%d">edit</a>]</span></div>`,
			sec.level, sec.level, sec.title, sec.title, sec.level, s+1)
		for p := range sec.paras {
			fmt.Fprintf(&b, `<p>%s<sup class="reference"><a href="#cite_note-%d">[%d]</a></sup></p>`, wikiPara(s, p), s*10+p, s*10+p)
		}
	}
	b.WriteString(`<div class="navbox"><table><tr><th>Programming languages</th><td><a href="/wiki/A">A</a> · <a href="/wiki/B">B</a> · <a href="/wiki/C">C</a></td></tr></table></div>`)
	b.WriteString(`</div></div></div><div id="footer"><p>This page was last edited on 1 January 2026.</p><p>Text is available under the Creative Commons Attribution-ShareAlike License.</p></div></body></html>`)
	return b.String()
}

const spaPage = `<html><head><title>Status board</title></head><body><div id="root"></div><script>
setTimeout(function () {
  document.getElementById('root').innerHTML = '<h1>Status board</h1><p>' +
    'All systems operational in every region today. '.repeat(20) + '</p>';
}, 300);
</script></body></html>`

// boardPackPDF is a 12-page report with a running header, a confidentiality
// footer and "Page N of 12" numbers on every page.
func boardPackPDF() []byte {
	var pages []string
	for i := 1; i <= 12; i++ {
		pages = append(pages, fmt.Sprintf("Northwind Board Pack Q3 2026\nAgenda item %d: %s\nThe board reviewed item %d and noted progress against plan.\nOwner: team %c. Status: %s.\nDecision log entry %d-B was recorded.\nConfidential - internal use only\nPage %d of 12",
			i, []string{"Minutes", "Finance", "Sales", "Hiring", "Risks", "Security", "Legal", "IT", "Facilities", "ESG", "Audit", "Any other business"}[i-1],
			i, 'A'+rune(i), []string{"green", "amber"}[i%2], i, i))
	}
	return testdoc.PDF("Board Pack Q3 2026", pages)
}

// The search API response mimics GitHub's: 100 repositories, each with dozens
// of URL templates, ids, nulls and nested owner/license objects.
type repo struct {
	ID               int      `json:"id"`
	NodeID           string   `json:"node_id"`
	Name             string   `json:"name"`
	FullName         string   `json:"full_name"`
	Private          bool     `json:"private"`
	Owner            owner    `json:"owner"`
	HTMLURL          string   `json:"html_url"`
	Description      *string  `json:"description"`
	Fork             bool     `json:"fork"`
	URL              string   `json:"url"`
	ForksURL         string   `json:"forks_url"`
	KeysURL          string   `json:"keys_url"`
	CollaboratorsURL string   `json:"collaborators_url"`
	TeamsURL         string   `json:"teams_url"`
	HooksURL         string   `json:"hooks_url"`
	IssueEventsURL   string   `json:"issue_events_url"`
	EventsURL        string   `json:"events_url"`
	AssigneesURL     string   `json:"assignees_url"`
	BranchesURL      string   `json:"branches_url"`
	TagsURL          string   `json:"tags_url"`
	BlobsURL         string   `json:"blobs_url"`
	GitTagsURL       string   `json:"git_tags_url"`
	GitRefsURL       string   `json:"git_refs_url"`
	TreesURL         string   `json:"trees_url"`
	StatusesURL      string   `json:"statuses_url"`
	LanguagesURL     string   `json:"languages_url"`
	StargazersURL    string   `json:"stargazers_url"`
	ContributorsURL  string   `json:"contributors_url"`
	SubscribersURL   string   `json:"subscribers_url"`
	CommitsURL       string   `json:"commits_url"`
	CreatedAt        string   `json:"created_at"`
	UpdatedAt        string   `json:"updated_at"`
	PushedAt         string   `json:"pushed_at"`
	GitURL           string   `json:"git_url"`
	SSHURL           string   `json:"ssh_url"`
	CloneURL         string   `json:"clone_url"`
	Homepage         *string  `json:"homepage"`
	Size             int      `json:"size"`
	StargazersCount  int      `json:"stargazers_count"`
	WatchersCount    int      `json:"watchers_count"`
	Language         string   `json:"language"`
	HasIssues        bool     `json:"has_issues"`
	HasWiki          bool     `json:"has_wiki"`
	HasPages         bool     `json:"has_pages"`
	ForksCount       int      `json:"forks_count"`
	MirrorURL        *string  `json:"mirror_url"`
	Archived         bool     `json:"archived"`
	OpenIssuesCount  int      `json:"open_issues_count"`
	License          *license `json:"license"`
	Topics           []string `json:"topics"`
	Visibility       string   `json:"visibility"`
	DefaultBranch    string   `json:"default_branch"`
	Score            float64  `json:"score"`
}

type owner struct {
	Login             string `json:"login"`
	ID                int    `json:"id"`
	NodeID            string `json:"node_id"`
	AvatarURL         string `json:"avatar_url"`
	GravatarID        string `json:"gravatar_id"`
	URL               string `json:"url"`
	HTMLURL           string `json:"html_url"`
	FollowersURL      string `json:"followers_url"`
	FollowingURL      string `json:"following_url"`
	GistsURL          string `json:"gists_url"`
	StarredURL        string `json:"starred_url"`
	SubscriptionsURL  string `json:"subscriptions_url"`
	OrganizationsURL  string `json:"organizations_url"`
	ReposURL          string `json:"repos_url"`
	EventsURL         string `json:"events_url"`
	ReceivedEventsURL string `json:"received_events_url"`
	Type              string `json:"type"`
	SiteAdmin         bool   `json:"site_admin"`
}

type license struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	SpdxID string `json:"spdx_id"`
	URL    string `json:"url"`
	NodeID string `json:"node_id"`
}

var (
	repoLangs  = []string{"Go", "Rust", "TypeScript", "Python", "Zig"}
	repoTopics = []string{"cli", "database", "http", "kubernetes", "observability", "parser", "testing"}
)

func searchRepo(i int) repo {
	login := fmt.Sprintf("org%d", i%17)
	name := fmt.Sprintf("project-%03d", i)
	api := "https://api.example.com/repos/" + login + "/" + name
	u := "https://api.example.com/users/" + login
	var desc, home *string
	if i%3 != 0 {
		d := fmt.Sprintf("A %s library for %s workloads", repoLangs[i%5], repoTopics[i%7])
		desc = &d
	}
	if i%4 == 0 {
		h := ""
		home = &h
	}
	var lic *license
	if i%5 != 0 {
		lic = &license{Key: "mit", Name: "MIT License", SpdxID: "MIT", URL: "https://api.example.com/licenses/mit", NodeID: "MDc6TGljZW5zZTEz"}
	}
	return repo{
		ID: 1000 + i, NodeID: fmt.Sprintf("R_kgDOH%05d", i), Name: name, FullName: login + "/" + name,
		Owner: owner{Login: login, ID: 50 + i%17, NodeID: "O_kgDO" + login, AvatarURL: "https://avatars.example.com/u/" + login + "?v=4",
			URL: u, HTMLURL: "https://example.com/" + login, FollowersURL: u + "/followers", FollowingURL: u + "/following{/other_user}",
			GistsURL: u + "/gists{/gist_id}", StarredURL: u + "/starred{/owner}{/repo}", SubscriptionsURL: u + "/subscriptions",
			OrganizationsURL: u + "/orgs", ReposURL: u + "/repos", EventsURL: u + "/events{/privacy}", ReceivedEventsURL: u + "/received_events", Type: "Organization"},
		HTMLURL: "https://example.com/" + login + "/" + name, Description: desc, URL: api,
		ForksURL: api + "/forks", KeysURL: api + "/keys{/key_id}", CollaboratorsURL: api + "/collaborators{/collaborator}",
		TeamsURL: api + "/teams", HooksURL: api + "/hooks", IssueEventsURL: api + "/issues/events{/number}", EventsURL: api + "/events",
		AssigneesURL: api + "/assignees{/user}", BranchesURL: api + "/branches{/branch}", TagsURL: api + "/tags", BlobsURL: api + "/git/blobs{/sha}",
		GitTagsURL: api + "/git/tags{/sha}", GitRefsURL: api + "/git/refs{/sha}", TreesURL: api + "/git/trees{/sha}", StatusesURL: api + "/statuses/{sha}",
		LanguagesURL: api + "/languages", StargazersURL: api + "/stargazers", ContributorsURL: api + "/contributors", SubscribersURL: api + "/subscribers",
		CommitsURL: api + "/commits{/sha}", CreatedAt: fmt.Sprintf("2019-%02d-%02dT10:00:00Z", 1+i%12, 1+i%28),
		UpdatedAt: "2026-09-01T08:30:00Z", PushedAt: "2026-09-01T08:29:00Z", GitURL: "git://example.com/" + login + "/" + name + ".git",
		SSHURL: "git@example.com:" + login + "/" + name + ".git", CloneURL: "https://example.com/" + login + "/" + name + ".git", Homepage: home,
		Size: 1000 + i*37, StargazersCount: 90000 - i*731, WatchersCount: 90000 - i*731, Language: repoLangs[i%5],
		HasIssues: true, HasWiki: i%2 == 0, ForksCount: 5000 - i*41, OpenIssuesCount: i * 3 % 200, License: lic,
		Topics: repoTopics[i%7 : i%7+1+i%3%(7-i%7)], Visibility: "public", DefaultBranch: "main", Score: 1,
	}
}

func searchJSON() []byte {
	var items []repo
	for i := range 100 {
		items = append(items, searchRepo(i))
	}
	b, _ := json.Marshal(struct {
		TotalCount        int    `json:"total_count"`
		IncompleteResults bool   `json:"incomplete_results"`
		Items             []repo `json:"items"`
	}{12345, false, items})
	return b
}
