package memorycompiler

import (
	"math"
	"sort"
	"strings"
	"time"
)

const (
	maxCompressedCausalAnchors = 12
	maxCompressionReports      = 30
	maxMemoryGraphNodes        = 300
	maxMemoryGraphEdges        = 600
	maxCompressionStrings      = 10
	maxLongTailCausalAnchors   = 3
	minCausalHierarchyGradient = 0.15
	maxGraphCouplingStrength   = 0.75
	highCausalEntropyThreshold = 0.85
	minObserverLagWindow       = 3
	defaultObserverLagWindow   = 6
	maxObserverLagWindow       = 10
	mediumOscillationWarning   = 0.35
	highOscillationWarning     = 0.55
	maxPredictionAdvisories    = 3
	maxLongTailSafetySignals   = 3
)

type CompressionReport struct {
	TraceID          string                  `json:"trace_id,omitempty"`
	Version          string                  `json:"version,omitempty"`
	CausalGraph      CausalGraphCompression  `json:"causal_graph,omitempty"`
	ExecutionTrace   ExecutionCompression    `json:"execution_trace,omitempty"`
	ControlGraph     ControlGraphCompression `json:"control_graph,omitempty"`
	MemoryGraph      MemoryGraphCompression  `json:"memory_graph,omitempty"`
	Alignment        CrossGraphAlignment     `json:"alignment,omitempty"`
	BiasCorrection   CompressionBiasReport   `json:"bias_correction,omitempty"`
	Dynamics         CausalSignalDynamics    `json:"dynamics,omitempty"`
	ObserverLoop     ObserverLoopReport      `json:"observer_loop,omitempty"`
	LayerCollapse    LayerCollapseReport     `json:"layer_collapse,omitempty"`
	CompressionRatio float64                 `json:"compression_ratio,omitempty"`
	CreatedAt        time.Time               `json:"created_at,omitempty"`
}

type CausalGraphCompression struct {
	TotalEdges      int            `json:"total_edges,omitempty"`
	RetainedEdges   int            `json:"retained_edges,omitempty"`
	DroppedEdges    int            `json:"dropped_edges,omitempty"`
	RelationCounts  map[string]int `json:"relation_counts,omitempty"`
	AnchorEdges     []CausalEdge   `json:"anchor_edges,omitempty"`
	LongTailEdges   []CausalEdge   `json:"long_tail_edges,omitempty"`
	LongTailSignals []string       `json:"long_tail_signals,omitempty"`
}

type ExecutionCompression struct {
	Outcome     string   `json:"outcome,omitempty"`
	Strategy    string   `json:"strategy,omitempty"`
	StepCount   int      `json:"step_count,omitempty"`
	ToolCalls   int      `json:"tool_calls,omitempty"`
	ToolErrors  int      `json:"tool_errors,omitempty"`
	KeyFindings []string `json:"key_findings,omitempty"`
	CostBand    string   `json:"cost_band,omitempty"`
	LatencyBand string   `json:"latency_band,omitempty"`
}

type ControlGraphCompression struct {
	Mode               string   `json:"mode,omitempty"`
	Controller         string   `json:"controller,omitempty"`
	ReportsFolded      int      `json:"reports_folded,omitempty"`
	StabilityBand      string   `json:"stability_band,omitempty"`
	OscillationBand    string   `json:"oscillation_band,omitempty"`
	EquilibriumState   string   `json:"equilibrium_state,omitempty"`
	TopSignals         []string `json:"top_signals,omitempty"`
	EquilibriumActions []string `json:"equilibrium_actions,omitempty"`
}

type MemoryGraphCompression struct {
	NodesFolded    int            `json:"nodes_folded,omitempty"`
	EdgesFolded    int            `json:"edges_folded,omitempty"`
	QualityCounts  map[string]int `json:"quality_counts,omitempty"`
	RelationCounts map[string]int `json:"relation_counts,omitempty"`
	AnchorNodes    []string       `json:"anchor_nodes,omitempty"`
	ConflictCount  int            `json:"conflict_count,omitempty"`
	NoiseCount     int            `json:"noise_count,omitempty"`
	TruthLockDecay []string       `json:"truth_lock_decay,omitempty"`
}

type CrossGraphAlignment struct {
	Status              string   `json:"status,omitempty"`
	AbstractionLevel    string   `json:"abstraction_level,omitempty"`
	SharedRelations     []string `json:"shared_relations,omitempty"`
	MissingFromMemory   []string `json:"missing_from_memory,omitempty"`
	MissingFromCausal   []string `json:"missing_from_causal,omitempty"`
	RawCouplingStrength float64  `json:"raw_coupling_strength,omitempty"`
	CouplingStrength    float64  `json:"coupling_strength,omitempty"`
	IndependenceStatus  string   `json:"independence_status,omitempty"`
	CouplingCapped      bool     `json:"coupling_capped,omitempty"`
}

type CompressionBiasReport struct {
	AnchorBudget      int      `json:"anchor_budget,omitempty"`
	LongTailRetained  int      `json:"long_tail_retained,omitempty"`
	LongTailRelations []string `json:"long_tail_relations,omitempty"`
	TruthLocksDecayed int      `json:"truth_locks_decayed,omitempty"`
	AlignmentStatus   string   `json:"alignment_status,omitempty"`
}

type CausalSignalDynamics struct {
	HierarchyGradient float64  `json:"hierarchy_gradient,omitempty"`
	SignalEntropy     float64  `json:"signal_entropy,omitempty"`
	EntropyBand       string   `json:"entropy_band,omitempty"`
	AmplitudeBand     string   `json:"amplitude_band,omitempty"`
	AmplifiedSignals  []string `json:"amplified_signals,omitempty"`
	EntropySpikes     []string `json:"entropy_spikes,omitempty"`
	CouplingStrength  float64  `json:"coupling_strength,omitempty"`
	Independence      string   `json:"independence,omitempty"`
	OverRegularized   bool     `json:"over_regularized,omitempty"`
}

type ObserverLoopReport struct {
	Timeline             string                  `json:"timeline,omitempty"`
	ReadOnlyProjection   bool                    `json:"read_only_projection,omitempty"`
	CurrentTraceExcluded bool                    `json:"current_trace_excluded,omitempty"`
	LaggedSamples        int                     `json:"lagged_samples,omitempty"`
	LagWindow            AdaptiveLagWindow       `json:"lag_window,omitempty"`
	ShadowObserver       ShadowObserverReport    `json:"shadow_observer,omitempty"`
	TemporalSync         TemporalSyncReport      `json:"temporal_sync,omitempty"`
	SignalBacklog        PredictiveSignalBacklog `json:"signal_backlog,omitempty"`
	AdvisoryBridge       PredictionActionBridge  `json:"advisory_bridge,omitempty"`
	PredictionBias       PredictionBiasGuard     `json:"prediction_bias,omitempty"`
	TemporalVariance     TemporalVarianceReport  `json:"temporal_variance,omitempty"`
	LongTailSafety       LongTailSafetyReport    `json:"long_tail_safety,omitempty"`
	FeedbackEligible     bool                    `json:"feedback_eligible,omitempty"`
	FeedbackSignals      []string                `json:"feedback_signals,omitempty"`
	Damping              GlobalDampingEnvelope   `json:"damping,omitempty"`
}

type AdaptiveLagWindow struct {
	Size            int    `json:"size,omitempty"`
	Basis           string `json:"basis,omitempty"`
	StabilityBand   string `json:"stability_band,omitempty"`
	OscillationBand string `json:"oscillation_band,omitempty"`
}

