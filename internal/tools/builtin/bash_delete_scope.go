package builtin

import (
	"context"
	"errors"
	"slices"
	"strings"

	"reasonix/internal/contract/tool"
)

const destructiveTargetMessage = "Use a target strictly inside the workspace or granted write roots; a target outside the workspace needs the user's approval, which this session cannot ask for."

const protectedTargetMessage = "Use a target strictly inside the workspace or granted write roots; this path is protected (a filesystem root, a home directory, an ancestor of the workspace, or one that cannot be resolved) and no approval can open it."

const deleteScopeDeclined = "the user declined this recursive delete outside the workspace; do not retry it, ask how they would like to proceed"

type deleteClass int

const (
	// deleteProtected is what no answer makes deletable: a path that cannot be
	// judged, a filesystem root, the home directory or an ancestor of it, the
	// workspace or an ancestor of it or of a write root.
	deleteProtected deleteClass = iota
	// deleteInside is strictly inside a write root and needs nothing.
	deleteInside
	// deleteOutside is anywhere else: it may be deleted if the user says so.
	deleteOutside
)

func appendMissing(list []string, item string) []string {
	if slices.Contains(list, item) {
		return list
	}
	return append(list, item)
}

// askDeleteOutsideScope puts a recursive delete outside the workspace to the
// user, once for every target in the command. Without an approver that asks this
// question the original refusal stands, so a headless run cannot widen its own
// reach.
func askDeleteOutsideScope(ctx context.Context, paths []string) error {
	approver, ok := tool.DeleteScopeApproverFrom(ctx)
	if !ok {
		return deleteRefusal(CodeDestructiveTarget, destructiveTargetMessage)
	}
	allow, reason, err := approver.ApproveDeleteScope(ctx, tool.DeleteScopeRequest{Paths: paths})
	if errors.Is(err, tool.ErrScopeTargetProtected) {
		return deleteRefusal(CodeDestructiveTarget, protectedTargetMessage)
	}
	if err != nil {
		return err
	}
	if !allow {
		if strings.TrimSpace(reason) == "" {
			reason = deleteScopeDeclined
		}
		return deleteRefusal(CodeDestructiveTarget, reason)
	}
	return nil
}
