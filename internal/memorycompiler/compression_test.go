package memorycompiler

import (
	"fmt"
	"testing"
	"time"
)

func TestCompressCausalEdgesRetainsAnchorsAndCounts(t *testing.T) {
	edges := []CausalEdge{}
	for i := 0; i < 40; i++ {
		relation := "influenced"
		if i%5 == 0 {
			relation = "explains_divergence"
		}
		edges = append(edges, CausalEdge{
			From:     fmt.Sprintf("from-%02d", i),
			To:       fmt.Sprintf("to-%02d", i),
			Relation: relation,
		})
	}
	compressed := compressCausalEdges(edges, 12)
	if compressed.TotalEdges != 40 || compressed.RetainedEdges != 12 || compressed.DroppedEdges != 28 {
		t.Fatalf("unexpected edge counts: %+v", compressed)
	}
	if compressed.RelationCounts["explains_divergence"] != 8 || compressed.RelationCounts["influenced"] != 32 {
		t.Fatalf("relation counts lost causality: %+v", compressed.RelationCounts)
	}
	for _, edge := range compressed.AnchorEdges[:8] {
		if edge.Relation != "explains_divergence" {
			t.Fatalf("high-priority divergence edge was not retained first: %+v", compressed.AnchorEdges)
		}
	}
}

func TestLearningTraceUsesCompressedCausalEdges(t *testing.T) {
	edges := []CausalEdge{}
	for i := 0; i < 50; i++ {
		edges = append(edges, CausalEdge{
			From:     fmt.Sprintf("tool:%d", i),
			To:       "outcome:trace-compress",
			Relation: "supported_outcome",
		})
	}
	tr := ExecutionTrace{
		ID:          "trace-compress",
		IRVersion:   version,
		Goal:        "compress traces",
		Outcome:     "success",
		CausalEdges: edges,
	}
	learning := SystemLearning{TraceID: tr.ID, CausalFindings: []string{"memory m1 supported successful outcome"}}
	lt, ok := learningTraceFor(tr, learning)
	if !ok {
		t.Fatal("expected learning trace")
	}
	if len(lt.CausalEdges) != maxCompressedCausalAnchors {
		t.Fatalf("learning trace kept %d causal edges, want %d", len(lt.CausalEdges), maxCompressedCausalAnchors)
	}
}

func TestCausalCompressionSummarizesStateAndRetainsImportantMemory(t *testing.T) {
	now := time.Now().UTC()
	nodes := []MemoryNode{{
		ID:          "truth-old",
		Type:        "tool_result",
		Content:     "stable result",
		Timestamp:   now.Add(-24 * time.Hour),
		Confidence:  0.2,
		Quality:     QualityNoise,
		TruthLocked: true,
	}}
	for i := 0; i < maxMemoryGraphNodes+20; i++ {
		nodes = append(nodes, MemoryNode{
			ID:         fmt.Sprintf("noise-%03d", i),
			Type:       "state",
			Content:    "low signal",
			Timestamp:  now.Add(time.Duration(i) * time.Second),
			Confidence: 0.1,
			Quality:    QualityNoise,
		})
	}
	st := state{
		Nodes:     nodes,
		Edges:     []MemoryEdge{{From: "truth-old", To: "trace-1", Relation: "supports"}},
		NoisyRefs: map[string]int{},
	}
	tr := ExecutionTrace{
		ID:           "trace-compression-state",
		Goal:         "compress runtime state",
		Outcome:      "success",
		StrategyUsed: []string{"general"},
		Cost:         CostMetrics{ToolCalls: 1, EstimatedInputTokens: 10},
		StartedAt:    now,
		CompletedAt:  now.Add(time.Second),
	}
	next, tr := applyCausalCompression(st, tr, SystemLearning{TraceID: tr.ID}, defaultControlPolicy(), now)
	if tr.Compression == nil {
		t.Fatal("missing trace compression report")
	}
	if len(next.CompressionReports) != 1 {
		t.Fatalf("compression reports = %d, want 1", len(next.CompressionReports))
	}
	if len(next.Nodes) != maxMemoryGraphNodes {
		t.Fatalf("retained nodes = %d, want %d", len(next.Nodes), maxMemoryGraphNodes)
	}
	foundTruth := false
	for _, node := range next.Nodes {
		if node.ID == "truth-old" {
			foundTruth = true
			break
		}
	}
	if !foundTruth {
		t.Fatalf("truth-locked node was lost during memory folding")
	}
	if tr.Compression.MemoryGraph.NodesFolded != len(nodes) {
		t.Fatalf("compression report nodes folded = %d, want %d", tr.Compression.MemoryGraph.NodesFolded, len(nodes))
	}
}

