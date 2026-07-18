package sessiontelemetry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

func TestUsageStatsPreserveLocalAccountingSemantics(t *testing.T) {
	var stats UsageStats
	stats.TurnStarted(1_000)
	stats.RecordUsage(&eventwire.Usage{
		PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110, ReasoningTokens: 4,
		CacheHitTokens: 1, CacheMissTokens: 2, SessionCacheHitTokens: 40, SessionCacheMissTokens: 60,
		Source: event.UsageSourceExecutor, Cost: 1.25, Currency: "¥",
	})
	stats.RecordUsage(&eventwire.Usage{
		PromptTokens: 20, CompletionTokens: 3, TotalTokens: 23,
		CacheHitTokens: 9, CacheMissTokens: 9, SessionCacheHitTokens: 45, SessionCacheMissTokens: 68,
		Source: event.UsageSourceExecutor, CostUSD: .5, Currency: "¥",
	})
	stats.RecordUsage(&eventwire.Usage{
		PromptTokens: 7, CompletionTokens: 2, TotalTokens: 9,
		CacheHitTokens: 3, CacheMissTokens: 4, Source: "subagent", Cost: .25, Currency: "$",
	})
	stats.TurnDone(1_250)

	if stats.PromptTokens != 127 || stats.CompletionTokens != 15 || stats.TotalTokens != 142 || stats.ReasoningTokens != 4 {
		t.Fatalf("token totals = %+v", stats)
	}
	// First executor event consumes the cumulative counters (40/60); the next
	// consumes only their deltas (5/8); subagent usage uses event-local values.
	if stats.CacheHitTokens != 48 || stats.CacheMissTokens != 72 {
		t.Fatalf("cache totals = %d/%d, want 48/72", stats.CacheHitTokens, stats.CacheMissTokens)
	}
	if stats.RequestCount != 3 || stats.ElapsedMs != 250 || stats.SessionCost != 2 || stats.SessionCostUsd != 2 || stats.SessionCurrency != "$" {
		t.Fatalf("session totals = %+v", stats)
	}
	executor := stats.Sources[event.UsageSourceExecutor]
	if executor.RequestCount != 2 || executor.CacheHitTokens != 45 || executor.CacheMissTokens != 68 || executor.SessionCost != 1.75 {
		t.Fatalf("executor source = %+v", executor)
	}
	if subagent := stats.Sources["subagent"]; subagent.RequestCount != 1 || subagent.CacheHitTokens != 3 || subagent.SessionCost != .25 {
		t.Fatalf("subagent source = %+v", subagent)
	}
}

func TestUsageSnapshotIncludesLiveElapsedWithoutMutatingState(t *testing.T) {
	stats := UsageStats{ElapsedMs: 10}
	stats.TurnStarted(100)
	first := stats.At(175)
	second := stats.At(200)
	if first.ElapsedMs != 85 || second.ElapsedMs != 110 || stats.ElapsedMs != 10 || stats.ActiveTurnStartedAt != 100 {
		t.Fatalf("elapsed snapshots first=%+v second=%+v source=%+v", first, second, stats)
	}
	if first.ActiveTurnStartedAt != 0 || first.SourceSessionCache != nil {
		t.Fatalf("runtime-only state leaked into snapshot: %+v", first)
	}
}

func TestReadFileFromEventNormalizesAndParses(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "docs", "guide.md")
	eventValue := eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{
		Name: "read_file", Args: `{"path":` + quoted(path) + `,"offset":3,"limit":7}`,
		Output: "File truncated", Truncated: false,
	}}
	record, ok := ReadFileFromEvent(eventValue, 4, 1234, root)
	if !ok {
		t.Fatal("read_file result was not recognized")
	}
	if record.Path != "docs/guide.md" || record.Turn != 4 || record.Time != 1234 || record.Offset != 3 || record.Limit != 7 || !record.Truncated {
		t.Fatalf("record = %+v", record)
	}
	if _, ok := ReadFileFromEvent(eventwire.Event{Kind: "tool_result", Tool: &eventwire.Tool{Name: "read_file", Err: "failed"}}, 0, 0, root); ok {
		t.Fatal("failed read_file was recorded")
	}
}

func TestSnapshotCloneAndPersistenceCompatibility(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.telemetry.json")
	snapshot := Snapshot{
		Version:   Version,
		ReadFiles: []ReadFileRecord{{Path: "README.md", Turn: 1, Time: 2}},
		Usage:     UsageStats{PromptTokens: 3, SessionCost: 4, Sources: map[string]UsageSourceStats{"executor": {TotalTokens: 5}}},
	}
	if err := Save(path, snapshot); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("new mode = %v, %v", info, err)
		}
		if err := os.Chmod(path, 0o640); err != nil {
			t.Fatal(err)
		}
		if err := Save(path, snapshot); err != nil {
			t.Fatal(err)
		}
		info, err = os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("preserved mode = %v, %v", info, err)
		}
	}
	loaded := Load(path)
	if !reflect.DeepEqual(loaded.ReadFiles, snapshot.ReadFiles) || loaded.Usage.PromptTokens != 3 || loaded.Usage.Sources["executor"].TotalTokens != 5 {
		t.Fatalf("loaded = %+v", loaded)
	}

	clone := loaded.Clone()
	clone.ReadFiles[0].Path = "changed"
	value := clone.Usage.Sources["executor"]
	value.TotalTokens = 99
	clone.Usage.Sources["executor"] = value
	if loaded.ReadFiles[0].Path != "README.md" || loaded.Usage.Sources["executor"].TotalTokens != 5 {
		t.Fatalf("clone aliases source: loaded=%+v clone=%+v", loaded, clone)
	}

	legacyPath := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(legacyPath, []byte(`[{"path":"old.go","turn":2,"time":3}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := Load(legacyPath)
	if legacy.Version != 1 || len(legacy.ReadFiles) != 1 || legacy.ReadFiles[0].Path != "old.go" {
		t.Fatalf("legacy = %+v", legacy)
	}

	aliasCostPath := filepath.Join(dir, "alias-cost.json")
	if err := os.WriteFile(aliasCostPath, []byte(`{"version":2,"readFiles":[],"usage":{"sessionCostUsd":1.5}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if alias := Load(aliasCostPath); alias.Usage.SessionCost != 1.5 {
		t.Fatalf("legacy cost alias = %+v", alias.Usage)
	}
}

func TestViewUsesNonNilReadFilesAndDeepCopiesSources(t *testing.T) {
	usage := UsageStats{Sources: map[string]UsageSourceStats{"executor": {RequestCount: 1}}}
	view := View(nil, usage, 0)
	if view.ReadFiles == nil || len(view.ReadFiles) != 0 {
		t.Fatalf("readFiles = %#v", view.ReadFiles)
	}
	value := view.Usage.Sources["executor"]
	value.RequestCount = 2
	view.Usage.Sources["executor"] = value
	if usage.Sources["executor"].RequestCount != 1 {
		t.Fatal("View aliased source map")
	}
}

func quoted(value string) string {
	body, _ := json.Marshal(value)
	return string(body)
}
