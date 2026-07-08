// Package memorycompiler implements the Memory v5 execution compiler runtime.
// It is deliberately local and rule-driven: execution traces can update
// strategy scores and compiler mutations, but the model never rewrites code.
package memorycompiler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"reasonix/internal/fileutil"
	"reasonix/internal/provider"
)

const (
	stateFile          = "state.json"
	tracesFile         = "traces.jsonl"
	learningTracesFile = "learning_traces.jsonl"
	debugTracesFile    = "debug_traces.jsonl"
	debugTraceEnv      = "REASONIX_MEMORY_COMPILER_DEBUG_TRACE"
	version            = "v5.9"

	explorationRatePercent    = 10
	minExplorationRatePercent = 3
	maxExplorationRatePercent = 12
	mutationMinEvalTrials     = 2
	mutationAcceptThreshold   = 0.60
	mutationRegressionMargin  = 0.05
	mutationFeedbackCooldown  = 30 * time.Minute
	strategyDecayK            = 10.0
	staleConfidenceThreshold  = 0.2

	compilerIROverheadSelfFeedback = "compiled IR overhead exceeded budget; reduce memory references before injection"
	planModeBlockedToolError       = "blocked: plan mode is read-only"

	maxRuntimeTraceJSONLLines  = 500
	maxLearningTraceJSONLLines = 100
	maxDebugTraceJSONLLines    = 100
)

var runtimeLocks sync.Map

// Runtime owns one project's Memory v5 state.
type Runtime struct {
	dir string
	mu  *sync.Mutex
}

// New returns a runtime backed by dir. A blank dir disables persistence and
// returns nil so callers can keep the fast path simple.
func New(dir string) *Runtime {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	dir = filepath.Clean(dir)
	return &Runtime{dir: dir, mu: runtimeLockForDir(dir)}
}

