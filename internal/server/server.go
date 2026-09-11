// Package server exposes tokensaver's converters as MCP tools.
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/MilanBehnam/tokensaver/internal/convert"
	"github.com/MilanBehnam/tokensaver/internal/jsonshrink"
	"github.com/MilanBehnam/tokensaver/internal/source"
	"github.com/MilanBehnam/tokensaver/internal/view"
)

// Tool descriptions and schemas are sent to the model on every turn, so they are
// kept short: every word here costs tokens in every conversation that loads us.

const instructions = `tokensaver turns web pages and files into compact Markdown and shrinks JSON, so far fewer tokens reach the context. Use read for web pages and documents (HTML, PDF, DOCX, XLSX, PPTX) and read_json for JSON APIs and files. Both page long output; outline=true shows structure first.`

type ReadInput struct {
	Source   string `json:"source" jsonschema:"URL (http/https) or local file path"`
	Outline  bool   `json:"outline,omitempty" jsonschema:"Only list the sections (id, heading, size)"`
	Section  string `json:"section,omitempty" jsonschema:"Only return this section: an id from outline, or heading text"`
	Page     int    `json:"page,omitempty" jsonschema:"Page of the output, from 1 (default 1)"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"Page size in characters (default 20000)"`
	JS       bool   `json:"js,omitempty" jsonschema:"Render in headless Chrome first (JavaScript apps); automatic when a page looks empty"`
}

type ReadJSONInput struct {
	Source        string   `json:"source" jsonschema:"URL (http/https, GET) or local file path"`
	Select        string   `json:"select,omitempty" jsonschema:"Fields to keep: items.name maps over arrays; items[0] / items[:5] index or slice; items.{id,owner.login} picks fields; a,b.c picks several"`
	DropKeys      []string `json:"drop_keys,omitempty" jsonschema:"Keys to remove everywhere, names or globs, e.g. [\"*_url\",\"node_id\"]"`
	MaxItems      int      `json:"max_items,omitempty" jsonschema:"Keep only the first N items of every array"`
	DropListsOver int      `json:"drop_lists_over,omitempty" jsonschema:"Replace arrays longer than N with a count (the top-level array is kept)"`
	MaxStr        int      `json:"max_str,omitempty" jsonschema:"Cut strings longer than N characters"`
	Table         bool     `json:"table,omitempty" jsonschema:"Render arrays of objects as Markdown tables"`
	KeepEmpty     bool     `json:"keep_empty,omitempty" jsonschema:"Keep null and empty values (dropped by default)"`
	Outline       bool     `json:"outline,omitempty" jsonschema:"Show the structure (keys, types, array sizes, examples) instead of the data"`
	Page          int      `json:"page,omitempty" jsonschema:"Page of the output, from 1 (default 1)"`
	MaxChars      int      `json:"max_chars,omitempty" jsonschema:"Page size in characters (default 20000)"`
}

const (
	defaultMaxChars = 20000
	minMaxChars     = 500
	maxMaxChars     = 500000
)

// New builds the MCP server. logger receives one line per tool call (on stderr
// in the stdio binary: stdout is the protocol channel).
func New(version string, logger *slog.Logger) *mcp.Server {
	h := &handler{log: logger, cache: newCache(), defaultMax: envInt("TOKENSAVER_MAX_CHARS", defaultMaxChars)}
	s := mcp.NewServer(&mcp.Implementation{Name: "tokensaver", Version: version}, &mcp.ServerOptions{Instructions: instructions})
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read",
		Description: "Read a web page or local file (PDF, DOCX, XLSX, PPTX, HTML, JSON, text) as clean Markdown, with navigation, ads, scripts and styling stripped: far fewer tokens than raw content. Long output is paged; for big documents use outline=true, then section=<id>.",
		Annotations: readOnly,
	}, h.read)
	mcp.AddTool(s, &mcp.Tool{
		Name:        "read_json",
		Description: "Fetch JSON (HTTP GET) or read a JSON file, shrunk before it reaches the context (null/empty values dropped). For big or unfamiliar JSON use outline=true first, then select only the fields you need.",
		Annotations: readOnly,
	}, h.readJSON)
	return s
}

