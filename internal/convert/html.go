package convert

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"codeberg.org/readeck/go-readability/v2"
	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/strikethrough"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/table"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/use-tokesaver/tokensaver/internal/browser"
	"github.com/use-tokesaver/tokensaver/internal/source"
)

var mdConverter = converter.NewConverter(converter.WithPlugins(
	base.NewBasePlugin(),
	commonmark.NewCommonmarkPlugin(
		commonmark.WithHeadingStyle(commonmark.HeadingStyleATX),
		commonmark.WithBulletListMarker("-"),
		commonmark.WithListEndComment(false),
	),
	table.NewTablePlugin(
		table.WithCellPaddingBehavior(table.CellPaddingBehaviorMinimal),
		table.WithSkipEmptyRows(true),
	),
	strikethrough.NewStrikethroughPlugin(),
))

// minArticleText is how much text readability must find before we trust it;
// below it we fall back to the whole (cleaned) page.
const minArticleText = 200

// jsThreshold: a page with scripts but less visible text than this is probably a
// JavaScript app shell that needs rendering.
const jsThreshold = 300

func convertHTML(ctx context.Context, src *source.Source, opts Options) (*Doc, error) {
	raw := src.UTF8()
	base := src.URL
	if base == nil {
		abs, _ := filepath.Abs(src.Path)
		base = &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	}
	doc, textLen, err := htmlToMarkdown(raw, base)
	if err != nil {
		return nil, err
	}
	if src.URL == nil {
		return doc, nil
	}
	if !opts.JS && !(textLen < jsThreshold && strings.Contains(strings.ToLower(raw), "<script")) {
		return doc, nil
	}
	if browser.ExecPath() == "" {
		if opts.JS {
			return nil, browser.ErrNoBrowser
		}
		doc.Note = "this page seems to need JavaScript; install Chrome/Chromium (or set TOKENSAVER_CHROME) so tokensaver can render it"
		return doc, nil
	}
	rendered, err := browser.Render(ctx, src.URL.String())
	if err != nil {
		if opts.JS {
			return nil, fmt.Errorf("render with browser: %w", err)
		}
		doc.Note = "JavaScript rendering failed: " + err.Error()
		return doc, nil
	}
	jsDoc, _, err := htmlToMarkdown(rendered, src.URL)
	if err != nil {
		return nil, err
	}
	jsDoc.Rendered = true
	return jsDoc, nil
}

// htmlToMarkdown extracts the main content of a page and converts it to compact
// Markdown. It also returns the length of the extracted text.
func htmlToMarkdown(raw string, base *url.URL) (*Doc, int, error) {
	root, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, 0, fmt.Errorf("parse HTML: %w", err)
	}
	title := strings.TrimSpace(textOf(findFirst(root, atom.Title)))
	promoteWrappedHeadings(root)
	removeCitationMarks(root)
	prepareCodeBlocks(root)

	var content *html.Node
	parser := readability.NewParser()
	parser.KeepClasses = true // keeps "language-go" on <code>, so fences get their language
	if article, err := parser.ParseDocument(root, base); err == nil && article.Node != nil {
		var text bytes.Buffer
		_ = article.RenderText(&text)
		if len(strings.TrimSpace(text.String())) >= minArticleText {
			content = article.Node
			if t := strings.TrimSpace(article.Title()); t != "" {
				title = t
			}
		}
	}
	// "Install Guide · Acme Docs" → "Install Guide" when the page's h1 says so.
	if h1 := strings.Join(strings.Fields(textOf(findFirst(root, atom.H1))), " "); h1 != "" && len(h1) < len(title) && strings.Contains(title, h1) {
		title = h1
	}
	if content == nil {
		content = findFirst(root, atom.Body)
		if content == nil {
			content = root
		}
		stripChrome(content)
	}
	clean(content, base)
	textLen := len(strings.TrimSpace(textOf(content)))

	out, err := mdConverter.ConvertNode(content)
	if err != nil {
		return nil, 0, fmt.Errorf("convert HTML: %w", err)
	}
	md := tidyMarkdown(string(out))
	if title != "" && !startsWithTitle(md, title) {
		md = "# " + title + "\n\n" + md
	}
	return &Doc{Markdown: md, Title: title}, textLen, nil
}

func startsWithTitle(md, title string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(md), "\n")
	return strings.HasPrefix(first, "#") &&
		strings.EqualFold(strings.TrimSpace(strings.TrimLeft(first, "#")), title)
}

