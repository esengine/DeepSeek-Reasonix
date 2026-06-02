package control

import (
	"testing"

	"reasonix/internal/skill"
)

func labelsOf(items []SlashItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

func has(items []SlashItem, label string) bool {
	for _, it := range items {
		if it.Label == label {
			return true
		}
	}
	return false
}

func TestSlashArgItems(t *testing.T) {
	data := ArgData{
		Skills:          []skill.Skill{{Name: "explore", Scope: skill.ScopeBuiltin}, {Name: "review", Scope: skill.ScopeBuiltin}},
		ServerNames:     []string{"fs", "git"},
		DisconnectedMCP: []string{"optional"},
		ModelRefs:       []string{"deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-pro"},
		CurrentModel:    "deepseek/deepseek-v4-flash",
	}

	// /skill subcommands
	items, from := SlashArgItems("/skill ", data)
	if from != len("/skill ") {
		t.Errorf("from = %d, want %d", from, len("/skill "))
	}
	for _, w := range []string{"list", "show", "new", "paths"} {
		if !has(items, w) {
			t.Errorf("/skill missing subcommand %q; got %v", w, labelsOf(items))
		}
	}
	// /skill show → skill names
	items, _ = SlashArgItems("/skill show ", data)
	if !has(items, "explore") || !has(items, "review") {
		t.Errorf("/skill show should list skill names; got %v", labelsOf(items))
	}
	// /mcp subcommands + filtering
	items, _ = SlashArgItems("/mcp re", data)
	if len(items) != 1 || items[0].Label != "remove" {
		t.Errorf("/mcp re should filter to remove; got %v", labelsOf(items))
	}
	// /mcp remove → server names
	items, _ = SlashArgItems("/mcp remove ", data)
	if !has(items, "fs") || !has(items, "git") {
		t.Errorf("/mcp remove should list servers; got %v", labelsOf(items))
	}
	// /mcp connect -> disconnected configured server names
	items, _ = SlashArgItems("/mcp connect ", data)
	if !has(items, "optional") {
		t.Errorf("/mcp connect should list disconnected configured servers; got %v", labelsOf(items))
	}
	// /model → refs, current marked
	items, _ = SlashArgItems("/model ", data)
	if !has(items, "deepseek/deepseek-v4-pro") {
		t.Errorf("/model should list refs; got %v", labelsOf(items))
	}
	for _, it := range items {
		if it.Label == data.CurrentModel && it.Hint != "current" {
			t.Errorf("active model should be hinted 'current', got %q", it.Hint)
		}
	}
	// /hooks
	items, _ = SlashArgItems("/hooks ", data)
	if !has(items, "list") || !has(items, "trust") {
		t.Errorf("/hooks should offer list/trust; got %v", labelsOf(items))
	}
	// /thinking
	items, _ = SlashArgItems("/thinking ", data)
	if !has(items, "high") || !has(items, "max") || !has(items, "off") {
		t.Errorf("/thinking should offer high/max/off; got %v", labelsOf(items))
	}
	// a non-structured command yields nothing
	if items, _ := SlashArgItems("/help ", data); len(items) != 0 {
		t.Errorf("/help should have no arg items; got %v", labelsOf(items))
	}
	// a fully-typed terminal subcommand offers nothing (no lingering no-op) so the
	// caller can submit instead of "accepting" a no-op — the /skill list bug.
	if items, _ := SlashArgItems("/skill list", data); len(items) != 0 {
		t.Errorf("/skill list (token complete) should offer no suggestion; got %v", labelsOf(items))
	}
	// but a partial token still completes.
	if items, _ := SlashArgItems("/skill li", data); !has(items, "list") {
		t.Errorf("/skill li should still complete to list; got %v", labelsOf(items))
	}
}