type ShadowObserverReport struct {
	Mode                         string   `json:"mode,omitempty"`
	CurrentTraceObserved         bool     `json:"current_trace_observed,omitempty"`
	AffectsExecution             bool     `json:"affects_execution"`
	PredictedOscillationIndex    float64  `json:"predicted_oscillation_index,omitempty"`
	PredictionHorizon            int      `json:"prediction_horizon,omitempty"`
	WarningLevel                 string   `json:"warning_level,omitempty"`
	ObservationOnlySignals       []string `json:"observation_only_signals,omitempty"`
	FeedbackSignalsSuppressed    bool     `json:"feedback_signals_suppressed,omitempty"`
	ExecutionInfluenceSuppressed bool     `json:"execution_influence_suppressed"`
}

type TemporalSyncReport struct {
	Clock            string  `json:"clock,omitempty"`
	LagWindow        int     `json:"lag_window,omitempty"`
	DampingWindow    int     `json:"damping_window,omitempty"`
	NormalizedWindow int     `json:"normalized_window,omitempty"`
	DesyncIndex      float64 `json:"desync_index,omitempty"`
	Status           string  `json:"status,omitempty"`
}

type PredictiveSignalBacklog struct {
	Mode           string   `json:"mode,omitempty"`
	MaxSignals     int      `json:"max_signals,omitempty"`
	PendingSignals []string `json:"pending_signals,omitempty"`
	StaleSignals   []string `json:"stale_signals,omitempty"`
	PendingCount   int      `json:"pending_count,omitempty"`
	StaleCount     int      `json:"stale_count,omitempty"`
}

type PredictionActionBridge struct {
	Mode                      string   `json:"mode,omitempty"`
	AdvisoryEligible          bool     `json:"advisory_eligible,omitempty"`
	AdvisorySignals           []string `json:"advisory_signals,omitempty"`
	MaxAdvisories             int      `json:"max_advisories,omitempty"`
	RequiresExplicitPromotion bool     `json:"requires_explicit_promotion"`
	AffectsExecution          bool     `json:"affects_execution"`
	FeedbackBypassBlocked     bool     `json:"feedback_bypass_blocked"`
	BacklogResolved           bool     `json:"backlog_resolved,omitempty"`
}

type PredictionBiasGuard struct {
	Mode                       string   `json:"mode,omitempty"`
	CounterfactualChecks       []string `json:"counterfactual_checks,omitempty"`
	DriftRisk                  string   `json:"drift_risk,omitempty"`
	PlanningDriftBlocked       bool     `json:"planning_drift_blocked"`
	ExplorationPreserved       bool     `json:"exploration_preserved"`
	AdvisoryNeutralityEnforced bool     `json:"advisory_neutrality_enforced"`
}

type TemporalVarianceReport struct {
	Mode              string  `json:"mode,omitempty"`
	LogicalClock      string  `json:"logical_clock,omitempty"`
	PhysicalClock     string  `json:"physical_clock,omitempty"`
	PhysicalLatencyMs int64   `json:"physical_latency_ms,omitempty"`
	JitterIndex       float64 `json:"jitter_index,omitempty"`
	VarianceBand      string  `json:"variance_band,omitempty"`
	Normalization     string  `json:"normalization,omitempty"`
	VarianceVisible   bool    `json:"variance_visible"`
}

type LongTailSafetyReport struct {
	Mode              string   `json:"mode,omitempty"`
	RetentionFloor    int      `json:"retention_floor,omitempty"`
	RetainedSignals   []string `json:"retained_signals,omitempty"`
	DecayedSignals    []string `json:"decayed_signals,omitempty"`
	ProtectedSignals  []string `json:"protected_signals,omitempty"`
	RareSignalCount   int      `json:"rare_signal_count,omitempty"`
	LongTailPreserved bool     `json:"long_tail_preserved"`
}

type LayerCollapseReport struct {
	Mode                   string   `json:"mode,omitempty"`
	LayerCount             int      `json:"layer_count,omitempty"`
	ActiveLayers           []string `json:"active_layers,omitempty"`
	SemanticSaturationBand string   `json:"semantic_saturation_band,omitempty"`
	OverlapSignals         []string `json:"overlap_signals,omitempty"`
	OverConstraintRisk     string   `json:"over_constraint_risk,omitempty"`
	TemporalComplexity     string   `json:"temporal_complexity,omitempty"`
	SuggestedAbstractions  []string `json:"suggested_abstractions,omitempty"`
	RuntimeInfluence       bool     `json:"runtime_influence"`
	CacheSafe              bool     `json:"cache_safe"`
}

type GlobalDampingEnvelope struct {
	State             string   `json:"state,omitempty"`
	Factor            float64  `json:"factor,omitempty"`
	OscillationIndex  float64  `json:"oscillation_index,omitempty"`
	SuppressedSignals []string `json:"suppressed_signals,omitempty"`
}

func applyCausalCompression(st state, tr ExecutionTrace, learning SystemLearning, policy ControlPolicy, now time.Time) (state, ExecutionTrace) {
	report := buildCompressionReport(st, tr, learning, policy, now)
	tr.Compression = &report
	st.CompressionReports = appendCompressionReport(st.CompressionReports, report)
	st.Nodes = retainMemoryNodes(st.Nodes, maxMemoryGraphNodes, now)
	st.Edges = retainMemoryEdges(st.Edges, maxMemoryGraphEdges)
	return st, tr
}

func buildCompressionReport(st state, tr ExecutionTrace, learning SystemLearning, policy ControlPolicy, now time.Time) CompressionReport {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	causal := compressCausalEdges(tr.CausalEdges, maxCompressedCausalAnchors)
	execution := compressExecutionTrace(tr, learning)
	control := compressControlGraph(st, policy)
	memory := compressMemoryGraph(st, now)
	alignment := crossGraphAlignment(causal, memory)
	dynamics := causalSignalDynamics(causal, alignment)
	observer := observerLoopReport(st.CompressionReports, dynamics, policy, executionLatencyMs(tr))
	layerCollapse := layerCollapseReport(causal, control, memory, alignment, dynamics, observer)
	bias := CompressionBiasReport{
		AnchorBudget:      maxCompressedCausalAnchors,
		LongTailRetained:  len(causal.LongTailEdges),
		LongTailRelations: append([]string(nil), causal.LongTailSignals...),
		TruthLocksDecayed: len(memory.TruthLockDecay),
		AlignmentStatus:   alignment.Status,
	}
	total := causal.TotalEdges + len(tr.ToolResults) + len(st.Nodes) + len(st.Edges)
	retained := causal.RetainedEdges + len(memory.AnchorNodes) + len(control.TopSignals) + len(execution.KeyFindings)
	ratio := 1.0
	if total > 0 {
		ratio = roundScore(float64(retained) / float64(total))
	}
	return CompressionReport{
		TraceID:          tr.ID,
		Version:          version,
		CausalGraph:      causal,
		ExecutionTrace:   execution,
		ControlGraph:     control,
		MemoryGraph:      memory,
		Alignment:        alignment,
		BiasCorrection:   bias,
		Dynamics:         dynamics,
		ObserverLoop:     observer,
		LayerCollapse:    layerCollapse,
		CompressionRatio: ratio,
		CreatedAt:        now.UTC(),
	}
}