type handler struct {
	log        *slog.Logger
	cache      *cache
	defaultMax int
}

func (h *handler) pageSize(n int) int {
	if n <= 0 {
		return h.defaultMax
	}
	return min(max(n, minMaxChars), maxMaxChars)
}

func (h *handler) read(ctx context.Context, _ *mcp.CallToolRequest, in ReadInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	page := max(in.Page, 1)
	followUp := page > 1 || in.Outline || in.Section != ""
	e, err := h.loadDoc(ctx, in.Source, in.JS, followUp)
	if err != nil {
		h.logCall("read", in.Source, start, 0, err)
		return nil, nil, err
	}
	if e.kind == source.JSON {
		if in.Section != "" {
			return nil, nil, errors.New("section= is for documents; for JSON use read_json with select=")
		}
		root, err := jsonshrink.Parse(e.src.Data)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid JSON: %w", err)
		}
		text, err := h.renderJSON(root, ReadJSONInput{Outline: in.Outline, Page: in.Page, MaxChars: in.MaxChars})
		if err != nil {
			return nil, nil, err
		}
		h.logCall("read", in.Source, start, len(text), nil, "kind", "json")
		return textResult(text), nil, nil
	}
	md := e.doc.Markdown
	var text string
	switch {
	case in.Outline:
		text = view.FormatOutline(md)
	case in.Section != "":
		sec, err := view.Section(md, in.Section)
		if err == nil {
			text, err = view.Page(sec, h.pageSize(in.MaxChars), page, "")
		}
		if err != nil {
			h.logCall("read", in.Source, start, 0, err)
			return nil, nil, err
		}
	default:
		hint := ""
		if len(view.Outline(md)) >= 3 {
			hint = "outline=true lists sections"
		}
		text, err = view.Page(md, h.pageSize(in.MaxChars), page, hint)
		if err != nil {
			h.logCall("read", in.Source, start, 0, err)
			return nil, nil, err
		}
	}
	if e.doc.Note != "" {
		text += "\n\n[note: " + e.doc.Note + "]"
	}
	h.logCall("read", in.Source, start, len(text), nil, "kind", string(e.kind), "doc_chars", len(md), "rendered", e.doc.Rendered)
	return textResult(text), nil, nil
}

// loadDoc returns the converted document for src. A plain first read always
// loads fresh (a dev server's page may have just changed); follow-ups (another
// page, a section, the outline) reuse a recent conversion so page numbers stay
// stable and big PDFs aren't re-parsed.
func (h *handler) loadDoc(ctx context.Context, src string, js, followUp bool) (*cacheEntry, error) {
	key, remote := source.CacheKey(src)
	key += "|js=" + strconv.FormatBool(js)
	if e := h.cache.get(key); e != nil && (followUp || !remote) {
		return e, nil
	}
	s, err := source.Load(ctx, src, "text/html,application/xhtml+xml,application/pdf,*/*;q=0.8")
	if err != nil {
		var se *source.StatusError
		if errors.As(err, &se) {
			return nil, fmt.Errorf("%s%s", err, excerpt(se.Source))
		}
		return nil, err
	}
	kind := source.Detect(s)
	e := &cacheEntry{kind: kind}
	if kind == source.JSON {
		e.src = s // shrunk per call with the caller's options
	} else {
		doc, err := convert.Convert(ctx, s, kind, convert.Options{JS: js})
		if err != nil {
			return nil, err
		}
		e.doc = doc
	}
	h.cache.put(key, e)
	return e, nil
}

