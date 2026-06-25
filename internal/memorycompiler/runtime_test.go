package memorycompiler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	runtimecanary "reasonix/internal/runtime/canary"
	runtimeresource "reasonix/internal/runtime/resource"
	runtimesnapshot "reasonix/internal/runtime/snapshot"
)

func TestStartTurnEmptyStateDoesNotInjectIR(t *testing.T) {
	rt := New(t.TempDir())
	ctx, turn := rt.StartTurn(context.Background(), "fix a bug", nil)
	if ctx != "" {
		t.Fatalf("empty compiler state injected context:\n%s", ctx)
	}
	if turn == nil {
		t.Fatal("StartTurn returned nil turn")
	}
	turn.Finish(nil)
}

func TestFailureTraceCreatesConstraintIR(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	_, turn := rt.StartTurn(context.Background(), "fix a bug", nil)
	turn.RecordToolResults([]ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	turn.Finish(nil)

	ctx, _ := rt.StartTurn(context.Background(), "continue", nil)
	if !strings.Contains(ctx, "<memory-compiler-execution>") {
		t.Fatalf("expected learned IR context, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "memory_v5_execution_contract") {
		t.Fatalf("expected compiled execution contract, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, `"source_event":"continue"`) {
		t.Fatalf("expected source event to be compiled into the contract, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "avoid repeating bash") {
		t.Fatalf("expected repeated-error constraint, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "bugfix-reproduce-first") {
		t.Fatalf("expected matching strategy in IR, got:\n%s", ctx)
	}

	st := readState(t, dir)
	if len(st.Learnings) != 1 {
		t.Fatalf("learnings = %d, want 1", len(st.Learnings))
	}
	if len(st.Mutations) == 0 {
		t.Fatal("expected structured compiler mutations")
	}
	if st.Mutations[0].Status != "testing" || !st.Mutations[0].Applied {
		t.Fatalf("mutation = %+v, want applied testing mutation", st.Mutations[0])
	}
	if len(st.NoisyRefs) == 0 {
		t.Fatal("expected noisy memory pattern to be tracked")
	}
	if st.ExecutionState.CurrentPhase != "needs_followup" {
		t.Fatalf("phase = %q, want needs_followup", st.ExecutionState.CurrentPhase)
	}
	if len(st.ExecutionState.ActiveConstraints) == 0 {
		t.Fatal("expected active constraints from learning")
	}
	assertNode(t, st.Nodes, func(n MemoryNode) bool {
		return n.Type == "tool_result" && n.TruthLocked && n.Constraint != nil
	}, "truth-locked failed tool result")
	assertNode(t, st.Nodes, func(n MemoryNode) bool {
		return n.Quality == QualityCorrupted && strings.HasPrefix(n.ID, "noise:")
	}, "corrupted noise node")
	assertEdge(t, st.Edges, "contradicts")
	traces := readTraces(t, dir)
	last := traces[len(traces)-1]
	if len(last.CausalEdges) != 0 || len(last.ToolResults) != 0 || len(last.DecisionBranches) != 0 {
		t.Fatalf("execution trace should stay minimal, got %+v", last)
	}
	if last.Cost.ToolCalls != 2 || last.Cost.ToolErrors != 2 {
		t.Fatalf("cost metrics = %+v, want two tool calls and errors", last.Cost)
	}
	learningTraces := readLearningTraces(t, dir)
	learningLast := learningTraces[len(learningTraces)-1]
	if len(learningLast.CausalEdges) == 0 {
		t.Fatalf("expected causal learning trace edges, got %+v", learningLast)
	}
}

func TestStartTurnExposesMemoryCitations(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	_, seed := rt.StartTurn(context.Background(), "fix a bug", nil)
	seed.RecordToolResults([]ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	seed.Finish(nil)

	_, turn := rt.StartTurn(context.Background(), "continue", nil)
	citations := turn.MemoryCitations()
	if len(citations) == 0 {
		t.Fatal("expected memory citations for learned compiler state")
	}
	found := false
	for _, c := range citations {
		if c.Source != "Memory v5" {
			t.Fatalf("citation source = %q, want Memory v5", c.Source)
		}
		if c.Kind != "compiler_reference" && c.Kind != "constraint" && c.Kind != "risk_note" {
			t.Fatalf("citation kind should expose compiler observability semantics: %+v", c)
		}
		if strings.Contains(c.Note, "avoid repeating bash") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected repeated bash learning in citations: %+v", citations)
	}
	citations[0].Note = "mutated"
	if turn.MemoryCitations()[0].Note == "mutated" {
		t.Fatal("MemoryCitations returned mutable backing slice")
	}
}

func TestSuccessTraceFeedsReusableStrategyAndGraph(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	_, turn := rt.StartTurn(context.Background(), "frontend ui setting", nil)
	turn.RecordToolResults([]ToolRecord{{Name: "go test", Output: "ok"}})
	turn.Finish(nil)

	ctx, _ := rt.StartTurn(context.Background(), "frontend ui setting", nil)
	if !strings.Contains(ctx, "frontend-visual-verify") {
		t.Fatalf("expected learned frontend strategy, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "go test succeeded") {
		t.Fatalf("expected tool-result memory reference, got:\n%s", ctx)
	}
	if !strings.Contains(ctx, "verify-ui") {
		t.Fatalf("expected strategy execution plan, got:\n%s", ctx)
	}

	st := readState(t, dir)
	assertNode(t, st.Nodes, func(n MemoryNode) bool {
		return n.Type == "decision" && strings.Contains(n.Content, "frontend-visual-verify")
	}, "decision node for selected strategy")
	assertEdge(t, st.Edges, "derived_from")
}

func TestGraphTraversalFiltersCorruptedMemoryAndExpandsConnectedNodes(t *testing.T) {
	now := time.Now().UTC()
	st := state{
		Nodes: []MemoryNode{
			{
				ID:         "seed",
				Type:       "tool_result",
				Content:    "validated source result",
				Timestamp:  now,
				Confidence: 0.9,
				Quality:    QualityHighSignal,
			},
			{
				ID:         "connected",
				Type:       "fact",
				Content:    "connected supporting constraint",
				Timestamp:  now,
				Confidence: 0.8,
				Quality:    QualityMediumSignal,
				Constraint: &Constraint{Type: "must_use", Text: "use connected graph evidence", Source: "connected"},
			},
			{
				ID:         "corrupted",
				Type:       "fact",
				Content:    "must never appear",
				Timestamp:  now,
				Confidence: 1,
				Quality:    QualityCorrupted,
				Constraint: &Constraint{Type: "must_use", Text: "bad constraint", Source: "corrupted"},
			},
		},
		Edges: []MemoryEdge{{From: "seed", To: "connected", Relation: "supports"}},
	}

	ir := buildIR("fix a bug", "fix a bug", st)
	got, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "use connected graph evidence") {
		t.Fatalf("expected connected graph constraint, got:\n%s", text)
	}
	if strings.Contains(text, "must never appear") || strings.Contains(text, "bad constraint") {
		t.Fatalf("corrupted memory leaked into IR:\n%s", text)
	}
}

func TestMutationEvaluationLoopAcceptsAppliedMutation(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	_, turn := rt.StartTurn(context.Background(), "fix a bug", nil)
	turn.RecordToolResults([]ToolRecord{
		{Name: "bash", Error: "exit status 1"},
		{Name: "bash", Error: "exit status 1"},
	})
	turn.Finish(nil)

	ctx, turn := rt.StartTurn(context.Background(), "fix a bug", nil)
	if !strings.Contains(ctx, "avoid repeating bash") {
		t.Fatalf("expected mutation to affect next compiled contract, got:\n%s", ctx)
	}
	turn.RecordToolResults([]ToolRecord{{Name: "go test", Output: "ok"}})
	turn.Finish(nil)

	_, turn = rt.StartTurn(context.Background(), "fix a bug", nil)
	turn.RecordToolResults([]ToolRecord{{Name: "go test", Output: "ok"}})
	turn.Finish(nil)

	st := readState(t, dir)
	found := false
	for _, m := range st.Mutations {
		if strings.Contains(m.Reason, "avoid repeating bash") {
			found = true
			if m.Status != "accepted" {
				t.Fatalf("mutation status = %q, want accepted: %+v", m.Status, m)
			}
			if len(m.EvaluationTraceIDs) == 0 {
				t.Fatalf("mutation missing evaluation trace: %+v", m)
			}
			if len(m.EvaluationTraceIDs) < mutationMinEvalTrials {
				t.Fatalf("mutation accepted before minimum validation trials: %+v", m)
			}
		}
	}
	if !found {
		t.Fatalf("missing repeated bash mutation: %+v", st.Mutations)
	}
	traces := readTraces(t, dir)
	last := traces[len(traces)-1]
	if len(last.MutationEvaluations) != 0 {
		t.Fatalf("execution trace should not carry mutation telemetry: %+v", last)
	}
	learningTraces := readLearningTraces(t, dir)
	learningLast := learningTraces[len(learningTraces)-1]
	if len(learningLast.MutationEvaluations) == 0 {
		t.Fatalf("expected mutation evaluation in learning trace: %+v", learningLast)
	}
}

func TestRecoveredToolFailureCountsAsSuccessfulOutcome(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	_, turn := rt.StartTurn(context.Background(), "fix a bug", nil)
	turn.RecordToolResults([]ToolRecord{
		{Name: "go test", Error: "failed before fix"},
		{Name: "go test", Output: "ok"},
	})
	turn.Finish(nil)

	traces := readTraces(t, dir)
	if got := traces[len(traces)-1].Outcome; got != "success" {
		t.Fatalf("outcome = %q, want success for recovered failure", got)
	}
	st := readState(t, dir)
	for _, s := range st.Strategies {
		if s.ID == "bugfix-reproduce-first" {
			if s.Successes != 1 || s.Failures != 0 {
				t.Fatalf("strategy counters = successes:%d failures:%d, want 1/0", s.Successes, s.Failures)
			}
			return
		}
	}
	t.Fatalf("bugfix strategy not found: %+v", st.Strategies)
}

func TestRuntimeStateUsesPrivateFilesAndSharedDirLock(t *testing.T) {
	dir := t.TempDir()
	rt1 := New(dir)
	rt2 := New(dir)
	if rt1.mu != rt2.mu {
		t.Fatal("runtimes for the same dir must share a lock")
	}
	_, turn := rt1.StartTurn(context.Background(), "fix a bug", nil)
	turn.RecordToolResults([]ToolRecord{{Name: "go test", Output: "ok"}})
	turn.Finish(nil)
	if runtime.GOOS == "windows" {
		return
	}
	for _, name := range []string{stateFile, tracesFile} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions = %o, want 0600", name, info.Mode().Perm())
		}
	}
}

func TestCompilerContractOrderingIsStable(t *testing.T) {
	st := state{
		NoisyRefs: map[string]int{
			"z-noise": 2,
			"a-noise": 2,
		},
		Mutations: []CompilerMutation{
			{Target: "noise_filter", Change: "quarantine_pattern", Reason: "noise mutation", Applied: true, Status: "accepted"},
		},
	}
	ir1 := buildIR("fix a bug", "fix a bug", st)
	ir2 := buildIR("fix a bug", "fix a bug", st)
	got1, err := compileExecutionContract(ir1)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := compileExecutionContract(ir2)
	if err != nil {
		t.Fatal(err)
	}
	if got1 != got2 {
		t.Fatalf("compiled contract is not stable:\n%s\n---\n%s", got1, got2)
	}
	if strings.Index(got1, "a-noise") > strings.Index(got1, "z-noise") {
		t.Fatalf("noisy refs were not emitted in stable order:\n%s", got1)
	}
}

func TestCompilerContractCanonicalizesSemanticOrder(t *testing.T) {
	ir1 := PlannerIR{
		Version:     version,
		Goal:        "fix a bug",
		SourceEvent: "fix a bug",
		RuntimeMode: "control",
		Constraints: []Constraint{
			{Type: "reference", Text: "z", Source: "z"},
			{Type: "avoid", Text: "a", Source: "a"},
		},
		RiskNotes: []string{"z risk", "a risk"},
		MemoryReferences: []MemoryRef{
			{ID: "m2", Content: "memory two", Influence: "reference"},
			{ID: "m1", Content: "memory one", Influence: "evidence"},
		},
		StrategySelection: &StrategyPick{Selected: "general", Reason: "default", Rejected: []RejectedStrategy{
			{ID: "z", Score: 0.1},
			{ID: "a", Score: 0.9},
		}},
	}
	ir2 := PlannerIR{
		Version:     version,
		Goal:        "fix a bug",
		SourceEvent: "fix a bug",
		RuntimeMode: "control",
		Constraints: []Constraint{
			{Type: "avoid", Text: "a", Source: "a"},
			{Type: "reference", Text: "z", Source: "z"},
		},
		RiskNotes: []string{"a risk", "z risk"},
		MemoryReferences: []MemoryRef{
			{ID: "m1", Content: "memory one", Influence: "evidence"},
			{ID: "m2", Content: "memory two", Influence: "reference"},
		},
		StrategySelection: &StrategyPick{Selected: "general", Reason: "default", Rejected: []RejectedStrategy{
			{ID: "a", Score: 0.9},
			{ID: "z", Score: 0.1},
		}},
	}
	got1, err := compileExecutionContract(ir1)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := compileExecutionContract(ir2)
	if err != nil {
		t.Fatal(err)
	}
	if got1 != got2 {
		t.Fatalf("canonical contracts differ:\n%s\n---\n%s", got1, got2)
	}
	if !strings.Contains(got1, `"constraints":[`) || !strings.Contains(got1, `"memory_references":[`) {
		t.Fatalf("expected explicit canonical IR arrays, got:\n%s", got1)
	}
}

func TestCompilerContractIncludesBoundedIRExplanation(t *testing.T) {
	ir := PlannerIR{
		Version:     version,
		Goal:        "optimize a workflow",
		SourceEvent: "optimize a workflow",
		RuntimeMode: "control",
		Constraints: []Constraint{
			{Type: "must_use", Text: "prefer low latency", Source: "memory:latency"},
		},
		MemoryReferences: []MemoryRef{
			{ID: "memory:latency", Content: "user prefers low latency", Quality: string(QualityHighSignal), Influence: "constraint"},
		},
		StrategySelection: &StrategyPick{Selected: "general", Reason: "matched prior low-latency pattern"},
	}
	contract, err := compileExecutionContract(ir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(contract, `"ir_explanation"`) {
		t.Fatalf("compiled contract missing IR explanation:\n%s", contract)
	}
	if !strings.Contains(contract, `"constraint_mapping":["must_use constraint from memory:latency: prefer low latency"]`) {
		t.Fatalf("compiled contract missing constraint explanation:\n%s", contract)
	}
	if !strings.Contains(contract, `"memory_influence":["memory:latency influenced decision as constraint (HIGH_SIGNAL)"]`) {
		t.Fatalf("compiled contract missing memory influence explanation:\n%s", contract)
	}
}

func TestStrategyExplorationIsDeterministicAndBounded(t *testing.T) {
	strategies := ensureBuiltInStrategies(nil)
	var exploredGoal string
	var firstPick StrategyPick
	for i := 0; i < 200; i++ {
		goal := "fix a bug variant " + string(rune('a'+i%26)) + "-" + string(rune('a'+(i/26)%26))
		ranked := rankStrategies(goal, strategies)
		pick := selectStrategy(goal, ranked)
		if pick.Mode == "explore" {
			exploredGoal = goal
			firstPick = pick
			break
		}
	}
	if exploredGoal == "" {
		t.Fatal("expected at least one deterministic exploration goal")
	}
	for i := 0; i < 5; i++ {
		got := selectStrategy(exploredGoal, rankStrategies(exploredGoal, strategies))
		if got.Selected != firstPick.Selected || got.Mode != firstPick.Mode {
			t.Fatalf("exploration selection is not deterministic: first=%+v got=%+v", firstPick, got)
		}
	}
	if firstPick.ExplorationRate != 0.1 {
		t.Fatalf("exploration rate = %v, want 0.1", firstPick.ExplorationRate)
	}
}

func TestEquilibriumExplorationRateAdaptsToLearningState(t *testing.T) {
	stable := state{Learnings: []SystemLearning{
		{TraceID: "1", GoodPatterns: []string{"general"}},
		{TraceID: "2", GoodPatterns: []string{"general"}},
		{TraceID: "3", GoodPatterns: []string{"general"}},
	}}
	if got := equilibriumExplorationRatePercent(stable, DriftReport{}); got != maxExplorationRatePercent {
		t.Fatalf("stable exploration rate = %d, want %d", got, maxExplorationRatePercent)
	}
	unstable := state{Learnings: []SystemLearning{
		{TraceID: "1", GoodPatterns: []string{"general"}},
		{TraceID: "2", BadStrategies: []string{"general"}},
	}}
	if got := equilibriumExplorationRatePercent(unstable, DriftReport{}); got != minExplorationRatePercent {
		t.Fatalf("unstable exploration rate = %d, want %d", got, minExplorationRatePercent)
	}
	oscillating := state{Learnings: []SystemLearning{
		{TraceID: "1", GoodPatterns: []string{"general"}},
		{TraceID: "2", GoodPatterns: []string{"low-latency-optimization"}},
		{TraceID: "3", BadStrategies: []string{"bugfix-reproduce-first"}},
		{TraceID: "4", GoodPatterns: []string{"general"}},
	}}
	if got := equilibriumExplorationRatePercent(oscillating, DriftReport{}); got != minExplorationRatePercent {
		t.Fatalf("oscillating exploration rate = %d, want damped %d", got, minExplorationRatePercent)
	}
	if got := equilibriumExplorationRatePercent(state{}, DriftReport{}); got != explorationRatePercent {
		t.Fatalf("neutral exploration rate = %d, want %d", got, explorationRatePercent)
	}
}

func TestControlPolicyHierarchyPrioritizesSemanticShift(t *testing.T) {
	st := state{Learnings: []SystemLearning{
		{TraceID: "1", GoodPatterns: []string{"general"}, CausalFindings: []string{"IR execution semantic variation: execution steps varied from planner IR"}},
		{TraceID: "2", GoodPatterns: []string{"general"}, CausalFindings: []string{"IR execution semantic variation: tool calls exceeded IR step budget"}},
		{TraceID: "3", GoodPatterns: []string{"general"}, CausalFindings: []string{"IR execution semantic variation: execution steps varied from planner IR"}},
	}}
	policy := controlPolicyForState(st, DriftReport{})
	if policy.Mode != "stabilize" {
		t.Fatalf("policy mode = %q, want stabilize: %+v", policy.Mode, policy)
	}
	if policy.ExplorationRatePercent != minExplorationRatePercent {
		t.Fatalf("semantic shift exploration rate = %d, want %d", policy.ExplorationRatePercent, minExplorationRatePercent)
	}
	if policy.Gain >= 1 {
		t.Fatalf("semantic shift should lower control gain, got %+v", policy)
	}
	if len(policy.SemanticShift) == 0 {
		t.Fatalf("semantic shift monitor did not emit signals: %+v", policy)
	}
}

func TestControlPolicyAdaptiveGainChangesMutationCooldown(t *testing.T) {
	stable := controlPolicyForState(state{Learnings: []SystemLearning{
		{TraceID: "1", GoodPatterns: []string{"general"}},
		{TraceID: "2", GoodPatterns: []string{"general"}},
		{TraceID: "3", GoodPatterns: []string{"general"}},
	}}, DriftReport{})
	unstable := controlPolicyForState(state{Learnings: []SystemLearning{
		{TraceID: "1", BadStrategies: []string{"general"}},
	}}, DriftReport{})
	if stable.Gain <= unstable.Gain {
		t.Fatalf("stable gain should exceed unstable gain: stable=%+v unstable=%+v", stable, unstable)
	}
	if stable.MutationCooldown >= mutationFeedbackCooldown {
		t.Fatalf("stable policy should shorten cooldown for plasticity: %+v", stable)
	}
	if unstable.MutationCooldown <= mutationFeedbackCooldown {
		t.Fatalf("unstable policy should lengthen cooldown for damping: %+v", unstable)
	}
}

func TestControlPolicyIncludesGlobalEquilibriumTrace(t *testing.T) {
	st := state{ControlReports: []ControlReport{
		{Mode: "explore", ExplorationRatePercent: maxExplorationRatePercent, Gain: 1.1},
		{Mode: "dampen", ExplorationRatePercent: minExplorationRatePercent, Gain: 0.6},
		{Mode: "explore", ExplorationRatePercent: maxExplorationRatePercent, Gain: 1.1},
		{Mode: "dampen", ExplorationRatePercent: minExplorationRatePercent, Gain: 0.6},
	}}
	base := controlPolicyForState(state{}, DriftReport{})
	policy := controlPolicyForState(st, DriftReport{})
	if policy.EquilibriumState != "damping" {
		t.Fatalf("equilibrium state = %q, want damping: %+v", policy.EquilibriumState, policy)
	}
	if policy.Mode != base.Mode {
		t.Fatalf("equilibrium must not override control mode: base=%+v got=%+v", base, policy)
	}
	if policy.Gain >= base.Gain && base.Gain > 0 {
		t.Fatalf("equilibrium should damp control gain without changing mode: base=%+v got=%+v", base, policy)
	}
	if policy.OscillationIndex < 0.7 {
		t.Fatalf("oscillation index = %.3f, want high", policy.OscillationIndex)
	}
	if len(policy.EquilibriumActions) == 0 {
		t.Fatalf("missing equilibrium actions: %+v", policy)
	}
	trace := equilibriumTraceForPolicy(policy)
	if trace == nil || trace.State != "damping" || len(trace.Actions) == 0 {
		t.Fatalf("invalid equilibrium trace: %+v", trace)
	}
	bundle := splitTrace(ExecutionTrace{
		ID:               "trace-equilibrium",
		IRVersion:        version,
		Goal:             "control stability",
		Outcome:          "success",
		EquilibriumTrace: trace,
	}, SystemLearning{TraceID: "trace-equilibrium", GoodPatterns: []string{"general"}}, false)
	if bundle.RuntimeTrace.EquilibriumTrace == nil {
		t.Fatalf("runtime trace missing equilibrium trace: %+v", bundle.RuntimeTrace)
	}
	if bundle.LearningTrace == nil || bundle.LearningTrace.EquilibriumTrace == nil {
		t.Fatalf("learning trace missing equilibrium trace: %+v", bundle.LearningTrace)
	}
}

func TestStrategyDebiasRewardsNovelContextFit(t *testing.T) {
	oldDominant := Strategy{ID: "old-dominant", Successes: 30, Preconditions: []string{"unrelated"}}
	freshFit := Strategy{ID: "fresh-fit", Preconditions: []string{"latency"}}
	ranked := rankStrategies("optimize latency", []Strategy{oldDominant, freshFit})
	if len(ranked) < 2 {
		t.Fatalf("ranked strategies = %+v", ranked)
	}
	if ranked[0].strategy.ID != "fresh-fit" {
		t.Fatalf("fresh context-fit strategy should outrank old dominant strategy: %+v", ranked)
	}
	if !strings.Contains(ranked[0].reason, "novelty bonus") || !strings.Contains(ranked[1].reason, "usage penalty") {
		t.Fatalf("strategy reasons should expose debias factors: %+v", ranked)
	}
}

func TestIRExecutionValidatorSplitsHardAndSoftSemanticDrift(t *testing.T) {
	ir := PlannerIR{
		Version:     version,
		Goal:        "fix a bug",
		SourceEvent: "fix a bug",
		MemoryReferences: []MemoryRef{
			{ID: "memory:source", Content: "use source"},
		},
		ExecutionSteps: []Step{{ID: "reproduce", Action: "Reproduce the bug."}},
		StrategySelection: &StrategyPick{
			Selected:        "bugfix-reproduce-first",
			Reason:          "matched bugfix",
			ExplorationRate: 0.1,
		},
	}
	trace := ExecutionTrace{
		Goal:         "fix a bug",
		Steps:        []Step{{ID: "patch", Action: "Patch without reproducing."}},
		StrategyUsed: []string{"general"},
		MemoryUsed:   []string{},
		Cost:         CostMetrics{ToolCalls: 7},
	}
	got := validateIRExecution(ir, trace)
	if !got.Reject {
		t.Fatalf("expected validator to reject hard semantic drift: %+v", got)
	}
	hard := strings.Join(got.HardFindings, "\n")
	for _, want := range []string{"selected strategy drift", "memory references drifted"} {
		if !strings.Contains(hard, want) {
			t.Fatalf("validator hard findings missing %q: %+v", want, got.HardFindings)
		}
	}
	soft := strings.Join(got.SoftFindings, "\n")
	for _, want := range []string{"execution steps varied", "tool calls exceeded IR step budget"} {
		if !strings.Contains(soft, want) {
			t.Fatalf("validator soft findings missing %q: %+v", want, got.SoftFindings)
		}
	}
	joined := strings.Join(got.Findings, "\n")
	for _, want := range []string{"selected strategy drift", "execution steps varied", "memory references drifted", "tool calls exceeded IR step budget"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("validator findings missing %q: %+v", want, got.Findings)
		}
	}
	learning := analyzeTrace(ExecutionTrace{ID: "trace-1", Goal: "fix", Outcome: "partial_success", SemanticDrift: got.Findings, SemanticDriftHard: got.HardFindings, SemanticDriftSoft: got.SoftFindings}, "general")
	improvements := strings.Join(learning.CompilerImprovements, "\n")
	if !strings.Contains(improvements, "enforce IR execution contract") {
		t.Fatalf("semantic drift did not feed compiler improvements: %+v", learning)
	}
	if strings.Contains(improvements, "execution steps varied") {
		t.Fatalf("soft execution variation should not be enforced as a compiler mutation: %+v", learning)
	}
}

func TestStrategyExplorationEntropyStaysAboveFloor(t *testing.T) {
	strategies := ensureBuiltInStrategies(nil)
	explored := 0
	total := 400
	for i := 0; i < total; i++ {
		goal := "strategy entropy sample " + string(rune('a'+i%26)) + "-" + string(rune('a'+(i/26)%26))
		if pick := selectStrategy(goal, rankStrategies(goal, strategies)); pick.Mode == "explore" {
			explored++
		}
	}
	rate := float64(explored) / float64(total)
	if rate <= 0.03 {
		t.Fatalf("exploration rate = %.3f, want > 0.03", rate)
	}
	if rate > 0.15 {
		t.Fatalf("exploration rate = %.3f, want bounded near configured rate", rate)
	}
}

func TestTraceSplitterKeepsRuntimeTraceSmall(t *testing.T) {
	tr := ExecutionTrace{
		ID:        "trace-large",
		IRVersion: version,
		Goal:      "debug trace split",
		Steps:     []Step{{ID: "run", Action: "Run a large command"}},
		Outcome:   "success",
		ToolResults: []ToolRecord{{
			Name:   "bash",
			Output: strings.Repeat("large-output ", 2000),
		}},
		CausalEdges: []CausalEdge{{From: "tool:trace-large:0", To: "outcome:trace-large", Relation: "supported_outcome"}},
	}
	bundle := splitTrace(tr, SystemLearning{TraceID: tr.ID, CausalFindings: []string{"tool result supported success"}}, true)
	runtimeBytes, err := json.Marshal(bundle.RuntimeTrace)
	if err != nil {
		t.Fatal(err)
	}
	debugBytes, err := json.Marshal(bundle.DebugTrace)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtimeBytes)*100 >= len(debugBytes)*30 {
		t.Fatalf("runtime trace is too large: runtime=%d debug=%d", len(runtimeBytes), len(debugBytes))
	}
	if len(bundle.RuntimeTrace.ToolResults) != 0 || len(bundle.RuntimeTrace.CausalEdges) != 0 {
		t.Fatalf("runtime trace leaked debug telemetry: %+v", bundle.RuntimeTrace)
	}
	if bundle.LearningTrace == nil || len(bundle.LearningTrace.CausalFindings) == 0 {
		t.Fatalf("learning trace missing structured signal: %+v", bundle.LearningTrace)
	}
}

func TestMutationFeedbackDampingSkipsCooldownResonance(t *testing.T) {
	now := time.Now().UTC()
	existing := []CompilerMutation{{
		Target:    "strategy_selector",
		Change:    "add_constraint",
		Reason:    "first signal",
		Status:    "testing",
		Applied:   true,
		CreatedAt: now.Add(-time.Minute),
		UpdatedAt: now.Add(-time.Minute),
	}}
	next := CompilerMutation{
		Target:    "strategy_selector",
		Change:    "add_constraint",
		Reason:    "second nearby signal",
		Status:    "testing",
		Applied:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	got := mergeMutations(existing, next)
	if len(got) != 1 {
		t.Fatalf("cooldown should damp same target/change resonance, got %+v", got)
	}
	later := next
	later.Reason = "later independent signal"
	later.CreatedAt = now.Add(mutationFeedbackCooldown + time.Minute)
	later.UpdatedAt = later.CreatedAt
	got = mergeMutations(existing, later)
	if len(got) != 2 {
		t.Fatalf("cooldown should allow later mutation signal, got %+v", got)
	}
	lowGainPolicy := defaultControlPolicy()
	lowGainPolicy.Gain = 0.5
	lowGainPolicy.MutationCooldown = controlMutationCooldown(lowGainPolicy.Gain)
	lowGainPolicy.MutationCooldownMs = lowGainPolicy.MutationCooldown.Milliseconds()
	damped := next
	damped.Reason = "low gain damped signal"
	damped.CreatedAt = now.Add(45 * time.Minute)
	damped.UpdatedAt = damped.CreatedAt
	got = mergeMutationsWithPolicy(lowGainPolicy, existing, damped)
	if len(got) != 1 {
		t.Fatalf("low gain policy should extend cooldown and damp feedback, got %+v", got)
	}
}

func TestDriftControlReportsStaleConflictingAndOverusedState(t *testing.T) {
	now := time.Now().UTC()
	st := state{
		Strategies: []Strategy{{ID: "general", Successes: 10, Failures: 0}},
		Nodes: []MemoryNode{
			{
				ID:         "old-fact",
				Type:       "fact",
				Content:    "old preference",
				Timestamp:  now.AddDate(0, 0, -365),
				Confidence: 0.1,
				Quality:    QualityMediumSignal,
			},
			{
				ID:          "tool-success",
				Type:        "tool_result",
				Content:     "go test succeeded",
				Timestamp:   now,
				Confidence:  0.9,
				Quality:     QualityHighSignal,
				TruthLocked: true,
			},
			{
				ID:          "tool-fail",
				Type:        "tool_result",
				Content:     "go test failed: timeout",
				Timestamp:   now,
				Confidence:  0.9,
				Quality:     QualityMediumSignal,
				TruthLocked: true,
			},
		},
	}
	next, report := applyDriftControl(st, now, "trace-drift")
	if !containsString(report.OverusedStrategies, "general") {
		t.Fatalf("missing overused strategy report: %+v", report)
	}
	if !containsString(report.StaleMemoryNodes, "old-fact") {
		t.Fatalf("missing stale memory report: %+v", report)
	}
	if len(report.ConflictingFacts) == 0 {
		t.Fatalf("missing conflicting fact report: %+v", report)
	}
	assertEdge(t, next.Edges, "contradicts")
	assertNode(t, next.Nodes, func(n MemoryNode) bool {
		return n.ID == "old-fact" && n.Quality == QualityNoise
	}, "stale node marked as noise")
}

func TestTruthLockedNodeCannotBeOverwritten(t *testing.T) {
	now := time.Now().UTC()
	nodes := []MemoryNode{{
		ID:          "tool:trace:0",
		Type:        "tool_result",
		Content:     "original result",
		Timestamp:   now,
		Confidence:  0.95,
		Quality:     QualityHighSignal,
		TruthLocked: true,
	}}

	nodes = upsertNode(nodes, MemoryNode{
		ID:         "tool:trace:0",
		Type:       "tool_result",
		Content:    "overwritten result",
		Timestamp:  now.Add(time.Second),
		Confidence: 0.1,
		Quality:    QualityNoise,
	})
	if nodes[0].Content != "original result" {
		t.Fatalf("truth-locked node was overwritten: %+v", nodes[0])
	}
}

func TestProductionHardeningRecordsBudgetExceededButStillInjects(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	st := state{
		Nodes: []MemoryNode{
			{
				ID:         "m1",
				Type:       "fact",
				Content:    "first relevant memory",
				Timestamp:  now,
				Confidence: 0.9,
				Quality:    QualityHighSignal,
				Constraint: &Constraint{Type: "must_use", Text: "use first relevant memory", Source: "m1"},
			},
			{
				ID:         "m2",
				Type:       "fact",
				Content:    "second relevant memory",
				Timestamp:  now,
				Confidence: 0.8,
				Quality:    QualityHighSignal,
			},
		},
		Production: normalizeProductionState(ProductionState{}),
	}
	st.Production.Budget.MaxMemoryNodes = 1
	if err := writeJSON(filepath.Join(dir, stateFile), st); err != nil {
		t.Fatal(err)
	}
	contract, turn := New(dir).StartTurn(context.Background(), "fix a bug", nil)
	// Hardening is observability only: an over-budget verdict must NOT suppress
	// the cache-safe contract. The contract is plain input text and the real
	// execution that follows is still bounded by tool permissions. (Regression
	// guard: gating here once made the whole compiler fall silent forever once
	// learned memory reached the GC cap.)
	if contract == "" {
		t.Fatal("over-budget turn must still inject the contract; hardening must not gate it")
	}
	if turn == nil || turn.trace.ProductionHardening == nil {
		t.Fatalf("missing production hardening trace: %+v", turn)
	}
	hardening := turn.trace.ProductionHardening
	if hardening.Allowed {
		t.Fatalf("hardening should record not-allowed when over budget: %+v", hardening)
	}
	if !strings.Contains(strings.Join(hardening.BlockReasons, "\n"), "memory node budget exceeded") {
		t.Fatalf("missing memory budget reason: %+v", hardening.BlockReasons)
	}
}

func TestProductionHardeningCreatesSnapshotAndRestoresState(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	now := time.Now().UTC()
	st := state{
		Nodes: []MemoryNode{{
			ID:         "stable",
			Type:       "fact",
			Content:    "stable memory",
			Timestamp:  now,
			Confidence: 0.9,
			Quality:    QualityHighSignal,
		}},
		Strategies: ensureBuiltInStrategies(nil),
		Production: normalizeProductionState(ProductionState{}),
		NoisyRefs:  map[string]int{},
	}
	tr := ExecutionTrace{
		ID:          "trace-production",
		IRVersion:   version,
		Goal:        "fix a bug",
		Steps:       []Step{{ID: "validate", Action: "Validate"}},
		Outcome:     "success",
		Cost:        CostMetrics{EstimatedInputTokens: 10, EstimatedCompiledTokens: 5, ToolCalls: 1},
		StartedAt:   now,
		CompletedAt: now.Add(time.Second),
	}
	next, tr := rt.applyProductionHardening(st, tr, defaultControlPolicy(), now)
	if tr.ProductionHardening == nil || tr.ProductionHardening.SnapshotID == "" {
		t.Fatalf("missing production snapshot in trace: %+v", tr.ProductionHardening)
	}
	if next.Production.LastSnapshotID != tr.ProductionHardening.SnapshotID {
		t.Fatalf("last snapshot = %q, trace snapshot = %q", next.Production.LastSnapshotID, tr.ProductionHardening.SnapshotID)
	}
	if snap, err := runtimesnapshot.Load(dir, tr.ProductionHardening.SnapshotID); err != nil || !snap.Stable {
		t.Fatalf("snapshot load = %+v err=%v, want stable snapshot", snap, err)
	} else if snap.BarrierID == "" || snap.StateHash == "" {
		t.Fatalf("snapshot missing atomic barrier metadata: %+v", snap)
	}
	mutated := next
	mutated.Nodes = append(mutated.Nodes, MemoryNode{ID: "corrupted", Type: "state", Content: "bad", Quality: QualityCorrupted})
	restored, err := restoreProductionSnapshot(dir, tr.ProductionHardening.SnapshotID, mutated.Production)
	if err != nil {
		t.Fatal(err)
	}
	assertNode(t, restored.Nodes, func(n MemoryNode) bool { return n.ID == "stable" }, "restored stable node")
	for _, node := range restored.Nodes {
		if node.ID == "corrupted" {
			t.Fatalf("restore kept corrupted node: %+v", restored.Nodes)
		}
	}
}

func TestProductionHardeningRecordsUnreservedToolCallButKeepsOutcome(t *testing.T) {
	now := time.Now().UTC()
	rt := New(t.TempDir())
	st := state{Production: normalizeProductionState(ProductionState{}), NoisyRefs: map[string]int{}}
	ir := PlannerIR{
		Version:        version,
		Goal:           "fix a bug",
		SourceEvent:    "fix a bug",
		Constraints:    []Constraint{{Type: "must_use", Text: "stay inside one tool call", Source: "test"}},
		ExecutionSteps: []Step{{ID: "one", Action: "Run one tool"}},
	}
	hardening := hardeningTraceForStart(context.Background(), ir, "fix a bug", st, now)
	tr := ExecutionTrace{
		ID:                  "trace-unreserved-tool",
		IRVersion:           version,
		Goal:                "fix a bug",
		Steps:               ir.ExecutionSteps,
		Outcome:             "success",
		Cost:                CostMetrics{EstimatedInputTokens: 5, EstimatedCompiledTokens: 5, ToolCalls: 2},
		ProductionHardening: hardening,
		StartedAt:           now,
		CompletedAt:         now.Add(time.Second),
	}
	_, tr = rt.applyProductionHardening(st, tr, defaultControlPolicy(), now)
	// The turn genuinely succeeded (err==nil); hardening must NOT rewrite the real
	// outcome. A budget overrun is recorded as an advisory observation only.
	// (Regression guard: this previously demoted every >budget success to
	// partial_success, poisoning the trace log.)
	if tr.Outcome != "success" {
		t.Fatalf("outcome = %q, want success preserved (hardening must not demote real results)", tr.Outcome)
	}
	if tr.ProductionHardening == nil || tr.ProductionHardening.ResourceDecision.Allowed {
		t.Fatalf("resource decision should record unreserved tool growth: %+v", tr.ProductionHardening)
	}
	if tr.ProductionHardening.Allowed {
		t.Fatalf("hardening should record not-allowed for unreserved tool growth: %+v", tr.ProductionHardening)
	}
	if !strings.Contains(strings.Join(tr.ProductionHardening.BlockReasons, "\n"), "unreserved tool call usage") {
		t.Fatalf("missing unreserved tool-call reason: %+v", tr.ProductionHardening.BlockReasons)
	}
	if tr.ProductionHardening.EnforcementAuthority != "production_hardening" {
		t.Fatalf("wrong enforcement authority: %+v", tr.ProductionHardening)
	}
}

func TestProductionHardeningConsumesCoordinatorReservation(t *testing.T) {
	dir := t.TempDir()
	rt := New(dir)
	now := time.Now().UTC()
	st := state{Production: normalizeProductionState(ProductionState{}), NoisyRefs: map[string]int{}}
	ir := PlannerIR{
		Version:        version,
		Goal:           "fix a bug",
		SourceEvent:    "fix a bug",
		Constraints:    []Constraint{{Type: "must_use", Text: "stay inside budget", Source: "test"}},
		ExecutionSteps: []Step{{ID: "one", Action: "Run one tool"}},
	}
	hardening := rt.hardeningTraceForStart(context.Background(), ir, "fix a bug", st, now, "trace-coordinator")
	if hardening.ResourceReservation.ID != "trace-coordinator" {
		t.Fatalf("reservation id = %q", hardening.ResourceReservation.ID)
	}
	if got := budgetCoordinatorForDir(dir).Snapshot(now).ActiveReservations; got != 1 {
		t.Fatalf("active reservations = %d, want 1", got)
	}
	tr := ExecutionTrace{
		ID:                  "trace-coordinator",
		IRVersion:           version,
		Goal:                "fix a bug",
		Steps:               ir.ExecutionSteps,
		Outcome:             "success",
		Cost:                CostMetrics{EstimatedInputTokens: 5, EstimatedCompiledTokens: 5, ToolCalls: 1},
		ProductionHardening: hardening,
		StartedAt:           now,
		CompletedAt:         now.Add(time.Second),
	}
	_, tr = rt.applyProductionHardening(st, tr, defaultControlPolicy(), now)
	if tr.ProductionHardening == nil || !tr.ProductionHardening.ResourceDecision.Allowed {
		t.Fatalf("coordinated reservation was not committed: %+v", tr.ProductionHardening)
	}
	if got := budgetCoordinatorForDir(dir).Snapshot(now).ActiveReservations; got != 0 {
		t.Fatalf("active reservations after commit = %d, want 0", got)
	}
	ghost := commitProductionBudget(budgetCoordinatorForDir(dir), hardening.ResourceReservation, runtimeresource.Usage{Tokens: 10, ToolCalls: 1, MemoryNodes: 0}, now.Add(time.Second))
	if ghost.Allowed || !strings.Contains(strings.Join(ghost.Reasons, "\n"), "reservation not found") {
		t.Fatalf("duplicate reservation commit was not rejected: %+v", ghost)
	}
}

func TestProductionHardeningCanaryDiffAddsCausalAttribution(t *testing.T) {
	now := time.Now().UTC()
	rt := New(t.TempDir())
	st := state{
		Production: normalizeProductionState(ProductionState{
			Canary: runtimecanary.Policy{Mode: runtimecanary.CanaryMode, TrafficPercent: 100, MinStableRuns: 1},
			CanaryBaseline: runtimecanary.BehaviorSample{
				Decision: "production_hardening",
				Strategy: "general",
				Outcome:  "success",
				Steps:    []string{"baseline"},
			},
		}),
		NoisyRefs: map[string]int{},
	}
	ir := PlannerIR{
		Version:        version,
		Goal:           "fix a bug",
		SourceEvent:    "fix a bug",
		Constraints:    []Constraint{{Type: "must_use", Text: "stay inside budget", Source: "test"}},
		ExecutionSteps: []Step{{ID: "current", Action: "Run one tool"}},
	}
	hardening := rt.hardeningTraceForStart(context.Background(), ir, "fix a bug", st, now, "trace-canary")
	tr := ExecutionTrace{
		ID:                  "trace-canary",
		IRVersion:           version,
		Goal:                "fix a bug",
		Steps:               ir.ExecutionSteps,
		Outcome:             "partial_success",
		StrategyUsed:        []string{"bugfix"},
		Cost:                CostMetrics{EstimatedInputTokens: 5, EstimatedCompiledTokens: 5, ToolCalls: 1},
		ProductionHardening: hardening,
		StartedAt:           now,
		CompletedAt:         now.Add(time.Second),
	}
	_, tr = rt.applyProductionHardening(st, tr, defaultControlPolicy(), now)
	if tr.ProductionHardening == nil || !tr.ProductionHardening.CanaryDiff.Diverged {
		t.Fatalf("missing canary divergence: %+v", tr.ProductionHardening)
	}
	if tr.ProductionHardening.CanaryDiff.Attribution.PrimaryCause == "" {
		t.Fatalf("missing canary attribution: %+v", tr.ProductionHardening.CanaryDiff)
	}
	assertCausalEdge(t, tr.CausalEdges, func(edge CausalEdge) bool {
		return strings.HasPrefix(edge.From, "canary:trace-canary:") && edge.To == "outcome:trace-canary" && edge.Relation == "explains_divergence"
	}, "canary divergence causal edge")
}

func readState(t *testing.T, dir string) state {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, stateFile))
	if err != nil {
		t.Fatal(err)
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func readTraces(t *testing.T, dir string) []ExecutionTrace {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, tracesFile))
	if err != nil {
		t.Fatal(err)
	}
	var traces []ExecutionTrace
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var tr ExecutionTrace
		if err := json.Unmarshal([]byte(line), &tr); err != nil {
			t.Fatal(err)
		}
		traces = append(traces, tr)
	}
	return traces
}

func readLearningTraces(t *testing.T, dir string) []LearningTrace {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, learningTracesFile))
	if err != nil {
		t.Fatal(err)
	}
	var traces []LearningTrace
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var tr LearningTrace
		if err := json.Unmarshal([]byte(line), &tr); err != nil {
			t.Fatal(err)
		}
		traces = append(traces, tr)
	}
	return traces
}

func assertNode(t *testing.T, nodes []MemoryNode, pred func(MemoryNode) bool, desc string) {
	t.Helper()
	for _, n := range nodes {
		if pred(n) {
			return
		}
	}
	t.Fatalf("missing node: %s\nnodes=%+v", desc, nodes)
}

func assertCausalEdge(t *testing.T, edges []CausalEdge, pred func(CausalEdge) bool, desc string) {
	t.Helper()
	for _, e := range edges {
		if pred(e) {
			return
		}
	}
	t.Fatalf("missing causal edge: %s\nedges=%+v", desc, edges)
}

func assertEdge(t *testing.T, edges []MemoryEdge, relation string) {
	t.Helper()
	for _, e := range edges {
		if e.Relation == relation {
			return
		}
	}
	t.Fatalf("missing %s edge: %+v", relation, edges)
}