func compressCausalEdges(edges []CausalEdge, limit int) CausalGraphCompression {
	if limit <= 0 {
		limit = maxCompressedCausalAnchors
	}
	out := CausalGraphCompression{
		TotalEdges:     len(edges),
		RelationCounts: map[string]int{},
	}
	for _, edge := range edges {
		relation := strings.TrimSpace(edge.Relation)
		if relation == "" {
			relation = "unknown"
		}
		out.RelationCounts[relation]++
	}
	candidates := append([]CausalEdge(nil), edges...)
	sortCausalAnchors(candidates)
	candidates = dedupeCausalEdges(candidates)
	anchors, longTail := selectCausalAnchors(candidates, out.RelationCounts, limit)
	out.LongTailEdges = append([]CausalEdge(nil), longTail...)
	for _, edge := range longTail {
		out.LongTailSignals = append(out.LongTailSignals, edge.Relation)
	}
	out.LongTailSignals = limitStrings(canonicalStrings(out.LongTailSignals), maxCompressionStrings)
	out.AnchorEdges = anchors
	out.RetainedEdges = len(out.AnchorEdges)
	if out.TotalEdges > out.RetainedEdges {
		out.DroppedEdges = out.TotalEdges - out.RetainedEdges
	}
	return out
}

func sortCausalAnchors(edges []CausalEdge) {
	sort.SliceStable(edges, func(i, j int) bool {
		pi := causalEdgePriority(edges[i])
		pj := causalEdgePriority(edges[j])
		if pi != pj {
			return pi < pj
		}
		return causalEdgeKey(edges[i]) < causalEdgeKey(edges[j])
	})
}

func selectCausalAnchors(candidates []CausalEdge, relationCounts map[string]int, limit int) ([]CausalEdge, []CausalEdge) {
	if len(candidates) <= limit {
		return append([]CausalEdge(nil), candidates...), nil
	}
	selected := append([]CausalEdge(nil), candidates[:limit]...)
	longTail := longTailCausalCandidates(candidates, relationCounts, selected, limit)
	if len(longTail) == 0 {
		return selected, nil
	}
	seen := map[string]bool{}
	for _, edge := range selected {
		seen[causalEdgeKey(edge)] = true
	}
	uniqueLongTail := make([]CausalEdge, 0, len(longTail))
	for _, edge := range longTail {
		key := causalEdgeKey(edge)
		if seen[key] {
			continue
		}
		uniqueLongTail = append(uniqueLongTail, edge)
		seen[key] = true
	}
	if len(uniqueLongTail) == 0 {
		return selected, nil
	}
	if len(uniqueLongTail) > len(selected) {
		uniqueLongTail = uniqueLongTail[:len(selected)]
	}
	selected = selected[:limit-len(uniqueLongTail)]
	selected = append(selected, uniqueLongTail...)
	sortCausalAnchors(selected)
	return selected, uniqueLongTail
}

func longTailCausalCandidates(candidates []CausalEdge, relationCounts map[string]int, selected []CausalEdge, limit int) []CausalEdge {
	if limit <= 2 || len(relationCounts) <= 1 {
		return nil
	}
	covered := map[string]bool{}
	for _, edge := range selected {
		covered[edge.Relation] = true
	}
	out := make([]CausalEdge, 0, maxLongTailCausalAnchors)
	longTailBudget := limit / 4
	if longTailBudget < 1 {
		longTailBudget = 1
	}
	if longTailBudget > maxLongTailCausalAnchors {
		longTailBudget = maxLongTailCausalAnchors
	}
	rare := append([]CausalEdge(nil), candidates...)
	sort.SliceStable(rare, func(i, j int) bool {
		ci := relationCounts[rare[i].Relation]
		cj := relationCounts[rare[j].Relation]
		if ci != cj {
			return ci < cj
		}
		pi := causalEdgePriority(rare[i])
		pj := causalEdgePriority(rare[j])
		if pi != pj {
			return pi < pj
		}
		return causalEdgeKey(rare[i]) < causalEdgeKey(rare[j])
	})
	seen := map[string]bool{}
	for _, edge := range rare {
		if covered[edge.Relation] {
			continue
		}
		key := causalEdgeKey(edge)
		if seen[key] {
			continue
		}
		out = append(out, edge)
		seen[key] = true
		if len(out) >= longTailBudget {
			break
		}
	}
	return out
}

func compressExecutionTrace(tr ExecutionTrace, learning SystemLearning) ExecutionCompression {
	findings := []string{}
	findings = append(findings, tr.FailureReason)
	findings = append(findings, tr.SemanticDriftHard...)
	findings = append(findings, learning.CausalFindings...)
	findings = append(findings, learning.CompilerImprovements...)
	return ExecutionCompression{
		Outcome:     tr.Outcome,
		Strategy:    firstNonEmpty(tr.StrategyUsed, classifyStrategy(tr.Goal)),
		StepCount:   len(tr.Steps),
		ToolCalls:   tr.Cost.ToolCalls,
		ToolErrors:  tr.Cost.ToolErrors,
		KeyFindings: limitStrings(canonicalStrings(findings), maxCompressionStrings),
		CostBand:    tokenBand(tr.Cost.EstimatedInputTokens + tr.Cost.EstimatedCompiledTokens),
		LatencyBand: latencyBand(tr.Cost.LatencyMs),
	}
}

func executionLatencyMs(tr ExecutionTrace) int64 {
	if tr.Cost.LatencyMs > 0 {
		return tr.Cost.LatencyMs
	}
	if !tr.StartedAt.IsZero() && !tr.CompletedAt.IsZero() && tr.CompletedAt.After(tr.StartedAt) {
		return tr.CompletedAt.Sub(tr.StartedAt).Milliseconds()
	}
	return 0
}

func compressControlGraph(st state, policy ControlPolicy) ControlGraphCompression {
	signals := []string{}
	signals = append(signals, policy.Reasons...)
	signals = append(signals, policy.SemanticShift...)
	return ControlGraphCompression{
		Mode:               policy.Mode,
		Controller:         policy.Controller,
		ReportsFolded:      0,
		StabilityBand:      scoreBand(policy.SystemStabilityScore),
		OscillationBand:    scoreBand(policy.OscillationIndex),
		EquilibriumState:   policy.EquilibriumState,
		TopSignals:         limitStrings(canonicalStrings(signals), maxCompressionStrings),
		EquilibriumActions: limitStrings(canonicalStrings(policy.EquilibriumActions), maxCompressionStrings),
	}
}

func compressMemoryGraph(st state, now time.Time) MemoryGraphCompression {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := MemoryGraphCompression{
		NodesFolded:    len(st.Nodes),
		EdgesFolded:    len(st.Edges),
		QualityCounts:  map[string]int{},
		RelationCounts: map[string]int{},
	}
	for _, node := range st.Nodes {
		quality := string(node.Quality)
		if quality == "" {
			quality = "UNKNOWN"
		}
		out.QualityCounts[quality]++
		if node.Quality == QualityNoise || node.Quality == QualityCorrupted {
			out.NoiseCount++
		}
		if node.TruthLocked && truthLockedImportance(node, now) < 0.5 {
			out.TruthLockDecay = append(out.TruthLockDecay, node.ID)
		}
	}
	for _, edge := range st.Edges {
		relation := strings.TrimSpace(edge.Relation)
		if relation == "" {
			relation = "unknown"
		}
		out.RelationCounts[relation]++
		if relation == "contradicts" {
			out.ConflictCount++
		}
	}
	nodes := append([]MemoryNode(nil), st.Nodes...)
	sort.SliceStable(nodes, func(i, j int) bool {
		pi := memoryNodeCompressionPriority(nodes[i], now)
		pj := memoryNodeCompressionPriority(nodes[j], now)
		if pi != pj {
			return pi < pj
		}
		if nodes[i].Confidence != nodes[j].Confidence {
			return nodes[i].Confidence > nodes[j].Confidence
		}
		if !nodes[i].Timestamp.Equal(nodes[j].Timestamp) {
			return nodes[i].Timestamp.After(nodes[j].Timestamp)
		}
		return nodes[i].ID < nodes[j].ID
	})
	out.TruthLockDecay = limitStrings(canonicalStrings(out.TruthLockDecay), maxCompressionStrings)
	anchors := []string{}
	for _, node := range nodes {
		if strings.TrimSpace(node.ID) == "" {
			continue
		}
		anchors = append(anchors, node.ID)
		if len(anchors) >= maxCompressionStrings {
			break
		}
	}
	out.AnchorNodes = anchors
	return out
}