var headingAtoms = map[atom.Atom]bool{atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true}

// promoteWrappedHeadings prepares headings for readability:
//   - wrappers like Wikipedia's <div class="mw-heading"><h2>History</h2>
//     <span>[edit]</span></div> are replaced by the bare heading; readability
//     turns such divs into paragraphs, flattening the heading into plain text;
//   - ids of headings and sections are dropped: they are slugs of the heading
//     text, and readability discards elements whose id contains words like
//     "extra", "menu" or "sidebar" — losing a section titled "Sidebar component".
func promoteWrappedHeadings(root *html.Node) {
	var wrappers []*html.Node
	walk(root, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if headingAtoms[n.DataAtom] || n.DataAtom == atom.Section {
			delAttr(n, "id")
			if n.DataAtom != atom.Section {
				delAttr(n, "class")
			}
		}
		if n.DataAtom != atom.Div {
			return
		}
		var heading *html.Node
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			switch {
			case c.Type == html.ElementNode && headingAtoms[c.DataAtom]:
				if heading != nil {
					return
				}
				heading = c
			case c.Type == html.ElementNode && (c.DataAtom == atom.Span || c.DataAtom == atom.A || c.DataAtom == atom.Button):
			case c.Type == html.TextNode && strings.TrimSpace(c.Data) == "":
			case c.Type == html.CommentNode:
			default:
				return
			}
		}
		if heading != nil {
			wrappers = append(wrappers, n)
		}
	})
	for _, w := range wrappers {
		for c := w.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && headingAtoms[c.DataAtom] {
				w.RemoveChild(c)
				w.Parent.InsertBefore(c, w)
				break
			}
		}
		w.Parent.RemoveChild(w)
	}
}

var citationRe = regexp.MustCompile(`(?i)^\[(\d+|[a-z]|note \d+|nb \d+|citation needed|clarification needed)\]$`)

// removeCitationMarks drops superscript reference markers such as [12]: pure
// noise to an LLM, and a large article carries hundreds of them.
func removeCitationMarks(root *html.Node) {
	removeIf(root, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.DataAtom == atom.Sup &&
			citationRe.MatchString(strings.Join(strings.Fields(textOf(n)), " "))
	})
}

// codeLangRes find a code block's language in class conventions the Markdown
// converter doesn't read (it knows only language-x and lang-x).
var codeLangRes = []*regexp.Regexp{
	regexp.MustCompile(`(?:^|\s)brush:\s*([\w+#-]+)`),         // MDN, SyntaxHighlighter
	regexp.MustCompile(`(?:^|\s)highlight-source-([\w+#-]+)`), // GitHub
	regexp.MustCompile(`(?:^|\s)sourceCode\s+([\w+#-]+)`),     // pandoc
}

// wrapperLangRe finds language-x on a highlighter wrapper around the <pre>
// (Jekyll/rouge: <div class="language-ruby highlighter-rouge">).
var wrapperLangRe = regexp.MustCompile(`(?:^|\s)language-([\w+#-]+)`)

// prepareCodeBlocks readies <pre> blocks for readability and the converter:
//   - links inside them become plain text: a Markdown code block can't hold
//     links anyway, and a linked-up declaration such as pkg.go.dev's
//     "func Unmarshal(data []byte, v any) error" otherwise looks like a
//     navigation block to readability, which drops it;
//   - the block's language, in whatever convention the site uses, is set as
//     language-x so the fence carries it (```js).
func prepareCodeBlocks(root *html.Node) {
	var pres []*html.Node
	walk(root, func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Pre {
			pres = append(pres, n)
		}
	})
	for _, pre := range pres {
		var links []*html.Node
		walk(pre, func(n *html.Node) {
			if n.Type == html.ElementNode && n.DataAtom == atom.A {
				links = append(links, n)
			}
		})
		for _, a := range links {
			unwrap(a)
		}
		if lang := codeLanguage(pre); lang != "" {
			setAttr(pre, "class", strings.TrimSpace(attr(pre, "class")+" language-"+strings.ToLower(lang)))
		}
	}
}

