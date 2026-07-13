package eventwire

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestToWireRetryingJSON(t *testing.T) {
	w := ToWire(event.Event{Kind: event.Retrying, RetryAttempt: 3, RetryMax: 10})
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"kind":"retrying"`, `"retryAttempt":3`, `"retryMax":10`} {
		if !strings.Contains(s, want) {
			t.Fatalf("retrying JSON = %s, want it to contain %s", s, want)
		}
	}
}

func TestToWireNoticeCarriesCode(t *testing.T) {
	w := ToWire(event.Event{Kind: event.Notice, Level: event.LevelInfo, Code: event.NoticeCodeFinalReadiness, Text: "readiness copy"})
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"code":"final_readiness"`) {
		t.Fatalf("notice JSON = %s, want a stable code field", b)
	}

	w = ToWire(event.Event{Kind: event.Notice, Level: event.LevelInfo, Text: "codeless notice"})
	if b, err = json.Marshal(w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"code"`) {
		t.Fatalf("codeless notice JSON = %s, must omit the code field", b)
	}

	w = ToWire(event.Event{Kind: event.Text, Code: "stray"})
	if b, err = json.Marshal(w); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), `"code"`) {
		t.Fatalf("non-notice JSON = %s, must not carry a code", b)
	}
}

func TestKindNamesComplete(t *testing.T) {
	for k := event.Kind(0); k < event.KindCount; k++ {
		if ToWire(event.Event{Kind: k}).Kind == "" {
			t.Fatalf("kind %d has no wire name", k)
		}
	}
}

func TestDesktopWireEventKindTypeCoversSharedKinds(t *testing.T) {
	ts := readDesktopTypes(t)
	for k := event.Kind(0); k < event.KindCount; k++ {
		kind := ToWire(event.Event{Kind: k}).Kind
		if !strings.Contains(ts, `"`+kind+`"`) {
			t.Fatalf("desktop WireEvent EventKind is missing %q", kind)
		}
	}
}

func TestDesktopWireEventTypeCoversSharedPayloadFields(t *testing.T) {
	ts := readDesktopTypes(t)
	for _, want := range []string{
		"detail?: string;",
		`outcome?: "final_readiness";`,
		"retryAttempt?: number;",
		"retryMax?: number;",
		"memoryCitations?: MemoryCitation[];",
		"export interface MemoryCitation",
		"memoryCompiler?: MemoryCompilerStats;",
		"export interface MemoryCompilerStats",
		"cacheDiagnostics?: WireCacheDiagnostics;",
		"export interface WireCacheDiagnostics",
		"prefixHash: string;",
		"prefixChanged: boolean;",
		"prefixChangeReasons?: string[];",
		"toolSchemaTokens: number;",
	} {
		if !strings.Contains(ts, want) {
			t.Fatalf("desktop WireEvent types are missing %q", want)
		}
	}
}

func TestToWireNoticeDetail(t *testing.T) {
	w := ToWire(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: "short", Detail: "diagnostics"})
	if w.Kind != "notice" || w.Level != "warn" || w.Text != "short" || w.Detail != "diagnostics" {
		t.Fatalf("wire notice = %+v", w)
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"kind":"notice"`, `"text":"short"`, `"detail":"diagnostics"`, `"level":"warn"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("notice JSON = %s, want it to contain %s", string(b), want)
		}
	}
}

func TestToWireTurnOutcomeIsOptionalAndMachineReadable(t *testing.T) {
	readiness := ToWire(event.Event{
		Kind:    event.TurnDone,
		Err:     errors.New("final-answer readiness failed 3 times: missing verification"),
		Outcome: event.TurnOutcomeFinalReadiness,
	})
	if readiness.Outcome != event.TurnOutcomeFinalReadiness || readiness.Err == "" {
		t.Fatalf("readiness wire event = %+v", readiness)
	}
	b, err := json.Marshal(readiness)
	if err != nil {
		t.Fatalf("marshal readiness: %v", err)
	}
	if !strings.Contains(string(b), `"outcome":"final_readiness"`) {
		t.Fatalf("readiness JSON = %s, want structured outcome", b)
	}

	ordinary, err := json.Marshal(ToWire(event.Event{Kind: event.TurnDone, Err: errors.New("provider failed")}))
	if err != nil {
		t.Fatalf("marshal ordinary error: %v", err)
	}
	if strings.Contains(string(ordinary), `"outcome"`) {
		t.Fatalf("ordinary error JSON must omit outcome: %s", ordinary)
	}
}

func TestToWireMemoryCompilerStats(t *testing.T) {
	w := ToWire(event.Event{
		Kind: event.MemoryCompilerStatsEvent,
		MemoryCompiler: &event.MemoryCompilerStats{
			Injected:         true,
			UsefulIR:         true,
			CompiledTokens:   1200,
			IROverheadTokens: 300,
			MemoryReferences: 3,
			Constraints:      2,
			RiskNotes:        1,
			ExecutionSteps:   4,
			TotalNodes:       42,
			HighSignalNodes:  11,
			ToolResultNodes:  7,
			DecisionNodes:    5,
			StrategyCount:    3,
			LearningCount:    6,
		},
	})
	if w.Kind != "memory_compiler_stats" || w.MemoryCompiler == nil {
		t.Fatalf("wire memory compiler stats = %+v", w)
	}
	if !w.MemoryCompiler.Injected || w.MemoryCompiler.TotalNodes != 42 || w.MemoryCompiler.CompiledTokens != 1200 {
		t.Fatalf("wire memory compiler payload = %+v", w.MemoryCompiler)
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "secret") || !strings.Contains(string(b), `"memoryCompiler":`) {
		t.Fatalf("memory compiler stats JSON should contain only metrics payload: %s", string(b))
	}
}

func TestToWireMessageMemoryCitations(t *testing.T) {
	w := ToWire(event.Event{
		Kind: event.Message,
		Text: "done",
		MemoryCitations: []provider.MemoryCitation{{
			ID:        "mem-1",
			Source:    "MEMORY.md",
			LineStart: 116,
			LineEnd:   123,
			Note:      "reasonix workflow",
			Kind:      "memory_reference",
		}},
	})
	if len(w.MemoryCitations) != 1 {
		t.Fatalf("memory citations = %+v, want one citation", w.MemoryCitations)
	}
	got := w.MemoryCitations[0]
	if got.Source != "MEMORY.md" || got.LineStart != 116 || got.LineEnd != 123 || got.Note != "reasonix workflow" {
		t.Fatalf("citation = %+v, want source/line/note preserved", got)
	}
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"memoryCitations":[`) {
		t.Fatalf("wire JSON missing memoryCitations: %s", string(b))
	}
}