func TestCompressCausalEdgesRetainsLongTailRelation(t *testing.T) {
	edges := []CausalEdge{}
	for i := 0; i < 40; i++ {
		edges = append(edges, CausalEdge{
			From:     fmt.Sprintf("important-%02d", i),
			To:       "decision:long-tail",
			Relation: "influenced",
		})
	}
	edges = append(edges, CausalEdge{
		From:     "rare-cause",
		To:       "decision:long-tail",
		Relation: "rare_relation",
	})

	compressed := compressCausalEdges(edges, 12)
	if compressed.RetainedEdges != 12 {
		t.Fatalf("retained edges = %d, want 12", compressed.RetainedEdges)
	}
	foundRare := false
	for _, edge := range compressed.AnchorEdges {
		if edge.Relation == "rare_relation" {
			foundRare = true
			break
		}
	}
	if !foundRare {
		t.Fatalf("long-tail causal relation was dropped: %+v", compressed.AnchorEdges)
	}
	if len(compressed.LongTailEdges) != 1 || compressed.LongTailSignals[0] != "rare_relation" {
		t.Fatalf("missing long-tail bias report: %+v", compressed)
	}
}

func TestCompressionReportIncludesCrossGraphAlignment(t *testing.T) {
	now := time.Now().UTC()
	st := state{
		Nodes: []MemoryNode{{
			ID:         "memory-1",
			Type:       "fact",
			Content:    "supports plan",
			Timestamp:  now,
			Confidence: 0.9,
			Quality:    QualityHighSignal,
		}},
		Edges: []MemoryEdge{
			{From: "memory-1", To: "trace-1", Relation: "supports"},
			{From: "memory-1", To: "decision-1", Relation: "depends_on"},
		},
	}
	tr := ExecutionTrace{
		ID:      "trace-alignment",
		Outcome: "success",
		CausalEdges: []CausalEdge{
			{From: "memory-1", To: "decision:trace-alignment", Relation: "influenced"},
			{From: "decision:trace-alignment", To: "outcome:trace-alignment", Relation: "selected_strategy:general"},
		},
		StartedAt:   now,
		CompletedAt: now.Add(time.Second),
	}

	report := buildCompressionReport(st, tr, SystemLearning{}, defaultControlPolicy(), now)
	if report.Alignment.Status != "partial" {
		t.Fatalf("alignment status = %q, want partial: %+v", report.Alignment.Status, report.Alignment)
	}
	if !containsString(report.Alignment.SharedRelations, "supports") {
		t.Fatalf("missing shared support relation: %+v", report.Alignment)
	}
	if !containsString(report.Alignment.MissingFromMemory, "causes") {
		t.Fatalf("missing causal-only cause relation: %+v", report.Alignment)
	}
	if report.BiasCorrection.AlignmentStatus != report.Alignment.Status {
		t.Fatalf("bias report did not mirror alignment status: %+v", report.BiasCorrection)
	}
}