func crossGraphAlignment(causal CausalGraphCompression, memory MemoryGraphCompression) CrossGraphAlignment {
	causalRelations := normalizedCausalRelations(causal.RelationCounts)
	memoryRelations := normalizedRelations(memory.RelationCounts)
	shared := intersectRelationKeys(causalRelations, memoryRelations)
	missingFromMemory := relationKeysMissing(causalRelations, memoryRelations)
	missingFromCausal := relationKeysMissing(memoryRelations, causalRelations)
	rawCoupling := relationCouplingStrength(causalRelations, memoryRelations)
	coupling := rawCoupling
	capped := false
	if coupling > maxGraphCouplingStrength {
		coupling = maxGraphCouplingStrength
		capped = true
	}
	status := "aligned"
	switch {
	case len(causalRelations) == 0 && len(memoryRelations) == 0:
		status = "empty"
	case len(shared) == 0:
		status = "divergent"
	case len(missingFromMemory) > 0 || len(missingFromCausal) > 0:
		status = "partial"
	}
	level := "shared_lattice"
	switch status {
	case "divergent":
		level = "separate_lattices"
	case "partial":
		level = "mixed_lattice"
	}
	independence := graphIndependenceStatus(rawCoupling, len(causalRelations), len(memoryRelations))
	return CrossGraphAlignment{
		Status:              status,
		AbstractionLevel:    level,
		SharedRelations:     limitStrings(canonicalStrings(shared), maxCompressionStrings),
		MissingFromMemory:   limitStrings(canonicalStrings(missingFromMemory), maxCompressionStrings),
		MissingFromCausal:   limitStrings(canonicalStrings(missingFromCausal), maxCompressionStrings),
		RawCouplingStrength: rawCoupling,
		CouplingStrength:    coupling,
		IndependenceStatus:  independence,
		CouplingCapped:      capped,
	}
}

func appendCompressionReport(existing []CompressionReport, report CompressionReport) []CompressionReport {
	if strings.TrimSpace(report.TraceID) != "" {
		for _, existingReport := range existing {
			if existingReport.TraceID == report.TraceID {
				return existing
			}
		}
	}
	existing = append(existing, report)
	if len(existing) > maxCompressionReports {
		existing = existing[len(existing)-maxCompressionReports:]
	}
	return existing
}

func retainMemoryNodes(nodes []MemoryNode, limit int, nowArg ...time.Time) []MemoryNode {
	if limit <= 0 || len(nodes) <= limit {
		return nodes
	}
	now := time.Now().UTC()
	if len(nowArg) > 0 && !nowArg[0].IsZero() {
		now = nowArg[0].UTC()
	}
	out := append([]MemoryNode(nil), nodes...)
	sort.SliceStable(out, func(i, j int) bool {
		pi := memoryNodeCompressionPriority(out[i], now)
		pj := memoryNodeCompressionPriority(out[j], now)
		if pi != pj {
			return pi < pj
		}
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		if !out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Timestamp.After(out[j].Timestamp)
		}
		return out[i].ID < out[j].ID
	})
	out = out[:limit]
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].ID < out[j].ID
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

func retainMemoryEdges(edges []MemoryEdge, limit int) []MemoryEdge {
	if limit <= 0 || len(edges) <= limit {
		return edges
	}
	out := append([]MemoryEdge(nil), edges...)
	sort.SliceStable(out, func(i, j int) bool {
		pi := memoryEdgePriority(out[i])
		pj := memoryEdgePriority(out[j])
		if pi != pj {
			return pi < pj
		}
		return memoryEdgeKey(out[i]) < memoryEdgeKey(out[j])
	})
	return out[:limit]
}

func cloneCompressionReport(in *CompressionReport) *CompressionReport {
	if in == nil {
		return nil
	}
	out := *in
	out.CausalGraph.RelationCounts = cloneStringIntMap(in.CausalGraph.RelationCounts)
	out.CausalGraph.AnchorEdges = append([]CausalEdge(nil), in.CausalGraph.AnchorEdges...)
	out.CausalGraph.LongTailEdges = append([]CausalEdge(nil), in.CausalGraph.LongTailEdges...)
	out.CausalGraph.LongTailSignals = append([]string(nil), in.CausalGraph.LongTailSignals...)
	out.ExecutionTrace.KeyFindings = append([]string(nil), in.ExecutionTrace.KeyFindings...)
	out.ControlGraph.TopSignals = append([]string(nil), in.ControlGraph.TopSignals...)
	out.ControlGraph.EquilibriumActions = append([]string(nil), in.ControlGraph.EquilibriumActions...)
	out.MemoryGraph.QualityCounts = cloneStringIntMap(in.MemoryGraph.QualityCounts)
	out.MemoryGraph.RelationCounts = cloneStringIntMap(in.MemoryGraph.RelationCounts)
	out.MemoryGraph.AnchorNodes = append([]string(nil), in.MemoryGraph.AnchorNodes...)
	out.MemoryGraph.TruthLockDecay = append([]string(nil), in.MemoryGraph.TruthLockDecay...)
	out.Alignment.SharedRelations = append([]string(nil), in.Alignment.SharedRelations...)
	out.Alignment.MissingFromMemory = append([]string(nil), in.Alignment.MissingFromMemory...)
	out.Alignment.MissingFromCausal = append([]string(nil), in.Alignment.MissingFromCausal...)
	out.BiasCorrection.LongTailRelations = append([]string(nil), in.BiasCorrection.LongTailRelations...)
	out.Dynamics.AmplifiedSignals = append([]string(nil), in.Dynamics.AmplifiedSignals...)
	out.Dynamics.EntropySpikes = append([]string(nil), in.Dynamics.EntropySpikes...)
	out.ObserverLoop.FeedbackSignals = append([]string(nil), in.ObserverLoop.FeedbackSignals...)
	out.ObserverLoop.ShadowObserver.ObservationOnlySignals = append([]string(nil), in.ObserverLoop.ShadowObserver.ObservationOnlySignals...)
	out.ObserverLoop.SignalBacklog.PendingSignals = append([]string(nil), in.ObserverLoop.SignalBacklog.PendingSignals...)
	out.ObserverLoop.SignalBacklog.StaleSignals = append([]string(nil), in.ObserverLoop.SignalBacklog.StaleSignals...)
	out.ObserverLoop.AdvisoryBridge.AdvisorySignals = append([]string(nil), in.ObserverLoop.AdvisoryBridge.AdvisorySignals...)
	out.ObserverLoop.PredictionBias.CounterfactualChecks = append([]string(nil), in.ObserverLoop.PredictionBias.CounterfactualChecks...)
	out.ObserverLoop.LongTailSafety.RetainedSignals = append([]string(nil), in.ObserverLoop.LongTailSafety.RetainedSignals...)
	out.ObserverLoop.LongTailSafety.DecayedSignals = append([]string(nil), in.ObserverLoop.LongTailSafety.DecayedSignals...)
	out.ObserverLoop.LongTailSafety.ProtectedSignals = append([]string(nil), in.ObserverLoop.LongTailSafety.ProtectedSignals...)
	out.ObserverLoop.Damping.SuppressedSignals = append([]string(nil), in.ObserverLoop.Damping.SuppressedSignals...)
	out.LayerCollapse.ActiveLayers = append([]string(nil), in.LayerCollapse.ActiveLayers...)
	out.LayerCollapse.OverlapSignals = append([]string(nil), in.LayerCollapse.OverlapSignals...)
	out.LayerCollapse.SuggestedAbstractions = append([]string(nil), in.LayerCollapse.SuggestedAbstractions...)
	return &out
}

func cloneStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func dedupeCausalEdges(edges []CausalEdge) []CausalEdge {
	seen := map[string]bool{}
	out := edges[:0]
	for _, edge := range edges {
		key := causalEdgeKey(edge)
		if key == "\x00\x00" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, edge)
	}
	return out
}

func causalEdgePriority(edge CausalEdge) int {
	switch {
	case edge.Relation == "explains_divergence":
		return 0
	case strings.HasPrefix(edge.Relation, "selected_strategy:"):
		return 1
	case edge.Relation == "weakened_outcome":
		return 2
	case edge.Relation == "supported_outcome":
		return 3
	case edge.Relation == "constrained":
		return 4
	case edge.Relation == "influenced":
		return 5
	default:
		return 9
	}
}

func causalEdgeKey(edge CausalEdge) string {
	return strings.TrimSpace(edge.Relation) + "\x00" + strings.TrimSpace(edge.From) + "\x00" + strings.TrimSpace(edge.To)
}

func memoryNodePriority(node MemoryNode) int {
	switch {
	case node.TruthLocked:
		return 0
	case node.Quality == QualityHighSignal:
		return 1
	case node.Type == "decision":
		return 2
	case node.Quality == QualityMediumSignal:
		return 3
	case node.Quality == QualityNoise:
		return 8
	case node.Quality == QualityCorrupted:
		return 9
	default:
		return 5
	}
}

func memoryNodeCompressionPriority(node MemoryNode, now time.Time) int {
	if !node.TruthLocked {
		return memoryNodePriority(node)
	}
	importance := truthLockedImportance(node, now)
	switch {
	case importance >= 0.75:
		return 0
	case importance >= 0.5:
		return 2
	default:
		return 4
	}
}

func truthLockedImportance(node MemoryNode, now time.Time) float64 {
	confidence := node.Confidence
	if confidence <= 0 {
		confidence = 1
	}
	if confidence > 1 {
		confidence = 1
	}
	ageDays := 0.0
	if !node.Timestamp.IsZero() && !now.IsZero() && now.After(node.Timestamp) {
		ageDays = now.Sub(node.Timestamp).Hours() / 24
	}
	weight := confidence * math.Exp(-ageDays/45)
	if node.Quality == QualityHighSignal {
		weight += 0.2
	}
	if node.Type == "tool_result" {
		weight += 0.1
	}
	if weight > 1 {
		weight = 1
	}
	return roundScore(weight)
}

func normalizedCausalRelations(counts map[string]int) map[string]bool {
	out := map[string]bool{}
	for relation, count := range counts {
		if count <= 0 {
			continue
		}
		if normalized := graphRelation(relation); normalized != "" {
			out[normalized] = true
		}
	}
	return out
}

func normalizedRelations(counts map[string]int) map[string]bool {
	out := map[string]bool{}
	for relation, count := range counts {
		if count <= 0 {
			continue
		}
		relation = strings.TrimSpace(relation)
		if relation != "" {
			out[relation] = true
		}
	}
	return out
}

func intersectRelationKeys(left, right map[string]bool) []string {
	out := []string{}
	for key := range left {
		if right[key] {
			out = append(out, key)
		}
	}
	return out
}

func relationKeysMissing(source, target map[string]bool) []string {
	out := []string{}
	for key := range source {
		if !target[key] {
			out = append(out, key)
		}
	}
	return out
}

func causalSignalDynamics(causal CausalGraphCompression, alignment CrossGraphAlignment) CausalSignalDynamics {
	gradient := causalHierarchyGradient(causal.RelationCounts)
	entropy := causalSignalEntropy(causal.RelationCounts)
	dynamics := CausalSignalDynamics{
		HierarchyGradient: gradient,
		SignalEntropy:     entropy,
		EntropyBand:       entropyBand(entropy),
		AmplitudeBand:     amplitudeBand(gradient),
		CouplingStrength:  alignment.CouplingStrength,
		Independence:      alignment.IndependenceStatus,
	}
	if entropy >= highCausalEntropyThreshold && gradient < minCausalHierarchyGradient {
		dynamics.OverRegularized = true
		dynamics.AmplifiedSignals = amplifiedCausalSignals(causal)
		dynamics.EntropySpikes = entropySpikeSignals(causal)
	}
	return dynamics
}

func causalHierarchyGradient(counts map[string]int) float64 {
	values := relationCountValues(counts)
	if len(values) == 0 {
		return 0
	}
	if len(values) == 1 {
		return 1
	}
	sort.Sort(sort.Reverse(sort.IntSlice(values)))
	total := 0
	for _, count := range values {
		total += count
	}
	if total == 0 {
		return 0
	}
	top := float64(values[0]) / float64(total)
	second := float64(values[1]) / float64(total)
	return roundScore(top - second)
}

func causalSignalEntropy(counts map[string]int) float64 {
	values := relationCountValues(counts)
	if len(values) <= 1 {
		return 0
	}
	total := 0
	for _, count := range values {
		total += count
	}
	if total == 0 {
		return 0
	}
	entropy := 0.0
	for _, count := range values {
		p := float64(count) / float64(total)
		if p > 0 {
			entropy -= p * math.Log(p)
		}
	}
	return roundScore(entropy / math.Log(float64(len(values))))
}

func relationCountValues(counts map[string]int) []int {
	values := make([]int, 0, len(counts))
	for _, count := range counts {
		if count > 0 {
			values = append(values, count)
		}
	}
	return values
}

func amplifiedCausalSignals(causal CausalGraphCompression) []string {
	signals := []string{}
	anchors := append([]CausalEdge(nil), causal.AnchorEdges...)
	sortCausalAnchors(anchors)
	for _, edge := range anchors {
		if causalEdgePriority(edge) > 3 {
			continue
		}
		signals = append(signals, edge.Relation)
		if len(signals) >= 3 {
			break
		}
	}
	if len(signals) == 0 {
		signals = dominantCausalRelations(causal.RelationCounts, 3)
	}
	return limitStrings(canonicalStrings(signals), maxCompressionStrings)
}

func entropySpikeSignals(causal CausalGraphCompression) []string {
	signals := []string{}
	for _, edge := range causal.LongTailEdges {
		signals = append(signals, edge.Relation)
	}
	if len(signals) == 0 {
		signals = rareCausalRelations(causal.RelationCounts, 3)
	}
	return limitStrings(canonicalStrings(signals), maxCompressionStrings)
}

func dominantCausalRelations(counts map[string]int, limit int) []string {
	return sortedCausalRelations(counts, limit, func(left, right int) bool { return left > right })
}

func rareCausalRelations(counts map[string]int, limit int) []string {
	return sortedCausalRelations(counts, limit, func(left, right int) bool { return left < right })
}

func sortedCausalRelations(counts map[string]int, limit int, less func(left, right int) bool) []string {
	if limit <= 0 {
		return nil
	}
	type relationCount struct {
		relation string
		count    int
	}
	items := make([]relationCount, 0, len(counts))
	for relation, count := range counts {
		if strings.TrimSpace(relation) != "" && count > 0 {
			items = append(items, relationCount{relation: relation, count: count})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return less(items[i].count, items[j].count)
		}
		return items[i].relation < items[j].relation
	})
	out := make([]string, 0, limit)
	for _, item := range items {
		out = append(out, item.relation)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func relationCouplingStrength(left, right map[string]bool) float64 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	union := map[string]bool{}
	shared := 0
	for key := range left {
		union[key] = true
		if right[key] {
			shared++
		}
	}
	for key := range right {
		union[key] = true
	}
	if len(union) == 0 {
		return 0
	}
	return roundScore(float64(shared) / float64(len(union)))
}