// codeLanguage returns the language of a <pre> block when it is given in a
// convention other than language-x / lang-x on the <pre> or its <code>.
func codeLanguage(pre *html.Node) string {
	code := findFirst(pre, atom.Code)
	for _, n := range []*html.Node{pre, code} {
		if n != nil && (strings.Contains(attr(n, "class"), "language-") || strings.Contains(attr(n, "class"), "lang-")) {
			return ""
		}
	}
	for _, n := range []*html.Node{pre, code, pre.Parent, parentOf(pre.Parent)} {
		if n == nil || n.Type != html.ElementNode {
			continue
		}
		for _, key := range []string{"data-lang", "data-language"} {
			if v := strings.TrimSpace(attr(n, key)); v != "" && !strings.ContainsAny(v, " \t") {
				return v
			}
		}
		class := attr(n, "class")
		for _, re := range codeLangRes {
			if m := re.FindStringSubmatch(class); m != nil {
				return m[1]
			}
		}
		if n != pre && n != code && strings.Contains(class, "highlight") {
			if m := wrapperLangRe.FindStringSubmatch(class); m != nil {
				return m[1]
			}
		}
	}
	return ""
}

func parentOf(n *html.Node) *html.Node {
	if n == nil {
		return nil
	}
	return n.Parent
}

// dropTags are removed wherever they appear: they carry no readable text, or
// only UI (forms, media players, embedded frames).
var dropTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Template: true,
	atom.Svg: true, atom.Canvas: true, atom.Iframe: true, atom.Object: true, atom.Embed: true,
	atom.Img: true, atom.Picture: true, atom.Source: true, atom.Video: true, atom.Audio: true,
	atom.Button: true, atom.Input: true, atom.Select: true, atom.Textarea: true, atom.Dialog: true,
	atom.Link: true, atom.Meta: true,
}

// chromeTags are page furniture, removed only when readability found no article
// and we fall back to the whole body.
var chromeTags = map[atom.Atom]bool{
	atom.Nav: true, atom.Header: true, atom.Footer: true, atom.Aside: true, atom.Form: true,
}

var chromeRoles = map[string]bool{
	"navigation": true, "banner": true, "contentinfo": true, "search": true,
	"dialog": true, "alertdialog": true, "menu": true, "menubar": true, "complementary": true,
}

func stripChrome(n *html.Node) {
	removeIf(n, func(c *html.Node) bool {
		return chromeTags[c.DataAtom] || chromeRoles[attr(c, "role")]
	})
}

// permalinkTexts are the symbols docs sites put next to headings as anchors.
var permalinkTexts = map[string]bool{"": true, "#": true, "¶": true, "§": true, "🔗": true, "link": true, "permalink": true}

func clean(n *html.Node, base *url.URL) {
	removeIf(n, func(c *html.Node) bool {
		if c.Type == html.CommentNode {
			return true
		}
		if c.Type != html.ElementNode {
			return false
		}
		if dropTags[c.DataAtom] || attr(c, "aria-hidden") == "true" || hasAttr(c, "hidden") {
			return true
		}
		if s := strings.ReplaceAll(strings.ToLower(attr(c, "style")), " ", ""); strings.Contains(s, "display:none") {
			return true
		}
		return c.DataAtom == atom.A && permalinkTexts[strings.ToLower(strings.TrimSpace(textOf(c)))]
	})
	// Collect first: rewriteLink may unwrap a link, which would derail a live walk.
	var links []*html.Node
	walk(n, func(c *html.Node) {
		if c.Type == html.ElementNode && c.DataAtom == atom.A {
			links = append(links, c)
		}
	})
	for _, a := range links {
		rewriteLink(a, base)
	}
}

// rewriteLink shortens links: same-site links become root-relative ("/docs/x"),
// tracking parameters go, and links that lead nowhere useful (same-page anchors,
// javascript:) are unwrapped to plain text.
func rewriteLink(a *html.Node, base *url.URL) {
	href := strings.TrimSpace(attr(a, "href"))
	delAttr(a, "title")
	if href == "" || strings.HasPrefix(href, "#") {
		unwrap(a)
		return
	}
	u, err := base.Parse(href)
	if err != nil || u.Scheme == "javascript" || u.Scheme == "data" {
		unwrap(a)
		return
	}
	if q := u.Query(); len(q) > 0 {
		changed := false
		for k := range q {
			if strings.HasPrefix(k, "utm_") || k == "fbclid" || k == "gclid" || k == "ref_src" {
				q.Del(k)
				changed = true
			}
		}
		if changed {
			u.RawQuery = q.Encode()
		}
	}
	if u.Scheme == base.Scheme && u.Host == base.Host {
		if u.Path == base.Path && u.RawQuery == base.RawQuery {
			unwrap(a) // a link to this same page
			return
		}
		u = &url.URL{Path: u.Path, RawPath: u.RawPath, RawQuery: u.RawQuery, Fragment: u.Fragment}
		if u.Path == "" {
			u.Path = "/"
		}
	}
	setAttr(a, "href", u.String())
}