func TestTruthLockedImportanceDecaysForCompressionPriority(t *testing.T) {
	now := time.Now().UTC()
	oldTruth := MemoryNode{
		ID:          "old-truth",
		Type:        "tool_result",
		Content:     "old low-confidence truth",
		Timestamp:   now.Add(-365 * 24 * time.Hour),
		Confidence:  0.2,
		Quality:     QualityNoise,
		TruthLocked: true,
	}
	newSignal := MemoryNode{
		ID:         "new-signal",
		Type:       "fact",
		Content:    "new high signal",
		Timestamp:  now,
		Confidence: 0.95,
		Quality:    QualityHighSignal,
	}
	retained := retainMemoryNodes([]MemoryNode{oldTruth, newSignal}, 1, now)
	if len(retained) != 1 || retained[0].ID != "new-signal" {
		t.Fatalf("stale truth lock dominated high-signal node: %+v", retained)
	}
	memory := compressMemoryGraph(state{Nodes: []MemoryNode{oldTruth, newSignal}}, now)
	if !containsString(memory.TruthLockDecay, "old-truth") {
		t.Fatalf("missing truth-lock decay report: %+v", memory)
	}
}

func TestCausalSignalDynamicsDetectsFlattening(t *testing.T) {
	now := time.Now().UTC()
	relations := []string{"constrained", "influenced", "supported_outcome", "weakened_outcome"}
	edges := []CausalEdge{}
	for i := 0; i < 6; i++ {
		for _, relation := range relations {
			edges = append(edges, CausalEdge{
				From:     fmt.Sprintf("%s-%02d", relation, i),
				To:       "decision:flat",
				Relation: relation,
			})
		}
	}
	report := buildCompressionReport(state{}, ExecutionTrace{
		ID:           "trace-flat",
		Outcome:      "partial_success",
		CausalEdges:  edges,
		StartedAt:    now,
		CompletedAt:  now.Add(time.Second),
		StrategyUsed: []string{"general"},
	}, SystemLearning{}, defaultControlPolicy(), now)
	if !report.Dynamics.OverRegularized {
		t.Fatalf("expected flattened causal dynamics to be flagged: %+v", report.Dynamics)
	}
	if report.Dynamics.EntropyBand != "high" || report.Dynamics.AmplitudeBand != "flat" {
		t.Fatalf("unexpected dynamics bands: %+v", report.Dynamics)
	}
	if len(report.Dynamics.AmplifiedSignals) == 0 || len(report.Dynamics.EntropySpikes) == 0 {
		t.Fatalf("missing hierarchy amplification or entropy spike hints: %+v", report.Dynamics)
	}
}

func TestCrossGraphAlignmentCapsOverCoupling(t *testing.T) {
	causal := compressCausalEdges([]CausalEdge{
		{From: "memory-1", To: "decision:coupled", Relation: "influenced"},
		{From: "constraint-1", To: "decision:coupled", Relation: "constrained"},
	}, maxCompressedCausalAnchors)
	memory := MemoryGraphCompression{
		RelationCounts: map[string]int{
			"supports":   3,
			"depends_on": 2,
		},
	}
	alignment := crossGraphAlignment(causal, memory)
	if alignment.RawCouplingStrength != 1 {
		t.Fatalf("raw coupling = %v, want 1: %+v", alignment.RawCouplingStrength, alignment)
	}
	if alignment.CouplingStrength != maxGraphCouplingStrength || !alignment.CouplingCapped {
		t.Fatalf("coupling was not capped: %+v", alignment)
	}
	if alignment.IndependenceStatus != "overcoupled" {
		t.Fatalf("independence status = %q, want overcoupled", alignment.IndependenceStatus)
	}
}