func readDesktopTypes(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	path := filepath.Join(filepath.Dir(file), "..", "..", "desktop", "frontend", "src", "lib", "types.ts")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read desktop types: %v", err)
	}
	return string(b)
}

func TestToWireToolPayloadJSON(t *testing.T) {
	w := ToWire(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{
		ID: "call-1", Name: "task", Args: `{"prompt":"x"}`, Output: "ignored",
		Err: "blocked", ReadOnly: true, Truncated: true, DurationMs: 522,
		Partial: true, ParentID: "parent-1",
		FileDiff: event.FileDiff{Diff: "@@ -1 +1 @@\n-old\n+new\n", Added: 1, Removed: 1},
		Profile:  &event.Profile{Model: "deepseek-pro", Effort: "max"},
	}})
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"kind":"tool_dispatch"`, `"id":"call-1"`, `"name":"task"`,
		`"args":"{\"prompt\":\"x\"}"`, `"output":"ignored"`, `"err":"blocked"`,
		`"readOnly":true`, `"truncated":true`, `"durationMs":522`, `"partial":true`,
		`"parentId":"parent-1"`, `"diff":"@@ -1 +1 @@\n-old\n+new\n"`,
		`"added":1`, `"removed":1`, `"profile":{"model":"deepseek-pro","effort":"max"}`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("tool JSON = %s, want it to contain %s", s, want)
		}
	}
}

func TestToWireUsagePayloadJSON(t *testing.T) {
	w := ToWire(event.Event{
		Kind: event.Usage,
		Usage: &provider.Usage{
			PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200,
			CacheHitTokens: 900, CacheMissTokens: 100, ReasoningTokens: 33,
		},
		Pricing:     &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2},
		UsageSource: event.UsageSourceTitle,
		CacheDiagnostics: &event.CacheDiagnostics{
			PrefixHash: "p", PrefixChanged: true, PrefixChangeReasons: []string{"log_rewrite"},
			SystemHash: "s", ToolsHash: "t", LogRewriteVersion: 1, ToolSchemaTokens: 42,
			CacheMissTokens: 100, CacheHitTokens: 900,
		},
		SessionHit: 8000, SessionMiss: 2000,
	})
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"kind":"usage"`, `"promptTokens":1000`, `"completionTokens":200`, `"totalTokens":1200`,
		`"cacheHitTokens":900`, `"cacheMissTokens":100`, `"reasoningTokens":33`,
		`"source":"title"`, `"sessionCacheHitTokens":8000`, `"sessionCacheMissTokens":2000`,
		`"currency":"¥"`, `"costUsd":`, `"cacheDiagnostics":`, `"prefixHash":"p"`,
		`"prefixChanged":true`, `"prefixChangeReasons":["log_rewrite"]`, `"toolSchemaTokens":42`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("usage JSON = %s, want it to contain %s", s, want)
		}
	}
}

