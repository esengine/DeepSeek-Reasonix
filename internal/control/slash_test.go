package control

import (
	"testing"

	"reasonix/internal/hook"
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
		DisabledSkills:  []skill.Skill{{Name: "security-review", Scope: skill.ScopeBuiltin}},
		ServerNames:     []string{"fs", "git"},
		ConfiguredMCP:   []string{"fs", "linear"},
		DisconnectedMCP: []string{"optional"},
		ModelRefs:       []string{"deepseek-flash/deepseek-v4-flash", "deepseek-pro/deepseek-v4-pro"},
		CurrentModel:    "deepseek-flash/deepseek-v4-flash",
		ProviderNames:   []string{"deepseek-flash", "deepseek-pro", "custom"},
		CurrentProvider: "deepseek-flash",
	}

	// /skills subcommands
	items, from := SlashArgItems("/skills ", data)
	if from != len("/skills ") {
		t.Errorf("from = %d, want %d", from, len("/skills "))
	}
	for _, w := range []string{"show", "enable", "disable", "new", "paths"} {
		if !has(items, w) {
			t.Errorf("/skills missing subcommand %q; got %v", w, labelsOf(items))
		}
	}
	if has(items, "manage") {
		t.Errorf("/skills should hide redundant manage subcommand; got %v", labelsOf(items))
	}
	if has(items, "list") {
		t.Errorf("/skills should hide redundant list subcommand; got %v", labelsOf(items))
	}
	// /skills show → skill names
	items, _ = SlashArgItems("/skills show ", data)
	if !has(items, "explore") || !has(items, "review") {
		t.Errorf("/skills show should list skill names; got %v", labelsOf(items))
	}
	// Legacy /skill still works as an alias.
	items, _ = SlashArgItems("/skill show ", data)
	if !has(items, "explore") || !has(items, "review") {
		t.Errorf("/skill show alias should list skill names; got %v", labelsOf(items))
	}
	items, _ = SlashArgItems("/skill disable ", data)
	if !has(items, "explore") || has(items, "security-review") {
		t.Errorf("/skill disable should list enabled skills only; got %v", labelsOf(items))
	}
	items, _ = SlashArgItems("/skill enable ", data)
	if !has(items, "security-review") || has(items, "review") {
		t.Errorf("/skill enable should list disabled skills only; got %v", labelsOf(items))
	}
	// /mcp subcommands + filtering
	items, _ = SlashArgItems("/mcp ", data)
	if has(items, "list") {
		t.Errorf("/mcp should hide redundant list subcommand; got %v", labelsOf(items))
	}
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
	// /mcp show/tools -> connected + configured server names
	items, _ = SlashArgItems("/mcp show ", data)
	if !has(items, "fs") || !has(items, "linear") || !has(items, "optional") {
		t.Errorf("/mcp show should list known servers; got %v", labelsOf(items))
	}
	items, _ = SlashArgItems("/mcp tools ", data)
	if !has(items, "git") || !has(items, "linear") {
		t.Errorf("/mcp tools should list known servers; got %v", labelsOf(items))
	}
	// /model → refs, current marked
	items, _ = SlashArgItems("/model ", data)
	if !has(items, "deepseek-pro/deepseek-v4-pro") {
		t.Errorf("/model should list refs; got %v", labelsOf(items))
	}
	for _, it := range items {
		if it.Label == data.CurrentModel && it.Hint != "current" {
			t.Errorf("active model should be hinted 'current', got %q", it.Hint)
		}
	}
	// /provider → provider names, current marked
	items, _ = SlashArgItems("/provider ", data)
	if !has(items, "deepseek-pro") || !has(items, "custom") {
		t.Errorf("/provider should list provider names; got %v", labelsOf(items))
	}
	for _, it := range items {
		if it.Label == data.CurrentProvider && it.Hint != "current" {
			t.Errorf("active provider should be hinted 'current', got %q", it.Hint)
		}
	}
	// /provider de → filter to deepseek-*
	items, _ = SlashArgItems("/provider de", data)
	if len(items) != 2 {
		t.Errorf("/provider de should filter to 2 deepseek providers; got %v", labelsOf(items))
	}
	// /hooks
	items, _ = SlashArgItems("/hooks ", data)
	if !has(items, "list") || !has(items, "trust") {
		t.Errorf("/hooks should offer list/trust; got %v", labelsOf(items))
	}
	// /effort
	items, _ = SlashArgItems("/effort ", data)
	if !has(items, "auto") || !has(items, "high") || !has(items, "max") || has(items, "off") {
		t.Errorf("/effort should offer auto/high/max only; got %v", labelsOf(items))
	}
	// /auto-plan
	items, _ = SlashArgItems("/auto-plan ", data)
	if !has(items, "off") || !has(items, "on") || has(items, "ask") {
		t.Errorf("/auto-plan should offer only off/on; got %v", labelsOf(items))
	}
	// /theme
	items, _ = SlashArgItems("/theme ", data)
	if !has(items, "auto") || !has(items, "light") || !has(items, "graphite") || !has(items, "glacier") {
		t.Errorf("/theme should offer modes and styles; got %v", labelsOf(items))
	}
	// a non-structured command yields nothing
	if items, _ := SlashArgItems("/help ", data); len(items) != 0 {
		t.Errorf("/help should have no arg items; got %v", labelsOf(items))
	}
	// a fully-typed terminal subcommand offers nothing (no lingering no-op) so the
	// caller can submit instead of "accepting" a no-op — the /skills list bug.
	if items, _ := SlashArgItems("/skills list", data); len(items) != 0 {
		t.Errorf("/skills list (token complete) should offer no suggestion; got %v", labelsOf(items))
	}
	// and hidden menu commands stay hidden while direct typed execution remains
	// handled by runSkillSubcommand.
	if items, _ := SlashArgItems("/skills li", data); len(items) != 0 {
		t.Errorf("/skills li should not offer hidden list suggestion; got %v", labelsOf(items))
	}
}

func TestManagementHooksTrustUsesWorkspaceRoot(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	c := New(Options{WorkspaceRoot: project})
	if !c.managementNotice("/hooks trust") {
		t.Fatal("/hooks trust was not handled")
	}
	if !hook.IsTrusted(project, home) {
		t.Fatal("/hooks trust did not trust the controller workspace root")
	}
}
