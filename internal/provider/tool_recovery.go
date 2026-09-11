package provider

import (
	"encoding/json"
	"strings"
)

// ToolRunState is local execution evidence; it is never sent as a wire field.
// Unknown legacy results are classified conservatively when interrupted.
type ToolRunState string

const (
	ToolRunPending       ToolRunState = "pending"
	ToolRunStarted       ToolRunState = "started"
	ToolRunRunning       ToolRunState = "running"
	ToolRunCompleted     ToolRunState = "completed"
	ToolRunFailed        ToolRunState = "failed"
	ToolRunCancelled     ToolRunState = "cancelled"
	ToolRunNotStarted    ToolRunState = "not_started"
	ToolRunUnknown       ToolRunState = "unknown"
	ToolRunUserConfirmed ToolRunState = "user_confirmed"
)

// ActionIdentity is the stable local identity of one logical tool action.
// It is provider-excluded and must not be inferred from an attempt alone.
type ActionIdentity struct {
	SessionID      string `json:"session_id,omitempty"`
	TurnID         string `json:"turn_id,omitempty"`
	AttemptID      string `json:"attempt_id,omitempty"`
	CallID         string `json:"call_id,omitempty"`
	CanonicalTool  string `json:"canonical_tool,omitempty"`
	ArgumentDigest string `json:"argument_digest,omitempty"`
	ResourceScope  string `json:"resource_scope,omitempty"`
}

// ToolCallRecord is a durable, provider-excluded execution receipt.
type ToolCallRecord struct {
	Identity         ActionIdentity  `json:"identity"`
	Arguments        json.RawMessage `json:"arguments,omitempty"`
	State            ToolRunState    `json:"state"`
	ReadOnly         bool            `json:"read_only"`
	IdempotencyKey   string          `json:"idempotency_key,omitempty"`
	StartedAt        int64           `json:"started_at,omitempty"`
	FinishedAt       int64           `json:"finished_at,omitempty"`
	ResultDigest     string          `json:"result_digest,omitempty"`
	EffectSummary    string          `json:"effect_summary,omitempty"`
	Resolution       string          `json:"resolution,omitempty"`
	ResolvedAt       int64           `json:"resolved_at,omitempty"`
	ResolutionSource string          `json:"resolution_source,omitempty"`
	InspectionID     string          `json:"inspection_id,omitempty"`
	InspectionState  string          `json:"inspection_state,omitempty"`
	SupersededBy     string          `json:"superseded_by,omitempty"`
}

func ToolResultRunState(m Message) ToolRunState {
	switch m.ToolRunState {
	case ToolRunPending, ToolRunStarted, ToolRunRunning, ToolRunCompleted, ToolRunFailed, ToolRunCancelled, ToolRunNotStarted, ToolRunUnknown, ToolRunUserConfirmed:
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

// IsInterruptedPlaceholder identifies the synthetic result inserted while a
// session is loaded. It is not execution evidence and must not override the
// ledger's durable start barrier.
func IsInterruptedPlaceholder(m Message) bool {
	return m.Role == RoleTool && m.ToolRunState == "" && strings.TrimSpace(m.Content) == interruptedToolResult
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
	case ToolRunFailed:
		r.FailedTools = append(r.FailedTools, call)
	case ToolRunUserConfirmed:
		r.UserConfirmedTools = append(r.UserConfirmedTools, call)
	case ToolRunNotStarted, ToolRunCancelled, ToolRunPending:
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
	TurnID                  string                   `json:"turn_id,omitempty"`
	AttemptID               string                   `json:"attempt_id,omitempty"`
	Cause                   string                   `json:"cause,omitempty"`
	TerminalStatus          string                   `json:"terminalStatus,omitempty"` // failed | interrupted; absent preserves legacy display
	FailureDiagnostic       *FailureDiagnostic       `json:"failureDiagnostic,omitempty"`
	WriteChecks             []WriteRecoveryCheck     `json:"write_checks,omitempty"`
	SatisfiedWrites         []InterruptedToolSummary `json:"satisfied_writes,omitempty"`
	Pending                 bool                     `json:"pending,omitempty"`
	CompletedTools          []InterruptedToolSummary `json:"completed_tools,omitempty"`
	FailedTools             []InterruptedToolSummary `json:"failed_tools,omitempty"`
	UserConfirmedTools      []InterruptedToolSummary `json:"user_confirmed_tools,omitempty"`
	InterruptedTools        []string                 `json:"interrupted_tools,omitempty"`
	NotStartedTools         []InterruptedToolSummary `json:"not_started_tools,omitempty"`
	UnknownTools            []InterruptedToolSummary `json:"unknown_tools,omitempty"`
	ToolCalls               []ToolCallRecord         `json:"tool_calls,omitempty"`
	RequiresUserDecision    bool                     `json:"requires_user_decision,omitempty"`
	SilentInterruption      bool                     `json:"silent_interruption,omitempty"`
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
