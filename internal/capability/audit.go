package capability

import (
	"maps"
	"sync"
)

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
	Discovery              DiscoveryAudit
	Arguments              ArgumentAudit
	LoopGuard              LoopGuardAudit
	MCPLists               MCPListAudit
	ToolExec               ToolExecAudit
	Phases                 PhaseAudit
}

// DiscoveryAudit counts model list/search/inspect actions, not MCP network.
type DiscoveryAudit struct {
	Lists, Searches, Inspects int
	ResultCount, ResultBytes  int
	NetworkCalls              int
}

// ArgumentAudit is host-side schema validation without argument values.
type ArgumentAudit struct {
	Validations, Fail, Skip, RemoteDispatch int
}

// LoopGuardAudit records deterministic stop/retry actions.
type LoopGuardAudit struct {
	RepeatFailures, RepeatClarifications, SoftBudgetNudges int
	BlockedCalls                                           int
}

// MCPListAudit distinguishes shared-host, disk-cache, and remote tools/list.
type MCPListAudit struct {
	SharedHost, DiskCache, Remote int
	DurationMs                    int64
	ToolCount, SchemaBytes        int
	Triggers                      map[string]int `json:"triggers,omitempty"`
}

// ToolExecAudit records classified tool execution without payloads.
type ToolExecAudit struct {
	Calls, ReadOnly, Parallel int
	QueueMs, ExecMs           int64
	RawBytes, VisibleBytes    int
}

// PhaseAudit is content-free time spent in host phases.
type PhaseAudit struct {
	ProviderWaitMs, ToolExecMs, SubagentWaitMs int64
	UserWaitMs, CompactMs, ReviewMs            int64
}

// RecordCapabilityDiscovery distinguishes model discovery actions from actual
// MCP network traffic. action is list, search, or inspect.
func (a *Audit) RecordCapabilityDiscovery(action string, results, bytes int, network bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch action {
	case "list":
		a.Discovery.Lists++
	case "search":
		a.Discovery.Searches++
	case "inspect":
		a.Discovery.Inspects++
	}
	a.Discovery.ResultCount += results
	a.Discovery.ResultBytes += bytes
	if network {
		a.Discovery.NetworkCalls++
	}
}

// RecordArgumentValidation records host validation without argument values.
// remoteDispatched must stay false for validation failures.
func (a *Audit) RecordArgumentValidation(failed, skipped, remoteDispatched bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Arguments.Validations++
	if failed {
		a.Arguments.Fail++
	}
	if skipped {
		a.Arguments.Skip++
	}
	if remoteDispatched {
		a.Arguments.RemoteDispatch++
	}
}

// RecordRemoteDispatch marks that a host-validated call reached tools/call.
func (a *Audit) RecordRemoteDispatch() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.Arguments.RemoteDispatch++
	a.mu.Unlock()
}

// RecordMCPList records one tools/list observation by source and host trigger.
func (a *Audit) RecordMCPList(source, trigger string, durationMs int64, toolCount, schemaBytes int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch source {
	case "shared_host":
		a.MCPLists.SharedHost++
	case "disk_cache":
		a.MCPLists.DiskCache++
	case "remote":
		a.MCPLists.Remote++
	}
	a.MCPLists.DurationMs += durationMs
	a.MCPLists.ToolCount += toolCount
	a.MCPLists.SchemaBytes += schemaBytes
	if trigger != "" {
		if a.MCPLists.Triggers == nil {
			a.MCPLists.Triggers = map[string]int{}
		}
		a.MCPLists.Triggers[trigger]++
	}
}

// RecordLoopGuard records a host loop-guard action without payloads.
func (a *Audit) RecordLoopGuard(kind string) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch kind {
	case "repeat_failure":
		a.LoopGuard.RepeatFailures++
	case "repeat_clarification":
		a.LoopGuard.RepeatClarifications++
	case "soft_budget":
		a.LoopGuard.SoftBudgetNudges++
	case "blocked":
		a.LoopGuard.BlockedCalls++
	}
}

// RecordToolExecution records one classified tool run without arguments.
func (a *Audit) RecordToolExecution(readOnly, parallel bool, queueMs, execMs int64, rawBytes, visibleBytes int) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ToolExec.Calls++
	if readOnly {
		a.ToolExec.ReadOnly++
	}
	if parallel {
		a.ToolExec.Parallel++
	}
	a.ToolExec.QueueMs += queueMs
	a.ToolExec.ExecMs += execMs
	a.ToolExec.RawBytes += rawBytes
	a.ToolExec.VisibleBytes += visibleBytes
}

// RecordPhaseMs accumulates content-free phase durations.
func (a *Audit) RecordPhaseMs(phase string, ms int64) {
	if a == nil || ms <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	switch phase {
	case "provider":
		a.Phases.ProviderWaitMs += ms
	case "tool":
		a.Phases.ToolExecMs += ms
	case "subagent":
		a.Phases.SubagentWaitMs += ms
	case "user":
		a.Phases.UserWaitMs += ms
	case "compact":
		a.Phases.CompactMs += ms
	case "review":
		a.Phases.ReviewMs += ms
	}
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
		Discovery:              a.Discovery,
		Arguments:              a.Arguments,
		LoopGuard:              a.LoopGuard,
		MCPLists:               cloneMCPListAudit(a.MCPLists),
		ToolExec:               a.ToolExec,
		Phases:                 a.Phases,
		ReviewBlocks:           a.ReviewBlocks,
		SecurityReviewBlocks:   a.SecurityReviewBlocks,
		RouterPromptTokens:     a.RouterPromptTokens,
		RouterCompletionTokens: a.RouterCompletionTokens,
		RouterCost:             a.RouterCost,
		RouterLatencyMs:        a.RouterLatencyMs,
	}
}

func cloneMCPListAudit(in MCPListAudit) MCPListAudit {
	out := in
	if len(in.Triggers) > 0 {
		out.Triggers = make(map[string]int, len(in.Triggers))
		maps.Copy(out.Triggers, in.Triggers)
	}
	return out
}
