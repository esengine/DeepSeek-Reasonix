package eventwire

import (
	"encoding/json"

	"reasonix/internal/contract/event"
)

// Approval is the JSON form of an event.Approval.
type Approval struct {
	ID         string `json:"id"`
	Tool       string `json:"tool"`
	Subject    string `json:"subject" externalizable:"true"`
	Reason     string `json:"reason,omitempty" externalizable:"true"`
	ReasonCode string `json:"reasonCode,omitempty"`
	Fresh      bool   `json:"fresh,omitempty"`
	// Which answers beyond "once" the host will honour for this call.
	AllowsSession bool              `json:"allowsSession,omitempty"`
	AllowsPersist bool              `json:"allowsPersist,omitempty"`
	Kind          string            `json:"kind,omitempty"` // tool | plan | recovery
	Recovery      *RecoveryApproval `json:"recovery,omitempty"`
	// Scope is what approving authorizes beyond the call; Input, the call's
	// arguments, rides only with a scope, since they are what it applies to.
	Scope string          `json:"scope,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// RecoveryApproval is the JSON form of an event.RecoveryApproval.
type RecoveryApproval struct {
	SourceAgent     string `json:"source_agent,omitempty"`
	FailedTool      string `json:"failed_tool,omitempty"`
	FailedSummary   string `json:"failed_summary,omitempty"`
	Diagnosis       string `json:"diagnosis,omitempty"`
	NextTool        string `json:"next_tool,omitempty"`
	NextAction      string `json:"next_action,omitempty"`
	ChangeKind      string `json:"change_kind,omitempty"`
	ChangeRationale string `json:"change_rationale,omitempty"`
	ReviewRationale string `json:"review_rationale,omitempty"`
	PlanBefore      string `json:"plan_before,omitempty"`
	PlanAfter       string `json:"plan_after,omitempty"`
	CanGrantTask    bool   `json:"can_grant_task,omitempty"`
	TaskGrantScope  string `json:"task_grant_scope,omitempty"`
}

func toWireApproval(a event.Approval) *Approval {
	out := &Approval{
		ID: a.ID, Tool: a.Tool, Subject: a.Subject,
		Reason: a.Reason, ReasonCode: a.ReasonCode, Fresh: a.Fresh, Kind: a.Kind,
		AllowsSession: a.AllowsSession, AllowsPersist: a.AllowsPersist,
	}
	if a.Scope != "" {
		out.Scope = a.Scope
		out.Input = append(json.RawMessage(nil), a.RawInput...)
	}
	if a.Recovery != nil {
		r := a.Recovery
		out.Recovery = &RecoveryApproval{
			SourceAgent:     r.SourceAgent,
			FailedTool:      r.FailedTool,
			FailedSummary:   r.FailedSummary,
			Diagnosis:       r.Diagnosis,
			NextTool:        r.NextTool,
			NextAction:      r.NextAction,
			ChangeKind:      r.ChangeKind,
			ChangeRationale: r.ChangeRationale,
			ReviewRationale: r.ReviewRationale,
			PlanBefore:      r.PlanBefore,
			PlanAfter:       r.PlanAfter,
			CanGrantTask:    r.CanGrantTask,
			TaskGrantScope:  r.TaskGrantScope,
		}
	}
	return out
}