func graphIndependenceStatus(coupling float64, causalRelations, memoryRelations int) string {
	switch {
	case causalRelations == 0 && memoryRelations == 0:
		return "empty"
	case coupling > maxGraphCouplingStrength:
		return "overcoupled"
	case coupling >= 0.5:
		return "coupled"
	case coupling > 0:
		return "partially_independent"
	default:
		return "independent"
	}
}

func entropyBand(entropy float64) string {
	switch {
	case entropy >= highCausalEntropyThreshold:
		return "high"
	case entropy >= 0.45:
		return "medium"
	case entropy > 0:
		return "low"
	default:
		return "none"
	}
}

func amplitudeBand(gradient float64) string {
	switch {
	case gradient >= 0.5:
		return "sharp"
	case gradient >= minCausalHierarchyGradient:
		return "balanced"
	case gradient >= 0:
		return "flat"
	default:
		return "none"
	}
}

func observerLoopReport(history []CompressionReport, current CausalSignalDynamics, policy ControlPolicy, physicalLatencyMs int64) ObserverLoopReport {
	lagWindow := adaptiveObserverLagWindow(policy)
	samples := laggedDynamicsSamples(history, lagWindow.Size)
	feedbackSignals := laggedFeedbackSignals(samples)
	shadow := shadowObserverReport(samples, current)
	dampingWindow := lagWindow.Size
	if dampingWindow < defaultObserverLagWindow {
		dampingWindow = defaultObserverLagWindow
	}
	damping := globalDampingEnvelope(laggedDynamicsSamples(history, dampingWindow), feedbackSignals)
	temporal := temporalSyncReport(lagWindow.Size, dampingWindow)
	backlog := predictiveSignalBacklog(history, shadow)
	bridge := predictionActionBridge(shadow, backlog)
	bias := predictionBiasGuard(bridge, policy)
	variance := temporalVarianceReport(history, temporal, physicalLatencyMs)
	longTail := longTailSafetyReport(backlog)
	report := ObserverLoopReport{
		Timeline:             "lagged",
		ReadOnlyProjection:   true,
		CurrentTraceExcluded: true,
		LaggedSamples:        len(samples),
		LagWindow:            lagWindow,
		ShadowObserver:       shadow,
		TemporalSync:         temporal,
		SignalBacklog:        backlog,
		AdvisoryBridge:       bridge,
		PredictionBias:       bias,
		TemporalVariance:     variance,
		LongTailSafety:       longTail,
		FeedbackSignals:      feedbackSignals,
		Damping:              damping,
	}
	report.FeedbackEligible = len(feedbackSignals) > 0 && damping.State != "damped"
	if damping.State == "damped" {
		report.FeedbackSignals = nil
	}
	return report
}

func adaptiveObserverLagWindow(policy ControlPolicy) AdaptiveLagWindow {
	stability := policy.SystemStabilityScore
	oscillation := policy.OscillationIndex
	size := defaultObserverLagWindow
	basis := "default"
	switch {
	case oscillation >= 0.5 || (stability > 0 && stability < 0.45):
		size = maxObserverLagWindow
		basis = "unstable"
	case stability >= 0.85 && oscillation < 0.2:
		size = minObserverLagWindow
		basis = "stable"
	case stability >= 0.65:
		size = defaultObserverLagWindow - 1
		basis = "balanced"
	case stability > 0:
		size = defaultObserverLagWindow + 2
		basis = "recovering"
	}
	return AdaptiveLagWindow{
		Size:            size,
		Basis:           basis,
		StabilityBand:   scoreBand(stability),
		OscillationBand: scoreBand(oscillation),
	}
}

func laggedDynamicsSamples(history []CompressionReport, limit int) []CausalSignalDynamics {
	if limit <= 0 {
		return nil
	}
	start := 0
	if len(history) > limit {
		start = len(history) - limit
	}
	out := make([]CausalSignalDynamics, 0, len(history)-start)
	for _, report := range history[start:] {
		out = append(out, report.Dynamics)
	}
	return out
}

func laggedFeedbackSignals(samples []CausalSignalDynamics) []string {
	if len(samples) == 0 {
		return nil
	}
	last := samples[len(samples)-1]
	signals := []string{}
	if last.OverRegularized {
		signals = append(signals, last.AmplifiedSignals...)
		signals = append(signals, last.EntropySpikes...)
	}
	return limitStrings(canonicalStrings(signals), maxCompressionStrings)
}

func shadowObserverReport(samples []CausalSignalDynamics, current CausalSignalDynamics) ShadowObserverReport {
	predicted := predictiveObserverOscillationIndex(samples, current)
	signals := []string{}
	if current.OverRegularized {
		signals = append(signals, current.AmplifiedSignals...)
		signals = append(signals, current.EntropySpikes...)
	}
	if predicted >= mediumOscillationWarning {
		signals = append(signals, "predicted_observer_oscillation")
	}
	level := "none"
	switch {
	case predicted >= highOscillationWarning:
		level = "high"
	case predicted >= mediumOscillationWarning:
		level = "medium"
	case current.OverRegularized:
		level = "low"
	}
	return ShadowObserverReport{
		Mode:                         "shadow_read_only",
		CurrentTraceObserved:         true,
		AffectsExecution:             false,
		PredictedOscillationIndex:    predicted,
		PredictionHorizon:            1,
		WarningLevel:                 level,
		ObservationOnlySignals:       limitStrings(canonicalStrings(signals), maxCompressionStrings),
		FeedbackSignalsSuppressed:    len(signals) > 0,
		ExecutionInfluenceSuppressed: true,
	}
}

func temporalSyncReport(lagWindow, dampingWindow int) TemporalSyncReport {
	if lagWindow <= 0 {
		lagWindow = defaultObserverLagWindow
	}
	if dampingWindow <= 0 {
		dampingWindow = defaultObserverLagWindow
	}
	normalized := lagWindow
	if dampingWindow > normalized {
		normalized = dampingWindow
	}
	desync := 0.0
	if normalized > 0 {
		desync = roundScore(math.Abs(float64(lagWindow-dampingWindow)) / float64(normalized))
	}
	status := "synchronized"
	switch {
	case desync > 0.5:
		status = "desynchronized"
	case desync > 0:
		status = "bounded_desync"
	}
	return TemporalSyncReport{
		Clock:            "causal_observation_window",
		LagWindow:        lagWindow,
		DampingWindow:    dampingWindow,
		NormalizedWindow: normalized,
		DesyncIndex:      desync,
		Status:           status,
	}
}

func predictiveSignalBacklog(history []CompressionReport, shadow ShadowObserverReport) PredictiveSignalBacklog {
	recentStart := 0
	if len(history) > maxObserverLagWindow {
		recentStart = len(history) - maxObserverLagWindow
	}
	recent := []string{}
	stale := []string{}
	for i, report := range history {
		signals := report.ObserverLoop.ShadowObserver.ObservationOnlySignals
		if i < recentStart {
			stale = append(stale, signals...)
			continue
		}
		recent = append(recent, signals...)
	}
	recent = append(recent, shadow.ObservationOnlySignals...)
	pending := prioritizedPredictiveSignals(recent, maxCompressionStrings)
	stale = stringsNotIn(prioritizedPredictiveSignals(stale, maxCompressionStrings), pending)
	return PredictiveSignalBacklog{
		Mode:           "bounded_decay",
		MaxSignals:     maxCompressionStrings,
		PendingSignals: pending,
		StaleSignals:   limitStrings(stale, maxCompressionStrings),
		PendingCount:   len(pending),
		StaleCount:     len(stale),
	}
}

