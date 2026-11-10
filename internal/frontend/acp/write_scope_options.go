package acp

import (
	"path/filepath"

	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

// permissionOptions is the answer list a client is offered for one approval.
func permissionOptions(a event.Approval) []PermissionOption {
	if a.Tool == control.WriteScopeApprovalTool {
		return writeScopeOptions(a.Subject, a.AllowsSession, a.AllowsPersist)
	}
	return approvalOptions(a.Tool, a.Subject, a.Fresh, a.AllowsPersist)
}

// writeScopeOptions is the answer list for a write outside the workspace scope.
// The card's subject is the file; a wider answer covers its folder, so the folder
// is named in the choices. Which wider answers appear is the host's own
// statement of what it will honour for that folder.
func writeScopeOptions(subject string, session, persist bool) []PermissionOption {
	dir := filepath.Dir(subject)
	options := []PermissionOption{{OptionID: string(OptAllowOnce), Name: "Allow this write", Kind: OptAllowOnce}}
	if session {
		options = append(options, PermissionOption{OptionID: string(OptAllowAlways), Name: "Allow writes under " + dir + " for this session", Kind: OptAllowAlways})
	}
	if persist {
		options = append(options, PermissionOption{OptionID: string(optAllowPersist), Name: "Always allow writes under " + dir + " in this project (saved to your settings)", Kind: OptAllowAlways})
	}
	return append(options, PermissionOption{OptionID: string(OptRejectOnce), Name: "Reject", Kind: OptRejectOnce})
}