var (
	blankLinesRe = regexp.MustCompile(`\n{3,}`)
	emptyLinkRe  = regexp.MustCompile(`\[\s*\]\([^)]*\)`)
)

func tidyMarkdown(md string) string {
	md = strings.ReplaceAll(md, "\u00a0", " ")
	md = strings.ReplaceAll(md, "\u200b", "")
	md = emptyLinkRe.ReplaceAllString(md, "")
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	md = strings.Join(lines, "\n")
	md = unescapeIntraword(md)
	return strings.TrimSpace(blankLinesRe.ReplaceAllString(md, "\n\n"))
}

// unescapeIntraword drops the backslash the converter puts before underscores
// inside words (max\_tokens, get\_user\_by\_id). CommonMark never reads an
// intraword underscore as emphasis, so the escape only costs a token and
// breaks identifiers the model may copy. Code blocks and spans are untouched.
func unescapeIntraword(md string) string {
	if !strings.Contains(md, `\_`) {
		return md
	}
	lines := strings.Split(md, "\n")
	fence := ""
	for i, l := range lines {
		t := strings.TrimLeft(l, " ")
		if fence != "" {
			if strings.HasPrefix(t, fence) && strings.TrimRight(t, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if n := len(t) - len(strings.TrimLeft(t, "`")); n >= 3 {
			fence = t[:n]
			continue
		}
		if n := len(t) - len(strings.TrimLeft(t, "~")); n >= 3 {
			fence = t[:n]
			continue
		}
		if strings.Contains(l, `\_`) {
			lines[i] = unescapeLine(l)
		}
	}
	return strings.Join(lines, "\n")
}

func unescapeLine(l string) string {
	var b strings.Builder
	for i := 0; i < len(l); {
		switch {
		case l[i] == '\\' && i+1 < len(l):
			prev, _ := utf8.DecodeLastRuneInString(b.String())
			next, _ := utf8.DecodeRuneInString(l[i+2:])
			if l[i+1] == '_' && isWordRune(prev) && isWordRune(next) {
				b.WriteByte('_')
			} else {
				b.WriteString(l[i : i+2])
			}
			i += 2
		case l[i] == '`':
			n := len(l[i:]) - len(strings.TrimLeft(l[i:], "`"))
			end := closingTicks(l, i+n, n)
			if end < 0 {
				end = i + n
			}
			b.WriteString(l[i:end])
			i = end
		default:
			b.WriteByte(l[i])
			i++
		}
	}
	return b.String()
}

// closingTicks returns the end of the code span that opened with n backticks
// just before from, or -1 if it is never closed.
func closingTicks(l string, from, n int) int {
	for j := from; j < len(l); {
		if l[j] != '`' {
			j++
			continue
		}
		m := len(l[j:]) - len(strings.TrimLeft(l[j:], "`"))
		if m == n {
			return j + m
		}
		j += m
	}
	return -1
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// --- small html.Node helpers ---

func walk(n *html.Node, fn func(*html.Node)) {
	fn(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, fn)
	}
}

func removeIf(n *html.Node, drop func(*html.Node) bool) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		if drop(c) {
			n.RemoveChild(c)
		} else {
			removeIf(c, drop)
		}
		c = next
	}
}

func unwrap(n *html.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling
		n.RemoveChild(c)
		n.Parent.InsertBefore(c, n)
		c = next
	}
	n.Parent.RemoveChild(n)
}

func findFirst(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := findFirst(c, a); f != nil {
			return f
		}
	}
	return nil
}

func textOf(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	walk(n, func(c *html.Node) {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	})
	return b.String()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func setAttr(n *html.Node, key, val string) {
	for i, a := range n.Attr {
		if a.Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}

func delAttr(n *html.Node, key string) {
	out := n.Attr[:0]
	for _, a := range n.Attr {
		if a.Key != key {
			out = append(out, a)
		}
	}
	n.Attr = out
}
