package builtin

import (
	"encoding/json"
	"testing"
)

func TestUnmarshalArgs_ValidJSON(t *testing.T) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	err := unmarshalArgs(json.RawMessage(`{"path":"3.txt","content":"hello"}`), &p)
	if err != nil {
		t.Fatalf("valid JSON should parse: %v", err)
	}
	if p.Path != "3.txt" || p.Content != "hello" {
		t.Errorf("got %+v", p)
	}
}

func TestUnmarshalArgs_TripleQuotes(t *testing.T) {
	var p struct {
		Content string `json:"content"`
		Path    string `json:"path"`
	}
	raw := json.RawMessage(`{"content": """你好天下人""", "path": """3.txt"""}`)
	err := unmarshalArgs(raw, &p)
	if err != nil {
		t.Fatalf("triple-quoted JSON should be repaired: %v", err)
	}
	if p.Content != "你好天下人" {
		t.Errorf("content = %q, want %q", p.Content, "你好天下人")
	}
	if p.Path != "3.txt" {
		t.Errorf("path = %q, want %q", p.Path, "3.txt")
	}
}

func TestUnmarshalArgs_TrailingComma(t *testing.T) {
	var p struct {
		A int `json:"a"`
		B int `json:"b"`
	}
	raw := json.RawMessage(`{"a": 1, "b": 2,}`)
	err := unmarshalArgs(raw, &p)
	if err != nil {
		t.Fatalf("trailing comma should be repaired: %v", err)
	}
	if p.A != 1 || p.B != 2 {
		t.Errorf("got %+v", p)
	}
}

func TestUnmarshalArgs_SingleQuotes(t *testing.T) {
	var p struct {
		Name string `json:"name"`
	}
	raw := json.RawMessage(`{'name': 'hello'}`)
	err := unmarshalArgs(raw, &p)
	if err != nil {
		t.Fatalf("single-quoted JSON should be repaired: %v", err)
	}
	if p.Name != "hello" {
		t.Errorf("name = %q, want %q", p.Name, "hello")
	}
}

func TestUnmarshalArgs_OuterQuotes(t *testing.T) {
	var p struct {
		Path string `json:"path"`
	}
	// Model wrapped the entire JSON in extra quotes
	raw := json.RawMessage(`"{\"path\":\"test.txt\"}"`)
	err := unmarshalArgs(raw, &p)
	if err != nil {
		t.Fatalf("outer-quoted JSON should be repaired: %v", err)
	}
	if p.Path != "test.txt" {
		t.Errorf("path = %q, want %q", p.Path, "test.txt")
	}
}

func TestUnmarshalArgs_ChineseContent(t *testing.T) {
	var p struct {
		Pattern string `json:"pattern"`
	}
	raw := json.RawMessage(`{"pattern":"@游戏攻略"}`)
	err := unmarshalArgs(raw, &p)
	if err != nil {
		t.Fatalf("Chinese content should parse: %v", err)
	}
	if p.Pattern != "@游戏攻略" {
		t.Errorf("pattern = %q, want %q", p.Pattern, "@游戏攻略")
	}
}

func TestUnmarshalArgs_Irreparable(t *testing.T) {
	var p struct {
		Path string `json:"path"`
	}
	raw := json.RawMessage(`not json at all`)
	err := unmarshalArgs(raw, &p)
	if err == nil {
		t.Fatal("irreparable JSON should still return an error")
	}
}

func TestUnmarshalArgs_ComboTripleQuotesAndTrailingComma(t *testing.T) {
	var p struct {
		Content string `json:"content"`
		Path    string `json:"path"`
	}
	raw := json.RawMessage(`{"content": """hello""", "path": """3.txt""",}`)
	err := unmarshalArgs(raw, &p)
	if err != nil {
		t.Fatalf("combined repairs should work: %v", err)
	}
	if p.Content != "hello" || p.Path != "3.txt" {
		t.Errorf("got %+v", p)
	}
}

