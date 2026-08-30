package sandbox

// ApprovalScope is the lifetime of a write-access grant.
type ApprovalScope string

const (
	ApprovalScopeOnce    ApprovalScope = "once"
	ApprovalScopeSession ApprovalScope = "session"
	ApprovalScopeProject ApprovalScope = "project"
)

// WriteAccessRequest is the host-normalized expansion request shown to the user.
type WriteAccessRequest struct {
	Directories              []string
	DisplayDirectories       []string
	Justification            string
	BroadHomeAccess          bool
	OrdinaryPermissionNeeded bool
}

// WriteAccessGrant is the user's decision after a write-access prompt.
type WriteAccessGrant struct {
	Scope       ApprovalScope
	Directories []string
}

// ParseApprovalScope maps a wire/API string to a scope. Unknown values are once.
func ParseApprovalScope(s string) ApprovalScope {
	switch ApprovalScope(s) {
	case ApprovalScopeSession, ApprovalScopeProject, ApprovalScopeOnce:
		return ApprovalScope(s)
	default:
		return ApprovalScopeOnce
	}
}