func predictionActionBridge(shadow ShadowObserverReport, backlog PredictiveSignalBacklog) PredictionActionBridge {
	advisories := []string(nil)
	if shadow.WarningLevel != "" && shadow.WarningLevel != "none" {
		advisories = append(advisories, backlog.PendingSignals...)
	}
	advisories = prioritizedPredictiveSignals(advisories, maxPredictionAdvisories)
	return PredictionActionBridge{
		Mode:                      "bounded_advisory",
		AdvisoryEligible:          len(advisories) > 0,
		AdvisorySignals:           advisories,
		MaxAdvisories:             maxPredictionAdvisories,
		RequiresExplicitPromotion: true,
		AffectsExecution:          false,
		FeedbackBypassBlocked:     true,
		BacklogResolved:           backlog.StaleCount > 0,
	}
}

func predictionBiasGuard(bridge PredictionActionBridge, policy ControlPolicy) PredictionBiasGuard {
	checks := []string{
		"preserve_non_advisory_baseline",
		"block_implicit_execution_promotion",
	}
	if bridge.AdvisoryEligible {
		checks = append(checks, "compare_advisory_counterfactual")
	}
	driftRisk := "none"
	switch {
	case bridge.AdvisoryEligible && policy.ExplorationRatePercent <= 0:
		driftRisk = "medium"
	case bridge.AdvisoryEligible:
		driftRisk = "low"
	}
	return PredictionBiasGuard{
		Mode:                       "counterfactual_advisory_guard",
		CounterfactualChecks:       limitStrings(canonicalStrings(checks), maxCompressionStrings),
		DriftRisk:                  driftRisk,
		PlanningDriftBlocked:       bridge.FeedbackBypassBlocked && !bridge.AffectsExecution,
		ExplorationPreserved:       policy.ExplorationRatePercent > 0,
		AdvisoryNeutralityEnforced: !bridge.AffectsExecution,
	}
}

func temporalVarianceReport(history []CompressionReport, temporal TemporalSyncReport, physicalLatencyMs int64) TemporalVarianceReport {
	latencies := previousPhysicalLatencies(history, maxObserverLagWindow-1)
	if physicalLatencyMs > 0 {
		latencies = append(latencies, physicalLatencyMs)
	}
	jitter := physicalLatencyJitterIndex(latencies)
	return TemporalVarianceReport{
		Mode:              "dual_clock_visible",
		LogicalClock:      temporal.Clock,
		PhysicalClock:     "execution_latency",
		PhysicalLatencyMs: physicalLatencyMs,
		JitterIndex:       jitter,
		VarianceBand:      varianceBand(jitter),
		Normalization:     temporal.Status,
		VarianceVisible:   true,
	}
}

func previousPhysicalLatencies(history []CompressionReport, limit int) []int64 {
	if limit <= 0 || len(history) == 0 {
		return nil
	}
	start := 0
	if len(history) > limit {
		start = len(history) - limit
	}
	out := make([]int64, 0, len(history)-start)
	for _, report := range history[start:] {
		latency := report.ObserverLoop.TemporalVariance.PhysicalLatencyMs
		if latency > 0 {
			out = append(out, latency)
		}
	}
	return out
}

func physicalLatencyJitterIndex(latencies []int64) float64 {
	if len(latencies) < 2 {
		return 0
	}
	var minLatency, maxLatency int64
	for i, latency := range latencies {
		if i == 0 || latency < minLatency {
			minLatency = latency
		}
		if latency > maxLatency {
			maxLatency = latency
		}
	}
	if maxLatency <= 0 {
		return 0
	}
	return roundScore(float64(maxLatency-minLatency) / float64(maxLatency))
}

func varianceBand(jitter float64) string {
	switch {
	case jitter >= 0.6:
		return "high"
	case jitter >= 0.25:
		return "medium"
	case jitter > 0:
		return "low"
	default:
		return "none"
	}
}

func longTailSafetyReport(backlog PredictiveSignalBacklog) LongTailSafetyReport {
	protected := prioritizedPredictiveSignals(longTailSafetyCandidates(backlog.StaleSignals), maxLongTailSafetySignals)
	decayed := stringsNotIn(backlog.StaleSignals, protected)
	retained := append([]string(nil), backlog.PendingSignals...)
	retained = append(retained, protected...)
	retained = prioritizedPredictiveSignals(retained, maxCompressionStrings)
	return LongTailSafetyReport{
		Mode:              "non_uniform_decay",
		RetentionFloor:    maxLongTailSafetySignals,
		RetainedSignals:   retained,
		DecayedSignals:    limitStrings(decayed, maxCompressionStrings),
		ProtectedSignals:  protected,
		RareSignalCount:   len(protected),
		LongTailPreserved: len(protected) > 0,
	}
}

func longTailSafetyCandidates(signals []string) []string {
	out := []string{}
	for _, signal := range canonicalStrings(signals) {
		if signal == "" || signal == "predicted_observer_oscillation" {
			continue
		}
		out = append(out, signal)
	}
	return out
}

func layerCollapseReport(causal CausalGraphCompression, control ControlGraphCompression, memory MemoryGraphCompression, alignment CrossGraphAlignment, dynamics CausalSignalDynamics, observer ObserverLoopReport) LayerCollapseReport {
	layers := activeSemanticLayers(causal, control, memory, alignment, dynamics, observer)
	overlap := semanticOverlapSignals(causal, control, memory, alignment, dynamics, observer)
	overConstraint := overConstraintRisk(observer)
	temporal := temporalComplexity(observer)
	suggestions := layerCollapseSuggestions(layers, overlap, overConstraint, temporal)
	return LayerCollapseReport{
		Mode:                   "v6_pre_layer_collapse_analyzer",
		LayerCount:             len(layers),
		ActiveLayers:           layers,
		SemanticSaturationBand: semanticSaturationBand(len(layers), len(overlap)),
		OverlapSignals:         overlap,
		OverConstraintRisk:     overConstraint,
		TemporalComplexity:     temporal,
		SuggestedAbstractions:  suggestions,
		RuntimeInfluence:       false,
		CacheSafe:              true,
	}
}

func activeSemanticLayers(causal CausalGraphCompression, control ControlGraphCompression, memory MemoryGraphCompression, alignment CrossGraphAlignment, dynamics CausalSignalDynamics, observer ObserverLoopReport) []string {
	layers := []string{}
	if causal.TotalEdges > 0 || causal.RetainedEdges > 0 {
		layers = append(layers, "causal")
	}
	if control.Controller != "" || control.Mode != "" || len(control.TopSignals) > 0 {
		layers = append(layers, "control")
	}
	if control.EquilibriumState != "" || len(control.EquilibriumActions) > 0 {
		layers = append(layers, "equilibrium")
	}
	if memory.NodesFolded > 0 || memory.EdgesFolded > 0 || len(memory.AnchorNodes) > 0 {
		layers = append(layers, "memory")
	}
	if alignment.Status != "" || alignment.AbstractionLevel != "" {
		layers = append(layers, "alignment")
	}
	if dynamics.EntropyBand != "" || dynamics.AmplitudeBand != "" || dynamics.OverRegularized {
		layers = append(layers, "compression_dynamics")
	}
	if observer.ShadowObserver.Mode != "" || observer.AdvisoryBridge.Mode != "" || len(observer.SignalBacklog.PendingSignals) > 0 {
		layers = append(layers, "prediction")
	}
	if observer.PredictionBias.Mode != "" || len(observer.PredictionBias.CounterfactualChecks) > 0 {
		layers = append(layers, "counterfactual")
	}
	if observer.TemporalSync.Clock != "" || observer.TemporalVariance.Mode != "" {
		layers = append(layers, "temporal")
	}
	if observer.LongTailSafety.Mode != "" || len(observer.Damping.SuppressedSignals) > 0 {
		layers = append(layers, "safety")
	}
	return limitStrings(canonicalStrings(layers), maxCompressionStrings)
}