func TestFixTripleQuotes(t *testing.T) {
	got := fixTripleQuotes(`{"a": """hello""", "b": """world"""}`)
	want := `{"a": "hello", "b": "world"}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripTrailingCommas(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`{"a": 1,}`, `{"a": 1}`},
		{`{"a": 1, "b": 2,}`, `{"a": 1, "b": 2}`},
		{`[1, 2, 3,]`, `[1, 2, 3]`},
		{`{"a": "hello,", "b": 2}`, `{"a": "hello,", "b": 2}`}, // comma inside string preserved
		{`{"a": [1, 2,],}`, `{"a": [1, 2]}`},
	}
	for _, tt := range tests {
		got := stripTrailingCommas(tt.in)
		if got != tt.want {
			t.Errorf("stripTrailingCommas(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFixSingleQuotes(t *testing.T) {
	got := fixSingleQuotes(`{'name': 'hello', 'count': 5}`)
	want := `{"name": "hello", "count": 5}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestUnmarshalArgs_MarkdownFence(t *testing.T) {
	var p struct {
		Path string `json:"path"`
	}
	raw := json.RawMessage("```json\n{\"path\":\"test.txt\"}\n```")
	err := unmarshalArgs(raw, &p)
	if err != nil {
		t.Fatalf("markdown-fenced JSON should be repaired: %v", err)
	}
	if p.Path != "test.txt" {
		t.Errorf("path = %q, want %q", p.Path, "test.txt")
	}
}

func TestUnmarshalArgs_PythonLiterals(t *testing.T) {
	var p struct {
		Done   bool        `json:"done"`
		Active bool        `json:"active"`
		Data   interface{} `json:"data"`
	}
	raw := json.RawMessage(`{"done": True, "active": False, "data": None}`)
	err := unmarshalArgs(raw, &p)
	if err != nil {
		t.Fatalf("Python literals should be repaired: %v", err)
	}
	if !p.Done {
		t.Error("done should be true")
	}
	if p.Active {
		t.Error("active should be false")
	}
	if p.Data != nil {
		t.Errorf("data should be nil, got %v", p.Data)
	}
}

func TestUnmarshalArgs_Comments(t *testing.T) {
	var p struct {
		Path string `json:"path"`
	}
	raw := json.RawMessage(`{
		// file path
		"path": "test.txt" /* the target */
	}`)
	err := unmarshalArgs(raw, &p)
	if err != nil {
		t.Fatalf("JSON with comments should be repaired: %v", err)
	}
	if p.Path != "test.txt" {
		t.Errorf("path = %q, want %q", p.Path, "test.txt")
	}
}

func TestStripMarkdownFence(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"```json\n{\"a\":1}\n```", `{"a":1}`},
		{"```\n{\"a\":1}\n```", `{"a":1}`},
		{`{"a":1}`, `{"a":1}`}, // no fence — unchanged
		{"```json\n{\"a\":1}", `{"a":1}`}, // missing closing fence
	}
	for _, tt := range tests {
		got := stripMarkdownFence(tt.in)
		if got != tt.want {
			t.Errorf("stripMarkdownFence(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFixPythonLiterals(t *testing.T) {
	got := fixPythonLiterals(`{"a": True, "b": False, "c": None}`)
	want := `{"a": true, "b": false, "c": null}`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// Should NOT replace inside strings
	got2 := fixPythonLiterals(`{"msg": "True story"}`)
	want2 := `{"msg": "True story"}`
	if got2 != want2 {
		t.Errorf("got %q, want %q", got2, want2)
	}
}

func TestStripComments(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`{"a": 1}`, `{"a": 1}`},                             // no comments
		{"{\"a\": 1} // comment", "{\"a\": 1} "},             // line comment
		{`{"a": /* inline */ 1}`, `{"a":  1}`},               // block comment
		{`{"msg": "hello // world"}`, `{"msg": "hello // world"}`}, // inside string preserved
	}
	for _, tt := range tests {
		got := stripComments(tt.in)
		if got != tt.want {
			t.Errorf("stripComments(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
