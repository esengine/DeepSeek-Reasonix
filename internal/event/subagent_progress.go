package event

import "strings"

// Reserved ToolProgress channel names for local sub-agent progress previews.
//
// These names are an internal wire contract between the agent progress
// tracker and local frontends (desktop and CLI). They must never be presented
// as provider-visible tool names: ACP and bot consumers ignore ToolProgress
// bodies entirely, and the names are filtered out of any transcript or
// provider context by construction (they only ever ride ToolProgress events).
//
// The status channel carries exactly one of the phase values produced by the
// tracker (queued/running/reasoning/responding/tool/retrying/completed/
// failed/cancelled) in Tool.Output; the other channels carry bounded UTF-8
// text deltas. Tool.Truncated marks a merged or truncated preview round,
// Tool.DurationMs rides terminal status events, and progress lookup is keyed
// by Tool.ID (the child task card ID).
const (
	SubagentProgressPrefix        = "reasonix.subagent."
	SubagentProgressStatusName    = SubagentProgressPrefix + "status"
	SubagentProgressReasoningName = SubagentProgressPrefix + "reasoning"
	SubagentProgressTextName      = SubagentProgressPrefix + "text"
	SubagentProgressNoticeName    = SubagentProgressPrefix + "notice"
)

// IsReservedSubagentProgressName reports whether name belongs to the reserved
// sub-agent progress namespace. Consumers must suppress unknown names in this
// namespace so an older frontend never renders a channel introduced by a newer
// agent as ordinary tool output.
func IsReservedSubagentProgressName(name string) bool {
	return strings.HasPrefix(name, SubagentProgressPrefix)
}

// IsSubagentProgressName reports whether the ToolProgress Name carries a
// currently supported sub-agent progress channel. Use
// IsReservedSubagentProgressName when routing unknown future channels away from
// ordinary tool output.
func IsSubagentProgressName(name string) bool {
	switch name {
	case SubagentProgressStatusName, SubagentProgressReasoningName,
		SubagentProgressTextName, SubagentProgressNoticeName:
		return true
	}
	return false
}
