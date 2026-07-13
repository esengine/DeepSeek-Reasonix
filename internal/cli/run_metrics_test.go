package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

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

func TestMetricsSinkAccumulatesMemoryCompilerStats(t *testing.T) {
	s := &metricsSink{inner: event.Discard}

	s.Emit(event.Event{Kind: event.MemoryCompilerStatsEvent, MemoryCompiler: &event.MemoryCompilerStats{
		Injected:         true,
		UsefulIR:         true,
		CompiledTokens:   120,
		IROverheadTokens: 30,
		MemoryReferences: 2,
		Constraints:      3,
		RiskNotes:        1,
		ExecutionSteps:   4,
		TotalNodes:       10,
		HighSignalNodes:  6,
		ToolResultNodes:  2,
		DecisionNodes:    1,
		StrategyCount:    2,
		LearningCount:    3,
	}})
	s.Emit(event.Event{Kind: event.MemoryCompilerStatsEvent, MemoryCompiler: &event.MemoryCompilerStats{
		Injected:         false,
		UsefulIR:         false,
		CompiledTokens:   0,
		IROverheadTokens: 0,
		MemoryReferences: 1,
		Constraints:      0,
		RiskNotes:        0,
		ExecutionSteps:   0,
		TotalNodes:       12,
		HighSignalNodes:  7,
		ToolResultNodes:  3,
		DecisionNodes:    2,
		StrategyCount:    4,
		LearningCount:    5,
	}})
	s.Emit(event.Event{Kind: event.MemoryCompilerStatsEvent})

	if s.m.MemoryCompilerTurns != 2 {
		t.Fatalf("memory compiler turns = %d, want 2", s.m.MemoryCompilerTurns)
	}
	if s.m.MemoryCompilerInjectedTurns != 1 {
		t.Fatalf("memory compiler injected turns = %d, want 1", s.m.MemoryCompilerInjectedTurns)
	}
	if s.m.MemoryCompilerUsefulIRTurns != 1 {
		t.Fatalf("memory compiler useful IR turns = %d, want 1", s.m.MemoryCompilerUsefulIRTurns)
	}
	if s.m.MemoryCompilerCompiledTokens != 120 || s.m.MemoryCompilerIROverheadTokens != 30 {
		t.Fatalf("memory compiler tokens = compiled %d overhead %d, want 120/30", s.m.MemoryCompilerCompiledTokens, s.m.MemoryCompilerIROverheadTokens)
	}
	if s.m.MemoryCompilerMemoryReferences != 3 || s.m.MemoryCompilerConstraints != 3 || s.m.MemoryCompilerRiskNotes != 1 || s.m.MemoryCompilerExecutionSteps != 4 {
		t.Fatalf("memory compiler totals = refs %d constraints %d risks %d steps %d, want 3/3/1/4", s.m.MemoryCompilerMemoryReferences, s.m.MemoryCompilerConstraints, s.m.MemoryCompilerRiskNotes, s.m.MemoryCompilerExecutionSteps)
	}
	if s.m.MemoryCompilerTotalNodes != 12 || s.m.MemoryCompilerHighSignalNodes != 7 || s.m.MemoryCompilerToolResultNodes != 3 {
		t.Fatalf("memory compiler latest nodes = total %d high %d tool %d, want 12/7/3", s.m.MemoryCompilerTotalNodes, s.m.MemoryCompilerHighSignalNodes, s.m.MemoryCompilerToolResultNodes)
	}
	if s.m.MemoryCompilerDecisionNodes != 2 || s.m.MemoryCompilerStrategyCount != 4 || s.m.MemoryCompilerLearningCount != 5 {
		t.Fatalf("memory compiler latest registry counts = decisions %d strategies %d learnings %d, want 2/4/5", s.m.MemoryCompilerDecisionNodes, s.m.MemoryCompilerStrategyCount, s.m.MemoryCompilerLearningCount)
	}
	if len(s.m.MemoryCompilerTurnDetails) != 2 {
		t.Fatalf("memory compiler turn details = %d, want 2", len(s.m.MemoryCompilerTurnDetails))
	}
	if got := s.m.MemoryCompilerTurnDetails[0]; !got.Injected || got.CompiledTokens != 120 || got.TotalNodes != 10 {
		t.Fatalf("first memory compiler detail = %+v, want injected compiled=120 total_nodes=10", got)
	}
	if got := s.m.MemoryCompilerTurnDetails[1]; got.Injected || got.MemoryReferences != 1 || got.TotalNodes != 12 {
		t.Fatalf("second memory compiler detail = %+v, want not injected refs=1 total_nodes=12", got)
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
		MemoryCompilerTurns:            1,
		MemoryCompilerInjectedTurns:    1,
		MemoryCompilerCompiledTokens:   42,
		MemoryCompilerTotalNodes:       9,
		MemoryCompilerTurnDetails: []RunMemoryCompilerMetrics{{
			Injected:         true,
			CompiledTokens:   42,
			MemoryReferences: 2,
			TotalNodes:       9,
		}},
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
		"memory_compiler_turns",
		"memory_compiler_injected_turns",
		"memory_compiler_useful_ir_turns",
		"memory_compiler_compiled_tokens",
		"memory_compiler_ir_overhead_tokens",
		"memory_compiler_memory_references",
		"memory_compiler_constraints",
		"memory_compiler_risk_notes",
		"memory_compiler_execution_steps",
		"memory_compiler_total_nodes",
		"memory_compiler_high_signal_nodes",
		"memory_compiler_tool_result_nodes",
		"memory_compiler_decision_nodes",
		"memory_compiler_strategy_count",
		"memory_compiler_learning_count",
		"memory_compiler_turn_details",
	} {
		if _, ok := got[key]; !ok {
			t.Fatalf("metrics JSON missing %q: %s", key, string(b))
		}
	}
	details, ok := got["memory_compiler_turn_details"].([]any)
	if !ok || len(details) != 1 {
		t.Fatalf("memory_compiler_turn_details = %#v, want one detail", got["memory_compiler_turn_details"])
	}
}