func TestCausalSignalDynamicsKeepsSharpHierarchy(t *testing.T) {
	edges := []CausalEdge{}
	for i := 0; i < 30; i++ {
		edges = append(edges, CausalEdge{
			From:     fmt.Sprintf("support-%02d", i),
			To:       "outcome:sharp",
			Relation: "supported_outcome",
		})
	}
	edges = append(edges, CausalEdge{
		From:     "rare",
		To:       "outcome:sharp",
		Relation: "influenced",
	})
	causal := compressCausalEdges(edges, maxCompressedCausalAnchors)
	dynamics := causalSignalDynamics(causal, CrossGraphAlignment{IndependenceStatus: "independent"})
	if dynamics.OverRegularized {
		t.Fatalf("sharp causal hierarchy was misclassified as over-regularized: %+v", dynamics)
	}
	if dynamics.AmplitudeBand != "sharp" {
		t.Fatalf("amplitude band = %q, want sharp: %+v", dynamics.AmplitudeBand, dynamics)
	}
	if len(dynamics.AmplifiedSignals) != 0 || len(dynamics.EntropySpikes) != 0 {
		t.Fatalf("sharp hierarchy should not request amplification: %+v", dynamics)
	}
}

func TestObserverLoopExcludesCurrentTrace(t *testing.T) {
	now := time.Now().UTC()
	relations := []string{"constrained", "influenced", "supported_outcome", "weakened_outcome"}
	edges := []CausalEdge{}
	for i := 0; i < 4; i++ {
		for _, relation := range relations {
			edges = append(edges, CausalEdge{From: fmt.Sprintf("%s-%d", relation, i), To: "decision:current", Relation: relation})
		}
	}
	report := buildCompressionReport(state{}, ExecutionTrace{
		ID:          "trace-current-only",
		Outcome:     "partial_success",
		CausalEdges: edges,
		StartedAt:   now,
		CompletedAt: now.Add(time.Second),
	}, SystemLearning{}, defaultControlPolicy(), now)
	if !report.Dynamics.OverRegularized {
		t.Fatalf("test setup expected current dynamics to be over-regularized: %+v", report.Dynamics)
	}
	if !report.ObserverLoop.ReadOnlyProjection || !report.ObserverLoop.CurrentTraceExcluded {
		t.Fatalf("observer loop is not read-only/current-excluding: %+v", report.ObserverLoop)
	}
	if report.ObserverLoop.LaggedSamples != 0 || report.ObserverLoop.FeedbackEligible || len(report.ObserverLoop.FeedbackSignals) != 0 {
		t.Fatalf("current trace leaked into observer feedback: %+v", report.ObserverLoop)
	}
}

func TestObserverLoopUsesLaggedFeedbackOnly(t *testing.T) {
	now := time.Now().UTC()
	st := state{CompressionReports: []CompressionReport{{
		TraceID: "previous-flat",
		Dynamics: CausalSignalDynamics{
			OverRegularized:  true,
			AmplifiedSignals: []string{"supported_outcome"},
			EntropySpikes:    []string{"rare_relation"},
		},
	}}}
	report := buildCompressionReport(st, ExecutionTrace{
		ID:          "trace-next",
		Outcome:     "success",
		CausalEdges: []CausalEdge{{From: "strong", To: "outcome:trace-next", Relation: "supported_outcome"}},
		StartedAt:   now,
		CompletedAt: now.Add(time.Second),
	}, SystemLearning{}, defaultControlPolicy(), now)
	if report.ObserverLoop.LaggedSamples != 1 || !report.ObserverLoop.FeedbackEligible {
		t.Fatalf("lagged feedback was not enabled: %+v", report.ObserverLoop)
	}
	if !containsString(report.ObserverLoop.FeedbackSignals, "supported_outcome") || !containsString(report.ObserverLoop.FeedbackSignals, "rare_relation") {
		t.Fatalf("lagged feedback signals missing: %+v", report.ObserverLoop.FeedbackSignals)
	}
	if report.ObserverLoop.Damping.State != "armed" {
		t.Fatalf("damping state = %q, want armed", report.ObserverLoop.Damping.State)
	}
}

