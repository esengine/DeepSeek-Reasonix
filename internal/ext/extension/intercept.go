package extension

import (
	"fmt"
	"sort"
)

// InterceptorPoint names a hook point inside the runtime where interceptors
// observe or rewrite flow. The point of a KindInterceptor contribution is its
// ID, which keeps grouping and validation in one place.
type InterceptorPoint string

const (
	PointSessionStart       InterceptorPoint = "session.start"
	PointSessionEnd         InterceptorPoint = "session.end"
	PointSessionLoad        InterceptorPoint = "session.load"
	PointSessionSave        InterceptorPoint = "session.save"
	PointSessionRotate      InterceptorPoint = "session.rotate"
	PointInputReceive       InterceptorPoint = "input.receive"
	PointAgentBeforeStart   InterceptorPoint = "agent.before_start"
	PointSystemPromptBuild  InterceptorPoint = "system_prompt.build"
	PointContextPrepare     InterceptorPoint = "context.prepare"
	PointProviderRequest    InterceptorPoint = "provider.request"
	PointProviderResponse   InterceptorPoint = "provider.response"
	PointToolBefore         InterceptorPoint = "tool.before"
	PointToolAfter          InterceptorPoint = "tool.after"
	PointPermissionDecision InterceptorPoint = "permission.decision"
	PointCompactionPrepare  InterceptorPoint = "compaction.prepare"
	PointCompactionComplete InterceptorPoint = "compaction.complete"
	PointFrontendEvent      InterceptorPoint = "frontend.event"
)

// knownInterceptorPoint reports whether p is a declared point. Unknown points
// are rejected at validation: an interceptor registered at a misspelled point
// would sit in the chain unused and its author would never notice.
func knownInterceptorPoint(p InterceptorPoint) bool {
	switch p {
	case PointSessionStart, PointSessionEnd, PointSessionLoad, PointSessionSave,
		PointSessionRotate, PointInputReceive, PointAgentBeforeStart,
		PointSystemPromptBuild, PointContextPrepare, PointProviderRequest,
		PointProviderResponse, PointToolBefore, PointToolAfter,
		PointPermissionDecision, PointCompactionPrepare, PointCompactionComplete,
		PointFrontendEvent:
		return true
	default:
		return false
	}
}

// Interceptor priority bounds. The range is wide enough for layering
// (security gates very early, observers very late) and narrow enough that a
// runaway value is certainly a bug.
const (
	MinInterceptorPriority = -1000
	MaxInterceptorPriority = 1000
)

// ValidatePriority bounds interceptor priorities. Unbounded priorities would
// let one contributor squeeze ahead of every future interceptor, including
// ones the runtime itself needs to run first.
func ValidatePriority(p int) error {
	if p < MinInterceptorPriority || p > MaxInterceptorPriority {
		return fmt.Errorf("extension: interceptor priority %d out of range [%d, %d]", p, MinInterceptorPriority, MaxInterceptorPriority)
	}
	return nil
}

// SortInterceptors orders an interceptor chain for execution: ascending
// priority (lower runs earlier), then plugin ID so one package's chain is
// contiguous, then per-contributor registration order. The final ID/Origin
// tiebreaks only fire when every specified key ties — they keep the result
// independent of contributor registration order, which callers permute
// freely. The input slice is not mutated; a sorted copy is returned.
func SortInterceptors(cs []Contribution) []Contribution {
	out := make([]Contribution, len(cs))
	copy(out, cs)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.Source.PluginID != b.Source.PluginID {
			return a.Source.PluginID < b.Source.PluginID
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Source.Origin < b.Source.Origin
	})
	return out
}
