package main

import "testing"

const dialectSource = `package p

var gatewayModels = []string{"zai/GLM-5.2", "anthropic/claude-sonnet-4.6"}

var presets = []ProviderPreset{
	{Entries: []ProviderEntry{{
		Name: "gateway", Kind: "openai", Models: gatewayModels,
	}}},
	{Entries: []ProviderEntry{{
		Name: "native", Kind: "anthropic", Models: gatewayModels,
	}}},
	{Entries: []ProviderEntry{{
		Name: "inline", Kind: "openai", Models: []string{"claude-opus-5"},
	}}},
	{Entries: []ProviderEntry{{
		Name: "single", Kind: "openai", Model: "claude-haiku-4-5",
	}}},
	{Entries: []ProviderEntry{{
		Name: "clean", Kind: "openai", Models: []string{"gpt-5.6-sol", "deepseek-v4-pro"},
	}}},
}
`

func flagged(t *testing.T, rel string) []Finding {
	t.Helper()
	entries, vars := dialectRefs(parseBytes(rel, []byte(dialectSource)))
	return checkClaudeDialect(entries, vars)
}

// The models a preset offers are usually a package-level slice, so an entry
// that never spells "claude" in its own literal is the ordinary case.
func TestClaudeDialectResolvesModelsThroughAVariable(t *testing.T) {
	got := flagged(t, "internal/contract/config/presets.go")
	if len(got) != 3 {
		t.Fatalf("findings = %d, want 3 (variable, inline, single): %v", len(got), got)
	}
	for _, f := range got {
		if f.Rule != ruleClaudeDialect || f.Weight != 1 {
			t.Fatalf("a pass/fail rule must weigh one: %+v", f)
		}
	}
}

// One finding per entry, not per model: the entry is what a fix changes.
func TestClaudeDialectReportsEachEntryOnce(t *testing.T) {
	seen := map[int]bool{}
	for _, f := range flagged(t, "internal/contract/config/presets.go") {
		if seen[f.Line] {
			t.Fatalf("line %d reported twice", f.Line)
		}
		seen[f.Line] = true
	}
}

// The anthropic dialect is where cache_control comes from, so the same models
// there are the fix, not the finding.
func TestClaudeDialectAllowsTheAnthropicDialect(t *testing.T) {
	for _, f := range flagged(t, "internal/contract/config/presets.go") {
		if f.Line == 11 {
			t.Fatalf("anthropic-kind entry was flagged: %+v", f)
		}
	}
}

func TestClaudeDialectSkipsFixtures(t *testing.T) {
	if got := flagged(t, "internal/contract/config/presets_test.go"); len(got) != 0 {
		t.Fatalf("a test fixture was flagged: %v", got)
	}
}
