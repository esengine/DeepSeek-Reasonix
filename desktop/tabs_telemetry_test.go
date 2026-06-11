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

	app := &App{tabs: map[string]*WorkspaceTab{"tab": tab}}
	if context := app.ContextUsageForTab("tab"); context.SessionTokens != 140 {
		t.Fatalf("context usage session tokens = %d, want 140", context.SessionTokens)
	}
	if panel := app.ContextPanel("tab"); panel.TotalTokens != 140 {
		t.Fatalf("context panel total tokens = %d, want 140", panel.TotalTokens)
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