func semanticOverlapSignals(causal CausalGraphCompression, control ControlGraphCompression, memory MemoryGraphCompression, alignment CrossGraphAlignment, dynamics CausalSignalDynamics, observer ObserverLoopReport) []string {
	signals := []string{}
	if control.Controller != "" && (control.EquilibriumState != "" || len(control.EquilibriumActions) > 0) {
		signals = append(signals, "control_equilibrium_overlap")
	}
	if observer.AdvisoryBridge.AdvisoryEligible && observer.PredictionBias.Mode != "" {
		signals = append(signals, "prediction_counterfactual_overlap")
	}
	if observer.TemporalSync.Clock != "" && observer.TemporalVariance.Mode != "" {
		signals = append(signals, "logical_physical_time_overlap")
	}
	if alignment.Status != "" && dynamics.CouplingStrength > 0 {
		signals = append(signals, "alignment_dynamics_overlap")
	}
	if causal.TotalEdges > 0 && len(memory.AnchorNodes) > 0 && alignment.Status != "empty" {
		signals = append(signals, "memory_causal_alignment_overlap")
	}
	if observer.LongTailSafety.LongTailPreserved && observer.AdvisoryBridge.AdvisoryEligible {
		signals = append(signals, "prediction_safety_overlap")
	}
	if memory.ConflictCount > 0 && control.OscillationBand != "none" {
		signals = append(signals, "memory_control_stability_overlap")
	}
	return limitStrings(canonicalStrings(signals), maxCompressionStrings)
}

func overConstraintRisk(observer ObserverLoopReport) string {
	switch {
	case observer.AdvisoryBridge.AdvisoryEligible && !observer.PredictionBias.ExplorationPreserved:
		return "high"
	case observer.AdvisoryBridge.AdvisoryEligible && len(observer.PredictionBias.CounterfactualChecks) >= 3:
		return "medium"
	case observer.PredictionBias.Mode != "":
		return "low"
	default:
		return "none"
	}
}

func temporalComplexity(observer ObserverLoopReport) string {
	switch {
	case observer.TemporalVariance.VarianceBand == "high" || observer.TemporalSync.Status == "desynchronized":
		return "high"
	case observer.TemporalVariance.Mode != "" || observer.TemporalSync.Status == "bounded_desync":
		return "medium"
	case observer.TemporalSync.Clock != "":
		return "low"
	default:
		return "none"
	}
}

func semanticSaturationBand(layerCount, overlapCount int) string {
	switch {
	case layerCount >= 8 || overlapCount >= 4:
		return "high"
	case layerCount >= 5 || overlapCount >= 2:
		return "medium"
	case layerCount > 0 || overlapCount > 0:
		return "low"
	default:
		return "none"
	}
}

func layerCollapseSuggestions(layers, overlap []string, overConstraintRisk, temporalComplexity string) []string {
	suggestions := []string{}
	if len(layers) >= 8 || len(overlap) >= 4 {
		suggestions = append(suggestions, "evaluate_causal_field_model")
	}
	if containsString(overlap, "control_equilibrium_overlap") || containsString(overlap, "prediction_counterfactual_overlap") {
		suggestions = append(suggestions, "unify_control_equilibrium_prediction")
	}
	if overConstraintRisk == "high" || overConstraintRisk == "medium" {
		suggestions = append(suggestions, "tune_counterfactual_guard_gain")
	}
	if temporalComplexity == "high" || temporalComplexity == "medium" {
		suggestions = append(suggestions, "fold_dual_clock_into_causal_time")
	}
	if len(suggestions) == 0 {
		suggestions = append(suggestions, "keep_layered_v5_runtime")
	}
	return limitStrings(canonicalStrings(suggestions), maxCompressionStrings)
}

func prioritizedPredictiveSignals(signals []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	out := canonicalStrings(signals)
	sort.SliceStable(out, func(i, j int) bool {
		pi := predictiveSignalPriority(out[i])
		pj := predictiveSignalPriority(out[j])
		if pi != pj {
			return pi < pj
		}
		return out[i] < out[j]
	})
	return limitStrings(out, limit)
}

func predictiveSignalPriority(signal string) int {
	switch {
	case signal == "predicted_observer_oscillation":
		return 0
	case strings.Contains(signal, "oscillation"):
		return 1
	case signal == "supported_outcome" || signal == "weakened_outcome":
		return 2
	case signal != "":
		return 3
	default:
		return 9
	}
}

func stringsNotIn(source, excluded []string) []string {
	if len(source) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, value := range excluded {
		seen[value] = true
	}
	out := make([]string, 0, len(source))
	for _, value := range source {
		if seen[value] {
			continue
		}
		out = append(out, value)
	}
	return out
}

func predictiveObserverOscillationIndex(samples []CausalSignalDynamics, current CausalSignalDynamics) float64 {
	predicted := append([]CausalSignalDynamics(nil), samples...)
	predicted = append(predicted, current)
	if len(predicted) > maxObserverLagWindow {
		predicted = predicted[len(predicted)-maxObserverLagWindow:]
	}
	return observerOscillationIndex(predicted)
}

func globalDampingEnvelope(samples []CausalSignalDynamics, feedbackSignals []string) GlobalDampingEnvelope {
	oscillation := observerOscillationIndex(samples)
	state := "passive"
	factor := 1.0
	suppressed := []string(nil)
	if len(samples) >= 4 && oscillation >= 0.5 {
		state = "damped"
		factor = 0.5
		suppressed = append([]string(nil), feedbackSignals...)
	} else if len(feedbackSignals) > 0 {
		state = "armed"
	}
	return GlobalDampingEnvelope{
		State:             state,
		Factor:            factor,
		OscillationIndex:  oscillation,
		SuppressedSignals: suppressed,
	}
}

func observerOscillationIndex(samples []CausalSignalDynamics) float64 {
	if len(samples) < 2 {
		return 0
	}
	transitions := 0
	for i := 1; i < len(samples); i++ {
		if samples[i-1].OverRegularized != samples[i].OverRegularized {
			transitions++
		}
	}
	return roundScore(float64(transitions) / float64(len(samples)-1))
}

func memoryEdgePriority(edge MemoryEdge) int {
	switch edge.Relation {
	case "supports":
		return 0
	case "causes":
		return 1
	case "depends_on":
		return 2
	case "derived_from":
		return 3
	case "contradicts":
		return 4
	default:
		return 9
	}
}

func memoryEdgeKey(edge MemoryEdge) string {
	return strings.TrimSpace(edge.Relation) + "\x00" + strings.TrimSpace(edge.From) + "\x00" + strings.TrimSpace(edge.To)
}

func scoreBand(score float64) string {
	switch {
	case score >= 0.8:
		return "high"
	case score >= 0.4:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "none"
	}
}

func tokenBand(tokens int) string {
	switch {
	case tokens >= 16000:
		return "very_high"
	case tokens >= 8000:
		return "high"
	case tokens >= 2000:
		return "medium"
	case tokens > 0:
		return "low"
	default:
		return "none"
	}
}

func latencyBand(ms int64) string {
	switch {
	case ms >= 300000:
		return "very_high"
	case ms >= 60000:
		return "high"
	case ms >= 10000:
		return "medium"
	case ms > 0:
		return "low"
	default:
		return "none"
	}
}
