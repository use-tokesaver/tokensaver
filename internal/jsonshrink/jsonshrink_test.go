package jsonshrink

import (
	"fmt"
	"strings"
	"testing"
)

const repos = `{
  "total_count": 3,
  "incomplete_results": false,
  "items": [
    {"id": 1, "name": "alpha", "node_id": "MDQ6", "html_url": "https://github.com/o/alpha",
     "owner": {"login": "octo", "avatar_url": "https://a/1", "id": 9},
     "description": null, "topics": ["go", "mcp"], "license": {"key": "mit"}},
    {"id": 2, "name": "beta", "node_id": "MDQ7", "html_url": "https://github.com/o/beta",
     "owner": {"login": "cat", "avatar_url": "https://a/2", "id": 8},
     "description": "Second <repo> & co", "topics": [], "license": null},
    {"id": 3, "name": "gamma", "node_id": "MDQ8", "html_url": "https://github.com/o/gamma",
     "owner": {"login": "dog", "avatar_url": "https://a/3", "id": 7},
     "description": "", "topics": ["x"]}
  ]
}`

func mustParse(t *testing.T, s string) *Node {
	t.Helper()
	n, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func shrink(t *testing.T, o Options) string {
	t.Helper()
	n, err := Apply(mustParse(t, repos), o)
	if err != nil {
		t.Fatal(err)
	}
	return n.Compact()
}

func TestParseKeepsOrderAndNumbers(t *testing.T) {
	n := mustParse(t, `{"z":1,"a":12345678901234567890,"m":[1.50,true,null]}`)
	if got := n.Compact(); got != `{"z":1,"a":12345678901234567890,"m":[1.50,true,null]}` {
		t.Fatalf("round trip = %s", got)
	}
}

func TestParseJSONLines(t *testing.T) {
	n := mustParse(t, "{\"a\":1}\n{\"a\":2}\n")
	if got := n.Compact(); got != `[{"a":1},{"a":2}]` {
		t.Fatalf("ndjson = %s", got)
	}
}

func TestNoHTMLEscaping(t *testing.T) {
	got := shrink(t, Options{Select: "items[1].description"})
	if got != `"Second <repo> & co"` {
		t.Fatalf("got %s", got)
	}
}

func TestSelect(t *testing.T) {
	cases := map[string]string{
		"total_count":                     `3`,
		"items.name":                      `["alpha","beta","gamma"]`,
		"items[].name":                    `["alpha","beta","gamma"]`,
		"$.items[*].owner.login":          `["octo","cat","dog"]`,
		".items[0].name":                  `"alpha"`,
		"items[-1].id":                    `3`,
		"items[0:2].id":                   `[1,2]`,
		"items[:1].{name, owner.login}":   `[{"name":"alpha","owner.login":"octo"}]`,
		"items[0].{who: owner.login, id}": `{"who":"octo","id":1}`,
		"total_count, items.id":           `{"total_count":3,"items.id":[1,2,3]}`,
		"items.name[1]":                   `"beta"`,
		`items[0]["html_url"]`:            `"https://github.com/o/alpha"`,
		"n: total_count":                  `{"n":3}`,
		"items[0].topics[0:5]":            `["go","mcp"]`,
		"items.topics[0]":                 `["go","mcp"]`,
		"items[].topics[0]":               `["go","x"]`,
		"items.{name,license.key}[0]":     `{"name":"alpha","license.key":"mit"}`,
		"items[0].owner[]":                `["octo","https://a/1",9]`,
		"items.{name,missing}":            `[{"name":"alpha"},{"name":"beta"},{"name":"gamma"}]`,
		"items[0].{name}.name":            `"alpha"`,
		"items[5:10]":                     ``, // empty array → dropped as empty
	}
	for sel, want := range cases {
		n, err := Apply(mustParse(t, repos), Options{Select: sel, KeepEmpty: true})
		if err != nil {
			t.Errorf("select %q: %v", sel, err)
			continue
		}
		got := n.Compact()
		if want == "" {
			want = "[]"
		}
		if got != want {
			t.Errorf("select %q = %s, want %s", sel, got, want)
		}
	}
}

func TestSelectErrors(t *testing.T) {
	_, err := Apply(mustParse(t, repos), Options{Select: "itemz"})
	if err == nil || !strings.Contains(err.Error(), "top-level keys: total_count, incomplete_results, items") {
		t.Fatalf("no-match error = %v", err)
	}
	if _, err := Apply(mustParse(t, repos), Options{Select: "items.{name"}); err == nil {
		t.Fatal("expected parse error for unclosed brace")
	}
	if _, err := Apply(mustParse(t, repos), Options{DropKeys: []string{"[a"}}); err == nil {
		t.Fatal("expected bad glob error")
	}
}

func TestDropEmptyIsDefault(t *testing.T) {
	got := shrink(t, Options{Select: "items[1]"})
	if strings.Contains(got, "topics") || strings.Contains(got, "license") {
		t.Fatalf("empty values kept: %s", got)
	}
	got = shrink(t, Options{Select: "items[1]", KeepEmpty: true})
	if !strings.Contains(got, `"topics":[]`) || !strings.Contains(got, `"license":null`) {
		t.Fatalf("KeepEmpty lost values: %s", got)
	}
}

func TestDropKeys(t *testing.T) {
	got := shrink(t, Options{DropKeys: []string{"*_url", "node_id"}})
	if strings.Contains(got, "_url") || strings.Contains(got, "node_id") {
		t.Fatalf("keys not dropped: %s", got)
	}
	if !strings.Contains(got, `"login":"octo"`) {
		t.Fatalf("dropped too much: %s", got)
	}
}

func TestListLimits(t *testing.T) {
	big := `{"meta":{"page":1},"rows":[` + strings.TrimSuffix(strings.Repeat(`{"v":1},`, 50), ",") + `],"tags":["a","b"]}`
	n, _ := Apply(mustParse(t, big), Options{DropListsOver: 10})
	if got := n.Compact(); got != `{"meta":{"page":1},"rows":"[50 items omitted]","tags":["a","b"]}` {
		t.Fatalf("drop_lists_over = %s", got)
	}
	n, _ = Apply(mustParse(t, big), Options{MaxItems: 2})
	if got := n.Compact(); got != `{"meta":{"page":1},"rows":[{"v":1},{"v":1},"… 48 more items"],"tags":["a","b"]}` {
		t.Fatalf("max_items = %s", got)
	}
	// The root list itself is never dropped, only truncated.
	n, _ = Apply(mustParse(t, `[1,2,3,4,5]`), Options{DropListsOver: 2, MaxItems: 3})
	if got := n.Compact(); got != `[1,2,3,"… 2 more items"]` {
		t.Fatalf("root list = %s", got)
	}
}

func TestMaxStr(t *testing.T) {
	n, _ := Apply(mustParse(t, `{"body":"héllo wörld, this is long"}`), Options{MaxStr: 5})
	if got := n.Compact(); got != `{"body":"héllo…(+20 chars)"}` {
		t.Fatalf("max_str = %s", got)
	}
}

func TestTable(t *testing.T) {
	n, _ := Apply(mustParse(t, repos), Options{Select: "items.{name, owner.login, topics}", MaxItems: 2})
	got := Render(n, true)
	want := "| name | owner.login | topics |\n|---|---|---|\n| alpha | octo | go, mcp |\n| beta | cat | |\n… 1 more items"
	if got != want {
		t.Fatalf("table:\n%s\nwant:\n%s", got, want)
	}
	// Objects render scalars as key: value and their record lists as tables.
	n, _ = Apply(mustParse(t, repos), Options{Select: "total_count, items.{id,name}"})
	got = Render(n, true)
	if !strings.HasPrefix(got, "total_count: 3\n\nitems.{id,name}:\n| id | name |") {
		t.Fatalf("object table:\n%s", got)
	}
	// Non-tabular input falls back to JSON.
	if got := Render(mustParse(t, `[1,2]`), true); got != `[1,2]` {
		t.Fatalf("fallback = %s", got)
	}
}

func TestPrettyWrapsOnlyBigContainers(t *testing.T) {
	var items []string
	for i := range 10 {
		items = append(items, fmt.Sprintf(`{"id":%d,"name":"repository-number-%d"}`, i, i))
	}
	n := mustParse(t, `{"ok":true,"items":[`+strings.Join(items, ",")+`]}`)
	got := n.Pretty()
	if !strings.Contains(got, "\n  {\"id\":0,\"name\":\"repository-number-0\"},\n") {
		t.Fatalf("pretty:\n%s", got)
	}
	if strings.Count(got, "\n") != 14 {
		t.Fatalf("expected one line per item plus framing, got:\n%s", got)
	}
	if mustParse(t, `{"a":[1,2,3]}`).Pretty() != `{"a":[1,2,3]}` {
		t.Fatal("small containers should stay inline")
	}
}

func TestShape(t *testing.T) {
	got := Shape(mustParse(t, repos))
	for _, want := range []string{
		"total_count: number (3)",
		"items: [3] {",
		`name: string ("alpha")`,
		`owner: {login: string ("octo"), avatar_url: string ("https://a/1"), id: number (9)}`,
		"license?: {key: string (\"mit\")}|null",
		"topics: [0-2] string (\"go\")",
		"description: string (\"Second <repo> & co\")|null",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("shape missing %q:\n%s", want, got)
		}
	}
}
