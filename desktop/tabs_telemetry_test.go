package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type usageProvider struct {
	usage *provider.Usage
}

func (p usageProvider) Name() string { return "usage" }

func (p usageProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: p.usage}
	close(ch)
	return ch, nil
}

func TestTelemetryLoadsLegacyReadFileArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl.telemetry.json")
	if err := os.WriteFile(path, []byte(`[{"path":"README.md","turn":2,"time":1000}]`), 0o644); err != nil {
		t.Fatalf("write legacy telemetry: %v", err)
	}

	got := loadTelemetry(path)
	if len(got.ReadFiles) != 1 || got.ReadFiles[0].Path != "README.md" {
		t.Fatalf("legacy read files = %+v", got.ReadFiles)
	}
	if got.Usage.RequestCount != 0 {
		t.Fatalf("legacy usage request count = %d, want 0", got.Usage.RequestCount)
	}
}

func TestWorkspaceTabAggregatesSessionUsageTelemetry(t *testing.T) {
	tab := &WorkspaceTab{}
	start := time.Now().Add(-2 * time.Second).UnixMilli()
	tab.recordTurnStarted(start)
	tab.recordUsage(event.Event{
		Usage:       &provider.Usage{PromptTokens: 100, CompletionTokens: 40, TotalTokens: 140, CacheHitTokens: 70, CacheMissTokens: 30, ReasoningTokens: 10},
		UsageSource: event.UsageSourceSubagent,
		SessionHit:  70,
		SessionMiss: 30,
		Pricing:     &provider.Pricing{CacheHit: 1, Input: 2, Output: 3, Currency: "¥"},
	})
	tab.recordTurnDone(start + 1500)

	got := tab.telemetrySnapshot().Usage
	if got.RequestCount != 1 || got.PromptTokens != 100 || got.CompletionTokens != 40 || got.TotalTokens != 140 || got.ReasoningTokens != 10 {
		t.Fatalf("usage tokens = %+v", got)
	}
	if got.CacheHitTokens != 70 || got.CacheMissTokens != 30 {
		t.Fatalf("cache tokens = hit %d miss %d", got.CacheHitTokens, got.CacheMissTokens)
	}
	if got.ElapsedMs != 1500 {
		t.Fatalf("elapsed = %d, want 1500", got.ElapsedMs)
	}
	if got.SessionCost <= 0 || got.SessionCurrency != "¥" {
		t.Fatalf("cost = %f %q, want positive ¥", got.SessionCost, got.SessionCurrency)
	}
	if got.Sources[event.UsageSourceSubagent].SessionCost <= 0 || got.Sources[event.UsageSourceSubagent].RequestCount != 1 {
		t.Fatalf("subagent source stats = %+v, want one costed request", got.Sources[event.UsageSourceSubagent])
	}

	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	context := app.ContextUsageForTab("tab")
	if context.SessionTokens != 140 {
		t.Fatalf("context usage session tokens = %d, want 140", context.SessionTokens)
	}
	if context.SessionCost <= 0 || context.SessionCurrency != "¥" {
		t.Fatalf("context usage cost = %f %q, want positive ¥", context.SessionCost, context.SessionCurrency)
	}
	if context.CacheHitTokens != 70 || context.CacheMissTokens != 30 {
		t.Fatalf("context usage cache tokens = hit %d miss %d, want 70/30", context.CacheHitTokens, context.CacheMissTokens)
	}
	if panel := app.ContextPanel("tab"); panel.TotalTokens != 140 {
		t.Fatalf("context panel total tokens = %d, want 140", panel.TotalTokens)
	}
}