func TestToWireInteractionAndLifecyclePayloads(t *testing.T) {
	tests := []struct {
		name string
		in   event.Event
		want []string
	}{
		{
			name: "approval",
			in:   event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "a1", Tool: "bash", Subject: "rm"}},
			want: []string{`"kind":"approval_request"`, `"approval":{"id":"a1","tool":"bash","subject":"rm"}`},
		},
		{
			name: "ask",
			in: event.Event{Kind: event.AskRequest, Ask: event.Ask{
				ID: "ask-1",
				Questions: []event.AskQuestion{{
					ID: "q1", Header: "Pick", Prompt: "Choose", Multi: true,
					Options: []event.AskOption{{Label: "A", Description: "Alpha"}, {Label: "B"}},
				}},
			}},
			want: []string{`"kind":"ask_request"`, `"ask":{"id":"ask-1"`, `"header":"Pick"`, `"description":"Alpha"`, `"multi":true`},
		},
		{
			name: "compaction",
			in: event.Event{Kind: event.CompactionDone, Compaction: event.Compaction{
				Trigger: "manual", Messages: 7, Summary: "brief", Archive: "/tmp/archive.jsonl",
			}},
			want: []string{`"kind":"compaction_done"`, `"trigger":"manual"`, `"messages":7`, `"summary":"brief"`, `"archive":"/tmp/archive.jsonl"`},
		},
		{
			name: "turn done error",
			in:   event.Event{Kind: event.TurnDone, Err: errors.New("boom")},
			want: []string{`"kind":"turn_done"`, `"err":"boom"`},
		},
		{
			name: "steer",
			in:   event.Event{Kind: event.Steer, Text: "mid-turn guidance"},
			want: []string{`"kind":"steer"`, `"text":"mid-turn guidance"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := json.Marshal(ToWire(tt.in))
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			s := string(b)
			for _, want := range tt.want {
				if !strings.Contains(s, want) {
					t.Fatalf("%s JSON = %s, want it to contain %s", tt.name, s, want)
				}
			}
		})
	}
}