func TestObserverLoopDampsOscillatingFeedback(t *testing.T) {
	st := state{CompressionReports: []CompressionReport{
		{TraceID: "r1", Dynamics: CausalSignalDynamics{OverRegularized: true, AmplifiedSignals: []string{"supported_outcome"}}},
		{TraceID: "r2", Dynamics: CausalSignalDynamics{OverRegularized: false}},
		{TraceID: "r3", Dynamics: CausalSignalDynamics{OverRegularized: true, EntropySpikes: []string{"rare_relation"}}},
		{TraceID: "r4", Dynamics: CausalSignalDynamics{OverRegularized: false}},
		{TraceID: "r5", Dynamics: CausalSignalDynamics{OverRegularized: true, AmplifiedSignals: []string{"weakened_outcome"}}},
	}}
	report := observerLoopReport(st.CompressionReports, CausalSignalDynamics{}, defaultControlPolicy(), 0)
	if report.Damping.State != "damped" {
		t.Fatalf("damping state = %q, want damped: %+v", report.Damping.State, report)
	}
	if report.FeedbackEligible || len(report.FeedbackSignals) != 0 {
		t.Fatalf("damped observer loop still exposes feedback: %+v", report)
	}
	if report.Damping.OscillationIndex < 0.5 || len(report.Damping.SuppressedSignals) == 0 {
		t.Fatalf("missing oscillation damping details: %+v", report.Damping)
	}
}

func TestShadowObserverWarnsWithoutFeedback(t *testing.T) {
	st := state{CompressionReports: []CompressionReport{{
		TraceID:  "previous-stable",
		Dynamics: CausalSignalDynamics{OverRegularized: false},
	}}}
	current := CausalSignalDynamics{
		OverRegularized:  true,
		AmplifiedSignals: []string{"supported_outcome"},
		EntropySpikes:    []string{"rare_relation"},
	}
	report := observerLoopReport(st.CompressionReports, current, defaultControlPolicy(), 0)
	if !report.ShadowObserver.CurrentTraceObserved || report.ShadowObserver.AffectsExecution {
		t.Fatalf("shadow observer is not read-only: %+v", report.ShadowObserver)
	}
	if report.ShadowObserver.WarningLevel != "high" {
		t.Fatalf("shadow warning = %q, want high: %+v", report.ShadowObserver.WarningLevel, report.ShadowObserver)
	}
	if !containsString(report.ShadowObserver.ObservationOnlySignals, "supported_outcome") ||
		!containsString(report.ShadowObserver.ObservationOnlySignals, "rare_relation") {
		t.Fatalf("shadow observer lost current observation-only signals: %+v", report.ShadowObserver)
	}
	if report.FeedbackEligible || len(report.FeedbackSignals) != 0 {
		t.Fatalf("shadow observation leaked into feedback: %+v", report)
	}
}

func TestObserverLoopAdaptsLagWindowToSystemStability(t *testing.T) {
	history := []CompressionReport{}
	for i := 0; i < 8; i++ {
		history = append(history, CompressionReport{
			TraceID:  fmt.Sprintf("r%d", i),
			Dynamics: CausalSignalDynamics{OverRegularized: i%2 == 0},
		})
	}
	stablePolicy := defaultControlPolicy()
	stablePolicy.SystemStabilityScore = 0.95
	stablePolicy.OscillationIndex = 0.1
	stable := observerLoopReport(history, CausalSignalDynamics{}, stablePolicy, 0)
	if stable.LagWindow.Size != minObserverLagWindow || stable.LaggedSamples != minObserverLagWindow {
		t.Fatalf("stable lag window = %+v, samples=%d", stable.LagWindow, stable.LaggedSamples)
	}
	unstablePolicy := defaultControlPolicy()
	unstablePolicy.SystemStabilityScore = 0.2
	unstablePolicy.OscillationIndex = 0.7
	unstable := observerLoopReport(history, CausalSignalDynamics{}, unstablePolicy, 0)
	if unstable.LagWindow.Size != maxObserverLagWindow || unstable.LaggedSamples != len(history) {
		t.Fatalf("unstable lag window = %+v, samples=%d", unstable.LagWindow, unstable.LaggedSamples)
	}
}

