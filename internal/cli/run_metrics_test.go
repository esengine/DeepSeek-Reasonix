package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
)

func TestMetricsSinkForwardsEachEventOnce(t *testing.T) {
	forwarded := 0
	s := &metricsSink{inner: event.FuncSink(func(event.Event) { forwarded++ })}
	s.Emit(event.Event{Kind: event.Text, Text: "x"})
	if forwarded != 1 {
		t.Fatalf("inner sink saw %d emissions for one event, want 1", forwarded)
	}
}

type metricsCapabilityProbe struct {
	event.Sink
	readiness, delegation, workspace, runBudget int
}

func (p *metricsCapabilityProbe) RecordReadinessAudit(evidence.ReadinessAudit) { p.readiness++ }
func (p *metricsCapabilityProbe) RecordDelegationAudit(evidence.DelegationAudit) {
	p.delegation++
}
func (p *metricsCapabilityProbe) RecordWorkspaceMutation(event.WorkspaceMutation) {
	p.workspace++
}
func (p *metricsCapabilityProbe) RecordRunBudget(event.RunBudgetSample) { p.runBudget++ }

func TestMetricsSinkForwardsObservedCapabilities(t *testing.T) {
	inner := &metricsCapabilityProbe{Sink: event.Discard}
	s := &metricsSink{inner: inner}

	s.RecordReadinessAudit(evidence.ReadinessAudit{})
	s.RecordDelegationAudit(evidence.DelegationAudit{})
	event.RecordWorkspaceMutation(s, event.WorkspaceMutation{ToolName: "write_file"})
	event.RecordRunBudget(s, event.RunBudgetSample{Currency: "USD"})

	if inner.readiness != 1 || inner.delegation != 1 || inner.workspace != 1 || inner.runBudget != 1 {
		t.Fatalf("observed capabilities not forwarded: %+v", inner)
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

func TestMetricsSinkAccumulatesReadinessAudit(t *testing.T) {
	s := &metricsSink{inner: event.Discard}

	s.RecordReadinessAudit(evidence.ReadinessAudit{
		Result:                    evidence.ReadinessBlocked,
		MissingProjectChecks:      2,
		IncompleteTodos:           3,
		CommandMismatchMissing:    2,
		MissingAcceptanceCriteria: 1,
		MissingVerification:       1,
		MissingReview:             1,
		MissingSignoff:            1,
		MissingActionEvidence:     1,
		MissingMutation:           1,
	})
	s.RecordReadinessAudit(evidence.ReadinessAudit{
		Result:    evidence.ReadinessAllowed,
		Recovered: true,
	})
	s.RecordReadinessAudit(evidence.ReadinessAudit{
		Result: evidence.ReadinessErrored,
	})

	if s.m.ReadinessChecks != 3 {
		t.Fatalf("readiness checks = %d, want 3", s.m.ReadinessChecks)
	}
	if s.m.ReadinessAllowed != 1 {
		t.Fatalf("readiness allowed = %d, want 1", s.m.ReadinessAllowed)
	}
	if s.m.ReadinessBlocks != 1 {
		t.Fatalf("readiness blocks = %d, want 1", s.m.ReadinessBlocks)
	}
	if s.m.ReadinessRecoveries != 1 {
		t.Fatalf("readiness recoveries = %d, want 1", s.m.ReadinessRecoveries)
	}
	if s.m.ReadinessErrors != 1 {
		t.Fatalf("readiness errors = %d, want 1", s.m.ReadinessErrors)
	}
	if s.m.ReadinessMissingProjectChecks != 2 {
		t.Fatalf("missing project checks = %d, want 2", s.m.ReadinessMissingProjectChecks)
	}
	if s.m.ReadinessIncompleteTodos != 3 {
		t.Fatalf("incomplete todos = %d, want 3", s.m.ReadinessIncompleteTodos)
	}
	if s.m.ReadinessCommandMismatches != 2 {
		t.Fatalf("command mismatches = %d, want 2", s.m.ReadinessCommandMismatches)
	}
	if s.m.ReadinessMissingAcceptance != 1 || s.m.ReadinessMissingVerification != 1 || s.m.ReadinessMissingReview != 1 || s.m.ReadinessMissingSignoff != 1 {
		t.Fatalf("delivery readiness misses = acceptance %d verification %d review %d signoff %d, want 1/1/1/1",
			s.m.ReadinessMissingAcceptance, s.m.ReadinessMissingVerification, s.m.ReadinessMissingReview, s.m.ReadinessMissingSignoff)
	}
	if s.m.ReadinessMissingActionEvidence != 1 || s.m.ReadinessMissingMutation != 1 {
		t.Fatalf("delivery work misses = action evidence %d mutation %d, want 1/1", s.m.ReadinessMissingActionEvidence, s.m.ReadinessMissingMutation)
	}
}

func TestMetricsSinkAccumulatesSilentReasoningRecovery(t *testing.T) {
	s := &metricsSink{inner: event.Discard}
	s.RecordProtocolRecovery(event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningDetected})
	s.RecordProtocolRecovery(event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryAttempted})
	s.RecordProtocolRecovery(event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryRecovered})
	s.RecordProtocolRecovery(event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetryReplaced})
	s.RecordProtocolRecovery(event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningRetrySuppressed})
	s.RecordProtocolRecovery(event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningFallback})

	if s.m.MissingReasoningDetected != 1 || s.m.MissingReasoningRetries != 1 || s.m.MissingReasoningRecovered != 1 || s.m.MissingReasoningReplaced != 1 || s.m.MissingReasoningSuppressed != 1 || s.m.MissingReasoningFallbacks != 1 {
		t.Fatalf("reasoning recovery metrics = %+v", s.m)
	}
	if s.m.Steps != 1 {
		t.Fatalf("retry model-call step = %d, want 1", s.m.Steps)
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

func TestWriteMetricsIncludesReadinessFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	if err := writeMetrics(path, RunMetrics{
		PromptTokens:                   10,
		CompletionTokens:               3,
		CacheHitTokens:                 7,
		CacheMissTokens:                3,
		Steps:                          2,
		ReadinessChecks:                1,
		ReadinessAllowed:               1,
		ReadinessBlocks:                0,
		ReadinessRecoveries:            1,
		ReadinessErrors:                0,
		ReadinessMissingProjectChecks:  0,
		ReadinessIncompleteTodos:       0,
		ReadinessCommandMismatches:     0,
		ReadinessMissingAcceptance:     0,
		ReadinessMissingVerification:   0,
		ReadinessMissingReview:         0,
		ReadinessMissingSignoff:        0,
		ReadinessMissingActionEvidence: 0,
		ReadinessMissingMutation:       0,
	}); err != nil {
		t.Fatalf("writeMetrics: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{
		"readiness_checks",
		"readiness_allowed",
		"readiness_blocks",
		"readiness_recoveries",
		"readiness_errors",
		"readiness_missing_project_checks",
		"readiness_incomplete_todos",
		"readiness_command_mismatches",
		"readiness_missing_acceptance_criteria",
		"readiness_missing_verification",
		"readiness_missing_review",
		"readiness_missing_signoff",
		"readiness_missing_action_evidence",
		"readiness_missing_mutation",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("metrics JSON missing %q: %s", key, string(b))
		}
	}
}
