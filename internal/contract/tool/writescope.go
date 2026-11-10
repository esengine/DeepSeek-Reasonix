package tool

import (
	"context"
	"errors"
)

// ErrDeleteTargetProtected is what a DeleteScopeApprover returns for a target no
// approval may open (Reasonix's own storage, a credential directory, a home
// directory, a filesystem root), so the caller refuses without anyone being asked.
var ErrDeleteTargetProtected = errors.New("delete target is protected")

// WriteScopeRequest describes a file-tool write whose target lies outside the
// workspace write scope (workspace_root plus allow_write and granted roots).
type WriteScopeRequest struct {
	// Path is the resolved absolute target the tool wants to write.
	Path string
	// Dir is the folder a "this folder" answer would cover: the target's parent.
	Dir string
}

// WriteScopeApprover asks the user whether one write outside the write scope may
// proceed. It is a fresh human decision: YOLO/auto approval must not answer it,
// and a context with no approver (headless runs, sub-agent loops with no
// interactive parent) leaves the write refused.
type WriteScopeApprover interface {
	ApproveWriteScope(ctx context.Context, req WriteScopeRequest) (allow bool, reason string, err error)
}

// WriteScopeSessionChecker reports whether the folder of a write has already
// been allowed for this session, so the user is not asked again.
type WriteScopeSessionChecker interface {
	WriteScopeSessionAllowed(ctx context.Context, req WriteScopeRequest) bool
}

// WriteScopeApproverFrom returns the write-scope approver carried by ctx. It
// rides on the same context value as the managed-config approver, so every
// agent that already passes one down passes this too.
func WriteScopeApproverFrom(ctx context.Context) (WriteScopeApprover, bool) {
	approver, ok := ConfigWriteApproverFrom(ctx)
	if !ok {
		return nil, false
	}
	scope, ok := approver.(WriteScopeApprover)
	return scope, ok
}

// DeleteScopeRequest describes a recursive delete whose targets lie outside the
// workspace and its granted folders, and are not ones nobody may delete (home, a
// filesystem root, an ancestor of the workspace).
type DeleteScopeRequest struct {
	// Paths are the resolved absolute targets the command would delete.
	Paths []string
}

// DeleteScopeApprover asks the user whether one recursive delete may run. It is a
// fresh human decision and covers this command only: there is no folder grant,
// because a grant for a parent would let the next delete reach everything in it.
type DeleteScopeApprover interface {
	ApproveDeleteScope(ctx context.Context, req DeleteScopeRequest) (allow bool, reason string, err error)
}

// DeleteScopeApproverFrom returns the delete-scope approver carried by ctx, on
// the same context value as the write-scope one.
func DeleteScopeApproverFrom(ctx context.Context) (DeleteScopeApprover, bool) {
	approver, ok := ConfigWriteApproverFrom(ctx)
	if !ok {
		return nil, false
	}
	scope, ok := approver.(DeleteScopeApprover)
	return scope, ok
}