func TestPredictionActionBridgeIsAdvisoryOnly(t *testing.T) {
	st := state{CompressionReports: []CompressionReport{{
		TraceID:  "previous-stable",
		Dynamics: CausalSignalDynamics{OverRegularized: false},
	}}}
	current := CausalSignalDynamics{
		OverRegularized:  true,
		AmplifiedSignals: []string{"supported_outcome"},
		EntropySpikes:    []string{"rare_relation"},
	}
	report := observerLoopReport(st.CompressionReports, current, defaultControlPolicy(), 0)
	if !report.AdvisoryBridge.AdvisoryEligible {
		t.Fatalf("missing advisory bridge signal: %+v", report.AdvisoryBridge)
	}
	if report.AdvisoryBridge.AffectsExecution || !report.AdvisoryBridge.RequiresExplicitPromotion || !report.AdvisoryBridge.FeedbackBypassBlocked {
		t.Fatalf("advisory bridge can affect execution: %+v", report.AdvisoryBridge)
	}
	if len(report.AdvisoryBridge.AdvisorySignals) > maxPredictionAdvisories {
		t.Fatalf("advisory bridge exceeded bound: %+v", report.AdvisoryBridge)
	}
	if report.FeedbackEligible || len(report.FeedbackSignals) != 0 {
		t.Fatalf("advisory bridge leaked into feedback: %+v", report)
	}
}

func TestTemporalSyncSeparatesLagAndDampingClocks(t *testing.T) {
	history := []CompressionReport{}
	for i := 0; i < 8; i++ {
		history = append(history, CompressionReport{
			TraceID:  fmt.Sprintf("r%d", i),
			Dynamics: CausalSignalDynamics{OverRegularized: i%2 == 0},
		})
	}
	stablePolicy := defaultControlPolicy()
	stablePolicy.SystemStabilityScore = 0.95
	stablePolicy.OscillationIndex = 0.1
	report := observerLoopReport(history, CausalSignalDynamics{}, stablePolicy, 0)
	if report.TemporalSync.LagWindow != minObserverLagWindow || report.TemporalSync.DampingWindow != defaultObserverLagWindow {
		t.Fatalf("unexpected synchronized windows: %+v", report.TemporalSync)
	}
	if report.TemporalSync.NormalizedWindow != defaultObserverLagWindow || report.TemporalSync.Status != "bounded_desync" {
		t.Fatalf("temporal sync did not normalize clocks: %+v", report.TemporalSync)
	}
}

func TestPredictiveSignalBacklogDecaysStaleWarnings(t *testing.T) {
	history := []CompressionReport{}
	for i := 0; i < maxObserverLagWindow+2; i++ {
		signals := []string{"predicted_observer_oscillation"}
		if i == 0 {
			signals = []string{"stale_warning"}
		}
		history = append(history, CompressionReport{
			TraceID: fmt.Sprintf("r%d", i),
			ObserverLoop: ObserverLoopReport{
				ShadowObserver: ShadowObserverReport{ObservationOnlySignals: signals},
			},
		})
	}
	current := CausalSignalDynamics{
		OverRegularized: true,
		EntropySpikes:   []string{"rare_relation"},
	}
	report := observerLoopReport(history, current, defaultControlPolicy(), 0)
	if !containsString(report.SignalBacklog.PendingSignals, "predicted_observer_oscillation") {
		t.Fatalf("missing active warning in backlog: %+v", report.SignalBacklog)
	}
	if !containsString(report.SignalBacklog.StaleSignals, "stale_warning") || !report.AdvisoryBridge.BacklogResolved {
		t.Fatalf("stale warning was not decayed/resolved: backlog=%+v bridge=%+v", report.SignalBacklog, report.AdvisoryBridge)
	}
	if report.SignalBacklog.PendingCount > report.SignalBacklog.MaxSignals {
		t.Fatalf("backlog exceeded bound: %+v", report.SignalBacklog)
	}
}

