package provider

import "strings"

// ToolRunState is local execution evidence; it is never sent as a wire field.
// Unknown legacy results are classified conservatively when interrupted.
type ToolRunState string

const (
	ToolRunCompleted  ToolRunState = "completed"
	ToolRunNotStarted ToolRunState = "not_started"
	ToolRunUnknown    ToolRunState = "unknown"
)

func ToolResultRunState(m Message) ToolRunState {
	switch m.ToolRunState {
	case ToolRunCompleted, ToolRunNotStarted, ToolRunUnknown:
		return m.ToolRunState
	case "":
	default:
		return ToolRunUnknown
	}
	text := strings.ToLower(strings.TrimSpace(m.Content))
	if strings.Contains(text, "write outcome unknown:") || text == interruptedToolResult {
		return ToolRunUnknown
	}
	if strings.HasPrefix(text, "cancelled: context cancelled before execution") || strings.HasPrefix(text, "cancelled: tool dispatch was not durable") {
		return ToolRunNotStarted
	}
	if strings.HasPrefix(text, "cancelled:") || strings.Contains(text, "context canceled") || strings.Contains(text, "context cancelled") {
		return ToolRunUnknown
	}
	return ToolRunCompleted
}

// RecordToolRecovery retains legacy interrupted names for older readers while
// new readers distinguish calls proven not to have run from uncertain effects.
func RecordToolRecovery(r *InterruptedTurnRecovery, call InterruptedToolSummary, state ToolRunState) {
	if call.Name == "" {
		return
	}
	switch state {
	case ToolRunCompleted:
		r.CompletedTools = append(r.CompletedTools, call)
	case ToolRunNotStarted:
		r.NotStartedTools = append(r.NotStartedTools, call)
		r.InterruptedTools = append(r.InterruptedTools, call.Name)
	default:
		r.UnknownTools = append(r.UnknownTools, call)
		r.InterruptedTools = append(r.InterruptedTools, call.Name)
	}
}

// InterruptedTurnRecovery is the durable,
// provider-excluded handoff for an unfinished turn. It contains bounded facts;
// raw partial reasoning remains local for display.
type InterruptedTurnRecovery struct {
	WriteChecks             []WriteRecoveryCheck     `json:"write_checks,omitempty"`
	SatisfiedWrites         []InterruptedToolSummary `json:"satisfied_writes,omitempty"`
	Pending                 bool                     `json:"pending,omitempty"`
	CompletedTools          []InterruptedToolSummary `json:"completed_tools,omitempty"`
	InterruptedTools        []string                 `json:"interrupted_tools,omitempty"`
	NotStartedTools         []InterruptedToolSummary `json:"not_started_tools,omitempty"`
	UnknownTools            []InterruptedToolSummary `json:"unknown_tools,omitempty"`
	DroppedPartialText      bool                     `json:"dropped_partial_text,omitempty"`
	DroppedPartialReasoning bool                     `json:"dropped_partial_reasoning,omitempty"`
}

// InterruptedToolSummary records a completed, fully paired tool call without duplicating arguments or results.
// The canonical assistant/tool messages immediately before the recovery record remain the source of truth.
type InterruptedToolSummary struct {
	ID      string   `json:"id,omitempty"`
	Name    string   `json:"name"`
	Files   []string `json:"files,omitempty"`
	Added   int      `json:"added,omitempty"`
	Removed int      `json:"removed,omitempty"`
}

// WriteRecoveryCheck reports a current postcondition, not an execution receipt.
type WriteRecoveryCheck struct {
	CallID string `json:"call_id"`
	Path   string `json:"path"`
	State  string `json:"state"`
}
