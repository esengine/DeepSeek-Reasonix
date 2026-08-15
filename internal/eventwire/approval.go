package eventwire

import "reasonix/internal/event"

// Approval is the JSON form of an event.Approval.
type Approval struct {
	ID          string               `json:"id"`
	Tool        string               `json:"tool"`
	Subject     string               `json:"subject" externalizable:"true"`
	Reason      string               `json:"reason,omitempty" externalizable:"true"`
	Fresh       bool                 `json:"fresh,omitempty"`
	Kind        string               `json:"kind,omitempty"` // tool | plan | recovery | write_access
	Recovery    *RecoveryApproval    `json:"recovery,omitempty"`
	WriteAccess *WriteAccessApproval `json:"write_access,omitempty"`
}

type WriteAccessApproval struct {
	Directories              []string `json:"directories"`
	DisplayDirectories       []string `json:"display_directories"`
	Justification            string   `json:"justification,omitempty"`
	BroadHomeAccess          bool     `json:"broad_home_access,omitempty"`
	OrdinaryPermissionNeeded bool     `json:"ordinary_permission_needed,omitempty"`
	PersistAllowed           bool     `json:"persist_allowed,omitempty"`
}

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
	w := &Approval{ID: a.ID, Tool: a.Tool, Subject: a.Subject, Reason: a.Reason, Fresh: a.Fresh, Kind: a.Kind}
	if wa := event.NormalizeWriteAccessApproval(a.WriteAccess); wa != nil {
		w.WriteAccess = &WriteAccessApproval{
			Directories: append([]string{}, wa.Directories...), DisplayDirectories: append([]string{}, wa.DisplayDirectories...),
			Justification: wa.Justification, BroadHomeAccess: wa.BroadHomeAccess,
			OrdinaryPermissionNeeded: wa.OrdinaryPermissionNeeded, PersistAllowed: wa.PersistAllowed,
		}
	}
	if r := a.Recovery; r != nil {
		w.Recovery = &RecoveryApproval{
			SourceAgent: r.SourceAgent, FailedTool: r.FailedTool, FailedSummary: r.FailedSummary, Diagnosis: r.Diagnosis,
			NextTool: r.NextTool, NextAction: r.NextAction, ChangeKind: r.ChangeKind, ChangeRationale: r.ChangeRationale,
			ReviewRationale: r.ReviewRationale, PlanBefore: r.PlanBefore, PlanAfter: r.PlanAfter,
			CanGrantTask: r.CanGrantTask, TaskGrantScope: r.TaskGrantScope,
		}
	}
	return w
}
