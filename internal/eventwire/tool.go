package eventwire

import (
	"encoding/json"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Tool is the JSON form of an event.Tool.
type Tool struct {
	RunState          provider.ToolRunState `json:"runState,omitempty"`
	Diagnostic        json.RawMessage       `json:"diagnostic,omitempty"`
	Verifying         bool                  `json:"verifying,omitempty"`
	ID                string                `json:"id,omitempty"`
	Name              string                `json:"name"`
	Args              string                `json:"args,omitempty" externalizable:"true"`
	ResolvedName      string                `json:"resolvedName,omitempty"`
	CapabilityID      string                `json:"capabilityId,omitempty"`
	Output            string                `json:"output,omitempty" externalizable:"true"`
	Err               string                `json:"err,omitempty" externalizable:"true"`
	ReadOnly          bool                  `json:"readOnly"`
	Truncated         bool                  `json:"truncated,omitempty"`
	DurationMs        int64                 `json:"durationMs,omitempty"`
	StartedAt         int64                 `json:"startedAt,omitempty"` // unix ms; zero when the call never ran
	EndedAt           int64                 `json:"endedAt,omitempty"`
	Partial           bool                  `json:"partial,omitempty"`
	ArgChars          int                   `json:"argChars,omitempty"`
	Refreshed         bool                  `json:"refreshed,omitempty"`
	ParentID          string                `json:"parentId,omitempty"`
	AttemptID         string                `json:"attemptId,omitempty"` // host-local stream_attempt id for speculative partials
	SubagentRef       string                `json:"subagentRef,omitempty"`
	SubagentStatus    string                `json:"subagentStatus,omitempty"`
	SubagentErrorCode string                `json:"subagentErrorCode,omitempty"`
	SubagentRetryable bool                  `json:"subagentRetryable,omitempty"`
	Diff              string                `json:"diff,omitempty" externalizable:"true"`
	Added             int                   `json:"added,omitempty"`
	Removed           int                   `json:"removed,omitempty"`
	Profile           *Profile              `json:"profile,omitempty"`
	Execution         *ShellExecution       `json:"execution,omitempty"`
}

func toWireTool(in event.Tool) *Tool {
	wt := &Tool{
		RunState:   in.RunState,
		Diagnostic: append(json.RawMessage(nil), in.Diagnostic...),
		ID:         in.ID, Name: in.Name, Args: in.Args,
		ResolvedName: in.ResolvedName, CapabilityID: in.CapabilityID,
		Output: in.Output, Err: in.Err,
		ReadOnly: in.ReadOnly, Truncated: in.Truncated,
		Verifying:  in.Verifying,
		DurationMs: in.DurationMs, Partial: in.Partial,
		StartedAt: in.StartedAt, EndedAt: in.EndedAt,
		ArgChars: in.ArgChars, Refreshed: in.Refreshed,
		ParentID: in.ParentID, AttemptID: in.AttemptID,
		Diff: in.Diff, Added: in.Added, Removed: in.Removed,
		SubagentRef: in.SubagentRef, SubagentStatus: in.SubagentStatus,
		SubagentErrorCode: in.SubagentErrorCode, SubagentRetryable: in.SubagentRetryable,
	}
	if in.Profile != nil {
		wt.Profile = &Profile{Model: in.Profile.Model, Effort: in.Profile.Effort}
	}
	if in.Execution != nil {
		wt.Execution = toWireShellExecution(in.Execution)
	}
	return wt
}
