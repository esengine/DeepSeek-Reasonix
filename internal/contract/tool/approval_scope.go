package tool

// ApprovalScoper is a tool whose approval authorizes more than the call
// itself. ApprovalScope names what, as a code a frontend renders in the
// reader's language; the person approving has to be able to see it.
type ApprovalScoper interface {
	ApprovalScope() string
}

// ApprovalScopeUnattendedAttempts: approving starts attempts that make their
// own tool calls without asking again.
const ApprovalScopeUnattendedAttempts = "unattended_attempts"

// ApprovalScopeOf is t's declared scope, or "" when approving it authorizes
// the call alone.
func ApprovalScopeOf(t Tool) string {
	if s, ok := t.(ApprovalScoper); ok {
		return s.ApprovalScope()
	}
	return ""
}
