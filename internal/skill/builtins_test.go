package skill

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/tool"
)

type builtinTestTool struct {
	name     string
	readOnly bool
}

func (t builtinTestTool) Name() string        { return t.name }
func (t builtinTestTool) Description() string { return t.name }
func (t builtinTestTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t builtinTestTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (t builtinTestTool) ReadOnly() bool { return t.readOnly }

// TestBuiltinReviewSkillsDeclareReadOnly pins the tool-boundary contract behind
// the review/security-review "Read-only" promise: runners select the read-only
// subagent registry from this flag, so losing it silently re-opens writer bash.
func TestBuiltinReviewSkillsDeclareReadOnly(t *testing.T) {
	want := map[string]bool{
		"explore":         false,
		"research":        false,
		"review":          true,
		"security-review": true,
	}
	for _, sk := range builtinSkills() {
		expected, tracked := want[sk.Name]
		if !tracked {
			continue
		}
		if sk.ReadOnly != expected {
			t.Errorf("builtin %q ReadOnly = %v, want %v", sk.Name, sk.ReadOnly, expected)
		}
	}
}

func TestCodeGraphReadToolsRequireKnownNameAndReadOnly(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(builtinTestTool{name: "mcp__codegraph__symbols", readOnly: true})
	reg.Add(builtinTestTool{name: "codegraph_search", readOnly: true})
	reg.Add(builtinTestTool{name: "mcp__codegraph__write_index", readOnly: false})
	reg.Add(builtinTestTool{name: "mcp__other__codegraph_search", readOnly: true})

	got := CodeGraphReadTools(reg)
	want := []string{"codegraph_search", "mcp__codegraph__symbols"}
	if len(got) != len(want) {
		t.Fatalf("CodeGraphReadTools = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("CodeGraphReadTools = %v, want %v", got, want)
		}
	}
}

func TestBuiltinSkillsIncludeCodeGraphHintAndToolsWhenDiscovered(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(builtinTestTool{name: "mcp__codegraph__symbols", readOnly: true})

	var explore Skill
	for _, sk := range builtinSkills() {
		if sk.Name == "explore" {
			explore = sk
			break
		}
	}
	if explore.Name == "" {
		t.Fatal("explore skill not found")
	}
	if strings.Contains(explore.Body, "Optional installed code graph MCP tools") {
		t.Fatalf("base explore body should not include session-specific codegraph hint:\n%s", explore.Body)
	}
	for _, name := range explore.AllowedTools {
		if name == "mcp__codegraph__symbols" {
			t.Fatalf("base explore allowed tools = %v, should not include session-specific codegraph tool", explore.AllowedTools)
		}
	}

	explore = WithCodeGraphTools(explore, CodeGraphReadTools(reg))
	if !strings.Contains(explore.Body, "Optional installed code graph MCP tools") {
		t.Fatalf("explore body missing optional codegraph hint:\n%s", explore.Body)
	}
	for _, want := range []string{
		"use LSP for language semantics",
		"use code graph tools first for call graph, impact analysis, and architecture relationships",
		"use code_index only as the built-in outline/definition-candidate fallback",
	} {
		if !strings.Contains(explore.Body, want) {
			t.Fatalf("explore body missing priority hint %q:\n%s", want, explore.Body)
		}
	}
	found := false
	for _, name := range explore.AllowedTools {
		if name == "mcp__codegraph__symbols" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("explore allowed tools = %v, want codegraph tool", explore.AllowedTools)
	}
}

func TestWithCodeGraphToolsOnlyTouchesCodeReadingBuiltins(t *testing.T) {
	initSkill := Skill{Name: "init", Scope: ScopeBuiltin, Body: "body", AllowedTools: []string{"read_file"}}
	got := WithCodeGraphTools(initSkill, []string{"mcp__codegraph__symbols"})
	if strings.Contains(got.Body, "Optional installed code graph MCP tools") {
		t.Fatalf("init skill should not receive codegraph hint:\n%s", got.Body)
	}
	if len(got.AllowedTools) != 1 || got.AllowedTools[0] != "read_file" {
		t.Fatalf("init allowed tools = %v, want unchanged", got.AllowedTools)
	}
}

func TestWithCodeGraphToolsSkipsUserSkillOverrides(t *testing.T) {
	sk := Skill{Name: "explore", Scope: ScopeProject, Body: "user body", AllowedTools: []string{"read_file"}}
	got := WithCodeGraphTools(sk, []string{"mcp__codegraph__symbols"})
	if strings.Contains(got.Body, "Optional installed code graph MCP tools") {
		t.Fatalf("project skill override should not receive codegraph hint:\n%s", got.Body)
	}
	if len(got.AllowedTools) != 1 || got.AllowedTools[0] != "read_file" {
		t.Fatalf("project skill override allowed tools = %v, want unchanged", got.AllowedTools)
	}
}

func TestWithCodeGraphToolsIsIdempotent(t *testing.T) {
	sk := Skill{Name: "explore", Scope: ScopeBuiltin, Body: "body", AllowedTools: []string{"read_file"}}
	sk = WithCodeGraphTools(sk, []string{"mcp__codegraph__symbols"})
	sk = WithCodeGraphTools(sk, []string{"mcp__codegraph__symbols"})
	if got := strings.Count(sk.Body, optionalCodeGraphHint); got != 1 {
		t.Fatalf("codegraph hint count = %d, want 1; body:\n%s", got, sk.Body)
	}
	count := 0
	for _, name := range sk.AllowedTools {
		if name == "mcp__codegraph__symbols" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("codegraph tool count = %d, want 1; allowed=%v", count, sk.AllowedTools)
	}
}