func TestWorkspaceTabSubagentUsageDoesNotOverwriteExecutorSessionCache(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordUsage(event.Event{
		Usage:       &provider.Usage{PromptTokens: 1000, CompletionTokens: 10, TotalTokens: 1010, CacheHitTokens: 0, CacheMissTokens: 0},
		UsageSource: event.UsageSourceExecutor,
		SessionHit:  700,
		SessionMiss: 300,
	})
	tab.recordUsage(event.Event{
		Usage:       &provider.Usage{PromptTokens: 20, CompletionTokens: 5, TotalTokens: 25, CacheHitTokens: 5, CacheMissTokens: 10},
		UsageSource: event.UsageSourceSubagent,
		SessionHit:  999,
		SessionMiss: 999,
	})
	tab.recordUsage(event.Event{
		Usage:       &provider.Usage{PromptTokens: 200, CompletionTokens: 20, TotalTokens: 220, CacheHitTokens: 100, CacheMissTokens: 100},
		UsageSource: event.UsageSourceExecutor,
		SessionHit:  800,
		SessionMiss: 400,
	})

	got := tab.telemetrySnapshot().Usage
	if got.CacheHitTokens != 805 || got.CacheMissTokens != 410 {
		t.Fatalf("cache tokens = hit %d miss %d, want executor deltas plus subagent delta 805/410", got.CacheHitTokens, got.CacheMissTokens)
	}
	if got.Sources[event.UsageSourceExecutor].CacheHitTokens != 800 || got.Sources[event.UsageSourceExecutor].CacheMissTokens != 400 {
		t.Fatalf("executor cache source = %+v, want session deltas 800/400", got.Sources[event.UsageSourceExecutor])
	}
	if got.Sources[event.UsageSourceSubagent].CacheHitTokens != 5 || got.Sources[event.UsageSourceSubagent].CacheMissTokens != 10 {
		t.Fatalf("subagent cache source = %+v, want usage delta 5/10", got.Sources[event.UsageSourceSubagent])
	}
}

func TestWorkspaceTabTracksPlannerAndExecutorCacheBySource(t *testing.T) {
	tab := &WorkspaceTab{}
	tab.recordUsage(event.Event{
		Usage:       &provider.Usage{PromptTokens: 120, CompletionTokens: 15, TotalTokens: 135},
		UsageSource: event.UsageSourcePlanner,
		SessionHit:  60,
		SessionMiss: 40,
	})
	tab.recordUsage(event.Event{
		Usage:       &provider.Usage{PromptTokens: 300, CompletionTokens: 40, TotalTokens: 340},
		UsageSource: event.UsageSourceExecutor,
		SessionHit:  210,
		SessionMiss: 90,
	})

	got := tab.telemetrySnapshot().Usage
	if got.CacheHitTokens != 270 || got.CacheMissTokens != 130 {
		t.Fatalf("aggregate cache tokens = hit %d miss %d, want planner+executor 270/130", got.CacheHitTokens, got.CacheMissTokens)
	}
	if got.Sources[event.UsageSourcePlanner].CacheHitTokens != 60 || got.Sources[event.UsageSourcePlanner].CacheMissTokens != 40 {
		t.Fatalf("planner source = %+v, want 60/40", got.Sources[event.UsageSourcePlanner])
	}
	if got.Sources[event.UsageSourceExecutor].CacheHitTokens != 210 || got.Sources[event.UsageSourceExecutor].CacheMissTokens != 90 {
		t.Fatalf("executor source = %+v, want 210/90", got.Sources[event.UsageSourceExecutor])
	}
}

func TestContextPanelUsesLastUsageBreakdownWithTelemetryTotal(t *testing.T) {
	lastUsage := &provider.Usage{
		PromptTokens:     10,
		CompletionTokens: 4,
		TotalTokens:      14,
		CacheHitTokens:   7,
		CacheMissTokens:  3,
		ReasoningTokens:  2,
	}
	ag := agent.New(
		usageProvider{usage: lastUsage},
		tool.NewRegistry(),
		agent.NewSession("system"),
		agent.Options{ContextWindow: 200},
		event.Discard,
	)
	if err := ag.Run(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	tab := &WorkspaceTab{
		ID:    "tab",
		Ctrl:  control.New(control.Options{Executor: ag, Sink: event.Discard}),
		Scope: "global",
		Ready: true,
	}
	tab.recordUsage(event.Event{
		Usage: &provider.Usage{
			PromptTokens:     100,
			CompletionTokens: 40,
			TotalTokens:      140,
			CacheHitTokens:   70,
			CacheMissTokens:  30,
			ReasoningTokens:  10,
		},
	})
	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}

	panel := app.ContextPanel("tab")
	if panel.TotalTokens != 140 {
		t.Fatalf("context panel total tokens = %d, want telemetry total 140", panel.TotalTokens)
	}
	if panel.PromptTokens != 10 || panel.CompletionTokens != 4 || panel.ReasoningTokens != 2 {
		t.Fatalf("context panel breakdown = prompt:%d completion:%d reasoning:%d, want last usage 10/4/2",
			panel.PromptTokens, panel.CompletionTokens, panel.ReasoningTokens)
	}
	if panel.CacheHitTokens != 7 || panel.CacheMissTokens != 3 {
		t.Fatalf("context panel cache breakdown = hit:%d miss:%d, want last usage 7/3",
			panel.CacheHitTokens, panel.CacheMissTokens)
	}
}

