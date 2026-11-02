package cli

import (
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

func TestMetricsSinkForwardsEachEventOnce(t *testing.T) {
	forwarded := 0
	s := &metricsSink{inner: event.FuncSink(func(event.Event) { forwarded++ })}
	s.Emit(event.Event{Kind: event.Text, Text: "x"})
	if forwarded != 1 {
		t.Fatalf("inner sink saw %d emissions for one event, want 1", forwarded)
	}
}

func TestMetricsSinkUsesProviderCacheWriteCost(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	s.Emit(event.Event{
		Kind: event.Usage,
		Usage: &provider.Usage{
			CacheMissTokens:        500_000,
			CacheWriteTokens:       100_000,
			CacheWriteBilledTokens: 200_000,
		},
		Pricing: &provider.Pricing{Input: 2},
	})
	if got := s.Snapshot().Cost; got != 1.2 {
		t.Fatalf("metrics cost = %f, want 1.2", got)
	}
}

func TestMetricsSinkAccountsToolCallsAndRetries(t *testing.T) {
	s := &metricsSink{inner: event.Discard}

	s.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{Name: "read_file"}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "read_file", DurationMs: 12}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "read_file", DurationMs: 8}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "bash", Err: "exit status 1", DurationMs: 30}})
	s.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{Name: "grep", ParentID: "task-1", DurationMs: 5}})
	s.Emit(event.Event{Kind: event.Retrying, RetryAttempt: 1, RetryMax: 3})

	if s.m.ToolCalls != 4 {
		t.Fatalf("tool calls = %d, want 4 (dispatch must not count)", s.m.ToolCalls)
	}
	if s.m.ToolFailures != 1 {
		t.Fatalf("tool failures = %d, want 1", s.m.ToolFailures)
	}
	if s.m.ToolDurationMs != 55 {
		t.Fatalf("tool duration = %dms, want 55", s.m.ToolDurationMs)
	}
	if s.m.SubagentToolCalls != 1 {
		t.Fatalf("subagent tool calls = %d, want 1", s.m.SubagentToolCalls)
	}
	if s.m.Retries != 1 {
		t.Fatalf("retries = %d, want 1", s.m.Retries)
	}
	if s.m.ToolCallsByName["read_file"] != 2 || s.m.ToolCallsByName["bash"] != 1 {
		t.Fatalf("calls by name = %v", s.m.ToolCallsByName)
	}
	if s.m.ToolFailuresByName["bash"] != 1 {
		t.Fatalf("failures by name = %v, want bash 1", s.m.ToolFailuresByName)
	}
	if _, ok := s.m.ToolFailuresByName["read_file"]; ok {
		t.Fatalf("a successful tool must not appear in failures: %v", s.m.ToolFailuresByName)
	}
}

// Outcome answers only whether the run errored. A reader that stops there is
// told "success" by the same run that warned its human about open gaps, so the
// turn's own verdict has to reach the file too.
func TestMetricsSinkCarriesTheTurnsOwnVerdict(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	s.Emit(event.Event{Kind: event.CompletionSummary, Completion: &event.CompletionSummaryInfo{
		Verdict: "partial", GapKinds: []string{"stale_verification", "unreviewed_change"},
	}})
	got := s.Snapshot()
	if got.CompletionVerdict != "partial" {
		t.Fatalf("completion verdict = %q, want partial", got.CompletionVerdict)
	}
	if len(got.CompletionGapKinds) != 2 {
		t.Fatalf("gap kinds = %v, want both", got.CompletionGapKinds)
	}
}

// A turn that reopened and finished supersedes what the one before concluded.
func TestMetricsSinkKeepsTheLastVerdict(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	s.Emit(event.Event{Kind: event.CompletionSummary, Completion: &event.CompletionSummaryInfo{
		Verdict: "partial", GapKinds: []string{"stale_verification"},
	}})
	s.Emit(event.Event{Kind: event.CompletionSummary, Completion: &event.CompletionSummaryInfo{Verdict: "done"}})
	got := s.Snapshot()
	if got.CompletionVerdict != "done" || len(got.CompletionGapKinds) != 0 {
		t.Fatalf("verdict = %q gaps = %v, want done with no gaps", got.CompletionVerdict, got.CompletionGapKinds)
	}
}

// The two halves were already on the wire and nothing subtracted them: one real
// session sat at a 13s median per bash call while the shell answered in
// milliseconds, and no surface said so.
func TestRunMetricsSeparatesHostOverheadFromTheCommand(t *testing.T) {
	s := &metricsSink{}
	s.recordToolResult(event.Tool{
		Name: "bash", DurationMs: 6800,
		Execution: &event.ShellExecution{DurationMs: 300},
	})
	if s.m.ToolDurationMs != 6800 {
		t.Fatalf("ToolDurationMs = %d, want the whole call", s.m.ToolDurationMs)
	}
	if s.m.ToolHostOverheadMs != 6500 {
		t.Fatalf("ToolHostOverheadMs = %d, want 6500 — what the host spent around the command", s.m.ToolHostOverheadMs)
	}
	// A tool that reports no inner duration attributes nothing rather than
	// claiming its whole runtime was overhead.
	s.recordToolResult(event.Tool{Name: "read_file", DurationMs: 40})
	if s.m.ToolHostOverheadMs != 6500 {
		t.Fatalf("ToolHostOverheadMs = %d; a call with no shell half must add nothing", s.m.ToolHostOverheadMs)
	}
}
