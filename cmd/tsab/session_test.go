package main

import (
	"strings"
	"testing"
)

// A trimmed stream-json transcript in the shape `claude -p --output-format
// stream-json --verbose` writes: one assistant event per content block (same
// message id and usage), tool results as user events, then the result.
const transcript = `{"type":"system","subtype":"init","tools":["Read","mcp__tokensaver__read"],"mcp_servers":[{"name":"tokensaver","status":"connected"}],"model":"claude-sonnet-5"}
{"type":"assistant","message":{"id":"msg_1","model":"claude-sonnet-5","content":[{"type":"text","text":"Let me read it."}],"usage":{"input_tokens":3,"cache_creation_input_tokens":1200,"cache_read_input_tokens":18000,"output_tokens":40}}}
{"type":"assistant","message":{"id":"msg_1","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"t1","name":"mcp__tokensaver__read","input":{"source":"/tmp/a.pdf"}}],"usage":{"input_tokens":3,"cache_creation_input_tokens":1200,"cache_read_input_tokens":18000,"output_tokens":40}}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"# Paper\n\nBLEU 28.4"}]}]}}
{"type":"assistant","message":{"id":"msg_2","model":"claude-sonnet-5","content":[{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"textutil -convert txt -stdout x.docx"}}],"usage":{"input_tokens":5,"cache_creation_input_tokens":300,"cache_read_input_tokens":19200,"output_tokens":30}}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"t2","content":"plain text result"}]}}
{"type":"assistant","message":{"id":"msg_3","model":"claude-sonnet-5","content":[{"type":"text","text":"It scores 28.4 BLEU with 8 heads."}],"usage":{"input_tokens":2,"cache_creation_input_tokens":100,"cache_read_input_tokens":19500,"output_tokens":20}}}
{"type":"result","subtype":"success","is_error":false,"result":"It scores **28.4** BLEU and the base model uses 8 attention heads.","total_cost_usd":0.0421,"num_turns":3,"duration_ms":12500,"usage":{"output_tokens":90},"modelUsage":{"claude-sonnet-5":{"inputTokens":10,"outputTokens":90,"cacheReadInputTokens":56700,"cacheCreationInputTokens":1600},"claude-haiku-4-5":{"inputTokens":4000,"outputTokens":200}},"permission_denials":[{"tool_name":"Bash"}]}
`

func TestParse(t *testing.T) {
	r := parse([]byte(transcript), armTokensaver)
	want := result{
		Arm: armTokensaver, Answer: "It scores **28.4** BLEU and the base model uses 8 attention heads.",
		ContextEnd: 2 + 100 + 19500,
		InputTotal: (3 + 1200 + 18000) + (5 + 300 + 19200) + (2 + 100 + 19500), // msg_1 counted once
		Output:     90, SideTokens: 4200, ToolChars: len("# Paper\n\nBLEU 28.4") + len("plain text result"),
		Cost: 0.0421, Turns: 3, Denied: 1,
	}
	if r.Error != "" || r.ContextEnd != want.ContextEnd || r.InputTotal != want.InputTotal || r.Output != want.Output ||
		r.SideTokens != want.SideTokens || r.ToolChars != want.ToolChars || r.Cost != want.Cost || r.Turns != want.Turns ||
		r.Denied != want.Denied || r.Answer != want.Answer || r.Duration.Seconds() != 12.5 {
		t.Errorf("parse:\n got %+v\nwant %+v", *r, want)
	}
	if got := toolsSummary(r.Tools); got != "Bash(textutil), read" {
		t.Errorf("tools = %q", got)
	}

	grade(r, [][]string{{"28.4"}, {"8 heads", "8 attention heads"}})
	if !r.Correct {
		t.Errorf("should be correct, missing %v", r.Missing)
	}
	grade(r, [][]string{{"8"}, {"27.3"}}) // "8" alone does match "8 attention"; 27.3 is absent
	if r.Correct || strings.Join(r.Missing, ";") != "27.3" {
		t.Errorf("missing = %v", r.Missing)
	}
}

func TestParseFailures(t *testing.T) {
	notConnected := strings.Replace(transcript, `"status":"connected"`, `"status":"failed"`, 1)
	if r := parse([]byte(notConnected), armTokensaver); r.Error != "tokensaver MCP server failed" {
		t.Errorf("error = %q", r.Error)
	}
	if r := parse([]byte(notConnected), armBuiltin); r.Error != "" {
		t.Errorf("builtin arm doesn't need tokensaver: %q", r.Error)
	}
	authFail := `{"type":"result","subtype":"success","is_error":true,"result":"Failed to authenticate: OAuth session expired"}`
	if r := parse([]byte(authFail), armBuiltin); !strings.Contains(r.Error, "Failed to authenticate") {
		t.Errorf("error = %q", r.Error)
	}
}

func TestPhraseMatching(t *testing.T) {
	cases := []struct {
		answer, phrase string
		want           bool
	}{
		{"It scored 28.4 BLEU.", "8", false},
		{"with 8 heads", "8 heads", true},
		{"Headcount was 1,284 people", "1284", true},
		{"approved by **Dana Whitfield**", "Dana Whitfield", true},
		{"`func Unmarshal(data []byte, v any) error`", "func Unmarshal(data []byte, v any) error", true},
		{"use map[string]any", "map[string]any", true},
		{"Go 1.18 added generics", "1.18", true},
		{"Go 1.180", "1.18", false},
		{"golang/go leads", "golang/go", true},
		{"golang/gopls", "golang/go", false},
	}
	for _, c := range cases {
		if got := phraseRe(normalize(c.phrase)).MatchString(normalize(c.answer)); got != c.want {
			t.Errorf("%q in %q = %v, want %v", c.phrase, c.answer, got, c.want)
		}
	}
}