func costedUsageEvent() event.Event {
	return event.Event{
		Usage:   &provider.Usage{PromptTokens: 100, CompletionTokens: 40, TotalTokens: 140},
		Pricing: &provider.Pricing{CacheHit: 1, Input: 2, Output: 3, Currency: "¥"},
	}
}

func TestSyncTelemetryToSessionReKeysAcrossRotation(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.jsonl")
	pathB := filepath.Join(dir, "b.jsonl")

	tab := &WorkspaceTab{}
	tab.syncTelemetryToSession(pathA)
	tab.recordUsage(costedUsageEvent())
	costA := tab.telemetrySnapshot().Usage.SessionCost
	if costA <= 0 {
		t.Fatalf("seed cost = %f, want positive", costA)
	}
	if err := saveTelemetry(pathA+".telemetry.json", tab.telemetrySnapshot()); err != nil {
		t.Fatalf("save telemetry A: %v", err)
	}

	// Same session: in-memory totals survive.
	tab.syncTelemetryToSession(pathA)
	if got := tab.telemetrySnapshot().Usage.SessionCost; got != costA {
		t.Fatalf("same-session sync cost = %f, want %f", got, costA)
	}

	// Rotation to a session without a sidecar starts from zero — the previous
	// session's totals must not bleed over (#5850).
	tab.syncTelemetryToSession(pathB)
	if got := tab.telemetrySnapshot().Usage; got.SessionCost != 0 || got.TotalTokens != 0 || got.RequestCount != 0 {
		t.Fatalf("rotated telemetry = %+v, want zeroed", got)
	}

	// Rotating back restores session A's persisted totals.
	tab.syncTelemetryToSession(pathA)
	if got := tab.telemetrySnapshot().Usage.SessionCost; got != costA {
		t.Fatalf("restored cost = %f, want %f", got, costA)
	}
}

