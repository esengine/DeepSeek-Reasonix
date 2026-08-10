package cli

import (
	"testing"

	"reasonix/internal/evidence"
)

// The instrument must be able to say, from one run, whether delegation
// produced verified work or just more tokens.
func TestDelegationMetricsAggregateAcrossChildren(t *testing.T) {
	s := &metricsSink{}
	s.RecordDelegationAudit(evidence.DelegationAudit{
		Depth: 1, ToolCalls: 6, Mutations: 2,
		MutationPaths:     []string{"api/handler.go", "api/handler_test.go"},
		HasReport:         true,
		AdjudicatedStatus: string(evidence.CompletionComplete),
	})
	s.RecordDelegationAudit(evidence.DelegationAudit{
		Depth: 2, ToolCalls: 4, Mutations: 1,
		MutationPaths:     []string{"api/handler.go"},
		ClaimViolations:   1,
		HasReport:         true,
		AdjudicatedStatus: string(evidence.CompletionPartial),
		Downgrades:        2,
	})
	s.RecordDelegationAudit(evidence.DelegationAudit{Depth: 1, ToolCalls: 3})

	m := s.m
	if m.SubagentRuns != 3 || m.SubagentNestedRuns != 1 {
		t.Fatalf("runs = %d nested = %d, want 3/1", m.SubagentRuns, m.SubagentNestedRuns)
	}
	if m.SubagentMutations != 3 {
		t.Fatalf("mutations = %d, want 3", m.SubagentMutations)
	}
	if m.CompletionReports != 2 || m.CompletionsProsedOnly != 1 {
		t.Fatalf("reports = %d prose-only = %d, want 2/1", m.CompletionReports, m.CompletionsProsedOnly)
	}
	// One child claimed criteria the host refused: that is the false-completion
	// signal an orchestration benchmark exists to surface.
	if m.FalseCompletions != 1 || m.CriterionDowngrades != 2 {
		t.Fatalf("false completions = %d downgrades = %d, want 1/2", m.FalseCompletions, m.CriterionDowngrades)
	}
	if m.WriteScopeViolations != 1 {
		t.Fatalf("write scope violations = %d, want 1", m.WriteScopeViolations)
	}
	// Two children mutated api/handler.go: duplicated work, counted once.
	if m.DuplicateWorkPaths != 1 {
		t.Fatalf("duplicate work paths = %d, want 1", m.DuplicateWorkPaths)
	}
}

// A run with no delegation must leave every delegation counter at zero, so the
// single-agent arm is a clean baseline rather than noise.
func TestDelegationMetricsStayZeroForSingleAgentArm(t *testing.T) {
	s := &metricsSink{}
	if m := s.m; m.SubagentRuns != 0 || m.CompletionReports != 0 || m.DuplicateWorkPaths != 0 {
		t.Fatalf("single-agent baseline is not zero: %+v", m)
	}
}
