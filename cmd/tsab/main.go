// Command tsab measures whether tokensaver saves tokens in real Claude Code
// sessions. It asks the same questions about the same sources in two arms and
// compares what Claude Code itself reports:
//
//   - builtin:    Claude Code with its own tools (WebFetch, Read, Bash);
//   - tokensaver: the same, plus the tokensaver MCP server, and the prompt says
//     to read sources with it.
//
// Answers are graded by string match. Every session is a real, billed
// `claude -p` run, so tsab never runs as part of `go test`.
//
//	go run ./cmd/tsab                        # all tasks, one run per arm
//	go run ./cmd/tsab -only pdf-paper -runs 3
//	go run ./cmd/tsab -list
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MilanBehnam/tokensaver/internal/source"
	"github.com/MilanBehnam/tokensaver/internal/testdoc"
)

const (
	armBuiltin    = "builtin"
	armTokensaver = "tokensaver"
	paperURL      = "https://arxiv.org/pdf/1706.03762"
)

var arms = []string{armBuiltin, armTokensaver}

func main() {
	model := flag.String("model", "sonnet", "model for both arms")
	runs := flag.Int("runs", 1, "sessions per task and arm")
	only := flag.String("only", "", "comma-separated task names to run (default: all)")
	out := flag.String("out", "ab-results", "directory for reports and transcripts")
	budget := flag.Float64("budget", 1, "max USD per session (claude --max-budget-usd)")
	timeout := flag.Duration("timeout", 6*time.Minute, "max time per session")
	claudeBin := flag.String("claude", "claude", "Claude Code CLI to run")
	list := flag.Bool("list", false, "list the tasks and exit")
	flag.Parse()

	selected := tasks
	if *only != "" {
		names := strings.Split(*only, ",")
		selected = slices.DeleteFunc(slices.Clone(tasks), func(t task) bool { return !slices.Contains(names, t.name) })
		if len(selected) == 0 {
			fail("no task matches -only %q; see -list", *only)
		}
	}
	if *list {
		for _, t := range tasks {
			fmt.Printf("%-20s %-5s %s\n", t.name, t.kind, t.prompt)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := preflight(*claudeBin); err != nil {
		fail("%v", err)
	}

	dir, err := filepath.Abs(filepath.Join(*out, time.Now().Format("2006-01-02_150405")))
	check(err)
	check(os.MkdirAll(filepath.Join(dir, "transcripts"), 0o755))
	workDir, err := os.MkdirTemp("", "tsab-work-") // outside the repo: no CLAUDE.md, no git
	check(err)
	defer os.RemoveAll(workDir)

	logf("building tokensaver")
	bin := filepath.Join(dir, "tokensaver")
	if b, err := exec.Command("go", "build", "-o", bin, "github.com/MilanBehnam/tokensaver/cmd/tokensaver").CombinedOutput(); err != nil {
		fail("build tokensaver: %v\n%s", err, b)
	}
	r := &runner{claude: *claudeBin, model: *model, workDir: workDir, budget: *budget, timeout: *timeout,
		mcpConfig: map[string]string{
			armBuiltin:    writeJSON(filepath.Join(dir, "mcp-builtin.json"), map[string]any{"mcpServers": map[string]any{}}),
			armTokensaver: writeJSON(filepath.Join(dir, "mcp-tokensaver.json"), map[string]any{"mcpServers": map[string]any{"tokensaver": map[string]any{"type": "stdio", "command": bin}}}),
		}}
	check(prepareFiles(workDir, filepath.Join(*out, "cache"), selected))

	// Calibration: the context of a session that only says OK is the fixed cost
	// of each arm (system prompt + tool definitions), before reading anything.
	overhead := map[string]int{}
	for _, arm := range arms {
		logf("calibrating %s", arm)
		res, err := r.run(ctx, arm, "Reply with just the word OK.", nil, filepath.Join(dir, "transcripts", "calibration-"+arm+".jsonl"))
		check(err)
		if res.Error != "" {
			fail("calibration session (%s) failed: %s", arm, res.Error)
		}
		overhead[arm] = res.ContextEnd
	}

	var results []*result
	total := len(selected) * len(arms) * *runs
	for _, t := range selected {
		expect := t.expect
		if t.expectFrom != nil {
			if expect, err = t.expectFrom(); err != nil {
				logf("skipping %s: %v", t.name, err)
				continue
			}
		}
		prompt := t.prompt
		if t.file != "" {
			prompt = fmt.Sprintf(prompt, filepath.Join(workDir, t.file))
		}
		for run := 1; run <= *runs; run++ {
			order := arms
			if run%2 == 0 { // alternate who goes first, so neither arm always gets the warmer cache
				order = []string{arms[1], arms[0]}
			}
			for _, arm := range order {
				if ctx.Err() != nil {
					break
				}
				p := prompt
				if arm == armTokensaver {
					p += "\n\nRead sources with the tokensaver MCP tools (read, read_json)."
				}
				logf("[%d/%d] %s · %s · run %d", len(results)+1, total, t.name, arm, run)
				res, err := r.run(ctx, arm, p, t.allow, filepath.Join(dir, "transcripts", fmt.Sprintf("%s-%s-%d.jsonl", t.name, arm, run)))
				check(err)
				res.Task, res.Run = t.name, run
				grade(res, expect)
				results = append(results, res)
				logf("        %s  context %s, cost $%.3f, %s, tools: %s", verdict(res), kTokens(res.ContextEnd), res.Cost, res.Duration.Round(time.Second), toolsSummary(res.Tools))
			}
		}
	}
	report := render(results, overhead, *model, *runs)
	check(os.WriteFile(filepath.Join(dir, "report.md"), []byte(report), 0o644))
	writeJSON(filepath.Join(dir, "results.json"), results)
	fmt.Print(report)
	logf("report, results and transcripts: %s", dir)
}

// preflight checks that Claude Code is installed and signed in.
func preflight(claudeBin string) error {
	if _, err := exec.LookPath(claudeBin); err != nil {
		return fmt.Errorf("Claude Code CLI %q not found: install it from https://claude.com/claude-code", claudeBin)
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" || os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" {
		return nil
	}
	cmd := exec.Command(claudeBin, "auth", "status")
	cmd.Env = childEnv()
	b, _ := cmd.Output()
	var st struct{ LoggedIn bool }
	if json.Unmarshal(b, &st) == nil && !st.LoggedIn {
		return errors.New("the Claude Code CLI is not signed in: run `claude auth login` (or set ANTHROPIC_API_KEY) and try again")
	}
	return nil
}

// prepareFiles writes the local documents the tasks ask about into dir. The
// paper is downloaded once and kept in cache.
func prepareFiles(dir, cache string, selected []task) error {
	for _, t := range selected {
		var data []byte
		switch t.file {
		case "":
			continue
		case "annual-report.docx":
			data = testdoc.ReportDOCX()
		case "inventory.xlsx":
			data = testdoc.InventoryXLSX(400)
		case "attention.pdf":
			cached := filepath.Join(cache, "attention.pdf")
			var err error
			if data, err = os.ReadFile(cached); err != nil {
				logf("downloading %s", paperURL)
				if data, err = fetch(paperURL); err != nil {
					return fmt.Errorf("download the paper: %w", err)
				}
				os.MkdirAll(cache, 0o755)
				os.WriteFile(cached, data, 0o644)
			}
		default:
			return fmt.Errorf("task %s: no fixture %q", t.name, t.file)
		}
		if err := os.WriteFile(filepath.Join(dir, t.file), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func fetch(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 50<<20))
}

// render builds the Markdown report.
func render(results []*result, overhead map[string]int, model string, runs int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# tokensaver A/B: %s, model %s, %d run(s) per task and arm\n\n", time.Now().Format("2006-01-02 15:04"), model, runs)
	fmt.Fprintf(&b, "Fixed cost per request before reading anything (system prompt + tool definitions): builtin %s tokens, tokensaver %s (%+d for tokensaver's tools).\n\n",
		kTokens(overhead[armBuiltin]), kTokens(overhead[armTokensaver]), overhead[armTokensaver]-overhead[armBuiltin])
	b.WriteString("**Context** is the conversation size when the answer was written, minus that arm's fixed cost: what reading the source added. " +
		"**Input** is all input tokens over all model calls (cached included). **Side** is tokens spent by helper models (WebFetch summarizes pages with a small model). " +
		"**Tool text** is characters tools returned as text (a PDF read natively counts only in Context).\n\n")
	b.WriteString("| Task | Arm | Correct | Context | Input | Output | Side | Tool text | Cost | Turns | Time | Tools |\n")
	b.WriteString("|---|---|---|--:|--:|--:|--:|--:|--:|--:|--:|---|\n")
	for _, r := range results {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | $%.3f | %d | %s | %s |\n",
			r.Task, r.Arm, verdict(r), kTokens(r.ContextEnd-overhead[r.Arm]), kTokens(r.InputTotal), kTokens(r.Output),
			kTokens(r.SideTokens), kTokens(r.ToolChars), r.Cost, r.Turns, r.Duration.Round(time.Second), toolsSummary(r.Tools))
	}

	b.WriteString("\n## Per task (mean over runs)\n\n| Task | Context builtin → tokensaver | Input builtin → tokensaver | Cost builtin → tokensaver | Correct builtin / tokensaver |\n|---|--:|--:|--:|---|\n")
	type agg struct {
		ctx, input, n, correct int
		cost                   float64
	}
	byTask := map[string]map[string]*agg{}
	var taskOrder []string
	totals := map[string]*agg{armBuiltin: {}, armTokensaver: {}}
	for _, r := range results {
		if byTask[r.Task] == nil {
			byTask[r.Task] = map[string]*agg{armBuiltin: {}, armTokensaver: {}}
			taskOrder = append(taskOrder, r.Task)
		}
		for _, a := range []*agg{byTask[r.Task][r.Arm], totals[r.Arm]} {
			a.ctx += r.ContextEnd - overhead[r.Arm]
			a.input += r.InputTotal
			a.cost += r.Cost
			a.n++
			if r.Correct {
				a.correct++
			}
		}
	}
	change := func(before, after float64) string {
		if before <= 0 {
			return ""
		}
		return fmt.Sprintf(" (%+.0f%%)", 100*(after-before)/before)
	}
	for _, name := range taskOrder {
		bi, ts := byTask[name][armBuiltin], byTask[name][armTokensaver]
		if bi.n == 0 || ts.n == 0 {
			continue
		}
		fmt.Fprintf(&b, "| %s | %s → %s%s | %s → %s%s | $%.3f → $%.3f%s | %d/%d · %d/%d |\n", name,
			kTokens(bi.ctx/bi.n), kTokens(ts.ctx/ts.n), change(float64(bi.ctx), float64(ts.ctx)),
			kTokens(bi.input/bi.n), kTokens(ts.input/ts.n), change(float64(bi.input), float64(ts.input)),
			bi.cost/float64(bi.n), ts.cost/float64(ts.n), change(bi.cost, ts.cost),
			bi.correct, bi.n, ts.correct, ts.n)
	}
	bi, ts := totals[armBuiltin], totals[armTokensaver]
	fmt.Fprintf(&b, "| **all** | %s → %s%s | %s → %s%s | $%.3f → $%.3f%s | %d/%d · %d/%d |\n",
		kTokens(bi.ctx), kTokens(ts.ctx), change(float64(bi.ctx), float64(ts.ctx)),
		kTokens(bi.input), kTokens(ts.input), change(float64(bi.input), float64(ts.input)),
		bi.cost, ts.cost, change(bi.cost, ts.cost), bi.correct, bi.n, ts.correct, ts.n)

	var notes []string
	for _, r := range results {
		switch {
		case r.Error != "":
			notes = append(notes, fmt.Sprintf("- %s · %s · run %d: error: %s", r.Task, r.Arm, r.Run, r.Error))
		case len(r.Missing) > 0:
			notes = append(notes, fmt.Sprintf("- %s · %s · run %d: answer lacks %s", r.Task, r.Arm, r.Run, strings.Join(r.Missing, "; ")))
		}
		if r.Denied > 0 {
			notes = append(notes, fmt.Sprintf("- %s · %s · run %d: %d tool call(s) refused by the permission rules", r.Task, r.Arm, r.Run, r.Denied))
		}
	}
	if len(notes) > 0 {
		b.WriteString("\n## Notes\n\n" + strings.Join(notes, "\n") + "\n")
	}
	return b.String()
}

func verdict(r *result) string {
	switch {
	case r.Error != "":
		return "error"
	case r.Correct:
		return "yes"
	default:
		return "no"
	}
}

func kTokens(n int) string {
	if n >= 10000 || n <= -10000 {
		return fmt.Sprintf("%.0fk", float64(n)/1000)
	}
	if n >= 1000 || n <= -1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprint(n)
}

func writeJSON(path string, v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	check(err)
	check(os.WriteFile(path, b, 0o644))
	return path
}

func logf(format string, args ...any) { fmt.Fprintf(os.Stderr, "tsab: "+format+"\n", args...) }

func fail(format string, args ...any) {
	logf(format, args...)
	os.Exit(1)
}

func check(err error) {
	if err != nil {
		fail("%v", err)
	}
}
