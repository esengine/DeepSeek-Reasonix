package agent

import (
	"sync"

	"reasonix/internal/provider"
)

// TokenGovernance aggregates the OPT-261~265 token-management modules behind
// one advisory surface the run loop calls at usage/compaction points:
//
//	OPT-261 TokenAwareLoadShedder        — shed load when token volume spikes
//	OPT-262 CacheInvalidationCompactor   — dedupe invalidated cache keys
//	OPT-263 ContextWindowDynamicResizer  — suggest resizing the context window
//	OPT-264 TokenAwareAdmissionGatekeeper— admit/reject work by token capacity
//	OPT-265 PromptCacheProactiveWarmer   — register & warm stable prompt prefixes
//
// Every module is advisory: TokenGovernance never vetoes a tool call or a
// model turn. It records statistics and returns a report the agent surfaces
// as events, so enabling it cannot change existing behavior — only add
// visibility into token pressure.
type TokenGovernance struct {
	mu sync.Mutex

	shedder    *TokenAwareLoadShedder
	compactor  *CacheInvalidationCompactor
	resizer    *ContextWindowDynamicResizer
	gatekeeper *TokenAwareAdmissionGatekeeper
	warmer     *PromptCacheProactiveWarmer

	// advisory counts surfaced through GetStats.
	observations    int
	shedCount       int
	admitted        int
	rejected        int
	compactCount    int
	compactedKeys   int
	warmTargets     int
	lastSuggestion  int
}

// TokenGovernanceOptions configures which modules are active and their
// thresholds. Zero values fall back to the module defaults.
type TokenGovernanceOptions struct {
	Enabled bool

	// OPT-261
	LoadShedder  bool
	LoadThreshold int
	ShedStrategy  string // "oldest" | "newest" | "random"

	// OPT-262
	CacheCompactor bool

	// OPT-263
	WindowResizer    bool
	ContextWindowMin int
	ContextWindowMax int

	// OPT-264
	AdmissionGate     bool
	AdmissionCapacity int

	// OPT-265
	CacheWarmer   bool
	WarmerStrategy string // "lru" | "fifo" | ...
}

// NewTokenGovernance wires the enabled modules. All five are optional; a
// fully disabled governance still counts observations (cheap and harmless).
func NewTokenGovernance(opts TokenGovernanceOptions) *TokenGovernance {
	g := &TokenGovernance{}
	if opts.LoadShedder || opts.LoadThreshold > 0 {
		thr := opts.LoadThreshold
		if thr <= 0 {
			thr = 100_000
		}
		strategy := opts.ShedStrategy
		if strategy == "" {
			strategy = "oldest"
		}
		g.shedder = NewTokenAwareLoadShedder(thr, strategy)
	}
	if opts.CacheCompactor {
		g.compactor = NewCacheInvalidationCompactor()
	}
	if opts.WindowResizer || opts.ContextWindowMin > 0 || opts.ContextWindowMax > 0 {
		minSize := opts.ContextWindowMin
		if minSize <= 0 {
			minSize = 16_000
		}
		maxSize := opts.ContextWindowMax
		if maxSize <= 0 {
			maxSize = 256_000
		}
		g.resizer = NewContextWindowDynamicResizer(minSize, maxSize, minSize)
	}
	if opts.AdmissionGate || opts.AdmissionCapacity > 0 {
		cap := opts.AdmissionCapacity
		if cap <= 0 {
			cap = 200_000
		}
		g.gatekeeper = NewTokenAwareAdmissionGatekeeper(cap)
	}
	if opts.CacheWarmer {
		strategy := opts.WarmerStrategy
		if strategy == "" {
			strategy = "lru"
		}
		g.warmer = NewPromptCacheProactiveWarmer(strategy)
	}
	return g
}

// TokenGovernanceReport is what one ObserveUsage call produced. The agent
// surfaces Shed/Rejected as warnings and the rest as stats.
type TokenGovernanceReport struct {
	ObservedTokens int
	Shed           bool // load exceeded the shedder threshold
	Rejected       bool // admission gatekeeper refused the load
	CompactedKeys  int  // cache invalidation keys deduped this observation
	WarmTarget     string
}

// ObserveUsage feeds one model turn's usage (and cache diagnostics) through
// the enabled modules. modelRef names the model that produced the usage (for
// the cache warmer). Safe for concurrent use.
func (g *TokenGovernance) ObserveUsage(usage *provider.Usage, modelRef string, diag *CacheDiagnostics) TokenGovernanceReport {
	g.mu.Lock()
	defer g.mu.Unlock()

	rep := TokenGovernanceReport{}
	if usage == nil {
		return rep
	}
	load := usage.TotalTokens
	rep.ObservedTokens = load
	g.observations++

	if g.shedder != nil && g.shedder.ShouldShed(load) {
		g.shedCount++
		rep.Shed = true
	}
	if g.gatekeeper != nil {
		if g.gatekeeper.Admit(load) {
			g.admitted++
		} else {
			g.rejected++
			rep.Rejected = true
		}
		g.gatekeeper.Release(load)
	}
	if g.compactor != nil && diag != nil {
		var keys []string
		if diag.PrefixHash != "" {
			keys = append(keys, "prefix:"+diag.PrefixHash)
		}
		if diag.SystemHash != "" {
			keys = append(keys, "system:"+diag.SystemHash)
		}
		if diag.ToolsHash != "" {
			keys = append(keys, "tools:"+diag.ToolsHash)
		}
		if len(keys) > 0 {
			compacted := g.compactor.Compact(keys)
			rep.CompactedKeys = len(keys) - len(compacted)
			g.compactCount++
			g.compactedKeys += rep.CompactedKeys
		}
	}
	if g.warmer != nil && modelRef != "" {
		g.warmer.AddTarget("model:"+modelRef, int64(load))
		rep.WarmTarget = modelRef
		g.warmTargets = g.warmer.GetTargetCount()
	}
	return rep
}

// SuggestWindow asks the dynamic resizer what the context window should be
// for the current pressure. Returns current unchanged when the resizer is
// disabled. Advisory only — the agent decides whether to apply it.
func (g *TokenGovernance) SuggestWindow(current int) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.resizer == nil || current <= 0 {
		g.lastSuggestion = current
		return current
	}
	g.lastSuggestion = g.resizer.Resize(current)
	return g.lastSuggestion
}

// GetStats returns a flat summary for diagnostics/status lines.
func (g *TokenGovernance) GetStats() map[string]interface{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := map[string]interface{}{
		"observations": g.observations,
		"shed_count":   g.shedCount,
		"admitted":     g.admitted,
		"rejected":     g.rejected,
		"compactions":  g.compactCount,
		"compacted_keys": g.compactedKeys,
		"warm_targets": g.warmTargets,
		"last_window_suggestion": g.lastSuggestion,
	}
	if g.shedder != nil {
		for k, v := range g.shedder.GetStats() {
			out["shedder_"+k] = v
		}
	}
	if g.gatekeeper != nil {
		for k, v := range g.gatekeeper.GetStats() {
			out["gatekeeper_"+k] = v
		}
	}
	return out
}
