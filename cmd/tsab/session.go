package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

// builtinTools is the built-in tool set in both arms, so their system prompts
// differ only by tokensaver's tool definitions.
const builtinTools = "Read,Grep,Glob,Bash,WebFetch"

// result is what one headless Claude Code session cost and answered.
type result struct {
	Task, Arm string
	Run       int
	Answer    string
	Correct   bool
	Missing   []string // expected facts not found in the answer
	Error     string   // session-level failure (API, budget, timeout, MCP not connected)

	ContextEnd int     // tokens in the context when the final answer was written
	InputTotal int     // input tokens over all model calls (incl. cached)
	Output     int     // output tokens
	SideTokens int     // tokens used by other models, e.g. WebFetch's page summarizer
	ToolChars  int     // characters of tool results added to the context
	Cost       float64 // USD, as reported by Claude Code
	Turns      int
	Duration   time.Duration
	Tools      map[string]int // tool calls by name (Bash by command)
	Denied     int            // tool calls refused by the permission rules
}

type usage struct {
	Input         int `json:"input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
	Output        int `json:"output_tokens"`
}

func (u usage) context() int { return u.Input + u.CacheCreation + u.CacheRead }

type event struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	MCPServers []struct {
		Name, Status string
	} `json:"mcp_servers"`
	Message struct {
		ID      string            `json:"id"`
		Model   string            `json:"model"`
		Usage   *usage            `json:"usage"`
		Content []json.RawMessage `json:"content"`
	} `json:"message"`
	// result
	IsError    bool    `json:"is_error"`
	Result     string  `json:"result"`
	Cost       float64 `json:"total_cost_usd"`
	Turns      int     `json:"num_turns"`
	DurationMS int     `json:"duration_ms"`
	Usage      *usage  `json:"usage"`
	ModelUsage map[string]struct {
		Input         int `json:"inputTokens"`
		Output        int `json:"outputTokens"`
		CacheRead     int `json:"cacheReadInputTokens"`
		CacheCreation int `json:"cacheCreationInputTokens"`
	} `json:"modelUsage"`
	Denials []json.RawMessage `json:"permission_denials"`
}

type block struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
	Content json.RawMessage `json:"content"`
}

type runner struct {
	claude, model, workDir string
	budget                 float64
	timeout                time.Duration
	mcpConfig              map[string]string // arm → MCP config file
}

// run executes one session and writes its event stream to transcript.
func (r *runner) run(ctx context.Context, arm, prompt string, allow []string, transcript string) (*result, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []string{"-p", "--output-format", "stream-json", "--verbose",
		"--model", r.model,
		"--strict-mcp-config", "--mcp-config", r.mcpConfig[arm],
		"--tools", builtinTools,
		"--permission-mode", "dontAsk",
		"--setting-sources", "",
		"--disable-slash-commands",
		"--no-session-persistence",
		"--max-budget-usd", fmt.Sprint(r.budget)}
	allow = slices.Clone(allow)
	if arm == armTokensaver {
		allow = append(allow, "mcp__tokensaver")
	}
	if len(allow) > 0 {
		args = append(append(args, "--allowedTools"), allow...)
	}
	cmd := exec.CommandContext(ctx, r.claude, args...)
	cmd.Dir = r.workDir
	cmd.Env = childEnv()
	cmd.Stdin = strings.NewReader(prompt) // not an argument: --allowedTools would swallow it
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	if err := os.WriteFile(transcript, stdout.Bytes(), 0o644); err != nil {
		return nil, err
	}
	res := parse(stdout.Bytes(), arm)
	if runErr != nil && res.Error == "" {
		res.Error = strings.TrimSpace(firstLine(stderr.String()) + " " + runErr.Error())
		if ctx.Err() != nil {
			res.Error = "timed out after " + r.timeout.String()
		}
	}
	return res, nil
}

// parse turns a stream-json transcript into a result.
func parse(stream []byte, arm string) *result {
	res := &result{Arm: arm, Tools: map[string]int{}}
	calls := map[string]usage{} // model call id → usage (a message is streamed once per block)
	var order []string
	sc := bufio.NewScanner(bytes.NewReader(stream))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var ev event
		if json.Unmarshal(sc.Bytes(), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "system":
			if ev.Subtype == "init" && arm == armTokensaver {
				status := "missing"
				for _, s := range ev.MCPServers {
					if s.Name == "tokensaver" {
						status = s.Status
					}
				}
				if status != "connected" {
					res.Error = "tokensaver MCP server " + status
				}
			}
		case "assistant":
			m := ev.Message
			if m.Usage != nil && m.ID != "" {
				if _, seen := calls[m.ID]; !seen {
					order = append(order, m.ID)
				}
				calls[m.ID] = *m.Usage
			}
			for _, raw := range m.Content {
				var b block
				if json.Unmarshal(raw, &b) == nil && b.Type == "tool_use" {
					res.Tools[toolLabel(b)]++
				}
			}
		case "user":
			for _, raw := range ev.Message.Content {
				var b block
				if json.Unmarshal(raw, &b) == nil && b.Type == "tool_result" {
					res.ToolChars += len(toolText(b.Content))
				}
			}
		case "result":
			res.Answer = ev.Result
			res.Cost = ev.Cost
			res.Turns = ev.Turns
			res.Duration = time.Duration(ev.DurationMS) * time.Millisecond
			res.Denied = len(ev.Denials)
			if ev.Usage != nil {
				res.Output = ev.Usage.Output
			}
			// The main model is the one that read the most; the others are
			// helpers (WebFetch summarizes pages with a small model).
			total, most := 0, 0
			for _, u := range ev.ModelUsage {
				n := u.Input + u.Output + u.CacheRead + u.CacheCreation
				total += n
				most = max(most, n)
			}
			res.SideTokens = total - most
			if ev.IsError && res.Error == "" {
				res.Error = firstLine(ev.Subtype + ": " + ev.Result)
			}
		}
	}
	for _, id := range order {
		u := calls[id]
		res.InputTotal += u.context()
		res.ContextEnd = max(res.ContextEnd, u.context())
	}
	return res
}

// toolLabel names a tool call for the report: mcp__tokensaver__read → read,
// Bash by its command.
func toolLabel(b block) string {
	name := strings.TrimPrefix(b.Name, "mcp__tokensaver__")
	if b.Name == "Bash" {
		var in struct{ Command string }
		json.Unmarshal(b.Input, &in)
		if f := strings.Fields(in.Command); len(f) > 0 {
			return "Bash(" + f[0] + ")"
		}
	}
	return name
}

// toolText returns the text of a tool result, which is a string or a list of
// content blocks.
func toolText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct{ Text string }
	json.Unmarshal(raw, &parts)
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

// grade checks the answer for every expected fact.
func grade(res *result, expect [][]string) {
	res.Missing = nil
	answer := normalize(res.Answer)
	for _, group := range expect {
		found := false
		for _, alt := range group {
			if phraseRe(normalize(alt)).MatchString(answer) {
				found = true
				break
			}
		}
		if !found {
			res.Missing = append(res.Missing, strings.Join(group, " | "))
		}
	}
	res.Correct = res.Error == "" && len(res.Missing) == 0
}

var digitCommaRe = regexp.MustCompile(`(\d),(\d{3})`)

func normalize(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.NewReplacer("**", "", "`", "").Replace(s)
	s = digitCommaRe.ReplaceAllString(s, "$1$2")
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// phraseRe matches p as a whole phrase: "8" must not match inside "28.4".
func phraseRe(p string) *regexp.Regexp {
	return regexp.MustCompile(`(?:^|[^\pL\pN])` + regexp.QuoteMeta(p) + `(?:$|[^\pL\pN])`)
}

// childEnv is the environment for claude, minus the variables that mark a
// nested session (set when tsab itself runs inside Claude Code).
func childEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(k, "CLAUDE") && k != "CLAUDE_CONFIG_DIR" && k != "CLAUDE_CODE_OAUTH_TOKEN" && !strings.HasPrefix(k, "CLAUDE_CODE_USE_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}

func toolsSummary(tools map[string]int) string {
	var parts []string
	for name, n := range tools {
		if n > 1 {
			name = fmt.Sprintf("%s×%d", name, n)
		}
		parts = append(parts, name)
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
