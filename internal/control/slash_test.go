package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/hook"
	"reasonix/internal/memory"
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
		PluginNames:     []string{"superpowers", "workflow-kit"},
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
	if !has(items, "auto") || !has(items, "disabled") || !has(items, "high") || !has(items, "max") || has(items, "off") {
		t.Errorf("/effort should offer auto/disabled/high/max; got %v", labelsOf(items))
	}
	// /auto-plan
	items, _ = SlashArgItems("/auto-plan ", data)
	if !has(items, "off") || !has(items, "on") || has(items, "ask") {
		t.Errorf("/auto-plan should offer only off/on; got %v", labelsOf(items))
	}
	// /goal
	items, _ = SlashArgItems("/goal ", data)
	if !has(items, "--research") || !has(items, "--simple") || !has(items, "status") || !has(items, "clear") {
		t.Errorf("/goal should offer research overrides and management commands; got %v", labelsOf(items))
	}
	if items, _ := SlashArgItems("/goal --research ", data); len(items) != 0 {
		t.Errorf("/goal after a research flag should accept free-form objectives; got %v", labelsOf(items))
	}
	// /reasoning-language
	items, _ = SlashArgItems("/reasoning-language ", data)
	if !has(items, "auto") || !has(items, "zh") || !has(items, "en") || has(items, "中文") {
		t.Errorf("/reasoning-language should offer only auto/zh/en; got %v", labelsOf(items))
	}
	// /memory-v5
	items, _ = SlashArgItems("/memory-v5 ", data)
	if !has(items, "status") || !has(items, "off") || !has(items, "observe") || !has(items, "compact") || !has(items, "on") || !has(items, "learnings") {
		t.Errorf("/memory-v5 should offer status/off/observe/compact/on/learnings; got %v", labelsOf(items))
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
	// /plugins mirrors the session-facing plugin inventory command.
	items, _ = SlashArgItems("/plugins ", data)
	if !has(items, "show") {
		t.Errorf("/plugins should offer show; got %v", labelsOf(items))
	}
	items, _ = SlashArgItems("/plugins show ", data)
	if !has(items, "superpowers") || !has(items, "workflow-kit") {
		t.Errorf("/plugins show should list plugin names; got %v", labelsOf(items))
	}
}

func TestMemoryListTextIncludesSavedMemories(t *testing.T) {
	store := memory.Store{Dir: t.TempDir()}
	if _, err := store.Save(memory.Memory{
		Name:        "cache-first",
		Title:       "Cache first",
		Description: "Preserve prompt cache stability",
		Type:        memory.TypeProject,
		Body:        "Use retrieval tools instead of dynamic prefix injection.",
	}); err != nil {
		t.Fatal(err)
	}
	c := New(Options{Memory: &memory.Set{Store: store}})
	out := c.memoryListText()
	for _, want := range []string{"saved memories", "[Cache first](cache-first.md)", "Preserve prompt cache stability"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/memory output missing %q:\n%s", want, out)
		}
	}
}

func TestMemoryListTextIncludesArchivedMemories(t *testing.T) {
	store := memory.Store{Dir: t.TempDir()}
	if _, err := store.Save(memory.Memory{
		Name:        "stale-plan",
		Title:       "Stale plan",
		Description: "Superseded by the new retrieval design",
		Type:        memory.TypeProject,
		Body:        "Old plan body.",
	}); err != nil {
		t.Fatal(err)
	}
	archive, err := store.Archive("stale-plan")
	if err != nil {
		t.Fatal(err)
	}
	c := New(Options{Memory: &memory.Set{Store: store}})
	out := c.memoryListText()
	for _, want := range []string{"archived memories", "[Stale plan](" + archive + ")", "Superseded by the new retrieval design"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/memory output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "saved memories\n  [Stale plan]") {
		t.Fatalf("archived memory should not appear as active saved memory:\n%s", out)
	}
}

func TestManagementHooksTrustUsesWorkspaceRoot(t *testing.T) {
	isolateControlConfigHome(t)
	project := t.TempDir()

	c := New(Options{WorkspaceRoot: project})
	if !c.managementNotice("/hooks trust") {
		t.Fatal("/hooks trust was not handled")
	}
	if !hook.IsTrusted(project, "") {
		t.Fatal("/hooks trust did not trust the controller workspace root")
	}
}

func TestManagementMemoryV5WritesUserConfig(t *testing.T) {
	isolateControlConfigHome(t)
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})

	if !c.managementNotice("/memory-v5 off") {
		t.Fatal("/memory-v5 was not handled")
	}
	cfg := config.LoadForEdit(config.UserConfigPath())
	if cfg.MemoryCompilerEnabled() {
		t.Fatal("memory_compiler.enabled = true, want false")
	}
	if got := cfg.MemoryCompilerVerbosity(); got != config.MemoryCompilerVerbosityObserve {
		t.Fatalf("memory_compiler.verbosity = %q, want observe", got)
	}
	if !strings.Contains(strings.Join(notices, "\n"), "memory-v5 set to off") {
		t.Fatalf("missing memory-v5 notice: %v", notices)
	}
}

func TestManagementMemoryV5LearningsNotice(t *testing.T) {
	isolateControlConfigHome(t)
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})

	if !c.managementNotice("/memory-v5 learnings") {
		t.Fatal("/memory-v5 learnings was not handled")
	}
	joined := strings.Join(notices, "\n")
	// A fresh controller has no learned state; either the no-state or the
	// no-directory notice is acceptable, but it must not fall through to the
	// usage error.
	if !strings.Contains(joined, "memory-v5: no") {
		t.Fatalf("missing learnings notice: %v", notices)
	}
}

func TestManagementMigrateEmitsProgress(t *testing.T) {
	isolateControlConfigHome(t)
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})

	if !c.managementNotice("/migrate") {
		t.Fatal("/migrate was not handled")
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{
		"migration rescue: checking legacy config and credentials",
		"migration rescue: scanning legacy memory",
		"migration rescue: scanning legacy sessions",
		"migration rescue complete:",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing notice %q in:\n%s", want, joined)
		}
	}
}

func TestManagementMigrateFromImportsExplicitSessions(t *testing.T) {
	home := isolateControlConfigHome(t)
	legacySessions := filepath.Join(home, "Old Reasonix", "sessions")
	if err := os.MkdirAll(legacySessions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacySessions, "old-chat.jsonl"), []byte(`{"role":"user","content":"hello from old install"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})

	if !c.managementNotice(`/migrate --from "` + filepath.Dir(legacySessions) + `"`) {
		t.Fatal("/migrate --from was not handled")
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{
		"migration rescue: scanning explicit legacy sessions from " + filepath.Dir(legacySessions),
		"imported 1 past session(s) from " + legacySessions,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing notice %q in:\n%s", want, joined)
		}
	}
}
