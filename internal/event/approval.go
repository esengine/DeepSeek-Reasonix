package event

import "encoding/json"

// Approval identifies a pending tool-call approval for an ApprovalRequest
// event. ID correlates the request with the controller's Approve(ID, …) reply.
type Approval struct {
	ID      string
	Tool    string
	Subject string
	Reason  string // optional annotation explaining why approval is needed
	// RawInput is the exact structured tool input. ACP permission clients use it
	// together with locations/reason instead of parsing a human title.
	RawInput json.RawMessage
	Fresh    bool // current human decision required; do not offer remembered grants
	// Kind classifies the approval surface: "tool" (default), "plan", "recovery",
	// or "write_access". Empty means ordinary permission for backward compat.
	Kind        string
	Recovery    *RecoveryApproval
	WriteAccess *WriteAccessApproval
}

// ApprovalKindWriteAccess is the Approval.Kind value for directory expansion.
const ApprovalKindWriteAccess = "write_access"

// WriteAccessApproval is the backward-compatible structured payload for
// extending writable roots. Slices are never nil on the wire.
type WriteAccessApproval struct {
	Directories              []string `json:"directories"`
	DisplayDirectories       []string `json:"display_directories"`
	Justification            string   `json:"justification,omitempty"`
	BroadHomeAccess          bool     `json:"broad_home_access,omitempty"`
	OrdinaryPermissionNeeded bool     `json:"ordinary_permission_needed,omitempty"`
	PersistAllowed           bool     `json:"persist_allowed,omitempty"`
}

// NormalizeWriteAccessApproval makes list fields non-nil for Wails/JSON.
func NormalizeWriteAccessApproval(w *WriteAccessApproval) *WriteAccessApproval {
	if w == nil {
		return nil
	}
	if w.Directories == nil {
		w.Directories = []string{}
	}
	if w.DisplayDirectories == nil {
		w.DisplayDirectories = []string{}
	}
	return w
}

// RecoveryApproval is the backward-compatible structured payload for Auto
// Guard decisions. Old clients can ignore this nested object safely.
type RecoveryApproval struct {
	SourceAgent     string // agent that proposed the next mutation
	FailedTool      string // tool that failed; empty for pre-action boundaries
	FailedSummary   string // short failure/error summary; optional
	Diagnosis       string // agent/host diagnosis when failure recovery is active
	NextTool        string // tool about to run
	NextAction      string // concrete next command/file change/MCP action
	ChangeKind      string // same_strategy | strategy | scope | risk | uncertain
	ChangeRationale string // what changed vs the original approach
	ReviewRationale string // why the host/reviewer needs confirmation
	PlanBefore      string // active structured plan before a material transition
	PlanAfter       string // proposed structured plan after a material transition
	CanGrantTask    bool   // offer a semantic grant scoped to the current task
	TaskGrantScope  string // concise host-classified operation + exact target
}
