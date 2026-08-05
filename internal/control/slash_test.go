package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/instruction"
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
		MemoryRefs:      []string{"mem-cache", "cache-first"},
		MemoryArchives:  []string{"/tmp/memory archive/cache-first.md"},
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
	if !has(items, "list") || has(items, "trust") {
		t.Errorf("/hooks should offer list without a trust step; got %v", labelsOf(items))
	}
	// /effort
	items, _ = SlashArgItems("/effort ", data)
	if !has(items, "auto") || !has(items, "disabled") || !has(items, "high") || !has(items, "max") || has(items, "off") {
		t.Errorf("/effort should offer auto/disabled/high/max; got %v", labelsOf(items))
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
	// /currency
	items, _ = SlashArgItems("/currency ", data)
	if !has(items, "auto") || !has(items, "CNY") || !has(items, "USD") {
		t.Errorf("/currency should offer only auto/CNY/USD; got %v", labelsOf(items))
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
	// /memory diagnostics and recovery commands.
	items, _ = SlashArgItems("/memory ", data)
	for _, want := range []string{"recall", "revisions", "restore", "archived", "recover", "instructions"} {
		if !has(items, want) {
			t.Errorf("/memory missing subcommand %q; got %v", want, labelsOf(items))
		}
	}
	items, _ = SlashArgItems("/memory revisions ", data)
	if !has(items, "mem-cache") || !has(items, "cache-first") {
		t.Errorf("/memory revisions should offer active memory refs; got %v", labelsOf(items))
	}
	items, _ = SlashArgItems("/memory recover ", data)
	if !has(items, "/tmp/memory archive/cache-first.md") {
		t.Errorf("/memory recover should offer archive paths; got %v", labelsOf(items))
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
	out := MemoryCommandText(c, "")
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
	out := MemoryCommandText(c, "")
	for _, want := range []string{"archived memories", "[Stale plan](" + archive + ")", "Superseded by the new retrieval design"} {
		if !strings.Contains(out, want) {
			t.Fatalf("/memory output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "saved memories\n  [Stale plan]") {
		t.Fatalf("archived memory should not appear as active saved memory:\n%s", out)
	}
}

func TestMemoryListTextIncludesEveryScopeAndObservableMetadata(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "project")
	globalDir := filepath.Join(root, "global")
	globalStore := memory.Store{Dir: globalDir}
	globalSaved, err := globalStore.SaveWithOptions(memory.Memory{
		Name: "shared-policy", Title: "Global policy", Description: "global fallback",
		Type: memory.TypeReference, Scope: memory.FactScopeGlobal, Body: "Use the global endpoint.",
	}, memory.SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	projectStore := memory.Store{Dir: projectDir}
	projectSaved, err := projectStore.SaveWithOptions(memory.Memory{
		Name: "shared-policy", Title: "Project policy", Description: "project override",
		Type: memory.TypeProject, Scope: memory.FactScopeProject, Body: "Use the project endpoint.",
	}, memory.SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}

	store := memory.Store{Dir: projectDir, GlobalDir: globalDir}
	c := New(Options{Memory: &memory.Set{Store: store}})
	out := MemoryCommandText(c, "")
	for _, want := range []string{
		globalSaved.Memory.ID,
		projectSaved.Memory.ID,
		"revision=1",
		"scope=global",
		"scope=project",
		"type=reference",
		"type=project",
		"freshness=fresh",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("/memory output missing %q:\n%s", want, out)
		}
	}
	if got := strings.Count(out, "shared-policy.md"); got != 2 {
		t.Fatalf("/memory should show both same-name scoped facts, got %d:\n%s", got, out)
	}
}

func TestManagementMemoryRecallAndInstructionDiagnostics(t *testing.T) {
	now := time.Now().UTC()
	set := &memory.Set{
		Docs: []memory.Source{{
			Path: "/workspace/AGENTS.md", Scope: memory.ScopeProject,
			Directory: "/workspace", Imports: []instruction.Import{{Path: "/workspace/shared.md", SourcePath: "/workspace/AGENTS.md"}},
			Order: 2,
		}},
		InstructionDiagnostics: []instruction.Diagnostic{{
			Code: "import_cycle", Path: "/workspace/shared.md", SourcePath: "/workspace/AGENTS.md", Line: 4, Message: "cycle detected",
		}},
	}
	var notices []string
	c := New(Options{
		Memory: set,
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})
	c.memory.recordRecall(memory.RecallResult{
		Query: "Which cache policy applies?",
		Hits: []memory.RecallHit{{
			Memory: memory.Memory{
				ID: "mem-cache", Revision: 3, Name: "cache-policy", Scope: memory.FactScopeProject,
				Type: memory.TypeProject, UpdatedAt: now,
			},
			Score: 4.25, Freshness: memory.FreshnessFresh, Reason: "matched cache, policy; project scope",
		}},
		CharBudget: 2400, UsedChars: 280, Omitted: 1,
	})

	if !c.managementNotice("/memory recall") {
		t.Fatal("/memory recall was not handled")
	}
	if !c.managementNotice("/memory instructions") {
		t.Fatal("/memory instructions was not handled")
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{
		"Which cache policy applies?",
		"mem-cache",
		"score=4.250",
		"reason=matched cache, policy; project scope",
		"budget=280/2400",
		"omitted=1",
		"precedence=1",
		"directory=/workspace",
		"import=/workspace/shared.md",
		"import_cycle",
		"/workspace/AGENTS.md:4",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("/memory diagnostics missing %q:\n%s", want, joined)
		}
	}
}

func TestManagementMemoryRevisionRestore(t *testing.T) {
	userDir := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "project")
	store := memory.StoreFor(userDir, cwd)
	first, err := store.SaveWithOptions(memory.Memory{
		Name: "provider-policy", Title: "Provider policy", Description: "first version",
		Type: memory.TypeProject, Body: "Use provider A.",
	}, memory.SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated := first.Memory
	updated.Description = "second version"
	updated.Body = "Use provider B."
	second, err := store.SaveWithOptions(updated, memory.SaveOptions{
		ExpectedRevision: first.Memory.Revision, RequireExpectedRevision: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var notices []string
	c := New(Options{
		Memory: &memory.Set{Store: store, CWD: cwd, UserDir: userDir},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})

	if !c.managementNotice("/memory revisions " + second.Memory.ID) {
		t.Fatal("/memory revisions was not handled")
	}
	if !c.managementNotice("/memory restore " + second.Memory.ID + " 1") {
		t.Fatal("/memory restore was not handled")
	}
	active, ok := c.Memory().Store.Read(second.Memory.ID)
	if !ok {
		t.Fatal("restored memory is not active")
	}
	if active.Revision != 3 || active.Body != "Use provider A." {
		t.Fatalf("restored memory = revision %d body %q", active.Revision, active.Body)
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{"revision=2", "active", "revision=1", "restored provider-policy", "revision=3"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("/memory revision flow missing %q:\n%s", want, joined)
		}
	}
}

func TestManagementMemoryArchiveRecoveryAcceptsQuotedPathWithSpaces(t *testing.T) {
	userDir := filepath.Join(t.TempDir(), "reasonix home with spaces")
	cwd := filepath.Join(t.TempDir(), "project")
	store := memory.StoreFor(userDir, cwd)
	saved, err := store.SaveWithOptions(memory.Memory{
		Name: "archived-policy", Title: "Archived policy", Description: "recover me",
		Type: memory.TypeProject, Body: "Archived body.",
	}, memory.SaveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archivePath, err := store.Archive(saved.Memory.ID)
	if err != nil {
		t.Fatal(err)
	}
	var notices []string
	c := New(Options{
		Memory: &memory.Set{Store: store, CWD: cwd, UserDir: userDir},
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})

	if !c.managementNotice("/memory archived") {
		t.Fatal("/memory archived was not handled")
	}
	if !c.managementNotice(`/memory recover "` + archivePath + `"`) {
		t.Fatal("/memory recover was not handled")
	}
	active, ok := c.Memory().Store.Read(saved.Memory.ID)
	if !ok {
		t.Fatal("recovered memory is not active")
	}
	if active.Revision != 2 {
		t.Fatalf("recovered revision = %d, want 2", active.Revision)
	}
	joined := strings.Join(notices, "\n")
	for _, want := range []string{archivePath, "recovered archived-policy", "revision=2"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("/memory archive flow missing %q:\n%s", want, joined)
		}
	}
}

func TestManagementHooksTrustCompatibilityNotice(t *testing.T) {
	isolateControlConfigHome(t)
	var notices []string
	c := New(Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			notices = append(notices, e.Text)
		}
	})})
	if !c.managementNotice("/hooks trust") {
		t.Fatal("legacy /hooks trust was not handled")
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "enabled automatically") {
		t.Fatalf("legacy /hooks trust notice = %v", notices)
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