func runtimeLockForDir(dir string) *sync.Mutex {
	actual, _ := runtimeLocks.LoadOrStore(filepath.Clean(dir), &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// PlannerIR is the memory-compiled execution plan language embedded in the
// cache-safe execution contract when there is useful learned state.
type PlannerIR struct {
	Version             string        `json:"version"`
	Goal                string        `json:"goal"`
	SourceEvent         string        `json:"source_event"`
	RuntimeMode         string        `json:"runtime_mode"`
	Constraints         []Constraint  `json:"constraints"`
	StrategySelection   *StrategyPick `json:"strategy_selection"`
	AvailableStrategies []StrategyRef `json:"available_strategies"`
	MemoryReferences    []MemoryRef   `json:"memory_references"`
	ExecutionSteps      []Step        `json:"execution_steps"`
	RiskNotes           []string      `json:"risk_notes"`
}

type Constraint struct {
	Type   string `json:"type"`
	Text   string `json:"text"`
	Source string `json:"source,omitempty"`
}

type StrategyRef struct {
	ID          string  `json:"id"`
	SuccessRate float64 `json:"success_rate"`
	Samples     int     `json:"samples"`
	Score       float64 `json:"score,omitempty"`
	Reason      string  `json:"reason,omitempty"`
}

type StrategyPick struct {
	Selected        string             `json:"selected"`
	Reason          string             `json:"reason"`
	Score           float64            `json:"score"`
	Mode            string             `json:"mode"`
	ExplorationRate float64            `json:"exploration_rate"`
	Rejected        []RejectedStrategy `json:"rejected"`
}

type RejectedStrategy struct {
	ID     string  `json:"id"`
	Reason string  `json:"reason"`
	Score  float64 `json:"score"`
}

type MemoryRef struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Quality   string `json:"quality,omitempty"`
	Influence string `json:"influence,omitempty"`
}

type Step struct {
	ID     string `json:"id"`
	Action string `json:"action"`
}

type ToolRecord struct {
	ID         string `json:"id,omitempty"`
	Name       string `json:"name"`
	Args       string `json:"args,omitempty"`
	Output     string `json:"output,omitempty"`
	Error      string `json:"error,omitempty"`
	ReadOnly   bool   `json:"read_only"`
	Blocked    bool   `json:"blocked,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type ExecutionTrace struct {
	ID                  string               `json:"id"`
	IRVersion           string               `json:"ir_version"`
	Goal                string               `json:"goal"`
	Steps               []Step               `json:"steps,omitempty"`
	Outcome             string               `json:"outcome"`
	Injected            bool                 `json:"injected,omitempty"`
	EfficiencyScore     float64              `json:"efficiency_score"`
	MemoryEffectiveness float64              `json:"memory_effectiveness"`
	StrategyUsed        []string             `json:"strategy_used,omitempty"`
	MemoryUsed          []string             `json:"memory_used,omitempty"`
	DecisionBranches    []DecisionBranch     `json:"decision_branches,omitempty"`
	CausalEdges         []CausalEdge         `json:"causal_edges,omitempty"`
	SemanticDrift       []string             `json:"semantic_drift,omitempty"`
	SemanticDriftHard   []string             `json:"semantic_drift_hard,omitempty"`
	SemanticDriftSoft   []string             `json:"semantic_drift_soft,omitempty"`
	SemanticShift       []string             `json:"semantic_shift,omitempty"`
	ControlMode         string               `json:"control_mode,omitempty"`
	ControlGain         float64              `json:"control_gain,omitempty"`
	ControlSignals      []string             `json:"control_signals,omitempty"`
	EquilibriumTrace    *EquilibriumTrace    `json:"equilibrium_trace,omitempty"`
	Compression         *CompressionReport   `json:"compression,omitempty"`
	Cost                CostMetrics          `json:"cost,omitempty"`
	MutationEvaluations []MutationEvaluation `json:"mutation_evaluations,omitempty"`
	FailureReason       string               `json:"failure_reason,omitempty"`
	ToolResults         []ToolRecord         `json:"tool_results,omitempty"`
	StartedAt           time.Time            `json:"started_at"`
	CompletedAt         time.Time            `json:"completed_at"`
}

type DecisionBranch struct {
	Question        string   `json:"question"`
	Selected        string   `json:"selected"`
	Rejected        []string `json:"rejected,omitempty"`
	SelectionReason string   `json:"selection_reason,omitempty"`
}

type CausalEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}

type CostMetrics struct {
	EstimatedInputTokens      int   `json:"estimated_input_tokens,omitempty"`
	EstimatedCompiledTokens   int   `json:"estimated_compiled_tokens,omitempty"`
	EstimatedIROverheadTokens int   `json:"estimated_ir_overhead_tokens,omitempty"`
	LatencyMs                 int64 `json:"latency_ms,omitempty"`
	ToolCalls                 int   `json:"tool_calls,omitempty"`
	ToolErrors                int   `json:"tool_errors,omitempty"`
	TruncatedToolResults      int   `json:"truncated_tool_results,omitempty"`
}

type EquilibriumTrace struct {
	State                string   `json:"state,omitempty"`
	ControlGraphEntropy  float64  `json:"control_graph_entropy,omitempty"`
	SystemStabilityScore float64  `json:"system_stability_score,omitempty"`
	ConvergenceVelocity  float64  `json:"convergence_velocity,omitempty"`
	OscillationIndex     float64  `json:"oscillation_index,omitempty"`
	Actions              []string `json:"actions,omitempty"`
}

type CompilerMutation struct {
	Target             string    `json:"target"`
	Change             string    `json:"change"`
	Reason             string    `json:"reason"`
	EvidenceTraceIDs   []string  `json:"evidence_trace_ids,omitempty"`
	Status             string    `json:"status,omitempty"`
	BaselineScore      float64   `json:"baseline_score,omitempty"`
	EvaluationTraceIDs []string  `json:"evaluation_trace_ids,omitempty"`
	EvaluationScore    float64   `json:"evaluation_score,omitempty"`
	EvaluationReason   string    `json:"evaluation_reason,omitempty"`
	Applied            bool      `json:"applied"`
	CreatedAt          time.Time `json:"created_at,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

type MutationEvaluation struct {
	Target   string  `json:"target"`
	Change   string  `json:"change"`
	Reason   string  `json:"reason"`
	Decision string  `json:"decision"`
	Score    float64 `json:"score"`
	Baseline float64 `json:"baseline"`
	Trials   int     `json:"trials"`
}

type IRValidationResult struct {
	Findings     []string
	HardFindings []string
	SoftFindings []string
	Reject       bool
}

type ControlPolicy struct {
	Version                string        `json:"version"`
	Mode                   string        `json:"mode"`
	Controller             string        `json:"controller"`
	ExplorationRatePercent int           `json:"exploration_rate_percent"`
	Gain                   float64       `json:"gain"`
	ConsensusScore         float64       `json:"consensus_score,omitempty"`
	Variance               float64       `json:"variance,omitempty"`
	EquilibriumState       string        `json:"equilibrium_state,omitempty"`
	EquilibriumActions     []string      `json:"equilibrium_actions,omitempty"`
	ControlGraphEntropy    float64       `json:"control_graph_entropy,omitempty"`
	SystemStabilityScore   float64       `json:"system_stability_score,omitempty"`
	ConvergenceVelocity    float64       `json:"convergence_velocity,omitempty"`
	OscillationIndex       float64       `json:"oscillation_index,omitempty"`
	MutationCooldown       time.Duration `json:"-"`
	MutationCooldownMs     int64         `json:"mutation_cooldown_ms"`
	SemanticShift          []string      `json:"semantic_shift,omitempty"`
	Reasons                []string      `json:"reasons,omitempty"`
}

type TraceBundle struct {
	RuntimeTrace  ExecutionTrace  `json:"runtime_trace"`
	LearningTrace *LearningTrace  `json:"learning_trace,omitempty"`
	DebugTrace    *ExecutionTrace `json:"debug_trace,omitempty"`
}

type LearningTrace struct {
	ID                   string               `json:"id"`
	IRVersion            string               `json:"ir_version"`
	Outcome              string               `json:"outcome"`
	Injected             bool                 `json:"injected,omitempty"`
	QualityScore         float64              `json:"quality_score"`
	StrategyUsed         []string             `json:"strategy_used,omitempty"`
	MemoryUsed           []string             `json:"memory_used,omitempty"`
	DecisionBranches     []DecisionBranch     `json:"decision_branches,omitempty"`
	CausalEdges          []CausalEdge         `json:"causal_edges,omitempty"`
	SemanticDrift        []string             `json:"semantic_drift,omitempty"`
	SemanticDriftHard    []string             `json:"semantic_drift_hard,omitempty"`
	SemanticDriftSoft    []string             `json:"semantic_drift_soft,omitempty"`
	SemanticShift        []string             `json:"semantic_shift,omitempty"`
	ControlMode          string               `json:"control_mode,omitempty"`
	ControlGain          float64              `json:"control_gain,omitempty"`
	ControlSignals       []string             `json:"control_signals,omitempty"`
	EquilibriumTrace     *EquilibriumTrace    `json:"equilibrium_trace,omitempty"`
	Compression          *CompressionReport   `json:"compression,omitempty"`
	CausalFindings       []string             `json:"causal_findings,omitempty"`
	CompilerImprovements []string             `json:"compiler_improvements,omitempty"`
	MutationEvaluations  []MutationEvaluation `json:"mutation_evaluations,omitempty"`
	Cost                 CostMetrics          `json:"cost,omitempty"`
	CreatedAt            time.Time            `json:"created_at"`
}

type DriftReport struct {
	TraceID            string    `json:"trace_id,omitempty"`
	OverusedStrategies []string  `json:"overused_strategies,omitempty"`
	StaleMemoryNodes   []string  `json:"stale_memory_nodes,omitempty"`
	ConflictingFacts   []string  `json:"conflicting_facts,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
}

type MemoryQuality string

const (
	QualityHighSignal   MemoryQuality = "HIGH_SIGNAL"
	QualityMediumSignal MemoryQuality = "MEDIUM_SIGNAL"
	QualityNoise        MemoryQuality = "NOISE"
	QualityCorrupted    MemoryQuality = "CORRUPTED"
)

type MemoryNode struct {
	ID          string        `json:"id"`
	Type        string        `json:"type"`
	Content     string        `json:"content"`
	Timestamp   time.Time     `json:"timestamp"`
	Confidence  float64       `json:"confidence"`
	Quality     MemoryQuality `json:"quality"`
	Constraint  *Constraint   `json:"constraint,omitempty"`
	TruthLocked bool          `json:"truth_locked,omitempty"`
}

type MemoryEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation"`
}

type DecisionNode struct {
	ID              string    `json:"id"`
	Question        string    `json:"question"`
	SelectedOption  string    `json:"selected_option"`
	RejectedOptions []string  `json:"rejected_options,omitempty"`
	Reasoning       string    `json:"reasoning"`
	Timestamp       time.Time `json:"timestamp"`
}

type ExecutionState struct {
	GoalState         string       `json:"goal_state,omitempty"`
	CurrentPhase      string       `json:"current_phase,omitempty"`
	KnownFacts        []string     `json:"known_facts,omitempty"`
	ActiveConstraints []Constraint `json:"active_constraints,omitempty"`
	FailedStrategies  []string     `json:"failed_strategies,omitempty"`
	UpdatedAt         time.Time    `json:"updated_at,omitempty"`
}

type SystemLearning struct {
	TraceID              string    `json:"trace_id"`
	BadStrategies        []string  `json:"bad_strategies,omitempty"`
	GoodPatterns         []string  `json:"good_patterns,omitempty"`
	MemoryNoisePatterns  []string  `json:"memory_noise_patterns,omitempty"`
	CausalFindings       []string  `json:"causal_findings,omitempty"`
	CompilerImprovements []string  `json:"compiler_improvements,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
}

type Strategy struct {
	ID            string   `json:"id"`
	Preconditions []string `json:"preconditions,omitempty"`
	ExecutionPlan []Step   `json:"execution_plan,omitempty"`
	Successes     int      `json:"successes"`
	Failures      int      `json:"failures"`
	// InjectedSuccesses/InjectedFailures split the counters above by whether
	// the compiled contract was provider-visible that turn, so injected and
	// observe-only outcomes can be compared for real lift.
	InjectedSuccesses int       `json:"injected_successes,omitempty"`
	InjectedFailures  int       `json:"injected_failures,omitempty"`
	LastUsedAt        time.Time `json:"last_used_at,omitempty"`
	Description       string    `json:"description,omitempty"`
}

func (s Strategy) Samples() int { return s.Successes + s.Failures }

func (s Strategy) SuccessRate() float64 {
	if s.Samples() == 0 {
		return 0
	}
	return float64(s.Successes) / float64(s.Samples())
}

type state struct {
	Nodes              []MemoryNode        `json:"nodes,omitempty"`
	Edges              []MemoryEdge        `json:"edges,omitempty"`
	Decisions          []DecisionNode      `json:"decisions,omitempty"`
	ExecutionState     ExecutionState      `json:"execution_state,omitempty"`
	Strategies         []Strategy          `json:"strategies,omitempty"`
	Mutations          []CompilerMutation  `json:"mutations,omitempty"`
	Learnings          []SystemLearning    `json:"learnings,omitempty"`
	DriftReports       []DriftReport       `json:"drift_reports,omitempty"`
	CompressionReports []CompressionReport `json:"compression_reports,omitempty"`
	NoisyRefs          map[string]int      `json:"noisy_refs,omitempty"`
	UpdatedAt          time.Time           `json:"updated_at,omitempty"`
}

// Turn records one top-level agent turn.
type Turn struct {
	rt        *Runtime
	ir        PlannerIR
	trace     ExecutionTrace
	strategy  string
	citations []provider.MemoryCitation
	metrics   TurnMetrics
}

// TurnMetrics is a content-free snapshot of Memory v5 participation for one
// turn. It intentionally contains only counts and estimated token sizes so
// desktop aggregate metrics can quantify memory usage without uploading memory
// text, tool output, file paths, or prompts.
type TurnMetrics struct {
	Injected         bool
	UsefulIR         bool
	CompiledTokens   int
	IROverheadTokens int
	MemoryReferences int
	Constraints      int
	RiskNotes        int
	ExecutionSteps   int
	TotalNodes       int
	HighSignalNodes  int
	ToolResultNodes  int
	DecisionNodes    int
	StrategyCount    int
	LearningCount    int
}

// StartTurn builds a cache-safe execution contract from prior learned state. It
// returns an empty compiled input until the runtime has enough signal to
// influence the next turn; when non-empty, callers should use the returned value
// as the whole user turn instead of appending it as side context.
func (r *Runtime) StartTurn(ctx context.Context, input string, _ []provider.Message) (string, *Turn) {
	if r == nil {
		return "", nil
	}
	// Classify the goal from the user's actual text, not the "Referenced context:"
	// preamble + file blocks the controller injects on @-references — otherwise
	// summarizeGoal and strategy matching key off file contents. SourceEvent (the
	// full input passed to buildIRWithPolicy) is kept intact on purpose: when the
	// compiled contract replaces the user turn, source_event is the model's only
	// view of the referenced files.
	goal := summarizeGoal(stripReferencedContext(input))
	st := r.loadState()
	ir, policy := buildIRWithPolicy(goal, input, st)
	now := time.Now().UTC()
	id := traceID(now)
	t := &Turn{
		rt:        r,
		ir:        ir,
		citations: memoryCitationsForIR(ir),
		metrics:   turnMetricsForIR(ir, st),
		trace: ExecutionTrace{
			ID:               id,
			IRVersion:        version,
			Goal:             goal,
			Steps:            ir.ExecutionSteps,
			MemoryUsed:       memoryRefIDs(ir.MemoryReferences),
			DecisionBranches: decisionBranches(ir),
			StartedAt:        now,
			SemanticShift:    append([]string(nil), policy.SemanticShift...),
			ControlMode:      policy.Mode,
			ControlGain:      policy.Gain,
			ControlSignals:   append([]string(nil), policy.Reasons...),
			EquilibriumTrace: equilibriumTraceForPolicy(policy),
			Cost: CostMetrics{
				EstimatedInputTokens: estimateTokens(input),
			},
		},
	}
	if ir.StrategySelection != nil {
		t.strategy = ir.StrategySelection.Selected
		t.trace.StrategyUsed = []string{t.strategy}
	}
	t.trace.CausalEdges = causalEdgesForIR(t.trace.ID, ir)
	if !hasUsefulIR(ir) {
		return "", t
	}
	// Production hardening is an observability signal recorded on the trace; it
	// must not gate whether the cache-safe contract is injected. The contract is
	// plain input text (not a privileged action), and the execution that follows
	// is still bounded by tool permissions. Gating here previously made the whole
	// compiler fall silent once learned memory nodes reached their GC cap.
	compiled, err := compileExecutionContract(ir)
	if err != nil {
		return "", t
	}
	if err := ctx.Err(); err != nil {
		return "", t
	}
	t.trace.Cost.EstimatedCompiledTokens = estimateTokens(compiled)
	if t.trace.Cost.EstimatedCompiledTokens > t.trace.Cost.EstimatedInputTokens {
		t.trace.Cost.EstimatedIROverheadTokens = t.trace.Cost.EstimatedCompiledTokens - t.trace.Cost.EstimatedInputTokens
	}
	t.metrics.Injected = true
	t.metrics.CompiledTokens = t.trace.Cost.EstimatedCompiledTokens
	t.metrics.IROverheadTokens = t.trace.Cost.EstimatedIROverheadTokens
	return compiled, t
}

// MemoryCitations returns the local UI references that explain which memories
// influenced this turn's compiled execution contract.
func (t *Turn) MemoryCitations() []provider.MemoryCitation {
	if t == nil || !t.metrics.Injected || len(t.citations) == 0 {
		return nil
	}
	return append([]provider.MemoryCitation(nil), t.citations...)
}

// SuppressInjection keeps the turn open for trace writeback and learning while
// marking the compiled contract as not used for this user turn. Agent-level
// throttles call this when Memory v5 should observe the turn but must not replace
// the user prompt or surface compiler citations.
func (t *Turn) SuppressInjection() {
	if t == nil {
		return
	}
	t.citations = nil
	t.metrics.Injected = false
	t.metrics.CompiledTokens = 0
	t.metrics.IROverheadTokens = 0
	t.trace.Cost.EstimatedCompiledTokens = 0
	t.trace.Cost.EstimatedIROverheadTokens = 0
	t.trace.Steps = nil
	t.trace.MemoryUsed = nil
	t.trace.DecisionBranches = nil
	t.trace.CausalEdges = nil
	t.trace.StrategyUsed = nil
	t.strategy = ""
}

// Metrics returns a content-free Memory v5 usage snapshot for this turn.
func (t *Turn) Metrics() TurnMetrics {
	if t == nil {
		return TurnMetrics{}
	}
	return t.metrics
}

func turnMetricsForIR(ir PlannerIR, st state) TurnMetrics {
	m := TurnMetrics{
		UsefulIR:         hasUsefulIR(ir),
		MemoryReferences: len(ir.MemoryReferences),
		Constraints:      len(ir.Constraints),
		RiskNotes:        len(ir.RiskNotes),
		ExecutionSteps:   len(ir.ExecutionSteps),
		TotalNodes:       len(st.Nodes),
		DecisionNodes:    len(st.Decisions),
		StrategyCount:    len(st.Strategies),
		LearningCount:    len(st.Learnings),
	}
	for _, node := range st.Nodes {
		if node.Quality == QualityHighSignal {
			m.HighSignalNodes++
		}
		if node.Type == "tool_result" {
			m.ToolResultNodes++
		}
	}
	return m
}

func buildIR(goal, sourceEvent string, st state) PlannerIR {
	ir, _ := buildIRWithPolicy(goal, sourceEvent, st)
	return ir
}

func buildIRWithPolicy(goal, sourceEvent string, st state) (PlannerIR, ControlPolicy) {
	now := time.Now().UTC()
	st, drift := applyDriftControl(st, now, "")
	policy := controlPolicyForState(st, drift)
	ir := PlannerIR{
		Version:     version,
		Goal:        goal,
		SourceEvent: sourceEvent,
		RuntimeMode: "control",
	}
	st.Strategies = ensureBuiltInStrategies(st.Strategies)
	rankedStrategies := rankStrategies(goal, st.Strategies)
	strategyPick := selectStrategy(goal, rankedStrategies, policy.ExplorationRatePercent)
	if strategyPick.Mode == "explore" {
		ir.RuntimeMode = "explore"
	}
	ir.StrategySelection = &strategyPick
	for _, c := range st.ExecutionState.ActiveConstraints {
		if isCompilerFeedbackNoise(c.Text) {
			continue
		}
		ir.Constraints = appendConstraint(ir.Constraints, c)
	}
	for _, failed := range st.ExecutionState.FailedStrategies {
		if strings.TrimSpace(failed) != "" {
			ir.RiskNotes = append(ir.RiskNotes, "avoid previously failed strategy "+failed)
		}
	}
	for _, noisy := range sortedNoisyRefs(st.NoisyRefs) {
		ref, count := noisy.ref, noisy.count
		if count < 2 || isCompilerFeedbackNoise(ref) {
			continue
		}
		ir.RiskNotes = append(ir.RiskNotes, "quarantined noisy memory pattern "+ref)
	}
	ir.RiskNotes = append(ir.RiskNotes, driftRiskNotes(drift)...)
	for _, node := range usableSubgraphNodes(st.Nodes, st.Edges, now) {
		if node.Constraint != nil && !isCompilerFeedbackNoise(node.Constraint.Text) {
			ir.Constraints = appendConstraint(ir.Constraints, *node.Constraint)
		}
		if (node.Quality == QualityHighSignal || node.Type == "tool_result") && !isCompilerFeedbackNoise(node.Content) {
			ir.MemoryReferences = append(ir.MemoryReferences, MemoryRef{
				ID:        node.ID,
				Content:   node.Content,
				Quality:   string(node.Quality),
				Influence: influenceForNode(node),
			})
			if len(ir.MemoryReferences) >= 5 {
				break
			}
		}
	}
	for _, m := range st.Mutations {
		if !m.Applied {
			continue
		}
		if isCompilerFeedbackNoise(m.Reason) {
			continue
		}
		switch m.Change {
		case "decrease_k", "decrease_weight", "quarantine_pattern":
			ir.Constraints = appendConstraint(ir.Constraints, Constraint{Type: "avoid", Text: m.Reason, Source: m.Target})
		case "increase_weight", "add_constraint":
			ir.Constraints = appendConstraint(ir.Constraints, Constraint{Type: "must_use", Text: m.Reason, Source: m.Target})
		default:
			ir.Constraints = appendConstraint(ir.Constraints, Constraint{Type: "reference", Text: m.Reason, Source: m.Target})
		}
	}
	for _, candidate := range rankedStrategies {
		s := candidate.strategy
		ref := StrategyRef{ID: s.ID, SuccessRate: s.SuccessRate(), Samples: s.Samples(), Score: candidate.score, Reason: candidate.reason}
		if lowSuccessStrategy(s) {
			if s.Samples() > 0 {
				ir.RiskNotes = append(ir.RiskNotes, "avoid low-success strategy "+s.ID)
			}
			continue
		}
		ir.AvailableStrategies = append(ir.AvailableStrategies, ref)
		if len(ir.AvailableStrategies) >= 3 {
			break
		}
	}
	if ir.StrategySelection != nil && ir.StrategySelection.Selected != "" {
		if plan := strategyPlan(st.Strategies, ir.StrategySelection.Selected); len(plan) > 0 {
			ir.ExecutionSteps = plan
		}
	}
	if len(ir.ExecutionSteps) == 0 && (len(ir.Constraints) > 0 || len(ir.MemoryReferences) > 0 || len(ir.RiskNotes) > 0) {
		if plan := strategyPlan(st.Strategies, bestStrategyID(goal, st.Strategies)); len(plan) > 0 {
			ir.ExecutionSteps = plan
		}
	}
	if len(ir.Constraints) > 0 || len(ir.AvailableStrategies) > 0 || len(ir.RiskNotes) > 0 {
		if len(ir.ExecutionSteps) == 0 {
			ir.ExecutionSteps = []Step{
				{ID: "analyze", Action: "Inspect the current task and verify the relevant source of truth."},
				{ID: "execute", Action: "Apply the highest-signal compatible strategy while respecting constraints."},
				{ID: "validate", Action: "Validate the outcome with direct evidence before finalizing."},
			}
		}
	}
	return canonicalizeIR(ir), policy
}

func hasUsefulIR(ir PlannerIR) bool {
	return len(ir.Constraints) > 0 || len(ir.MemoryReferences) > 0 || len(ir.RiskNotes) > 0
}

// contractIR is the bounded, model-facing projection of PlannerIR that gets
// serialized into the per-turn prompt contract. It is deliberately a separate
// type from PlannerIR with omitempty everywhere so the injected JSON carries no
// zero-value/null noise. The display layer (agent.memoryCompilerSourceEvent)
// only needs planner_ir.source_event, which is preserved.
type contractIR struct {
	Version           string            `json:"version,omitempty"`
	Goal              string            `json:"goal,omitempty"`
	SourceEvent       string            `json:"source_event"`
	RuntimeMode       string            `json:"runtime_mode,omitempty"`
	Constraints       []Constraint      `json:"constraints,omitempty"`
	StrategySelection *contractStrategy `json:"strategy_selection,omitempty"`
	MemoryReferences  []MemoryRef       `json:"memory_references,omitempty"`
	ExecutionSteps    []Step            `json:"execution_steps,omitempty"`
	RiskNotes         []string          `json:"risk_notes,omitempty"`
}

type contractStrategy struct {
	Selected string `json:"selected"`
	Reason   string `json:"reason,omitempty"`
	Mode     string `json:"mode,omitempty"`
}

func compileExecutionContract(ir PlannerIR) (string, error) {
	contract := struct {
		Type        string     `json:"type"`
		Instruction string     `json:"instruction"`
		PlannerIR   contractIR `json:"planner_ir"`
	}{
		Type:        "memory_v5_execution_contract",
		Instruction: "Execute source_event through planner_ir. Treat constraints, risk_notes, strategy_selection, and execution_steps as the controlling plan for this turn. Do not bypass contradictory or quarantined memory outside this IR.",
		PlannerIR:   compactContractIR(canonicalizeIR(ir)),
	}
	body, err := json.Marshal(contract)
	if err != nil {
		return "", err
	}
	return "<memory-compiler-execution>\n" + string(body) + "\n</memory-compiler-execution>", nil
}

// compactContractIR projects the full canonical IR onto the model-facing
// contractIR by dropping only the fields the model cannot act on. It does NOT
// cap or re-truncate the actionable fields, so the constraints, memory
// references, execution steps, risk notes, and selected strategy it injects are
// byte-identical to what the previous full contract carried. The full IR is
// still recorded on the execution trace for learning (writeTraceAndLearn keeps
// the canonical IR); only the prompt copy is slimmed.
//
// Before this the contract re-serialized the entire planner IR every turn,
// including a prose ir_explanation that just restated the constraints and
// memory references, the ranked available_strategies candidate table, and each
// strategy's rejected/score/exploration-rate control-loop state. On short user
// turns that inflated the user message ~20-50x and grew unbounded as the
// candidate table and explanation accreted. None of the dropped fields carry
// guidance the kept fields don't already supply; the canonical IR's own limits
// keep the kept fields bounded.
func compactContractIR(ir PlannerIR) contractIR {
	out := contractIR{
		Version:          ir.Version,
		Goal:             ir.Goal,
		SourceEvent:      ir.SourceEvent,
		RuntimeMode:      ir.RuntimeMode,
		Constraints:      ir.Constraints,
		MemoryReferences: ir.MemoryReferences,
		ExecutionSteps:   ir.ExecutionSteps,
		RiskNotes:        ir.RiskNotes,
	}
	// Keep only the chosen strategy and its reason/mode; the rejected
	// candidates, numeric score, and exploration rate are internal control-loop
	// state the model cannot act on, and the ranked available_strategies table
	// is dropped entirely for the same reason.
	if ir.StrategySelection != nil {
		out.StrategySelection = &contractStrategy{
			Selected: ir.StrategySelection.Selected,
			Reason:   ir.StrategySelection.Reason,
			Mode:     ir.StrategySelection.Mode,
		}
	}
	return out
}

func memoryCitationsForIR(ir PlannerIR) []provider.MemoryCitation {
	ir = canonicalizeIR(ir)
	out := []provider.MemoryCitation{}
	seen := map[string]bool{}
	add := func(c provider.MemoryCitation) {
		c.ID = strings.TrimSpace(c.ID)
		c.Source = strings.TrimSpace(c.Source)
		c.Note = summarizeText(c.Note, 180)
		c.Kind = strings.TrimSpace(c.Kind)
		if c.Source == "" {
			c.Source = "Memory v5"
		}
		key := c.Kind + "\x00" + c.ID + "\x00" + c.Source + "\x00" + c.Note
		if c.Note == "" || seen[key] || len(out) >= 5 {
			return
		}
		seen[key] = true
		out = append(out, c)
	}
	for _, ref := range ir.MemoryReferences {
		if ref.Influence == "evidence" {
			continue // tool_result nodes are internal graph state, not user-facing
		}
		note := ref.Content
		if ref.Influence != "" {
			note = ref.Influence + ": " + note
		}
		if ref.Quality != "" {
			note += " (" + ref.Quality + ")"
		}
		add(provider.MemoryCitation{
			ID:     ref.ID,
			Source: "Memory v5",
			Note:   note,
			Kind:   "compiler_reference",
		})
	}
	for _, c := range ir.Constraints {
		note := c.Type + ": " + c.Text
		if c.Source != "" {
			note += " [" + c.Source + "]"
		}
		add(provider.MemoryCitation{
			ID:     c.Source,
			Source: "Memory v5",
			Note:   note,
			Kind:   "constraint",
		})
	}
	for _, note := range ir.RiskNotes {
		add(provider.MemoryCitation{
			Source: "Memory v5",
			Note:   "risk: " + note,
			Kind:   "risk_note",
		})
	}
	return out
}

func selectedStrategy(ir PlannerIR) string {
	if ir.StrategySelection != nil && strings.TrimSpace(ir.StrategySelection.Selected) != "" {
		return strings.TrimSpace(ir.StrategySelection.Selected)
	}
	return "general"
}

func canonicalizeIR(ir PlannerIR) PlannerIR {
	ir.Version = strings.TrimSpace(ir.Version)
	if ir.Version == "" {
		ir.Version = version
	}
	ir.Goal = summarizeGoal(ir.Goal)
	ir.SourceEvent = strings.TrimSpace(ir.SourceEvent)
	ir.RuntimeMode = strings.TrimSpace(ir.RuntimeMode)
	if ir.RuntimeMode == "" {
		ir.RuntimeMode = "control"
	}
	ir.Constraints = canonicalConstraints(ir.Constraints)
	ir.AvailableStrategies = canonicalStrategyRefs(ir.AvailableStrategies)
	ir.MemoryReferences = canonicalMemoryRefs(ir.MemoryReferences)
	ir.ExecutionSteps = canonicalSteps(ir.ExecutionSteps)
	ir.RiskNotes = canonicalStrings(ir.RiskNotes)
	if ir.StrategySelection == nil {
		ir.StrategySelection = &StrategyPick{
			Selected:        "general",
			Reason:          "default strategy",
			Mode:            "control",
			ExplorationRate: float64(explorationRatePercent) / 100,
			Rejected:        []RejectedStrategy{},
		}
	} else {
		ir.StrategySelection.Selected = strings.TrimSpace(ir.StrategySelection.Selected)
		if ir.StrategySelection.Selected == "" {
			ir.StrategySelection.Selected = "general"
		}
		ir.StrategySelection.Reason = strings.TrimSpace(ir.StrategySelection.Reason)
		ir.StrategySelection.Score = roundScore(ir.StrategySelection.Score)
		ir.StrategySelection.Mode = strings.TrimSpace(ir.StrategySelection.Mode)
		if ir.StrategySelection.Mode == "" {
			ir.StrategySelection.Mode = "control"
		}
		ratePercent := int(math.Round(ir.StrategySelection.ExplorationRate * 100))
		if ratePercent <= 0 {
			ratePercent = explorationRatePercent
		}
		ir.StrategySelection.ExplorationRate = float64(clampExplorationRatePercent(ratePercent)) / 100
		ir.StrategySelection.Rejected = canonicalRejectedStrategies(ir.StrategySelection.Rejected)
	}
	if ir.Constraints == nil {
		ir.Constraints = []Constraint{}
	}
	if ir.AvailableStrategies == nil {
		ir.AvailableStrategies = []StrategyRef{}
	}
	if ir.MemoryReferences == nil {
		ir.MemoryReferences = []MemoryRef{}
	}
	if ir.ExecutionSteps == nil {
		ir.ExecutionSteps = []Step{}
	}
	if ir.RiskNotes == nil {
		ir.RiskNotes = []string{}
	}
	return ir
}

func canonicalConstraints(in []Constraint) []Constraint {
	out := make([]Constraint, 0, len(in))
	for _, c := range in {
		c.Type = strings.TrimSpace(c.Type)
		c.Text = strings.TrimSpace(c.Text)
		c.Source = strings.TrimSpace(c.Source)
		if c.Type == "" || c.Text == "" {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Text < out[j].Text
	})
	return dedupeConstraints(out)
}

func dedupeConstraints(in []Constraint) []Constraint {
	seen := map[Constraint]bool{}
	out := make([]Constraint, 0, len(in))
	for _, c := range in {
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func canonicalStrategyRefs(in []StrategyRef) []StrategyRef {
	out := make([]StrategyRef, 0, len(in))
	for _, s := range in {
		s.ID = strings.TrimSpace(s.ID)
		s.Reason = strings.TrimSpace(s.Reason)
		if s.ID == "" {
			continue
		}
		s.SuccessRate = roundScore(s.SuccessRate)
		s.Score = roundScore(s.Score)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func canonicalRejectedStrategies(in []RejectedStrategy) []RejectedStrategy {
	out := make([]RejectedStrategy, 0, len(in))
	for _, r := range in {
		r.ID = strings.TrimSpace(r.ID)
		r.Reason = strings.TrimSpace(r.Reason)
		if r.ID == "" {
			continue
		}
		r.Score = roundScore(r.Score)
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	return out
}

func canonicalMemoryRefs(in []MemoryRef) []MemoryRef {
	out := make([]MemoryRef, 0, len(in))
	for _, ref := range in {
		ref.ID = strings.TrimSpace(ref.ID)
		ref.Content = strings.TrimSpace(ref.Content)
		ref.Quality = strings.TrimSpace(ref.Quality)
		ref.Influence = strings.TrimSpace(ref.Influence)
		if ref.ID == "" || ref.Content == "" {
			continue
		}
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Influence != out[j].Influence {
			return out[i].Influence < out[j].Influence
		}
		return out[i].ID < out[j].ID
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func canonicalSteps(in []Step) []Step {
	out := make([]Step, 0, len(in))
	for _, step := range in {
		step.ID = strings.TrimSpace(step.ID)
		step.Action = strings.TrimSpace(step.Action)
		if step.ID == "" || step.Action == "" {
			continue
		}
		out = append(out, step)
	}
	return out
}

func canonicalStrings(in []string) []string {
	out := dedupeStrings(in)
	sort.Strings(out)
	return out
}

func limitStrings(in []string, n int) []string {
	if n < 0 {
		n = 0
	}
	if len(in) > n {
		return in[:n]
	}
	if in == nil {
		return []string{}
	}
	return in
}

func summarizeText(s string, maxRunes int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if maxRunes <= 0 {
		return ""
	}
	if len([]rune(s)) <= maxRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxRunes]) + "..."
}

func roundScore(v float64) float64 {
	if v > -0.00005 && v < 0.00005 {
		return 0
	}
	return math.Round(v*10000) / 10000
}

func appendConstraint(existing []Constraint, next Constraint) []Constraint {
	next.Type = strings.TrimSpace(next.Type)
	next.Text = strings.TrimSpace(next.Text)
	if next.Type == "" || next.Text == "" {
		return existing
	}
	for _, c := range existing {
		if c.Type == next.Type && c.Text == next.Text && c.Source == next.Source {
			return existing
		}
	}
	return append(existing, next)
}

func usableNodes(nodes []MemoryNode, now time.Time) []MemoryNode {
	out := make([]MemoryNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Quality == QualityNoise || node.Quality == QualityCorrupted {
			continue
		}
		node.Confidence = decayedConfidence(node, now)
		if node.Confidence < 0.2 && !node.TruthLocked {
			continue
		}
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence == out[j].Confidence {
			if out[i].Timestamp.Equal(out[j].Timestamp) {
				return out[i].ID < out[j].ID
			}
			return out[i].Timestamp.After(out[j].Timestamp)
		}
		return out[i].Confidence > out[j].Confidence
	})
	return out
}

func usableSubgraphNodes(nodes []MemoryNode, edges []MemoryEdge, now time.Time) []MemoryNode {
	usable := usableNodes(nodes, now)
	if len(usable) == 0 {
		return nil
	}
	byID := map[string]MemoryNode{}
	for _, node := range usable {
		byID[node.ID] = node
	}
	selected := map[string]bool{}
	frontier := make([]string, 0, 5)
	for _, node := range usable {
		selected[node.ID] = true
		frontier = append(frontier, node.ID)
		if len(frontier) >= 5 {
			break
		}
	}
	for len(frontier) > 0 && len(selected) < 12 {
		current := frontier[0]
		frontier = frontier[1:]
		for _, edge := range edges {
			if !traversableRelation(edge.Relation) {
				continue
			}
			next := ""
			switch {
			case edge.From == current:
				next = edge.To
			case edge.To == current:
				next = edge.From
			}
			if next == "" || selected[next] {
				continue
			}
			if _, ok := byID[next]; !ok {
				continue
			}
			selected[next] = true
			frontier = append(frontier, next)
			if len(selected) >= 12 {
				break
			}
		}
	}
	out := make([]MemoryNode, 0, len(selected))
	for _, node := range usable {
		if selected[node.ID] {
			out = append(out, node)
		}
	}
	return out
}

func traversableRelation(relation string) bool {
	switch relation {
	case "supports", "depends_on", "derived_from", "causes":
		return true
	default:
		return false
	}
}

type noisyRefCount struct {
	ref   string
	count int
}

func sortedNoisyRefs(noisy map[string]int) []noisyRefCount {
	out := make([]noisyRefCount, 0, len(noisy))
	for ref, count := range noisy {
		out = append(out, noisyRefCount{ref: ref, count: count})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count == out[j].count {
			return out[i].ref < out[j].ref
		}
		return out[i].count > out[j].count
	})
	return out
}

func influenceForNode(node MemoryNode) string {
	if node.Constraint != nil {
		return node.Constraint.Type
	}
	switch node.Type {
	case "tool_result":
		return "evidence"
	case "decision":
		return "decision_history"
	default:
		return "reference"
	}
}

func decayedConfidence(node MemoryNode, now time.Time) float64 {
	if node.TruthLocked || node.Timestamp.IsZero() {
		return node.Confidence
	}
	days := now.Sub(node.Timestamp).Hours() / 24
	if days <= 0 {
		return node.Confidence
	}
	factor := 1.0
	for days >= 7 {
		factor *= 0.95
		days -= 7
	}
	return node.Confidence * factor
}

func strategyPlan(strategies []Strategy, id string) []Step {
	for _, s := range strategies {
		if s.ID == id {
			return append([]Step(nil), s.ExecutionPlan...)
		}
	}
	return nil
}

type scoredStrategy struct {
	strategy Strategy
	score    float64
	reason   string
}

func rankStrategies(goal string, strategies []Strategy) []scoredStrategy {
	out := make([]scoredStrategy, 0, len(strategies))
	for _, s := range strategies {
		score, reason := strategyScoreWithReason(goal, s)
		out = append(out, scoredStrategy{strategy: s, score: score, reason: reason})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].score == out[j].score {
			return out[i].strategy.ID < out[j].strategy.ID
		}
		return out[i].score > out[j].score
	})
	return out
}

func selectStrategy(goal string, ranked []scoredStrategy, explorationRates ...int) StrategyPick {
	explorationRate := clampExplorationRatePercent(explorationRatePercent)
	if len(explorationRates) > 0 {
		explorationRate = clampExplorationRatePercent(explorationRates[0])
	}
	pick := StrategyPick{
		Selected:        "general",
		Reason:          "default strategy",
		Mode:            "control",
		ExplorationRate: float64(explorationRate) / 100,
		Rejected:        []RejectedStrategy{},
	}
	eligible := make([]scoredStrategy, 0, len(ranked))
	for _, candidate := range ranked {
		if !lowSuccessStrategy(candidate.strategy) {
			eligible = append(eligible, candidate)
		}
	}
	if len(eligible) > 0 {
		selected := eligible[0]
		if explore, candidate := explorationCandidate(goal, eligible, explorationRate); explore {
			selected = candidate
			pick.Mode = "explore"
		}
		pick.Selected = selected.strategy.ID
		pick.Reason = selected.reason
		pick.Score = selected.score
		if pick.Mode == "explore" {
			pick.Reason = "deterministic exploration buffer; " + pick.Reason
		}
	}
	for _, candidate := range ranked {
		if candidate.strategy.ID == pick.Selected {
			continue
		}
		reason := candidate.reason
		if lowSuccessStrategy(candidate.strategy) {
			reason = "rejected because prior success rate is below the risk threshold"
		}
		pick.Rejected = append(pick.Rejected, RejectedStrategy{
			ID:     candidate.strategy.ID,
			Reason: reason,
			Score:  candidate.score,
		})
		if len(pick.Rejected) >= 3 {
			break
		}
	}
	return pick
}

func explorationCandidate(goal string, eligible []scoredStrategy, explorationRate int) (bool, scoredStrategy) {
	explorationRate = clampExplorationRatePercent(explorationRate)
	if len(eligible) < 2 || explorationRate <= 0 {
		return false, scoredStrategy{}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(goal))))
	for _, candidate := range eligible {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(candidate.strategy.ID))
		_, _ = fmt.Fprintf(h, ":%d:%d", candidate.strategy.Successes, candidate.strategy.Failures)
	}
	if int(h.Sum32()%100) >= explorationRate {
		return false, scoredStrategy{}
	}
	candidates := append([]scoredStrategy(nil), eligible[1:]...)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].strategy.Samples() == candidates[j].strategy.Samples() {
			if candidates[i].score == candidates[j].score {
				return candidates[i].strategy.ID < candidates[j].strategy.ID
			}
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].strategy.Samples() < candidates[j].strategy.Samples()
	})
	return true, candidates[0]
}

func clampExplorationRatePercent(rate int) int {
	if rate < minExplorationRatePercent {
		return minExplorationRatePercent
	}
	if rate > maxExplorationRatePercent {
		return maxExplorationRatePercent
	}
	return rate
}

func equilibriumExplorationRatePercent(st state, drift DriftReport) int {
	return controlPolicyForState(st, drift).ExplorationRatePercent
}

// controlPolicyForState derives the per-turn control policy from a few legible
// stability signals — sustained clean successes (stable), recent
// failures/drift (unstable), and an alternating strategy history (oscillating).
//
// This replaces the former equilibrium/controlplane/controlsemantics packages
// (~1600 LOC of oscillation/consensus/convergence/entropy math). Measured over
// a varied multi-turn session, that whole apparatus reached the model-facing
// contract through exactly one value — the exploration rate — and changed the
// selected strategy on ~3% of turns versus a constant rate; it was an elaborate
// "explore less when unstable" switch. The heuristic keeps that behavior and
// populates the telemetry fields from the same signals so traces stay legible.
func controlPolicyForState(st state, drift DriftReport) ControlPolicy {
	shift := semanticShiftSignals(st)
	policy := ControlPolicy{
		Version:                version,
		Mode:                   "balanced",
		Controller:             "adaptive-heuristic",
		ExplorationRatePercent: explorationRatePercent,
		Gain:                   1.0,
		EquilibriumState:       "steady",
		ControlGraphEntropy:    0.7,
		SystemStabilityScore:   0.7,
		SemanticShift:          shift,
	}
	switch {
	case equilibriumOscillating(st):
		// Strategies are thrashing: damp exploration to the floor and slow the
		// mutation-feedback loop so it can settle.
		policy.ExplorationRatePercent = minExplorationRatePercent
		policy.Gain = 0.5
		policy.Mode = "stabilize"
		policy.EquilibriumState = "oscillating"
		policy.OscillationIndex = 0.8
		policy.SystemStabilityScore = 0.3
		policy.EquilibriumActions = []string{"oscillating strategy history damped exploration"}
	case len(shift) > 0:
		// IR execution is drifting from the planner IR (accumulated semantic
		// variation/drift): stabilize even when outcomes still look clean — this
		// takes priority over the stable-convergence branch below.
		policy.ExplorationRatePercent = minExplorationRatePercent
		policy.Gain = 0.6
		policy.Mode = "stabilize"
		policy.EquilibriumState = "semantic_shift"
		policy.OscillationIndex = 0.5
		policy.SystemStabilityScore = 0.4
		policy.EquilibriumActions = []string{"accumulated semantic shift damped exploration"}
	case equilibriumUnstable(st, drift):
		// Recent failures or drift: stay conservative, explore less.
		policy.ExplorationRatePercent = minExplorationRatePercent
		policy.Gain = 0.7
		policy.Mode = "dampen"
		policy.EquilibriumState = "unstable"
		policy.OscillationIndex = 0.4
		policy.SystemStabilityScore = 0.4
		policy.EquilibriumActions = []string{"recent instability reduced exploration"}
	case equilibriumStable(st, drift):
		// Sustained clean successes: widen exploration to keep learning.
		policy.ExplorationRatePercent = maxExplorationRatePercent
		policy.Gain = 1.15
		policy.Mode = "explore"
		policy.EquilibriumState = "stable"
		policy.ConvergenceVelocity = 0.8
		policy.SystemStabilityScore = 1.0
		policy.EquilibriumActions = []string{"stable convergence widened exploration"}
	}
	policy.Reasons = append([]string(nil), policy.EquilibriumActions...)

	policy.ExplorationRatePercent = clampExplorationRatePercent(policy.ExplorationRatePercent)
	policy.Gain = roundScore(policy.Gain)
	policy.MutationCooldown = controlMutationCooldown(policy.Gain)
	policy.MutationCooldownMs = policy.MutationCooldown.Milliseconds()
	policy.EquilibriumActions = limitStrings(canonicalStrings(policy.EquilibriumActions), 6)
	policy.SemanticShift = limitStrings(canonicalStrings(policy.SemanticShift), 5)
	policy.Reasons = limitStrings(canonicalStrings(policy.Reasons), 5)
	return policy
}

func equilibriumTraceForPolicy(policy ControlPolicy) *EquilibriumTrace {
	return &EquilibriumTrace{
		State:                policy.EquilibriumState,
		ControlGraphEntropy:  policy.ControlGraphEntropy,
		SystemStabilityScore: policy.SystemStabilityScore,
		ConvergenceVelocity:  policy.ConvergenceVelocity,
		OscillationIndex:     policy.OscillationIndex,
		Actions:              append([]string(nil), policy.EquilibriumActions...),
	}
}

func controlMutationCooldown(gain float64) time.Duration {
	if gain <= 0 {
		gain = 1
	}
	if gain < 0.35 {
		gain = 0.35
	}
	if gain > 1.25 {
		gain = 1.25
	}
	return time.Duration(float64(mutationFeedbackCooldown) / gain)
}

func semanticShiftSignals(st state) []string {
	recent := recentLearnings(st.Learnings, 6)
	softVariations := 0
	hardDrifts := 0
	failureMemoryFindings := 0
	for _, learning := range recent {
		for _, finding := range learning.CausalFindings {
			lower := strings.ToLower(finding)
			if strings.Contains(lower, "semantic variation") {
				softVariations++
			}
			if strings.Contains(lower, "semantic drift") {
				hardDrifts++
			}
			if strings.Contains(lower, "memory ") && strings.Contains(lower, "failed outcome") {
				failureMemoryFindings++
			}
		}
	}
	var signals []string
	if softVariations >= 3 {
		signals = append(signals, fmt.Sprintf("soft semantic variations accumulated across recent turns: %d", softVariations))
	}
	if hardDrifts >= 2 {
		signals = append(signals, fmt.Sprintf("hard semantic drift repeated across recent turns: %d", hardDrifts))
	}
	if failureMemoryFindings >= 3 {
		signals = append(signals, fmt.Sprintf("memory attribution repeatedly aligned with failed outcomes: %d", failureMemoryFindings))
	}
	return limitStrings(canonicalStrings(signals), 5)
}

func equilibriumUnstable(st state, drift DriftReport) bool {
	if hasDrift(drift) {
		return true
	}
	for _, learning := range recentLearnings(st.Learnings, 5) {
		if len(learning.BadStrategies) > 0 || len(learning.MemoryNoisePatterns) > 0 || len(learning.CompilerImprovements) > 0 {
			return true
		}
	}
	return false
}

func equilibriumOscillating(st state) bool {
	seq := learningStrategySequence(recentLearnings(st.Learnings, 6))
	if len(seq) < 4 {
		return false
	}
	unique := map[string]bool{}
	transitions := 0
	for i, id := range seq {
		unique[id] = true
		if i > 0 && id != seq[i-1] {
			transitions++
		}
	}
	return len(unique) >= 3 && transitions >= len(seq)-2
}

func learningStrategySequence(learnings []SystemLearning) []string {
	out := make([]string, 0, len(learnings))
	for _, learning := range learnings {
		id := firstNonEmpty(learning.GoodPatterns, "")
		if id == "" {
			id = firstNonEmpty(learning.BadStrategies, "")
		}
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func equilibriumStable(st state, drift DriftReport) bool {
	if hasDrift(drift) {
		return false
	}
	recent := recentLearnings(st.Learnings, 5)
	if len(recent) < 3 {
		return false
	}
	for _, learning := range recent {
		if len(learning.GoodPatterns) == 0 || len(learning.BadStrategies) > 0 || len(learning.MemoryNoisePatterns) > 0 || len(learning.CompilerImprovements) > 0 {
			return false
		}
	}
	return true
}

func recentLearnings(in []SystemLearning, n int) []SystemLearning {
	if n <= 0 || len(in) == 0 {
		return nil
	}
	if len(in) <= n {
		return in
	}
	return in[len(in)-n:]
}

func bestStrategyID(goal string, strategies []Strategy) string {
	bestID := "general"
	bestScore := -1.0
	for _, s := range strategies {
		score := strategyScore(goal, s)
		if score > bestScore {
			bestScore = score
			bestID = s.ID
		}
	}
	return bestID
}

func strategyScore(goal string, s Strategy) float64 {
	score, _ := strategyScoreWithReason(goal, s)
	return score
}

func strategyScoreWithReason(goal string, s Strategy) (float64, string) {
	score, reason := normalizedOutcomeScore(s)
	reasons := []string{reason}
	if bonus := strategyNoveltyBonus(s); bonus > 0 {
		score += bonus
		reasons = append(reasons, fmt.Sprintf("%.2f novelty bonus", bonus))
	}
	if penalty := strategyUsagePenalty(s.Samples()); penalty > 0 {
		score -= penalty
		reasons = append(reasons, fmt.Sprintf("%.2f usage penalty", penalty))
	}
	for _, p := range s.Preconditions {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && strategyPreconditionMatches(goal, p) {
			score += 0.75
			reasons = append(reasons, "matched precondition "+p)
		}
	}
	if s.ID == classifyStrategy(goal) {
		score += 0.5
		reasons = append(reasons, "matched goal classifier")
	}
	if lowSuccessStrategy(s) {
		score -= 1.0
		reasons = append(reasons, "low success history")
	}
	return score, strings.Join(reasons, "; ")
}

// strategyPreconditionMatches reports whether goal matches a strategy
// precondition. Very short preconditions (<=2 runes, e.g. "ui") match only on
// whole-token boundaries; a plain substring test let "ui" hit "pursuing",
// "build", etc., which misrouted unrelated turns — including the synthetic
// "Continue pursuing the active goal." message — to frontend-visual-verify.
func strategyPreconditionMatches(goal, precondition string) bool {
	precondition = strings.ToLower(strings.TrimSpace(precondition))
	if precondition == "" {
		return false
	}
	goal = strings.ToLower(goal)
	if len([]rune(precondition)) > 2 {
		return strings.Contains(goal, precondition)
	}
	for _, token := range strings.FieldsFunc(goal, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if token == precondition {
			return true
		}
	}
	return false
}

func normalizedOutcomeScore(s Strategy) (float64, string) {
	samples := s.Samples()
	if samples == 0 {
		return 0.5, "neutral prior"
	}
	return s.SuccessRate(), fmt.Sprintf("%.0f%% prior success after %d use(s)", s.SuccessRate()*100, samples)
}

func strategyNoveltyBonus(s Strategy) float64 {
	switch samples := s.Samples(); {
	case samples == 0:
		return 0.25
	case samples < 3:
		return 0.15
	default:
		return 0
	}
}

func strategyUsagePenalty(samples int) float64 {
	if samples <= 0 {
		return 0
	}
	return roundScore((1 - strategyUsageDecay(samples)) * 0.35)
}

func strategyUsageDecay(samples int) float64 {
	if samples <= 0 {
		return 1
	}
	return math.Exp(-float64(samples) / strategyDecayK)
}

func lowSuccessStrategy(s Strategy) bool {
	return s.Failures >= 2 && s.SuccessRate() < 0.34
}

func memoryRefIDs(refs []MemoryRef) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.ID) != "" {
			out = append(out, ref.ID)
		}
	}
	return out
}

func decisionBranches(ir PlannerIR) []DecisionBranch {
	if ir.StrategySelection == nil || ir.StrategySelection.Selected == "" {
		return nil
	}
	rejected := make([]string, 0, len(ir.StrategySelection.Rejected))
	for _, r := range ir.StrategySelection.Rejected {
		rejected = append(rejected, r.ID)
	}
	return []DecisionBranch{{
		Question:        "Which strategy should control this turn?",
		Selected:        ir.StrategySelection.Selected,
		Rejected:        rejected,
		SelectionReason: ir.StrategySelection.Reason,
	}}
}

func causalEdgesForIR(traceID string, ir PlannerIR) []CausalEdge {
	decisionID := "decision:" + traceID
	outcomeID := "outcome:" + traceID
	edges := make([]CausalEdge, 0, len(ir.MemoryReferences)+len(ir.Constraints)+1)
	for _, ref := range ir.MemoryReferences {
		edges = appendCausalEdge(edges, CausalEdge{From: ref.ID, To: decisionID, Relation: "influenced"})
	}
	for _, c := range ir.Constraints {
		if c.Source != "" {
			edges = appendCausalEdge(edges, CausalEdge{From: c.Source, To: decisionID, Relation: "constrained"})
		}
	}
	if ir.StrategySelection != nil && ir.StrategySelection.Selected != "" {
		edges = appendCausalEdge(edges, CausalEdge{From: decisionID, To: outcomeID, Relation: "selected_strategy:" + ir.StrategySelection.Selected})
	}
	return edges
}

func validateIRExecution(ir PlannerIR, tr ExecutionTrace) IRValidationResult {
	ir = canonicalizeIR(ir)
	result := IRValidationResult{}
	addHard := func(finding string) {
		finding = strings.TrimSpace(finding)
		if finding == "" {
			return
		}
		result.HardFindings = append(result.HardFindings, finding)
		result.Reject = true
	}
	addSoft := func(finding string) {
		finding = strings.TrimSpace(finding)
		if finding == "" {
			return
		}
		result.SoftFindings = append(result.SoftFindings, finding)
	}
	if selected := selectedStrategy(ir); selected != "" && selected != "general" {
		if len(tr.StrategyUsed) == 0 || tr.StrategyUsed[0] != selected {
			addHard("selected strategy drift: IR=" + selected + " trace=" + firstNonEmpty(tr.StrategyUsed, ""))
		}
	}
	if !sameStepIDs(ir.ExecutionSteps, tr.Steps) {
		addSoft("execution steps varied from planner IR")
	}
	if !sameStringSet(memoryRefIDs(ir.MemoryReferences), tr.MemoryUsed) {
		addHard("memory references drifted from planner IR")
	}
	if len(ir.ExecutionSteps) > 0 && tr.Cost.ToolCalls > len(ir.ExecutionSteps)+3 && tr.Cost.ToolCalls >= 6 {
		addSoft(fmt.Sprintf("tool calls exceeded IR step budget: steps=%d tool_calls=%d", len(ir.ExecutionSteps), tr.Cost.ToolCalls))
	}
	result.HardFindings = limitStrings(canonicalStrings(result.HardFindings), 5)
	result.SoftFindings = limitStrings(canonicalStrings(result.SoftFindings), 5)
	result.Findings = limitStrings(canonicalStrings(append(append([]string(nil), result.HardFindings...), result.SoftFindings...)), 5)
	return result
}

func sameStepIDs(a, b []Step) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i].ID) != strings.TrimSpace(b[i].ID) {
			return false
		}
	}
	return true
}

func sameStringSet(a, b []string) bool {
	a = canonicalStrings(a)
	b = canonicalStrings(b)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func appendCausalEdge(edges []CausalEdge, next CausalEdge) []CausalEdge {
	if next.From == "" || next.To == "" || next.Relation == "" {
		return edges
	}
	for _, e := range edges {
		if e == next {
			return edges
		}
	}
	return append(edges, next)
}

func estimateTokens(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Cheap conservative estimate used only for local learning, not billing.
	return (len([]rune(s)) + 3) / 4
}

func (t *Turn) RecordToolResults(records []ToolRecord) {
	if t == nil || len(records) == 0 {
		return
	}
	t.trace.ToolResults = append(t.trace.ToolResults, records...)
}

func (t *Turn) Finish(err error) {
	if t == nil || t.rt == nil {
		return
	}
	t.trace.CompletedAt = time.Now().UTC()
	t.trace.Injected = t.metrics.Injected
	t.trace.Outcome = outcomeFor(t.trace.ToolResults, err)
	if err != nil {
		t.trace.FailureReason = firstLine(err.Error())
	}
	t.trace.Cost = finishCostMetrics(t.trace.Cost, t.trace.ToolResults, t.trace.StartedAt, t.trace.CompletedAt)
	if t.metrics.Injected {
		validation := validateIRExecution(t.ir, t.trace)
		t.trace.SemanticDrift = validation.Findings
		t.trace.SemanticDriftHard = validation.HardFindings
		t.trace.SemanticDriftSoft = validation.SoftFindings
		if validation.Reject && t.trace.Outcome == "success" {
			t.trace.Outcome = "partial_success"
			t.trace.FailureReason = "IR validation rejected inconsistent execution: " + strings.Join(validation.HardFindings, "; ")
		}
	}
	for i, rec := range t.trace.ToolResults {
		toolID := fmt.Sprintf("tool:%s:%d", t.trace.ID, i)
		relation := "supported_outcome"
		if strings.TrimSpace(rec.Error) != "" {
			relation = "weakened_outcome"
		}
		t.trace.CausalEdges = appendCausalEdge(t.trace.CausalEdges, CausalEdge{
			From:     toolID,
			To:       "outcome:" + t.trace.ID,
			Relation: relation,
		})
	}
	t.trace.EfficiencyScore = efficiencyScore(t.trace.ToolResults, t.trace.StartedAt, t.trace.CompletedAt)
	t.trace.MemoryEffectiveness = memoryEffectiveness(t.trace)
	t.rt.writeTraceAndLearn(t.trace, t.strategy)
}

func outcomeFor(records []ToolRecord, err error) string {
	if err != nil {
		// A user-cancelled turn says nothing about the strategy's quality;
		// keep it out of the success/failure counters entirely.
		if errors.Is(err, context.Canceled) {
			return "aborted"
		}
		return "failure"
	}
	// Verification-shaped commands (tests, vet, typecheck, lint) are a stronger
	// signal than ordinary tool errors: a final failing test run caps the turn
	// at partial_success, and a passing one confirms success unless a later
	// tool errored after it.
	if rec, idx, ok := lastVerificationToolRecord(records); ok {
		if strings.TrimSpace(rec.Error) != "" {
			return "partial_success"
		}
		if !anyToolErrorAfter(records, idx) {
			return "success"
		}
	}
	if len(records) == 0 {
		// A turn that finishes without error and without tool calls is a
		// successful plain-text answer, not a partial success. Returning
		// partial_success here made updateStrategy count every no-tool turn as a
		// strategy failure, poisoning scores once Memory v5 is on by default.
		return "success"
	}
	for i := len(records) - 1; i >= 0; i-- {
		if strings.TrimSpace(records[i].Name) == "" {
			continue
		}
		if isPlanModeBlockedToolRecord(records[i]) {
			continue
		}
		if strings.TrimSpace(records[i].Error) == "" {
			return "success"
		}
		return "partial_success"
	}
	return "success"
}

func isPlanModeBlockedToolRecord(rec ToolRecord) bool {
	if !rec.Blocked {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(rec.Error), planModeBlockedToolError)
}

// verificationCommandMarkers identify shell commands whose exit status verifies
// the turn's work (tests, vet, typecheck, lint, build). Matched as lowercase
// substrings of the bash tool's raw argument JSON.
var verificationCommandMarkers = []string{
	"go test", "go vet", "go build", "gofmt -l", "golangci-lint",
	"npm test", "npm run test", "pnpm test", "pnpm run test", "yarn test",
	"vitest", "jest", "npx tsc", "tsc ", "typecheck", "type-check", "check:css",
	"eslint", "pytest", "ruff check", "cargo test", "cargo check", "cargo build",
	"mvn test", "gradle test", "make test", "ctest", "phpunit", "rspec",
}

func isVerificationToolRecord(rec ToolRecord) bool {
	if rec.Blocked || !strings.EqualFold(strings.TrimSpace(rec.Name), "bash") {
		return false
	}
	args := strings.ToLower(rec.Args)
	if args == "" {
		return false
	}
	for _, marker := range verificationCommandMarkers {
		if strings.Contains(args, marker) {
			return true
		}
	}
	return false
}

func lastVerificationToolRecord(records []ToolRecord) (ToolRecord, int, bool) {
	for i := len(records) - 1; i >= 0; i-- {
		if isVerificationToolRecord(records[i]) {
			return records[i], i, true
		}
	}
	return ToolRecord{}, -1, false
}

func anyToolErrorAfter(records []ToolRecord, idx int) bool {
	for i := idx + 1; i < len(records); i++ {
		if strings.TrimSpace(records[i].Name) == "" || isPlanModeBlockedToolRecord(records[i]) {
			continue
		}
		if strings.TrimSpace(records[i].Error) != "" {
			return true
		}
	}
	return false
}

func efficiencyScore(records []ToolRecord, start, end time.Time) float64 {
	if len(records) == 0 {
		return 0.5
	}
	seconds := end.Sub(start).Seconds()
	if seconds <= 0 {
		return 1
	}
	score := 1 / (1 + seconds/120)
	if score < 0 {
		return 0
	}
	return score
}

func memoryEffectiveness(tr ExecutionTrace) float64 {
	if len(tr.MemoryUsed) == 0 && len(tr.StrategyUsed) == 0 && len(tr.Steps) == 0 {
		return 0
	}
	switch tr.Outcome {
	case "success":
		return 1
	case "partial_success":
		return 0.5
	default:
		return 0
	}
}

func finishCostMetrics(cost CostMetrics, records []ToolRecord, start, end time.Time) CostMetrics {
	cost.LatencyMs = end.Sub(start).Milliseconds()
	cost.ToolCalls = len(records)
	for _, rec := range records {
		if strings.TrimSpace(rec.Error) != "" {
			cost.ToolErrors++
		}
		if rec.Truncated {
			cost.TruncatedToolResults++
		}
	}
	return cost
}

func (r *Runtime) writeTraceAndLearn(tr ExecutionTrace, strategyID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return
	}
	st := r.loadStateLocked()
	st.Strategies = ensureBuiltInStrategies(st.Strategies)
	if strategyID == "" {
		strategyID = classifyStrategy(tr.Goal)
	}
	var evaluations []MutationEvaluation
	// Aborted turns carry no quality signal: they must not grade mutations
	// under evaluation or move strategy success/failure counters.
	if tr.Outcome != "aborted" {
		st.Mutations, evaluations = evaluateMutations(st.Mutations, tr)
	}
	tr.MutationEvaluations = evaluations
	now := time.Now().UTC()
	baseline := baselineScore(st, strategyID)
	if tr.Outcome != "aborted" {
		st.Strategies = updateStrategy(st.Strategies, strategyID, tr.Outcome, tr.Injected)
	}
	learning := analyzeTrace(tr, strategyID)
	if hasLearning(learning) {
		st.Learnings = appendLearning(st.Learnings, learning)
	}
	policy := controlPolicyForState(st, DriftReport{})
	st.Nodes, st.Edges, st.Decisions = updateGraph(st.Nodes, st.Edges, st.Decisions, tr, learning)
	st.ExecutionState = updateExecutionState(st.ExecutionState, tr, learning)
	st.NoisyRefs = updateNoisyRefs(st.NoisyRefs, learning)
	st.Mutations = mergeMutationsWithPolicy(policy, st.Mutations, mutationsFromLearning(learning, baseline)...)
	st, drift := applyDriftControl(st, now, tr.ID)
	policy = controlPolicyForState(st, drift)
	tr.SemanticShift = append([]string(nil), policy.SemanticShift...)
	tr.ControlMode = policy.Mode
	tr.ControlGain = policy.Gain
	tr.ControlSignals = append([]string(nil), policy.Reasons...)
	tr.EquilibriumTrace = equilibriumTraceForPolicy(policy)
	if hasDrift(drift) {
		st.DriftReports = appendDriftReport(st.DriftReports, drift)
	}
	st, tr = applyCausalCompression(st, tr, learning, policy, now)
	st.UpdatedAt = now
	bundle := splitTrace(tr, learning, debugTraceEnabled())
	_ = appendBoundedJSONL(filepath.Join(r.dir, tracesFile), bundle.RuntimeTrace, maxRuntimeTraceJSONLLines)
	if bundle.LearningTrace != nil {
		_ = appendBoundedJSONL(filepath.Join(r.dir, learningTracesFile), *bundle.LearningTrace, maxLearningTraceJSONLLines)
	}
	if bundle.DebugTrace != nil {
		_ = appendBoundedJSONL(filepath.Join(r.dir, debugTracesFile), *bundle.DebugTrace, maxDebugTraceJSONLLines)
	}
	_ = writeJSON(filepath.Join(r.dir, stateFile), st)
}

func splitTrace(tr ExecutionTrace, learning SystemLearning, includeDebug bool) TraceBundle {
	bundle := TraceBundle{RuntimeTrace: executionTraceProjection(tr)}
	if lt, ok := learningTraceFor(tr, learning); ok {
		bundle.LearningTrace = &lt
	}
	if includeDebug {
		debug := tr
		bundle.DebugTrace = &debug
	}
	return bundle
}

func executionTraceProjection(tr ExecutionTrace) ExecutionTrace {
	return ExecutionTrace{
		ID:                  tr.ID,
		IRVersion:           tr.IRVersion,
		Goal:                tr.Goal,
		Steps:               append([]Step(nil), tr.Steps...),
		Outcome:             tr.Outcome,
		Injected:            tr.Injected,
		EfficiencyScore:     tr.EfficiencyScore,
		MemoryEffectiveness: tr.MemoryEffectiveness,
		StrategyUsed:        append([]string(nil), tr.StrategyUsed...),
		MemoryUsed:          append([]string(nil), tr.MemoryUsed...),
		SemanticDrift:       append([]string(nil), tr.SemanticDrift...),
		SemanticDriftHard:   append([]string(nil), tr.SemanticDriftHard...),
		SemanticDriftSoft:   append([]string(nil), tr.SemanticDriftSoft...),
		ControlMode:         tr.ControlMode,
		ControlGain:         tr.ControlGain,
		EquilibriumTrace:    cloneEquilibriumTrace(tr.EquilibriumTrace),
		Compression:         cloneCompressionReport(tr.Compression),
		Cost:                tr.Cost,
		FailureReason:       tr.FailureReason,
		StartedAt:           tr.StartedAt,
		CompletedAt:         tr.CompletedAt,
	}
}

func learningTraceFor(tr ExecutionTrace, learning SystemLearning) (LearningTrace, bool) {
	if !hasLearning(learning) && len(tr.MutationEvaluations) == 0 {
		return LearningTrace{}, false
	}
	return LearningTrace{
		ID:                   tr.ID,
		IRVersion:            tr.IRVersion,
		Outcome:              tr.Outcome,
		Injected:             tr.Injected,
		QualityScore:         traceQualityScore(tr),
		StrategyUsed:         append([]string(nil), tr.StrategyUsed...),
		MemoryUsed:           append([]string(nil), tr.MemoryUsed...),
		DecisionBranches:     append([]DecisionBranch(nil), tr.DecisionBranches...),
		CausalEdges:          compressCausalEdges(tr.CausalEdges, maxCompressedCausalAnchors).AnchorEdges,
		SemanticDrift:        append([]string(nil), tr.SemanticDrift...),
		SemanticDriftHard:    append([]string(nil), tr.SemanticDriftHard...),
		SemanticDriftSoft:    append([]string(nil), tr.SemanticDriftSoft...),
		SemanticShift:        append([]string(nil), tr.SemanticShift...),
		ControlMode:          tr.ControlMode,
		ControlGain:          tr.ControlGain,
		ControlSignals:       append([]string(nil), tr.ControlSignals...),
		EquilibriumTrace:     cloneEquilibriumTrace(tr.EquilibriumTrace),
		Compression:          cloneCompressionReport(tr.Compression),
		CausalFindings:       append([]string(nil), learning.CausalFindings...),
		CompilerImprovements: append([]string(nil), learning.CompilerImprovements...),
		MutationEvaluations:  append([]MutationEvaluation(nil), tr.MutationEvaluations...),
		Cost:                 tr.Cost,
		CreatedAt:            time.Now().UTC(),
	}, true
}

func cloneEquilibriumTrace(in *EquilibriumTrace) *EquilibriumTrace {
	if in == nil {
		return nil
	}
	out := *in
	out.Actions = append([]string(nil), in.Actions...)
	return &out
}

func debugTraceEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(debugTraceEnv)))
	return v == "1" || v == "true" || v == "yes"
}

func analyzeTrace(tr ExecutionTrace, strategyID string) SystemLearning {
	learning := SystemLearning{TraceID: tr.ID, CreatedAt: time.Now().UTC()}
	errorCounts := map[string]int{}
	for _, rec := range tr.ToolResults {
		if rec.Error != "" {
			errorCounts[rec.Name+"\x00"+rec.Error]++
		}
	}
	for sig, n := range errorCounts {
		if n < 2 {
			continue
		}
		parts := strings.SplitN(sig, "\x00", 2)
		toolName := parts[0]
		errLine := firstLine(parts[1])
		if isCompilerFeedbackNoise(errLine) {
			continue
		}
		learning.BadStrategies = append(learning.BadStrategies, strategyID)
		learning.MemoryNoisePatterns = append(learning.MemoryNoisePatterns, fmt.Sprintf("%s repeated error: %s", toolName, errLine))
		learning.CompilerImprovements = append(learning.CompilerImprovements, fmt.Sprintf("avoid repeating %s after repeated error: %s", toolName, errLine))
	}
	if tr.Outcome == "failure" {
		learning.BadStrategies = append(learning.BadStrategies, strategyID)
		learning.CompilerImprovements = append(learning.CompilerImprovements, "previous execution failed; require source-of-truth verification before acting")
		for _, memoryID := range tr.MemoryUsed {
			learning.CausalFindings = append(learning.CausalFindings, "memory "+memoryID+" participated in failed outcome")
		}
	}
	if tr.Outcome == "success" {
		learning.GoodPatterns = append(learning.GoodPatterns, strategyID)
		for _, memoryID := range tr.MemoryUsed {
			learning.CausalFindings = append(learning.CausalFindings, "memory "+memoryID+" supported successful outcome")
		}
	}
	hardDrift := tr.SemanticDriftHard
	softDrift := tr.SemanticDriftSoft
	if len(hardDrift) == 0 && len(softDrift) == 0 {
		hardDrift = tr.SemanticDrift
	}
	for _, finding := range hardDrift {
		learning.CausalFindings = append(learning.CausalFindings, "IR execution semantic drift: "+finding)
		learning.CompilerImprovements = append(learning.CompilerImprovements, "enforce IR execution contract: "+finding)
	}
	for _, finding := range softDrift {
		learning.CausalFindings = append(learning.CausalFindings, "IR execution semantic variation: "+finding)
	}
	if tr.Cost.ToolCalls > len(tr.Steps)+3 && tr.Cost.ToolCalls >= 6 {
		learning.CompilerImprovements = append(learning.CompilerImprovements, "tool call count exceeded plan shape; prefer tighter execution steps")
	}
	return dedupeLearning(learning)
}

func isCompilerFeedbackNoise(s string) bool {
	normalized := strings.Join(strings.Fields(s), " ")
	if normalized == compilerIROverheadSelfFeedback {
		return true
	}
	lower := strings.ToLower(normalized)
	if lower == planModeBlockedToolError {
		return true
	}
	return strings.Contains(lower, planModeBlockedToolError) &&
		(strings.Contains(lower, "repeated error") || strings.Contains(lower, "avoid repeating"))
}

func mutationsFromLearning(learning SystemLearning, baseline float64) []CompilerMutation {
	var out []CompilerMutation
	now := time.Now().UTC()
	for _, reason := range learning.CompilerImprovements {
		target := "strategy_selector"
		change := "add_constraint"
		if strings.Contains(reason, "source-of-truth") {
			target = "ir_builder"
		} else if strings.Contains(reason, "IR execution") {
			target = "ir_builder"
		} else if strings.Contains(reason, "tool call count") {
			target = "strategy_selector"
			change = "decrease_k"
		} else if strings.Contains(reason, "IR overhead") {
			target = "memory_router"
			change = "decrease_k"
		}
		out = append(out, CompilerMutation{
			Target:           target,
			Change:           change,
			Reason:           reason,
			EvidenceTraceIDs: []string{learning.TraceID},
			Status:           "testing",
			BaselineScore:    baseline,
			Applied:          true,
			CreatedAt:        now,
			UpdatedAt:        now,
		})
	}
	for _, pattern := range learning.MemoryNoisePatterns {
		out = append(out, CompilerMutation{
			Target:           "noise_filter",
			Change:           "quarantine_pattern",
			Reason:           pattern,
			EvidenceTraceIDs: []string{learning.TraceID},
			Status:           "testing",
			BaselineScore:    baseline,
			Applied:          true,
			CreatedAt:        now,
			UpdatedAt:        now,
		})
	}
	return out
}

func evaluateMutations(existing []CompilerMutation, tr ExecutionTrace) ([]CompilerMutation, []MutationEvaluation) {
	if len(existing) == 0 {
		return existing, nil
	}
	now := time.Now().UTC()
	score := traceQualityScore(tr)
	evaluations := []MutationEvaluation{}
	for i := range existing {
		m := &existing[i]
		if !m.Applied || m.Status == "accepted" || m.Status == "rejected" {
			continue
		}
		if m.Status == "" {
			m.Status = "testing"
		}
		if containsString(m.EvaluationTraceIDs, tr.ID) || containsString(m.EvidenceTraceIDs, tr.ID) {
			continue
		}
		m.EvaluationTraceIDs = append(m.EvaluationTraceIDs, tr.ID)
		trials := len(m.EvaluationTraceIDs)
		m.EvaluationScore = averageEvaluationScore(m.EvaluationScore, trials-1, score)
		m.UpdatedAt = now
		decision := "testing"
		m.EvaluationReason = fmt.Sprintf("collecting mutation validation traces (%d/%d)", trials, mutationMinEvalTrials)
		if trials >= mutationMinEvalTrials {
			if m.EvaluationScore >= mutationAcceptThreshold && m.EvaluationScore+mutationRegressionMargin >= m.BaselineScore {
				decision = "accepted"
				m.Applied = true
				m.Status = "accepted"
				m.EvaluationReason = "validation traces met confidence threshold without regressing baseline"
			} else {
				decision = "rejected"
				m.Applied = false
				m.Status = "rejected"
				m.EvaluationReason = "validation traces failed confidence threshold or regressed baseline; mutation rolled back"
			}
		}
		evaluations = append(evaluations, MutationEvaluation{
			Target:   m.Target,
			Change:   m.Change,
			Reason:   m.Reason,
			Decision: decision,
			Score:    m.EvaluationScore,
			Baseline: m.BaselineScore,
			Trials:   trials,
		})
	}
	return existing, evaluations
}

func averageEvaluationScore(previous float64, previousTrials int, next float64) float64 {
	if previousTrials <= 0 {
		return next
	}
	return (previous*float64(previousTrials) + next) / float64(previousTrials+1)
}

func traceQualityScore(tr ExecutionTrace) float64 {
	score := 0.0
	switch tr.Outcome {
	case "success":
		score += 0.7
	case "partial_success":
		score += 0.4
	default:
		score += 0.1
	}
	score += tr.EfficiencyScore * 0.2
	score += tr.MemoryEffectiveness * 0.1
	if tr.Cost.ToolCalls > 0 {
		score -= float64(tr.Cost.ToolErrors) / float64(tr.Cost.ToolCalls) * 0.2
	}
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func baselineScore(st state, strategyID string) float64 {
	for _, s := range st.Strategies {
		if s.ID == strategyID && s.Samples() > 0 {
			return 0.2 + s.SuccessRate()*0.6
		}
	}
	return 0.5
}

func mergeMutations(existing []CompilerMutation, next ...CompilerMutation) []CompilerMutation {
	return mergeMutationsWithPolicy(defaultControlPolicy(), existing, next...)
}

func mergeMutationsWithPolicy(policy ControlPolicy, existing []CompilerMutation, next ...CompilerMutation) []CompilerMutation {
	if policy.MutationCooldown <= 0 {
		policy.MutationCooldown = mutationFeedbackCooldown
	}
	seen := map[string]bool{}
	out := existing[:0]
	for _, m := range existing {
		key := m.Target + "\x00" + m.Change + "\x00" + m.Reason
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	for _, m := range next {
		key := m.Target + "\x00" + m.Change + "\x00" + m.Reason
		if seen[key] || !validMutation(m) || mutationFeedbackInCooldown(out, m, policy.MutationCooldown) {
			continue
		}
		seen[key] = true
		out = append(out, m)
	}
	if len(out) > 50 {
		out = out[len(out)-50:]
	}
	return out
}

func defaultControlPolicy() ControlPolicy {
	policy := ControlPolicy{
		Version:                version,
		Mode:                   "balanced",
		Controller:             "distributed-control-plane",
		ExplorationRatePercent: explorationRatePercent,
		Gain:                   1.0,
		EquilibriumState:       "stable",
		EquilibriumActions:     []string{"maintain global equilibrium"},
		ControlGraphEntropy:    1,
		SystemStabilityScore:   1,
		MutationCooldown:       mutationFeedbackCooldown,
		MutationCooldownMs:     mutationFeedbackCooldown.Milliseconds(),
		Reasons:                []string{"balanced distributed control policy"},
	}
	return policy
}

func mutationFeedbackInCooldown(existing []CompilerMutation, next CompilerMutation, cooldown time.Duration) bool {
	if next.CreatedAt.IsZero() {
		return false
	}
	if cooldown <= 0 {
		cooldown = mutationFeedbackCooldown
	}
	for _, m := range existing {
		if m.Target != next.Target || m.Change != next.Change {
			continue
		}
		if m.Status == "accepted" || m.Status == "rejected" {
			continue
		}
		ref := m.UpdatedAt
		if ref.IsZero() {
			ref = m.CreatedAt
		}
		if ref.IsZero() {
			continue
		}
		delta := next.CreatedAt.Sub(ref)
		if delta < 0 {
			delta = -delta
		}
		if delta < cooldown {
			return true
		}
	}
	return false
}

func hasLearning(l SystemLearning) bool {
	return len(l.BadStrategies) > 0 || len(l.GoodPatterns) > 0 || len(l.MemoryNoisePatterns) > 0 || len(l.CausalFindings) > 0 || len(l.CompilerImprovements) > 0
}

func appendLearning(existing []SystemLearning, learning SystemLearning) []SystemLearning {
	for _, l := range existing {
		if l.TraceID == learning.TraceID {
			return existing
		}
	}
	existing = append(existing, learning)
	if len(existing) > 100 {
		existing = existing[len(existing)-100:]
	}
	return existing
}

func updateNoisyRefs(existing map[string]int, learning SystemLearning) map[string]int {
	if existing == nil {
		existing = map[string]int{}
	}
	for _, pattern := range learning.MemoryNoisePatterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		existing[pattern]++
	}
	return existing
}

func applyDriftControl(st state, now time.Time, traceID string) (state, DriftReport) {
	report := DriftReport{TraceID: traceID, CreatedAt: now}
	st.Strategies = ensureBuiltInStrategies(st.Strategies)
	for _, s := range st.Strategies {
		if s.Samples() >= 5 && strategyUsageDecay(s.Samples()) < 0.65 {
			report.OverusedStrategies = append(report.OverusedStrategies, s.ID)
		}
	}
	for i := range st.Nodes {
		node := &st.Nodes[i]
		if node.TruthLocked || node.Quality == QualityCorrupted {
			continue
		}
		decayed := decayedConfidence(*node, now)
		if decayed < staleConfidenceThreshold {
			node.Quality = QualityNoise
			report.StaleMemoryNodes = append(report.StaleMemoryNodes, node.ID)
		}
	}
	conflicts, edges := detectMemoryConflicts(st.Nodes)
	for _, edge := range edges {
		st.Edges = appendEdge(st.Edges, edge)
	}
	if len(st.Edges) > 600 {
		st.Edges = st.Edges[len(st.Edges)-600:]
	}
	report.ConflictingFacts = conflicts
	report.OverusedStrategies = limitStrings(canonicalStrings(report.OverusedStrategies), 10)
	report.StaleMemoryNodes = limitStrings(canonicalStrings(report.StaleMemoryNodes), 10)
	report.ConflictingFacts = limitStrings(canonicalStrings(report.ConflictingFacts), 10)
	return st, report
}

func detectMemoryConflicts(nodes []MemoryNode) ([]string, []MemoryEdge) {
	var conflicts []string
	var edges []MemoryEdge
	for i := 0; i < len(nodes); i++ {
		for j := i + 1; j < len(nodes); j++ {
			if !factsContradict(nodes[i], nodes[j]) {
				continue
			}
			conflicts = append(conflicts, nodes[i].ID+" contradicts "+nodes[j].ID)
			edges = appendEdge(edges, MemoryEdge{From: nodes[i].ID, To: nodes[j].ID, Relation: "contradicts"})
			if len(conflicts) >= 25 {
				return conflicts, edges
			}
		}
	}
	return conflicts, edges
}

func factsContradict(a, b MemoryNode) bool {
	if a.ID == b.ID || a.Quality == QualityCorrupted || b.Quality == QualityCorrupted {
		return false
	}
	aSubject, aOK := toolResultPolarity(a.Content)
	bSubject, bOK := toolResultPolarity(b.Content)
	return aOK && bOK && aSubject.name == bSubject.name && aSubject.success != bSubject.success
}

type toolPolarity struct {
	name    string
	success bool
}

func toolResultPolarity(content string) (toolPolarity, bool) {
	content = strings.TrimSpace(content)
	if strings.HasSuffix(content, " succeeded") {
		name := strings.TrimSpace(strings.TrimSuffix(content, " succeeded"))
		return toolPolarity{name: name, success: true}, name != ""
	}
	if name, _, ok := strings.Cut(content, " failed:"); ok {
		name = strings.TrimSpace(name)
		return toolPolarity{name: name, success: false}, name != ""
	}
	return toolPolarity{}, false
}

func appendDriftReport(existing []DriftReport, report DriftReport) []DriftReport {
	if !hasDrift(report) {
		return existing
	}
	existing = append(existing, report)
	if len(existing) > 30 {
		existing = existing[len(existing)-30:]
	}
	return existing
}

func hasDrift(report DriftReport) bool {
	return len(report.OverusedStrategies) > 0 || len(report.StaleMemoryNodes) > 0 || len(report.ConflictingFacts) > 0
}

func driftRiskNotes(report DriftReport) []string {
	if !hasDrift(report) {
		return nil
	}
	var out []string
	for _, id := range report.OverusedStrategies {
		out = append(out, "drift control: reduce overused strategy "+id)
	}
	for _, id := range report.StaleMemoryNodes {
		out = append(out, "drift control: ignore stale memory "+id)
	}
	for _, conflict := range report.ConflictingFacts {
		out = append(out, "drift control: resolve memory conflict "+conflict)
	}
	return limitStrings(canonicalStrings(out), 6)
}

func dedupeLearning(l SystemLearning) SystemLearning {
	l.BadStrategies = dedupeStrings(l.BadStrategies)
	l.GoodPatterns = dedupeStrings(l.GoodPatterns)
	l.MemoryNoisePatterns = dedupeStrings(l.MemoryNoisePatterns)
	l.CausalFindings = dedupeStrings(l.CausalFindings)
	l.CompilerImprovements = dedupeStrings(l.CompilerImprovements)
	return l
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func containsString(ss []string, target string) bool {
	for _, s := range ss {
		if s == target {
			return true
		}
	}
	return false
}

func updateGraph(nodes []MemoryNode, edges []MemoryEdge, decisions []DecisionNode, tr ExecutionTrace, learning SystemLearning) ([]MemoryNode, []MemoryEdge, []DecisionNode) {
	now := time.Now().UTC()
	traceNode := MemoryNode{
		ID:          "trace:" + tr.ID,
		Type:        "state",
		Content:     fmt.Sprintf("goal=%s outcome=%s", tr.Goal, tr.Outcome),
		Timestamp:   now,
		Confidence:  confidenceForOutcome(tr.Outcome),
		Quality:     qualityForOutcome(tr.Outcome),
		TruthLocked: false,
	}
	nodes = upsertNode(nodes, traceNode)
	decision := DecisionNode{
		ID:              "decision:" + tr.ID,
		Question:        "Which execution strategy should guide this turn?",
		SelectedOption:  firstNonEmpty(tr.StrategyUsed, classifyStrategy(tr.Goal)),
		RejectedOptions: rejectedOptions(tr.DecisionBranches),
		Reasoning:       "Selected by Memory v5 strategy registry from goal classification and prior outcomes.",
		Timestamp:       now,
	}
	decisions = appendDecision(decisions, decision)
	nodes = upsertNode(nodes, MemoryNode{
		ID:          decision.ID,
		Type:        "decision",
		Content:     decision.SelectedOption + ": " + decision.Reasoning,
		Timestamp:   now,
		Confidence:  confidenceForOutcome(tr.Outcome),
		Quality:     QualityMediumSignal,
		TruthLocked: false,
	})
	edges = appendEdge(edges, MemoryEdge{From: decision.ID, To: traceNode.ID, Relation: "derived_from"})
	for i, rec := range tr.ToolResults {
		id := fmt.Sprintf("tool:%s:%d", tr.ID, i)
		quality := QualityHighSignal
		constraint := (*Constraint)(nil)
		conf := 0.95
		content := rec.Name + " succeeded"
		if rec.Error != "" {
			quality = QualityMediumSignal
			conf = 0.85
			content = rec.Name + " failed: " + firstLine(rec.Error)
			constraint = &Constraint{Type: "avoid", Text: "Do not repeat " + rec.Name + " with the same failing condition: " + firstLine(rec.Error), Source: id}
		}
		nodes = upsertNode(nodes, MemoryNode{
			ID:          id,
			Type:        "tool_result",
			Content:     content,
			Timestamp:   now,
			Confidence:  conf,
			Quality:     quality,
			Constraint:  constraint,
			TruthLocked: true,
		})
		edges = appendEdge(edges, MemoryEdge{From: id, To: traceNode.ID, Relation: "derived_from"})
	}
	for _, causal := range tr.CausalEdges {
		relation := graphRelation(causal.Relation)
		if relation == "" {
			continue
		}
		to := causal.To
		if strings.HasPrefix(to, "outcome:") {
			to = traceNode.ID
		}
		edges = appendEdge(edges, MemoryEdge{From: causal.From, To: to, Relation: relation})
	}
	for i, reason := range learning.CompilerImprovements {
		id := fmt.Sprintf("learning:%s:%d", tr.ID, i)
		nodes = upsertNode(nodes, MemoryNode{
			ID:          id,
			Type:        "fact",
			Content:     reason,
			Timestamp:   now,
			Confidence:  0.75,
			Quality:     QualityHighSignal,
			Constraint:  &Constraint{Type: "reference", Text: reason, Source: id},
			TruthLocked: false,
		})
		edges = appendEdge(edges, MemoryEdge{From: id, To: traceNode.ID, Relation: "supports"})
	}
	for i, pattern := range learning.MemoryNoisePatterns {
		id := fmt.Sprintf("noise:%s:%d", tr.ID, i)
		nodes = upsertNode(nodes, MemoryNode{
			ID:          id,
			Type:        "state",
			Content:     pattern,
			Timestamp:   now,
			Confidence:  0.9,
			Quality:     QualityCorrupted,
			Constraint:  &Constraint{Type: "avoid", Text: pattern, Source: id},
			TruthLocked: false,
		})
		edges = appendEdge(edges, MemoryEdge{From: id, To: traceNode.ID, Relation: "contradicts"})
	}
	nodes = retainMemoryNodes(nodes, maxMemoryGraphNodes)
	edges = retainMemoryEdges(edges, maxMemoryGraphEdges)
	if len(decisions) > 100 {
		decisions = decisions[len(decisions)-100:]
	}
	return nodes, edges, decisions
}

func rejectedOptions(branches []DecisionBranch) []string {
	for _, branch := range branches {
		if branch.Question == "Which strategy should control this turn?" {
			return append([]string(nil), branch.Rejected...)
		}
	}
	return nil
}

func graphRelation(relation string) string {
	switch {
	case relation == "influenced", relation == "supported_outcome":
		return "supports"
	case relation == "constrained":
		return "depends_on"
	case relation == "weakened_outcome":
		return "contradicts"
	case strings.HasPrefix(relation, "selected_strategy:"):
		return "causes"
	default:
		return ""
	}
}

func updateExecutionState(prev ExecutionState, tr ExecutionTrace, learning SystemLearning) ExecutionState {
	st := ExecutionState{
		GoalState:         tr.Goal,
		CurrentPhase:      phaseForOutcome(tr.Outcome),
		KnownFacts:        append([]string(nil), prev.KnownFacts...),
		ActiveConstraints: append([]Constraint(nil), prev.ActiveConstraints...),
		FailedStrategies:  append([]string(nil), prev.FailedStrategies...),
		UpdatedAt:         time.Now().UTC(),
	}
	if tr.Outcome == "success" {
		st.KnownFacts = append(st.KnownFacts, "strategy succeeded: "+strings.Join(tr.StrategyUsed, ","))
	} else {
		st.FailedStrategies = append(st.FailedStrategies, learning.BadStrategies...)
	}
	for _, improvement := range learning.CompilerImprovements {
		st.ActiveConstraints = appendConstraint(st.ActiveConstraints, Constraint{Type: "reference", Text: improvement, Source: "learning:" + learning.TraceID})
	}
	st.KnownFacts = lastNStrings(dedupeStrings(st.KnownFacts), 40)
	st.FailedStrategies = lastNStrings(dedupeStrings(st.FailedStrategies), 20)
	if len(st.ActiveConstraints) > 40 {
		st.ActiveConstraints = st.ActiveConstraints[len(st.ActiveConstraints)-40:]
	}
	return st
}

func upsertNode(nodes []MemoryNode, next MemoryNode) []MemoryNode {
	if next.ID == "" {
		return nodes
	}
	for i, node := range nodes {
		if node.ID != next.ID {
			continue
		}
		if node.TruthLocked {
			return nodes
		}
		nodes[i] = next
		return nodes
	}
	return append(nodes, next)
}

func appendDecision(decisions []DecisionNode, next DecisionNode) []DecisionNode {
	for _, d := range decisions {
		if d.ID == next.ID {
			return decisions
		}
	}
	return append(decisions, next)
}

func appendEdge(edges []MemoryEdge, next MemoryEdge) []MemoryEdge {
	if next.From == "" || next.To == "" || next.Relation == "" {
		return edges
	}
	for _, e := range edges {
		if e == next {
			return edges
		}
	}
	return append(edges, next)
}

func confidenceForOutcome(outcome string) float64 {
	switch outcome {
	case "success":
		return 0.9
	case "partial_success":
		return 0.65
	default:
		return 0.45
	}
}

func qualityForOutcome(outcome string) MemoryQuality {
	switch outcome {
	case "success":
		return QualityHighSignal
	case "partial_success":
		return QualityMediumSignal
	default:
		return QualityNoise
	}
}

func phaseForOutcome(outcome string) string {
	switch outcome {
	case "success":
		return "validated"
	case "partial_success":
		return "needs_followup"
	default:
		return "failed"
	}
}

func firstNonEmpty(ss []string, fallback string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return fallback
}

func lastNStrings(ss []string, n int) []string {
	if len(ss) <= n {
		return ss
	}
	return ss[len(ss)-n:]
}

func validMutation(m CompilerMutation) bool {
	switch m.Target {
	case "memory_router", "scoring", "ir_builder", "strategy_selector", "noise_filter":
	default:
		return false
	}
	switch m.Change {
	case "increase_weight", "decrease_weight", "decrease_k", "increase_k", "change_decay", "add_constraint", "quarantine_pattern":
		return true
	default:
		return false
	}
}

func updateStrategy(strategies []Strategy, id, outcome string, injected bool) []Strategy {
	if outcome == "aborted" {
		return strategies
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = "general"
	}
	strategies = ensureBuiltInStrategies(strategies)
	for i := range strategies {
		if strategies[i].ID != id {
			continue
		}
		if outcome == "success" {
			strategies[i].Successes++
			if injected {
				strategies[i].InjectedSuccesses++
			}
		} else {
			strategies[i].Failures++
			if injected {
				strategies[i].InjectedFailures++
			}
		}
		strategies[i].LastUsedAt = time.Now().UTC()
		return strategies
	}
	s := Strategy{ID: id, LastUsedAt: time.Now().UTC()}
	if outcome == "success" {
		s.Successes = 1
		if injected {
			s.InjectedSuccesses = 1
		}
	} else {
		s.Failures = 1
		if injected {
			s.InjectedFailures = 1
		}
	}
	return append(strategies, s)
}

func ensureBuiltInStrategies(strategies []Strategy) []Strategy {
	byID := map[string]int{}
	for i, s := range strategies {
		byID[s.ID] = i
	}
	for _, builtin := range builtInStrategies() {
		if idx, ok := byID[builtin.ID]; ok {
			if strategies[idx].Description == "" {
				strategies[idx].Description = builtin.Description
			}
			if len(strategies[idx].ExecutionPlan) == 0 {
				strategies[idx].ExecutionPlan = append([]Step(nil), builtin.ExecutionPlan...)
			}
			if len(strategies[idx].Preconditions) == 0 {
				strategies[idx].Preconditions = append([]string(nil), builtin.Preconditions...)
			}
			continue
		}
		strategies = append(strategies, builtin)
	}
	return strategies
}

func builtInStrategies() []Strategy {
	return []Strategy{
		{
			ID:            "code-review",
			Description:   "Inspect the real execution path, prioritize bugs and regressions, then verify with focused checks.",
			Preconditions: []string{"review", "pr", "diff"},
			ExecutionPlan: []Step{
				{ID: "review-diff", Action: "Inspect the real diff and touched code paths."},
				{ID: "verify-behavior", Action: "Run or identify focused checks that cover the changed behavior."},
				{ID: "report-findings", Action: "Report only actionable findings with file and line evidence."},
			},
		},
		{
			ID:            "bugfix-reproduce-first",
			Description:   "Reproduce or localize the failing behavior before patching, then validate the repair.",
			Preconditions: []string{"bug", "fix", "error", "修复"},
			ExecutionPlan: []Step{
				{ID: "reproduce", Action: "Reproduce or trace the failure to a concrete source of truth."},
				{ID: "patch", Action: "Patch the smallest boundary that owns the failing behavior."},
				{ID: "validate", Action: "Run focused validation that would fail before the patch."},
			},
		},
		{
			ID:            "frontend-visual-verify",
			Description:   "Validate frontend work with type checks and a rendered UI inspection when behavior is visual.",
			Preconditions: []string{"frontend", "ui", "desktop", "前端"},
			ExecutionPlan: []Step{
				{ID: "inspect-ui", Action: "Locate the relevant component, state, and i18n wiring."},
				{ID: "implement-ui", Action: "Implement the control using existing design-system patterns."},
				{ID: "verify-ui", Action: "Run type checks and inspect the rendered interaction when practical."},
			},
		},
		{
			ID:            "long-horizon-autoresearch",
			Description:   "Use durable state, evidence, and pivots for long-running goals.",
			Preconditions: []string{"goal", "research", "持续"},
			ExecutionPlan: []Step{
				{ID: "load-state", Action: "Read the durable task state and previous directions."},
				{ID: "evidence-chunk", Action: "Execute the smallest evidence-producing next chunk."},
				{ID: "writeback", Action: "Persist trace, findings, and next constraints before reporting."},
			},
		},
		{
			ID:          "general",
			Description: "Default source-first execution strategy.",
			ExecutionPlan: []Step{
				{ID: "inspect", Action: "Inspect current state before acting."},
				{ID: "change", Action: "Make the smallest change that satisfies the task."},
				{ID: "check", Action: "Run focused validation and summarize evidence."},
			},
		},
	}
}

func classifyStrategy(goal string) string {
	lower := strings.ToLower(goal)
	switch {
	case strings.Contains(lower, "review") || strings.Contains(goal, "评审"):
		return "code-review"
	case strings.Contains(lower, "bug") || strings.Contains(lower, "fix") || strings.Contains(goal, "修复"):
		return "bugfix-reproduce-first"
	case strings.Contains(lower, "frontend") || strategyPreconditionMatches(goal, "ui") || strings.Contains(goal, "前端"):
		return "frontend-visual-verify"
	case strings.Contains(lower, "goal") || strings.Contains(lower, "research") || strings.Contains(goal, "持续"):
		return "long-horizon-autoresearch"
	default:
		return "general"
	}
}

func summarizeGoal(input string) string {
	input = strings.TrimSpace(input)
	input = strings.Join(strings.Fields(input), " ")
	if len([]rune(input)) > 180 {
		r := []rune(input)
		return string(r[:180]) + "..."
	}
	return input
}

// stripReferencedContext removes the "Referenced context:" preamble and the XML
// reference blocks (<file>/<dir>/<resource>/<image>) the controller injects when
// the user @-references files, returning the user's actual text. Used only for
// goal classification (not SourceEvent). This duplicates
// control.StripReferencedContextPrefix on purpose: memorycompiler cannot import
// control because control imports the agent package that drives this runtime, so
// the two must stay in sync by convention.
func stripReferencedContext(content string) string {
	const preamble = "Referenced context:"
	s := strings.TrimSpace(content)
	if !strings.HasPrefix(s, preamble) {
		return content
	}
	s = strings.TrimSpace(s[len(preamble):])
	for {
		s = strings.TrimSpace(s)
		if s == "" {
			return ""
		}
		if !strings.HasPrefix(s, "<file ") && !strings.HasPrefix(s, "<dir ") &&
			!strings.HasPrefix(s, "<resource ") && !strings.HasPrefix(s, "<image ") {
			break
		}
		tagEnd := strings.IndexByte(s, ' ')
		if tagEnd < 0 {
			break
		}
		tagName := s[1:tagEnd]
		closeTag := "</" + tagName + ">"
		closeIdx := strings.Index(s, closeTag)
		if closeIdx < 0 {
			break
		}
		s = strings.TrimSpace(s[closeIdx+len(closeTag):])
	}
	return s
}

func traceID(t time.Time) string {
	return fmt.Sprintf("%s-%x", t.UTC().Format("20060102T150405.000000000"), rand.Int63())
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func (r *Runtime) loadState() state {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.loadStateLocked()
}

func (r *Runtime) loadStateLocked() state {
	var st state
	path := filepath.Join(r.dir, stateFile)
	b, err := os.ReadFile(path)
	if err != nil {
		return state{NoisyRefs: map[string]int{}}
	}
	if err := json.Unmarshal(b, &st); err != nil {
		_ = os.WriteFile(fmt.Sprintf("%s.corrupt-%d", path, time.Now().UnixNano()), b, 0o600)
		return state{NoisyRefs: map[string]int{}}
	}
	if st.NoisyRefs == nil {
		st.NoisyRefs = map[string]int{}
	}
	return st
}

func appendJSONL(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_ = f.Chmod(0o600)
	w := bufio.NewWriter(f)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return err
	}
	return w.Flush()
}

func appendBoundedJSONL(path string, v any, maxLines int) error {
	if maxLines <= 0 {
		return appendJSONL(path, v)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(v)
	if err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines [][]byte
	if trimmed := bytes.TrimRight(existing, "\n"); len(trimmed) > 0 {
		lines = bytes.Split(trimmed, []byte("\n"))
	}
	lines = append(lines, line)
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	out := bytes.Join(lines, []byte("\n"))
	out = append(out, '\n')
	return fileutil.AtomicWriteFile(path, out, 0o600)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return fileutil.AtomicWriteFile(path, b, 0o600)
}