func TestPredictionBiasGuardBlocksImplicitPlanningDrift(t *testing.T) {
	current := CausalSignalDynamics{
		OverRegularized:  true,
		AmplifiedSignals: []string{"supported_outcome"},
	}
	report := observerLoopReport(nil, current, defaultControlPolicy(), 0)
	if !report.PredictionBias.PlanningDriftBlocked || !report.PredictionBias.AdvisoryNeutralityEnforced {
		t.Fatalf("prediction bias guard did not block implicit drift: %+v", report.PredictionBias)
	}
	if !containsString(report.PredictionBias.CounterfactualChecks, "compare_advisory_counterfactual") {
		t.Fatalf("missing advisory counterfactual check: %+v", report.PredictionBias.CounterfactualChecks)
	}
	if report.AdvisoryBridge.AffectsExecution || report.FeedbackEligible {
		t.Fatalf("prediction guard leaked into execution/feedback: %+v", report)
	}
}

func TestTemporalVarianceKeepsPhysicalLatencyVisible(t *testing.T) {
	history := []CompressionReport{{
		TraceID: "slow",
		ObserverLoop: ObserverLoopReport{
			TemporalVariance: TemporalVarianceReport{PhysicalLatencyMs: 1000},
		},
	}}
	report := observerLoopReport(history, CausalSignalDynamics{}, defaultControlPolicy(), 100)
	if report.TemporalVariance.LogicalClock != "causal_observation_window" || report.TemporalVariance.PhysicalClock != "execution_latency" {
		t.Fatalf("temporal variance clocks not recorded: %+v", report.TemporalVariance)
	}
	if report.TemporalVariance.PhysicalLatencyMs != 100 || report.TemporalVariance.JitterIndex < 0.8 {
		t.Fatalf("physical jitter was hidden: %+v", report.TemporalVariance)
	}
	if !report.TemporalVariance.VarianceVisible || report.TemporalVariance.VarianceBand != "high" {
		t.Fatalf("physical variance was not surfaced: %+v", report.TemporalVariance)
	}
}

func TestLongTailSafetyPreservesRareStaleSignals(t *testing.T) {
	history := []CompressionReport{}
	for i := 0; i < maxObserverLagWindow+2; i++ {
		signals := []string{"predicted_observer_oscillation"}
		if i == 0 {
			signals = []string{"rare_slow_burn_failure"}
		}
		history = append(history, CompressionReport{
			TraceID: fmt.Sprintf("r%d", i),
			ObserverLoop: ObserverLoopReport{
				ShadowObserver: ShadowObserverReport{ObservationOnlySignals: signals},
			},
		})
	}
	report := observerLoopReport(history, CausalSignalDynamics{}, defaultControlPolicy(), 0)
	if !report.LongTailSafety.LongTailPreserved || !containsString(report.LongTailSafety.ProtectedSignals, "rare_slow_burn_failure") {
		t.Fatalf("rare stale signal was not protected: %+v", report.LongTailSafety)
	}
	if containsString(report.LongTailSafety.DecayedSignals, "rare_slow_burn_failure") {
		t.Fatalf("protected long-tail signal also decayed: %+v", report.LongTailSafety)
	}
	if report.LongTailSafety.RareSignalCount > report.LongTailSafety.RetentionFloor {
		t.Fatalf("long-tail retention exceeded floor: %+v", report.LongTailSafety)
	}
}

