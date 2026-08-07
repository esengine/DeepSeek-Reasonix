package cli

import (
	"encoding/json"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/fileutil"
)

// SourceUsage is one Usage origin's share of a run. Steps counts every billed
// model call regardless of origin, so a run can exceed the executor's max_steps
// budget without the main loop having done so; this breakdown is what makes
// that total explicable instead of alarming.
type SourceUsage struct {
	Calls            int     `json:"calls"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

// RunMetrics is the machine-readable token/cache/cost summary `run --metrics`
// writes, so a benchmark harness can read a run's cost without scraping stdout.
type RunMetrics struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CacheHitTokens   int `json:"cache_hit_tokens"`
	CacheMissTokens  int `json:"cache_miss_tokens"`
	// PrefixChangeReasonCounts tallies how many usage events reported each
	// cache-prefix-change reason (e.g. "compact_auto", "snip", "tools") across
	// the run, so a regression in cache-reset frequency shows which operation
	// is responsible instead of just a dropped hit-rate percentage.
	PrefixChangeReasonCounts       map[string]int `json:"prefix_change_reason_counts,omitempty"`
	Steps                          int            `json:"steps"` // model calls (one per stream, incl. tool rounds)
	Cost                           float64        `json:"cost"`
	Currency                       string         `json:"currency"`
	Estimated                      bool           `json:"estimated,omitempty"`
	Compactions                    int            `json:"compactions"`
	ReadinessChecks                int            `json:"readiness_checks"`
	ReadinessAllowed               int            `json:"readiness_allowed"`
	ReadinessBlocks                int            `json:"readiness_blocks"`
	ReadinessRecoveries            int            `json:"readiness_recoveries"`
	ReadinessErrors                int            `json:"readiness_errors"`
	ReadinessMissingProjectChecks  int            `json:"readiness_missing_project_checks"`
	ReadinessIncompleteTodos       int            `json:"readiness_incomplete_todos"`
	ReadinessCommandMismatches     int            `json:"readiness_command_mismatches"`
	ReadinessMissingAcceptance     int            `json:"readiness_missing_acceptance_criteria"`
	ReadinessMissingVerification   int            `json:"readiness_missing_verification"`
	ReadinessMissingReview         int            `json:"readiness_missing_review"`
	ReadinessMissingSignoff        int            `json:"readiness_missing_signoff"`
	ReadinessMissingActionEvidence int            `json:"readiness_missing_action_evidence"`
	ReadinessMissingMutation       int            `json:"readiness_missing_mutation"`
	MissingReasoningDetected       int            `json:"missing_reasoning_detected,omitempty"`
	MissingReasoningRetries        int            `json:"missing_reasoning_retries,omitempty"`
	MissingReasoningRecovered      int            `json:"missing_reasoning_recovered,omitempty"`
	MissingReasoningReplaced       int            `json:"missing_reasoning_retry_replaced_response,omitempty"`
	MissingReasoningSuppressed     int            `json:"missing_reasoning_retry_suppressed,omitempty"`
	MissingReasoningFallbacks      int            `json:"missing_reasoning_fallbacks,omitempty"`
	// Capability / Delivery routing counters (optional; zero for older readers).
	CapabilityRoutes               int     `json:"capability_routes,omitempty"`
	CapabilityRoutedCandidates     int     `json:"capability_routed_candidates,omitempty"`
	CapabilityRoutedRequire        int     `json:"capability_routed_require,omitempty"`
	CapabilityRoutedPrefer         int     `json:"capability_routed_prefer,omitempty"`
	CapabilityRoutedSuggest        int     `json:"capability_routed_suggest,omitempty"`
	CapabilityDeclines             int     `json:"capability_declines,omitempty"`
	CapabilitySemanticRoutes       int     `json:"capability_semantic_routes,omitempty"`
	CapabilitySemanticFallbacks    int     `json:"capability_semantic_fallbacks,omitempty"`
	CapabilityRequireMissing       int     `json:"capability_require_missing,omitempty"`
	CapabilityRequireRecovered     int     `json:"capability_require_recovered,omitempty"`
	CapabilityPreferMissing        int     `json:"capability_prefer_missing,omitempty"`
	CapabilityPreferRecovered      int     `json:"capability_prefer_recovered,omitempty"`
	CapabilitySkillInvocations     int     `json:"capability_skill_invocations,omitempty"`
	CapabilitySkillFailures        int     `json:"capability_skill_failures,omitempty"`
	CapabilitySkillUnavailable     int     `json:"capability_skill_unavailable,omitempty"`
	CapabilityMCPInspect           int     `json:"capability_mcp_inspect,omitempty"`
	CapabilityMCPCall              int     `json:"capability_mcp_call,omitempty"`
	CapabilityMCPCallFailures      int     `json:"capability_mcp_call_failures,omitempty"`
	CapabilityReviewBlocks         int     `json:"capability_review_blocks,omitempty"`
	CapabilitySecurityReviewBlocks int     `json:"capability_security_review_blocks,omitempty"`
	CapabilityRouterPromptTokens   int     `json:"capability_router_prompt_tokens,omitempty"`
	CapabilityRouterCompletionTok  int     `json:"capability_router_completion_tokens,omitempty"`
	CapabilityRouterCost           float64 `json:"capability_router_cost,omitempty"`
	CapabilityRouterLatencyMs      int64   `json:"capability_router_latency_ms,omitempty"`

	// Run accounting: what a benchmark needs to price one solved task and name
	// the guard that ended a failed one.

	// Complete distinguishes a final record from an in-flight snapshot. A killed
	// agent leaves only the latter, and its numbers are lower bounds.
	Complete           bool                   `json:"complete"`
	UsageBySource      map[string]SourceUsage `json:"usage_by_source,omitempty"`
	Arm                string                 `json:"arm"`
	DurationMs         int64                  `json:"duration_ms"`
	Outcome            string                 `json:"outcome"`
	ToolCalls          int                    `json:"tool_calls"`
	ToolFailures       int                    `json:"tool_failures"`
	ToolDurationMs     int64                  `json:"tool_duration_ms"`
	SubagentToolCalls  int                    `json:"subagent_tool_calls"`
	Retries            int                    `json:"retries"`
	ToolCallsByName    map[string]int         `json:"tool_calls_by_name,omitempty"`
	ToolFailuresByName map[string]int         `json:"tool_failures_by_name,omitempty"`
}

// metricsSink forwards every event to the real sink and accumulates the per-call
// Usage events into a RunMetrics. Cache totals are summed per call (not read from
// the cumulative SessionHit/Miss) so they match PromptTokens exactly.
type metricsSink struct {
	inner event.Sink

	// mu guards m. Emit alone is serialized by the session's event.Sync wrapper,
	// but the final read from the run command races background job emission, and
	// the snapshot goroutine reads the same fields.
	mu sync.Mutex
	m  RunMetrics

	// partialPath receives throttled in-flight snapshots, so a run killed by a
	// timeout still leaves accounting behind instead of nothing. Empty disables
	// them; snapshotEvery bounds the write rate.
	partialPath   string
	snapshotEvery time.Duration
	lastSnapshot  time.Time
	clock         func() time.Time
}

func (s *metricsSink) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

// Snapshot returns a deep copy safe to marshal while the run continues.
func (s *metricsSink) Snapshot() RunMetrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m.clone()
}

func (m RunMetrics) clone() RunMetrics {
	out := m
	out.PrefixChangeReasonCounts = cloneCounts(m.PrefixChangeReasonCounts)
	out.UsageBySource = cloneSourceUsage(m.UsageBySource)
	out.ToolCallsByName = cloneCounts(m.ToolCallsByName)
	out.ToolFailuresByName = cloneCounts(m.ToolFailuresByName)
	return out
}

func cloneCounts(in map[string]int) map[string]int {
	if in == nil {
		return nil
	}
	out := make(map[string]int, len(in))
	maps.Copy(out, in)
	return out
}

func cloneSourceUsage(in map[string]SourceUsage) map[string]SourceUsage {
	if in == nil {
		return nil
	}
	out := make(map[string]SourceUsage, len(in))
	maps.Copy(out, in)
	return out
}

// writeSnapshot publishes the in-flight record to the sidecar. Callers hold mu.
// A snapshot is never marked complete, so a reader can always tell it apart
// from a final record even if the process dies immediately after.
func (s *metricsSink) writeSnapshot() {
	if s.partialPath == "" {
		return
	}
	now := s.now()
	if !s.lastSnapshot.IsZero() && now.Sub(s.lastSnapshot) < s.snapshotEvery {
		return
	}
	s.lastSnapshot = now
	snap := s.m.clone()
	snap.Complete = false
	if data, err := json.MarshalIndent(snap, "", "  "); err == nil {
		_ = fileutil.AtomicWriteFile(s.partialPath, data, 0o644)
	}
}

func (s *metricsSink) Emit(e event.Event) {
	s.mu.Lock()
	s.record(e)
	s.writeSnapshot()
	s.mu.Unlock()
	s.inner.Emit(e)
}

func (s *metricsSink) record(e event.Event) {
	if e.Kind == event.Usage && e.Usage != nil {
		u := e.Usage
		s.m.PromptTokens += u.PromptTokens
		s.m.CompletionTokens += u.CompletionTokens
		s.m.CacheHitTokens += u.CacheHitTokens
		s.m.CacheMissTokens += u.CacheMissTokens
		s.m.Steps++
		s.m.Estimated = s.m.Estimated || u.Estimated
		var stepCost float64
		if p := e.Pricing; p != nil {
			stepCost = p.Cost(u)
			s.m.Cost += stepCost
			s.m.Currency = p.Currency
		}
		s.recordSource(e.UsageSource, u.PromptTokens, u.CompletionTokens, stepCost)
		if e.UsageSource == event.UsageSourceCapabilityRouter {
			s.m.CapabilityRouterPromptTokens += u.PromptTokens
			s.m.CapabilityRouterCompletionTok += u.CompletionTokens
			s.m.CapabilityRouterCost += stepCost
		}
		if e.CacheDiagnostics != nil && len(e.CacheDiagnostics.PrefixChangeReasons) > 0 {
			if s.m.PrefixChangeReasonCounts == nil {
				s.m.PrefixChangeReasonCounts = map[string]int{}
			}
			for _, reason := range e.CacheDiagnostics.PrefixChangeReasons {
				s.m.PrefixChangeReasonCounts[reason]++
			}
		}
	}
	if e.Kind == event.CompactionStarted {
		s.m.Compactions++
	}
	if e.Kind == event.ToolResult {
		s.recordToolResult(e.Tool)
	}
	if e.Kind == event.Retrying {
		s.m.Retries++
	}
	s.inner.Emit(e)
}

// recordSource buckets one model call by its origin. An empty source means the
// executor, per the Usage event contract. An unrecognised source is kept under
// its own key rather than dropped, so a future origin cannot silently vanish
// from a total that is meant to reconcile.
func (s *metricsSink) recordSource(source string, prompt, completion int, cost float64) {
	if strings.TrimSpace(source) == "" {
		source = event.UsageSourceExecutor
	}
	if s.m.UsageBySource == nil {
		s.m.UsageBySource = map[string]SourceUsage{}
	}
	agg := s.m.UsageBySource[source]
	agg.Calls++
	agg.PromptTokens += prompt
	agg.CompletionTokens += completion
	agg.Cost += cost
	s.m.UsageBySource[source] = agg
}

// recordToolResult attributes a finished call by the name the model emitted,
// not Tool.ResolvedName — a wasted call is a wrong model decision, and the
// proxy target it resolved to would hide which name was picked.
func (s *metricsSink) recordToolResult(t event.Tool) {
	name := strings.TrimSpace(t.Name)
	if name == "" {
		name = "unknown"
	}
	s.m.ToolCalls++
	s.m.ToolDurationMs += t.DurationMs
	if t.ParentID != "" {
		s.m.SubagentToolCalls++
	}
	if s.m.ToolCallsByName == nil {
		s.m.ToolCallsByName = map[string]int{}
	}
	s.m.ToolCallsByName[name]++
	if t.Err == "" {
		return
	}
	s.m.ToolFailures++
	if s.m.ToolFailuresByName == nil {
		s.m.ToolFailuresByName = map[string]int{}
	}
	s.m.ToolFailuresByName[name]++
}

func (s *metricsSink) RecordReadinessAudit(a evidence.ReadinessAudit) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.m.ReadinessMissingAcceptance += a.MissingAcceptanceCriteria
	s.m.ReadinessMissingVerification += a.MissingVerification
	s.m.ReadinessMissingReview += a.MissingReview
	s.m.ReadinessMissingSignoff += a.MissingSignoff
	s.m.ReadinessMissingActionEvidence += a.MissingActionEvidence
	s.m.ReadinessMissingMutation += a.MissingMutation
}

func (s *metricsSink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	s.mu.Lock()
	switch a.Kind {
	case event.ProtocolRecoveryMissingReasoningDetected:
		s.m.MissingReasoningDetected++
	case event.ProtocolRecoveryMissingReasoningRetryAttempted:
		s.m.MissingReasoningRetries++
		// Usage from the original and recovery responses is intentionally merged
		// into one invisible UI event, but Steps remains a true model-call count.
		s.m.Steps++
	case event.ProtocolRecoveryMissingReasoningRetryRecovered:
		s.m.MissingReasoningRecovered++
	case event.ProtocolRecoveryMissingReasoningRetryReplaced:
		s.m.MissingReasoningReplaced++
	case event.ProtocolRecoveryMissingReasoningRetrySuppressed:
		s.m.MissingReasoningSuppressed++
	case event.ProtocolRecoveryMissingReasoningFallback:
		s.m.MissingReasoningFallbacks++
	}
	s.mu.Unlock()
	event.RecordProtocolRecovery(s.inner, a)
}

// MergeCapabilityAuditCounters copies capability counters into RunMetrics.
func (m *RunMetrics) MergeCapabilityAuditCounters(
	routes, routedCandidates, routedRequire, routedPrefer, routedSuggest, declines int,
	semantic, fallbacks, requireMiss, requireRec, preferMiss, preferRec int,
	skillInv, skillFail, skillUnavail int,
	mcpInspect, mcpCall, mcpFail int,
	reviewBlocks, securityBlocks int,
	routerPrompt, routerCompletion int,
	routerCost float64,
	routerLatencyMs int64,
) {
	if m == nil {
		return
	}
	m.CapabilityRoutes += routes
	m.CapabilityRoutedCandidates += routedCandidates
	m.CapabilityRoutedRequire += routedRequire
	m.CapabilityRoutedPrefer += routedPrefer
	m.CapabilityRoutedSuggest += routedSuggest
	m.CapabilityDeclines += declines
	m.CapabilitySemanticRoutes += semantic
	m.CapabilitySemanticFallbacks += fallbacks
	m.CapabilityRequireMissing += requireMiss
	m.CapabilityRequireRecovered += requireRec
	m.CapabilityPreferMissing += preferMiss
	m.CapabilityPreferRecovered += preferRec
	m.CapabilitySkillInvocations += skillInv
	m.CapabilitySkillFailures += skillFail
	m.CapabilitySkillUnavailable += skillUnavail
	m.CapabilityMCPInspect += mcpInspect
	m.CapabilityMCPCall += mcpCall
	m.CapabilityMCPCallFailures += mcpFail
	m.CapabilityReviewBlocks += reviewBlocks
	m.CapabilitySecurityReviewBlocks += securityBlocks
	m.CapabilityRouterPromptTokens += routerPrompt
	m.CapabilityRouterCompletionTok += routerCompletion
	m.CapabilityRouterCost += routerCost
	m.CapabilityRouterLatencyMs += routerLatencyMs
}

// partialMetricsPath is the sidecar an unfinished run leaves behind. It is a
// distinct filename so a reader that predates snapshots cannot mistake one for
// a final record.
func partialMetricsPath(path string) string { return path + ".partial" }

// writeMetrics publishes the final record and retires the sidecar, so the two
// can never both be read and double-counted. The final file is written first:
// if the process dies between the two steps, a stale partial alongside a
// complete final is resolvable, whereas the reverse would lose everything.
func writeMetrics(path string, m RunMetrics) error {
	m.Complete = true
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := fileutil.AtomicWriteFile(path, b, 0o644); err != nil {
		return err
	}
	_ = os.Remove(partialMetricsPath(path))
	return nil
}