func (h *handler) readJSON(ctx context.Context, _ *mcp.CallToolRequest, in ReadJSONInput) (*mcp.CallToolResult, any, error) {
	start := time.Now()
	root, err := h.loadJSON(ctx, in.Source, in.Page > 1)
	if err == nil {
		var text string
		if text, err = h.renderJSON(root, in); err == nil {
			h.logCall("read_json", in.Source, start, len(text), nil)
			return textResult(text), nil, nil
		}
	}
	h.logCall("read_json", in.Source, start, 0, err)
	return nil, nil, err
}

// renderJSON applies the shrink options (or builds the structure outline) and
// returns the requested page.
func (h *handler) renderJSON(root *jsonshrink.Node, in ReadJSONInput) (string, error) {
	var text string
	if in.Outline {
		n := root
		if in.Select != "" {
			var err error
			if n, err = jsonshrink.Apply(root, jsonshrink.Options{Select: in.Select, KeepEmpty: true}); err != nil {
				return "", err
			}
		}
		text = fmt.Sprintf("JSON structure (~%d tokens as compact JSON). Use select= to keep only what you need.\n%s",
			view.EstTokens(n.Compact()), jsonshrink.Shape(n))
	} else {
		n, err := jsonshrink.Apply(root, jsonshrink.Options{
			Select:        in.Select,
			DropKeys:      in.DropKeys,
			KeepEmpty:     in.KeepEmpty,
			MaxStr:        in.MaxStr,
			DropListsOver: in.DropListsOver,
			MaxItems:      in.MaxItems,
		})
		if err != nil {
			return "", err
		}
		text = jsonshrink.Render(n, in.Table)
	}
	return view.Page(text, h.pageSize(in.MaxChars), max(in.Page, 1), "outline=true shows the structure; select= narrows it")
}

// loadJSON always fetches fresh for page 1 (APIs change, especially the one the
// agent is developing); later pages reuse the cached response.
func (h *handler) loadJSON(ctx context.Context, src string, followUp bool) (*jsonshrink.Node, error) {
	key, remote := source.CacheKey(src)
	key += "|json"
	var s *source.Source
	if e := h.cache.get(key); e != nil && (followUp || !remote) {
		s = e.src
	} else {
		var err error
		s, err = source.Load(ctx, src, "application/json, */*;q=0.5")
		if err != nil {
			var se *source.StatusError
			if errors.As(err, &se) {
				if body, perr := jsonshrink.Parse(se.Source.Data); perr == nil {
					return nil, fmt.Errorf("%s: %s", err, clip(jsonshrink.Render(body, false), 2000))
				}
				return nil, fmt.Errorf("%s%s", err, excerpt(se.Source))
			}
			return nil, err
		}
		h.cache.put(key, &cacheEntry{kind: source.JSON, src: s})
	}
	root, err := jsonshrink.Parse(s.Data)
	if err != nil {
		kind := source.Detect(s)
		if kind != source.JSON && kind != source.Unknown && kind != source.Text {
			return nil, fmt.Errorf("not JSON (looks like %s); use the read tool instead", kind)
		}
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return root, nil
}

// excerpt is a short plain-text preview of an error response body.
func excerpt(s *source.Source) string {
	if s == nil || len(s.Data) == 0 {
		return ""
	}
	text := s.UTF8()
	if source.Detect(s) == source.HTML {
		if doc, err := convert.Convert(context.Background(), s, source.HTML, convert.Options{}); err == nil {
			text = doc.Markdown
		}
	}
	return ": " + clip(strings.Join(strings.Fields(text), " "), 300)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func (h *handler) logCall(tool, src string, start time.Time, outChars int, err error, attrs ...any) {
	if h.log == nil {
		return
	}
	args := append([]any{"source", src, "out_chars", outChars, "took", time.Since(start).Round(time.Millisecond)}, attrs...)
	if err != nil {
		h.log.Warn(tool+" failed", append(args, "err", err)...)
		return
	}
	h.log.Info(tool, args...)
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}
