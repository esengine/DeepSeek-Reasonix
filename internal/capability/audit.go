package capability

import "sync"

// Audit is a non-persisted capability/routing counters sink, mirroring
// readiness audit collection for run --metrics and e2ebench.
type Audit struct {
	mu sync.Mutex

	Routes                 int
	RoutedCandidates       int
	RoutedRequire          int
	RoutedPrefer           int
	RoutedSuggest          int
	Declines               int
	SemanticRoutes         int
	SemanticFallbacks      int
	RequireMissing         int
	RequireRecovered       int
	PreferMissing          int
	PreferRecovered        int
	SkillInvocations       int
	SkillFailures          int
	SkillUnavailable       int
	MCPInspect             int
	MCPCall                int
	MCPCallFailures        int
	ReviewBlocks           int
	SecurityReviewBlocks   int
	RouterPromptTokens     int
	RouterCompletionTokens int
	RouterCost             float64
	RouterLatencyMs        int64
}

// RecordDecision captures the route-to-invocation funnel before the model acts.
func (a *Audit) RecordDecision(decision RouteDecision) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, candidate := range decision.Candidates {
		a.RoutedCandidates++
		switch candidate.Policy {
		case AutoUseRequire:
			a.RoutedRequire++
		case AutoUsePrefer:
			a.RoutedPrefer++
		case AutoUseSuggest:
			a.RoutedSuggest++
		}
	}
}

// RecordDecline counts an explicit model decision not to use a preferred route.
func (a *Audit) RecordDecline() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.Declines++
	a.mu.Unlock()
}

// RecordRoute increments deterministic/hybrid route counts.
func (a *Audit) RecordRoute(semantic, fallback bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Routes++
	if semantic {
		a.SemanticRoutes++
	}
	if fallback {
		a.SemanticFallbacks++
	}
}

// RecordGate records require/prefer missing and recovery.
func (a *Audit) RecordGate(requireMissing, preferMissing, recovered bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if requireMissing {
		a.RequireMissing++
	}
	if preferMissing {
		a.PreferMissing++
	}
	if recovered {
		if requireMissing {
			a.RequireRecovered++
		}
		if preferMissing {
			a.PreferRecovered++
		}
	}
}

// RecordSkill records skill invocation outcomes.
func (a *Audit) RecordSkill(failed, unavailable bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.SkillInvocations++
	if failed {
		a.SkillFailures++
	}
	if unavailable {
		a.SkillUnavailable++
	}
}

// RecordMCPProxy records use_capability proxy activity.
func (a *Audit) RecordMCPProxy(inspect, call, failed bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if inspect {
		a.MCPInspect++
	}
	if call {
		a.MCPCall++
	}
	if failed {
		a.MCPCallFailures++
	}
}

// RecordGateRecovery records that gate kinds which missed earlier in the turn
// later passed cleanly — the capability was actually invoked after the nudge.
// Kept separate from RecordGate so a recovery never double-counts as a miss.
func (a *Audit) RecordGateRecovery(require, prefer bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if require {
		a.RequireRecovered++
	}
	if prefer {
		a.PreferRecovered++
	}
}

// RecordRouterUsage accumulates the semantic router's own model spend:
// prompt/completion tokens, priced cost, and wall-clock latency per call.
func (a *Audit) RecordRouterUsage(promptTokens, completionTokens int, cost float64, latencyMs int64) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.RouterPromptTokens += promptTokens
	a.RouterCompletionTokens += completionTokens
	a.RouterCost += cost
	a.RouterLatencyMs += latencyMs
}

// RecordReviewBlock records blocking structured review outcomes.
func (a *Audit) RecordReviewBlock(security bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if security {
		a.SecurityReviewBlocks++
	} else {
		a.ReviewBlocks++
	}
}

// Snapshot returns a copy of counters for metrics export.
func (a *Audit) Snapshot() Audit {
	if a == nil {
		return Audit{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return Audit{
		Routes:                 a.Routes,
		RoutedCandidates:       a.RoutedCandidates,
		RoutedRequire:          a.RoutedRequire,
		RoutedPrefer:           a.RoutedPrefer,
		RoutedSuggest:          a.RoutedSuggest,
		Declines:               a.Declines,
		SemanticRoutes:         a.SemanticRoutes,
		SemanticFallbacks:      a.SemanticFallbacks,
		RequireMissing:         a.RequireMissing,
		RequireRecovered:       a.RequireRecovered,
		PreferMissing:          a.PreferMissing,
		PreferRecovered:        a.PreferRecovered,
		SkillInvocations:       a.SkillInvocations,
		SkillFailures:          a.SkillFailures,
		SkillUnavailable:       a.SkillUnavailable,
		MCPInspect:             a.MCPInspect,
		MCPCall:                a.MCPCall,
		MCPCallFailures:        a.MCPCallFailures,
		ReviewBlocks:           a.ReviewBlocks,
		SecurityReviewBlocks:   a.SecurityReviewBlocks,
		RouterPromptTokens:     a.RouterPromptTokens,
		RouterCompletionTokens: a.RouterCompletionTokens,
		RouterCost:             a.RouterCost,
		RouterLatencyMs:        a.RouterLatencyMs,
	}
}