func TestContextUsageForTabReKeysAfterControllerRotation(t *testing.T) {
	dir := t.TempDir()
	rotated := filepath.Join(dir, "rotated.jsonl")
	stale := filepath.Join(dir, "stale.jsonl")

	ag := agent.New(usageProvider{usage: &provider.Usage{}}, tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	tab := &WorkspaceTab{
		ID:   "tab",
		Ctrl: control.New(control.Options{Executor: ag, Sink: event.Discard, SessionDir: dir, SessionPath: rotated}),
	}
	// Telemetry still keyed to the pre-rotation session: a typed /new routes
	// through Controller.Submit and rotates without App.NewSession running.
	tab.syncTelemetryToSession(stale)
	tab.recordUsage(costedUsageEvent())

	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	info := app.ContextUsageForTab("tab")
	if info.SessionCost != 0 || info.SessionTokens != 0 {
		t.Fatalf("context after rotation = cost %f tokens %d, want zeros", info.SessionCost, info.SessionTokens)
	}
	if got := tab.telemetrySnapshot().Usage.RequestCount; got != 0 {
		t.Fatalf("telemetry request count after rotation = %d, want 0", got)
	}
}

func TestNewSessionResetsTabUsageTelemetry(t *testing.T) {
	isolateDesktopUserDirs(t)

	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	sessPath := filepath.Join(dir, "session.jsonl")
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "hello"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "world"})
	exec := agent.New(stubProvider{}, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	app := &App{
		tabs:             map[string]*WorkspaceTab{},
		detachedSessions: map[string]*WorkspaceTab{},
		activeTabID:      "tab",
	}
	tab := &WorkspaceTab{
		ID:            "tab",
		Scope:         "global",
		WorkspaceRoot: root,
		SessionPath:   sessPath,
		Ready:         true,
		model:         "test-model",
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	tab.Ctrl = control.New(control.Options{
		Executor:    exec,
		SessionDir:  dir,
		SessionPath: sessPath,
		Label:       "test",
		Sink:        tab.sink,
	})
	app.tabs[tab.ID] = tab

	tab.syncTelemetryToSession(sessPath)
	tab.recordUsage(costedUsageEvent())
	if seed := tab.telemetrySnapshot().Usage.SessionCost; seed <= 0 {
		t.Fatalf("seed cost = %f, want positive", seed)
	}

	if err := app.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := tab.telemetrySnapshot().Usage; got.SessionCost != 0 || got.RequestCount != 0 || got.TotalTokens != 0 {
		t.Fatalf("telemetry after NewSession = %+v, want zeroed", got)
	}
	if info := app.ContextUsageForTab("tab"); info.SessionCost != 0 || info.SessionTokens != 0 {
		t.Fatalf("context after NewSession = cost %f tokens %d, want zeros", info.SessionCost, info.SessionTokens)
	}
}

func TestSnapshotConflictRecoveryCarriesTelemetryToFork(t *testing.T) {
	isolateDesktopUserDirs(t)

	root := globalTabWorkspaceRoot()
	dir := desktopSessionDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	originalPath := filepath.Join(dir, "session.jsonl")
	current := agent.NewSession("sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	if err := current.Save(originalPath); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	staleSess := agent.NewSession("sys")
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	staleSess.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	staleSess.Add(provider.Message{Role: provider.RoleUser, Content: "local second"})
	staleExec := agent.New(stubProvider{}, tool.NewRegistry(), staleSess, agent.Options{}, event.Discard)
	app := &App{
		tabs:             map[string]*WorkspaceTab{},
		detachedSessions: map[string]*WorkspaceTab{},
		activeTabID:      "recovery_tab",
	}
	tab := &WorkspaceTab{
		ID:            "recovery_tab",
		Scope:         "global",
		WorkspaceRoot: root,
		SessionPath:   originalPath,
		Ready:         true,
		model:         "test-model",
		disabledMCP:   map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	tab.Ctrl = control.New(control.Options{
		Executor:            staleExec,
		SessionDir:          dir,
		SessionPath:         originalPath,
		Label:               "test",
		Sink:                tab.sink,
		SessionRecoveryMeta: app.tabSessionRecoveryMeta(tab),
		OnSessionRecovered:  app.handleTabSessionRecovered(tab),
	})
	app.tabs[tab.ID] = tab

	tab.syncTelemetryToSession(originalPath)
	tab.recordUsage(costedUsageEvent())
	want := tab.telemetrySnapshot().Usage.SessionCost
	if want <= 0 {
		t.Fatalf("seed cost = %f, want positive", want)
	}

	if err := tab.Ctrl.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	recoveryPath := tab.Ctrl.SessionPath()
	if recoveryPath == "" || recoveryPath == originalPath {
		t.Fatalf("recovery path = %q, want distinct path", recoveryPath)
	}

	// The fork continues the conversation: in-memory totals carry over and a
	// later sync against the fork path must not wipe them.
	tab.syncTelemetryToSession(recoveryPath)
	if got := tab.telemetrySnapshot().Usage.SessionCost; got != want {
		t.Fatalf("carried cost = %f, want %f", got, want)
	}
	// The fork's sidecar was persisted at retarget time, so cost survives an
	// app exit before the next usage event.
	if got := loadTelemetry(recoveryPath + ".telemetry.json").Usage.SessionCost; got != want {
		t.Fatalf("fork sidecar cost = %f, want %f", got, want)
	}
}
