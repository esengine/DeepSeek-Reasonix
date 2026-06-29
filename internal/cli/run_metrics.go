package cli

import (
	"encoding/json"
	"os"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

// RunMetrics is the machine-readable token/cache/cost summary `run --metrics`
// writes, so a benchmark harness can read a run's cost without scraping stdout.
type RunMetrics struct {
	PromptTokens                   int                        `json:"prompt_tokens"`
	CompletionTokens               int                        `json:"completion_tokens"`
	CacheHitTokens                 int                        `json:"cache_hit_tokens"`
	CacheMissTokens                int                        `json:"cache_miss_tokens"`
	Steps                          int                        `json:"steps"` // model calls (one per stream, incl. tool rounds)
	Cost                           float64                    `json:"cost"`
	Currency                       string                     `json:"currency"`
	Compactions                    int                        `json:"compactions"`
	ReadinessChecks                int                        `json:"readiness_checks"`
	ReadinessAllowed               int                        `json:"readiness_allowed"`
	ReadinessBlocks                int                        `json:"readiness_blocks"`
	ReadinessRecoveries            int                        `json:"readiness_recoveries"`
	ReadinessErrors                int                        `json:"readiness_errors"`
	ReadinessMissingProjectChecks  int                        `json:"readiness_missing_project_checks"`
	ReadinessIncompleteTodos       int                        `json:"readiness_incomplete_todos"`
	ReadinessCommandMismatches     int                        `json:"readiness_command_mismatches"`
	MemoryCompilerTurns            int                        `json:"memory_compiler_turns"`
	MemoryCompilerInjectedTurns    int                        `json:"memory_compiler_injected_turns"`
	MemoryCompilerUsefulIRTurns    int                        `json:"memory_compiler_useful_ir_turns"`
	MemoryCompilerCompiledTokens   int                        `json:"memory_compiler_compiled_tokens"`
	MemoryCompilerIROverheadTokens int                        `json:"memory_compiler_ir_overhead_tokens"`
	MemoryCompilerMemoryReferences int                        `json:"memory_compiler_memory_references"`
	MemoryCompilerConstraints      int                        `json:"memory_compiler_constraints"`
	MemoryCompilerRiskNotes        int                        `json:"memory_compiler_risk_notes"`
	MemoryCompilerExecutionSteps   int                        `json:"memory_compiler_execution_steps"`
	MemoryCompilerTotalNodes       int                        `json:"memory_compiler_total_nodes"`
	MemoryCompilerHighSignalNodes  int                        `json:"memory_compiler_high_signal_nodes"`
	MemoryCompilerToolResultNodes  int                        `json:"memory_compiler_tool_result_nodes"`
	MemoryCompilerDecisionNodes    int                        `json:"memory_compiler_decision_nodes"`
	MemoryCompilerStrategyCount    int                        `json:"memory_compiler_strategy_count"`
	MemoryCompilerLearningCount    int                        `json:"memory_compiler_learning_count"`
	MemoryCompilerTurnDetails      []RunMemoryCompilerMetrics `json:"memory_compiler_turn_details,omitempty"`
}

// RunMemoryCompilerMetrics is a content-free per-turn Memory v5 snapshot in
// `reasonix run --metrics`. It mirrors the event payload's counts and estimated
// token sizes without carrying memory text, prompts, tool output, paths, or IDs.
type RunMemoryCompilerMetrics struct {
	Injected         bool `json:"injected"`
	UsefulIR         bool `json:"useful_ir"`
	CompiledTokens   int  `json:"compiled_tokens"`
	IROverheadTokens int  `json:"ir_overhead_tokens"`
	MemoryReferences int  `json:"memory_references"`
	Constraints      int  `json:"constraints"`
	RiskNotes        int  `json:"risk_notes"`
	ExecutionSteps   int  `json:"execution_steps"`
	TotalNodes       int  `json:"total_nodes"`
	HighSignalNodes  int  `json:"high_signal_nodes"`
	ToolResultNodes  int  `json:"tool_result_nodes"`
	DecisionNodes    int  `json:"decision_nodes"`
	StrategyCount    int  `json:"strategy_count"`
	LearningCount    int  `json:"learning_count"`
}

// metricsSink forwards every event to the real sink and accumulates the per-call
// Usage events into a RunMetrics. Cache totals are summed per call (not read from
// the cumulative SessionHit/Miss) so they match PromptTokens exactly.
type metricsSink struct {
	inner event.Sink
	m     RunMetrics
}

func (s *metricsSink) Emit(e event.Event) {
	if e.Kind == event.Usage && e.Usage != nil {
		u := e.Usage
		s.m.PromptTokens += u.PromptTokens
		s.m.CompletionTokens += u.CompletionTokens
		s.m.CacheHitTokens += u.CacheHitTokens
		s.m.CacheMissTokens += u.CacheMissTokens
		s.m.Steps++
		if p := e.Pricing; p != nil {
			s.m.Cost += (float64(u.CacheHitTokens)*p.CacheHit +
				float64(u.CacheMissTokens)*p.Input +
				float64(u.CompletionTokens)*p.Output) / 1e6
			s.m.Currency = p.Currency
		}
	}
	if e.Kind == event.CompactionStarted {
		s.m.Compactions++
	}
	if e.Kind == event.MemoryCompilerStatsEvent {
		s.recordMemoryCompilerStats(e.MemoryCompiler)
	}
	s.inner.Emit(e)
}

func (s *metricsSink) recordMemoryCompilerStats(m *event.MemoryCompilerStats) {
	if s == nil || m == nil {
		return
	}
	detail := RunMemoryCompilerMetrics{
		Injected:         m.Injected,
		UsefulIR:         m.UsefulIR,
		CompiledTokens:   m.CompiledTokens,
		IROverheadTokens: m.IROverheadTokens,
		MemoryReferences: m.MemoryReferences,
		Constraints:      m.Constraints,
		RiskNotes:        m.RiskNotes,
		ExecutionSteps:   m.ExecutionSteps,
		TotalNodes:       m.TotalNodes,
		HighSignalNodes:  m.HighSignalNodes,
		ToolResultNodes:  m.ToolResultNodes,
		DecisionNodes:    m.DecisionNodes,
		StrategyCount:    m.StrategyCount,
		LearningCount:    m.LearningCount,
	}
	s.m.MemoryCompilerTurns++
	if detail.Injected {
		s.m.MemoryCompilerInjectedTurns++
	}
	if detail.UsefulIR {
		s.m.MemoryCompilerUsefulIRTurns++
	}
	s.m.MemoryCompilerCompiledTokens += detail.CompiledTokens
	s.m.MemoryCompilerIROverheadTokens += detail.IROverheadTokens
	s.m.MemoryCompilerMemoryReferences += detail.MemoryReferences
	s.m.MemoryCompilerConstraints += detail.Constraints
	s.m.MemoryCompilerRiskNotes += detail.RiskNotes
	s.m.MemoryCompilerExecutionSteps += detail.ExecutionSteps
	s.m.MemoryCompilerTotalNodes = detail.TotalNodes
	s.m.MemoryCompilerHighSignalNodes = detail.HighSignalNodes
	s.m.MemoryCompilerToolResultNodes = detail.ToolResultNodes
	s.m.MemoryCompilerDecisionNodes = detail.DecisionNodes
	s.m.MemoryCompilerStrategyCount = detail.StrategyCount
	s.m.MemoryCompilerLearningCount = detail.LearningCount
	s.m.MemoryCompilerTurnDetails = append(s.m.MemoryCompilerTurnDetails, detail)
}

func (s *metricsSink) RecordReadinessAudit(a evidence.ReadinessAudit) {
	if s == nil {
		return
	}
	s.m.ReadinessChecks++
	switch a.Result {
	case evidence.ReadinessAllowed:
		s.m.ReadinessAllowed++
	case evidence.ReadinessBlocked:
		s.m.ReadinessBlocks++
	case evidence.ReadinessErrored:
		s.m.ReadinessErrors++
	}
	if a.Recovered {
		s.m.ReadinessRecoveries++
	}
	s.m.ReadinessMissingProjectChecks += a.MissingProjectChecks
	s.m.ReadinessIncompleteTodos += a.IncompleteTodos
	s.m.ReadinessCommandMismatches += a.CommandMismatchMissing
}

func writeMetrics(path string, m RunMetrics) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