func TestLayerCollapseReportDetectsSemanticSaturation(t *testing.T) {
	now := time.Now().UTC()
	relations := []string{"constrained", "influenced", "supported_outcome", "weakened_outcome"}
	edges := []CausalEdge{}
	for i := 0; i < 6; i++ {
		for _, relation := range relations {
			edges = append(edges, CausalEdge{
				From:     fmt.Sprintf("%s-%02d", relation, i),
				To:       "decision:layer-collapse",
				Relation: relation,
			})
		}
	}
	st := state{
		Nodes: []MemoryNode{{
			ID:         "memory-layer",
			Type:       "fact",
			Content:    "layer interaction",
			Timestamp:  now,
			Confidence: 0.8,
			Quality:    QualityHighSignal,
		}},
		Edges: []MemoryEdge{{From: "memory-layer", To: "decision:layer-collapse", Relation: "supports"}},
	}
	report := buildCompressionReport(st, ExecutionTrace{
		ID:          "trace-layer-collapse",
		Outcome:     "partial_success",
		CausalEdges: edges,
		Cost:        CostMetrics{LatencyMs: 150},
	}, SystemLearning{}, defaultControlPolicy(), now)
	if report.LayerCollapse.Mode != "v6_pre_layer_collapse_analyzer" || report.LayerCollapse.RuntimeInfluence || !report.LayerCollapse.CacheSafe {
		t.Fatalf("layer collapse analyzer changed runtime/cache contract: %+v", report.LayerCollapse)
	}
	if report.LayerCollapse.SemanticSaturationBand != "high" || report.LayerCollapse.LayerCount < 8 {
		t.Fatalf("semantic saturation was not detected: %+v", report.LayerCollapse)
	}
	for _, signal := range []string{"control_equilibrium_overlap", "prediction_counterfactual_overlap", "logical_physical_time_overlap"} {
		if !containsString(report.LayerCollapse.OverlapSignals, signal) {
			t.Fatalf("missing overlap signal %q: %+v", signal, report.LayerCollapse)
		}
	}
	if !containsString(report.LayerCollapse.SuggestedAbstractions, "evaluate_causal_field_model") ||
		!containsString(report.LayerCollapse.SuggestedAbstractions, "unify_control_equilibrium_prediction") {
		t.Fatalf("missing v6 abstraction suggestions: %+v", report.LayerCollapse)
	}
}

func TestLayerCollapseReportFlagsCounterfactualOverConstraint(t *testing.T) {
	policy := defaultControlPolicy()
	policy.ExplorationRatePercent = 0
	current := CausalSignalDynamics{
		OverRegularized:  true,
		AmplifiedSignals: []string{"supported_outcome"},
	}
	observer := observerLoopReport(nil, current, policy, 0)
	control := compressControlGraph(state{}, policy)
	collapse := layerCollapseReport(CausalGraphCompression{}, control, MemoryGraphCompression{}, CrossGraphAlignment{}, current, observer)
	if collapse.OverConstraintRisk != "high" {
		t.Fatalf("over-constraint risk = %q, want high: %+v", collapse.OverConstraintRisk, collapse)
	}
	if !containsString(collapse.SuggestedAbstractions, "tune_counterfactual_guard_gain") {
		t.Fatalf("missing counterfactual guard tuning suggestion: %+v", collapse)
	}
}

func TestLayerCollapseReportSurfacesDualClockComplexity(t *testing.T) {
	history := []CompressionReport{{
		TraceID: "slow-clock",
		ObserverLoop: ObserverLoopReport{
			TemporalVariance: TemporalVarianceReport{PhysicalLatencyMs: 1000},
		},
	}}
	observer := observerLoopReport(history, CausalSignalDynamics{}, defaultControlPolicy(), 100)
	collapse := layerCollapseReport(CausalGraphCompression{}, ControlGraphCompression{}, MemoryGraphCompression{}, CrossGraphAlignment{}, CausalSignalDynamics{}, observer)
	if collapse.TemporalComplexity != "high" {
		t.Fatalf("temporal complexity = %q, want high: %+v", collapse.TemporalComplexity, collapse)
	}
	if !containsString(collapse.SuggestedAbstractions, "fold_dual_clock_into_causal_time") {
		t.Fatalf("missing causal time simplification suggestion: %+v", collapse)
	}
}
