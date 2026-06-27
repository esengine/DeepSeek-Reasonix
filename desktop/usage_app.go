package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/usage"
)

// DesktopUsageTracker is an App-level usage recorder shared by all tabs.
// It holds a single *usage.Store and exposes Wails-bound query methods so
// the frontend can display usage data.
type DesktopUsageTracker struct {
	store *usage.Store
	dir   string
}

// newDesktopUsageTracker opens (or creates) the shared usage store at
// ~/.reasonix/usage/.
func newDesktopUsageTracker() (*DesktopUsageTracker, error) {
	dir := filepath.Join(config.MemoryUserDir(), "usage")
	store, err := usage.Open(dir)
	if err != nil {
		return nil, err
	}
	return &DesktopUsageTracker{store: store, dir: dir}, nil
}

// usageStoreForBoot returns the shared *usage.Store for boot.Options, or nil
// if the tracker is not initialized.
func (a *App) usageStoreForBoot() *usage.Store {
	t := a.usageTracker.Load()
	store := t.store
	enrichDebugf("[usageStoreForBoot] tracker=%v store=%v", t != nil, store != nil)
	return store
}

// Observe records a usage event. Called from tabEventSink.Emit for every
// event.Usage.
func (t *DesktopUsageTracker) Observe(e event.Event, sessionID string) {
	if e.Kind != event.Usage || e.Usage == nil {
		return
	}
	enrichDebugf("[observe] src=%s prov=%q model=%q sid=%s", e.UsageSource, e.ProviderName, e.ModelName, sessionID)
	r := usage.Record{
		TS:               time.Now(),
		Provider:         e.ProviderName,
		Model:            e.ModelName,
		UsageSource:      e.UsageSource,
		PromptTokens:     e.Usage.PromptTokens,
		CompletionTokens: e.Usage.CompletionTokens,
		CacheHitTokens:   e.Usage.CacheHitTokens,
		CacheMissTokens:  e.Usage.CacheMissTokens,
		ReasoningTokens:  e.Usage.ReasoningTokens,
		TotalTokens:      e.Usage.TotalTokens,
		FinishReason:     e.Usage.FinishReason,
		LatencyMS:        e.LatencyMS,
		SessionID:        sessionID,
	}
	if p := e.Pricing; p != nil {
		r.Cost, r.Currency = usage.CalcCost(r.CacheHitTokens, r.CacheMissTokens, r.CompletionTokens, p)
	}
	t.store.Write(r)
}

// Close closes the store.
func (t *DesktopUsageTracker) Close() {
	if t.store != nil {
		t.store.Close()
	}
}

// ─── Wails-bound query methods ──────────────────────────────────────────────

// UsageOverviewData is the JSON shape returned to the frontend.
type UsageOverviewData struct {
	Overview usage.Overview  `json:"overview"`
	Models   []usage.ModelRow `json:"models"`
}

// UsageOverview returns aggregated usage for the given number of days.
// days=0 means today.
func (a *App) UsageOverview(days int) UsageOverviewData {
	t := a.usageTracker.Load()
	if t == nil {
		return UsageOverviewData{}
	}
	ov, _ := usage.QueryOverview(t.dir, days)
	md, _ := usage.QueryModels(t.dir, days)
	return UsageOverviewData{Overview: ov, Models: md}
}

// UsageTrend returns daily usage trend.
func (a *App) UsageTrend(days int) []usage.TrendPoint {
	t := a.usageTracker.Load()
	if t == nil {
		return nil
	}
	data, _ := usage.QueryTrend(t.dir, days)
	return data
}

// UsageModels returns per-model usage breakdown.
func (a *App) UsageModels(days int) []usage.ModelRow {
	t := a.usageTracker.Load()
	if t == nil {
		return nil
	}
	data, _ := usage.QueryModels(t.dir, days)
	return data
}

// UsageLogsResult wraps a list of log entries for JSON.
type UsageLogsResult struct {
	Entries []usage.LogEntry `json:"entries"`
}

// UsageLogs returns recent usage log entries, filtered by days (0 = all).
func (a *App) UsageLogs(limit, days int, provider, model string) UsageLogsResult {
	t := a.usageTracker.Load()
	if t == nil {
		return UsageLogsResult{}
	}
	data, _ := usage.QueryLogs(t.dir, limit, provider, model, 0)
	if days > 0 {
		cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
		filtered := make([]usage.LogEntry, 0, len(data))
		for _, e := range data {
			if e.TS.Format("2006-01-02") >= cutoff {
				filtered = append(filtered, e)
			}
		}
		data = filtered
	}
	return UsageLogsResult{Entries: data}
}

// UsageProviders returns distinct provider names.
func (a *App) UsageProviders() []string {
	t := a.usageTracker.Load()
	if t == nil {
		return nil
	}
	data, _ := usage.QueryProviders(t.dir)
	return data
}

// UsageModelNames returns distinct model names.
func (a *App) UsageModelNames() []string {
	t := a.usageTracker.Load()
	if t == nil {
		return nil
	}
	data, _ := usage.QueryModelNames(t.dir)
	return data
}

// SaveFile opens a native save dialog and writes content to the chosen path.
// Returns the saved path, or empty string if cancelled.
func (a *App) SaveFile(filename, content string) string {
	if a.ctx == nil {
		return ""
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "Save " + filename,
		DefaultFilename: filename,
	})
	if err != nil || path == "" {
		return ""
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return ""
	}
	return path
}

// UsageDiskUsage returns the total bytes occupied by usage JSONL files.
func (a *App) UsageDiskUsage() int64 {
	t := a.usageTracker.Load()
	if t == nil {
		return 0
	}
	var total int64
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil {
			total += info.Size()
		}
	}
	return total
}

// DeleteUsageData deletes all usage JSONL files and resets the store so new
// writes create fresh files. Returns true on success.
func (a *App) DeleteUsageData() bool {
	t := a.usageTracker.Load()
	if t == nil {
		return false
	}
	// Close the current file handle first so we don't write to a deleted inode.
	t.store.Reset()
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return false
	}
	ok := true
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			if err := os.Remove(filepath.Join(t.dir, e.Name())); err != nil {
				ok = false
			}
		}
	}
	return ok
}
