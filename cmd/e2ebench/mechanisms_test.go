package main

import (
	"os"
	"strings"
	"testing"
)

func TestRenderMechanismLedgerSplitsFiredVsQuiet(t *testing.T) {
	fired := result{task: task{ID: "a"}}
	fired.Trajectory = &trajectorySummary{
		HandoffNudges:  2,
		RoundOutcomeMs: map[string]int64{"handoff_retry": 7_000},
		StreamRetries:  1,
		RecoveryGapMsByKind: map[string]int64{
			"stream_retry": 9_000,
		},
	}
	quiet := result{task: task{ID: "b"}, Passed: true}
	quiet.Trajectory = &trajectorySummary{}

	got := renderMechanismLedger([]result{fired, quiet})
	for _, want := range []string{
		"**Mechanism ledger**",
		"causal rescue rates need an `-ablate` A/B",
		"| handoff_nudge | 2 | 1/2 | 7.0s | 0% | 100% |",
		"| stream_retry | 1 | 1/2 | 9.0s | 0% | 100% |",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ledger missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "| planner |") {
		t.Fatalf("mechanisms that never fired must not render rows:\n%s", got)
	}

	allQuiet := renderMechanismLedger([]result{quiet})
	if !strings.Contains(allQuiet, "all quiet — no extra-round machinery fired across 1 recorded runs") {
		t.Fatalf("all-quiet suite must state the absence:\n%s", allQuiet)
	}
}

func TestSummarizeTrajectoryCollectsToolSurface(t *testing.T) {
	path := t.TempDir() + "/surface.trajectory.jsonl"
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":1500,"event":{"kind":"usage","usage":{"source":"executor","promptTokens":14000,"cacheDiagnostics":{"toolSchemaTokens":2000,"prefixChanged":false}}}}`,
		`{"seq":3,"ts":2000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"connect_tool_source","args":"{\"source\":\"web\"}"}}}`,
		`{"seq":4,"ts":2100,"event":{"kind":"tool_result","tool":{"id":"a","name":"connect_tool_source","durationMs":100,"startedAt":2000,"endedAt":2100}}}`,
		`{"seq":5,"ts":3000,"event":{"kind":"usage","usage":{"source":"executor","promptTokens":16000,"cacheDiagnostics":{"toolSchemaTokens":5000,"prefixChanged":true}}}}`,
		`{"seq":6,"ts":3500,"event":{"kind":"usage","usage":{"source":"subagent","promptTokens":9000,"cacheDiagnostics":{"toolSchemaTokens":9999,"prefixChanged":true}}}}`,
		`{"seq":7,"ts":4000,"event":{"kind":"turn_done"}}`,
	}
	if err := writeLines(path, lines); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.SchemaTokensMax != 5000 || s.SchemaTokensTotal != 7000 {
		t.Errorf("schema max=%d total=%d, want 5000/7000 (subagent usage excluded)", s.SchemaTokensMax, s.SchemaTokensTotal)
	}
	if s.PromptTokensSeen != 30000 {
		t.Errorf("prompt tokens = %d, want 30000", s.PromptTokensSeen)
	}
	if s.PrefixResets != 1 {
		t.Errorf("prefix resets = %d, want 1 (subagent reset must not count)", s.PrefixResets)
	}
	if s.ConnectCalls != 1 {
		t.Errorf("connect calls = %d, want 1", s.ConnectCalls)
	}
}

func TestKPILineIncludesTTFTAndFirstRequestCacheHit(t *testing.T) {
	cold := result{task: task{ID: "a"}, Passed: true, WallMs: 10_000, Attempt: 1, TTCSMs: 10_000}
	cold.Trajectory = &trajectorySummary{
		TTFTMs:                  2800,
		FirstReqCacheHitTokens:  400,
		FirstReqCacheMissTokens: 13_600,
	}
	got := renderBody([]result{cold})
	for _, want := range []string{
		"**TTFT median** 2.8s",
		"**first-request cache hit** 3%",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("KPI line missing %q:\n%s", want, got)
		}
	}
}

func TestRenderToolSurfaceLine(t *testing.T) {
	r := result{task: task{ID: "a"}, Passed: true}
	r.Trajectory = &trajectorySummary{
		SchemaTokensMax: 12784, SchemaTokensTotal: 89488,
		PromptTokensSeen: 140000, PrefixResets: 2, ConnectCalls: 1,
	}
	got := renderToolSurface([]result{r})
	for _, want := range []string{
		"**schema footprint** 12,784 tok/request",
		"**Σ schema tax** 89,488 tok (64% of prompt)",
		"**connect_tool_source** ×1",
		"**prefix resets** 2",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("tool surface line missing %q:\n%s", want, got)
		}
	}
	if renderToolSurface([]result{{task: task{ID: "b"}}}) != "" {
		t.Fatal("runs without schema data must not render the line")
	}
}

func TestSummarizeTrajectorySplitsRecoveryGapByKind(t *testing.T) {
	path := t.TempDir() + "/kinds.trajectory.jsonl"
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":2000,"event":{"kind":"retrying","retryScope":"stream"}}`,
		`{"seq":3,"ts":6000,"event":{"kind":"tool_dispatch","tool":{"id":"a","name":"bash","args":"{}"}}}`,
		`{"seq":4,"ts":6100,"event":{"kind":"tool_result","tool":{"id":"a","name":"bash","durationMs":100,"startedAt":6000,"endedAt":6100}}}`,
		`{"seq":5,"ts":7000,"protocol_recovery":"missing_reasoning_retry_attempted"}`,
		`{"seq":6,"ts":9000,"event":{"kind":"tool_dispatch","tool":{"id":"b","name":"bash","args":"{\"x\":1}"}}}`,
		`{"seq":7,"ts":9100,"event":{"kind":"tool_result","tool":{"id":"b","name":"bash","durationMs":100,"startedAt":9000,"endedAt":9100}}}`,
		`{"seq":8,"ts":10000,"event":{"kind":"turn_done"}}`,
	}
	if err := writeLines(path, lines); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.RecoveryGapMsByKind["stream_retry"] != 5000 {
		t.Errorf("stream_retry gap = %d, want 5000", s.RecoveryGapMsByKind["stream_retry"])
	}
	if s.RecoveryGapMsByKind["reasoning_replay"] != 2900 {
		t.Errorf("reasoning_replay gap = %d, want 2900", s.RecoveryGapMsByKind["reasoning_replay"])
	}
	if s.RecoveryRounds != 2 {
		t.Errorf("recovery rounds = %d, want 2", s.RecoveryRounds)
	}
}

func writeLines(path string, lines []string) error {
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

func TestRenderContractShadowAgreement(t *testing.T) {
	agree := result{task: task{ID: "a"}, Passed: true}
	agree.Trajectory = &trajectorySummary{ShadowVerdict: "complete", ShadowComplete: true, ShadowIntent: "mutation"}
	miss := result{task: task{ID: "b"}, Passed: true}
	miss.Trajectory = &trajectorySummary{ShadowVerdict: "continue", ShadowComplete: false}
	got := renderContractShadow([]result{agree, miss})
	for _, want := range []string{
		"verdicts complete ×1 · continue ×1",
		"**agreement with grader** 50% (1/2)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("shadow line missing %q:\n%s", want, got)
		}
	}
	if renderContractShadow([]result{{task: task{ID: "c"}}}) != "" {
		t.Fatal("runs without shadow audits must render nothing")
	}
}

func TestSummarizeTrajectoryReadsContractShadow(t *testing.T) {
	path := t.TempDir() + "/shadow.trajectory.jsonl"
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":2000,"contract_shadow":{"intent":"mutation","verdict":"complete","complete":true}}`,
		`{"seq":3,"ts":3000,"event":{"kind":"turn_done"}}`,
	}
	if err := writeLines(path, lines); err != nil {
		t.Fatal(err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.ShadowVerdict != "complete" || !s.ShadowComplete || s.ShadowIntent != "mutation" {
		t.Fatalf("shadow = %q/%v/%q", s.ShadowVerdict, s.ShadowComplete, s.ShadowIntent)
	}
}
