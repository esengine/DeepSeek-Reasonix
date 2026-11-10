package builtin

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessiontemp"
)

const writeScopeDeclined = "the user declined this write outside the workspace; do not retry it, ask how they would like to proceed"

// askWriteScope puts a write outside the write scope to the user, through the
// approver carried on ctx. Without one, or with one that does not ask this
// question, the original refusal stands: a headless run or a sub-agent with no
// interactive parent can never widen its own scope. The session-data guard is
// checked before the user is asked, so an answer never lifts it.
func askWriteScope(ctx context.Context, guard SessionDataGuard, temp *sessiontemp.Manager, refusal error, target string) error {
	refused := withSessionTempAlternative(temp, refusal)
	approver, ok := tool.WriteScopeApproverFrom(ctx)
	if !ok {
		return refused
	}
	abs, err := realPath(target)
	if err != nil {
		return refused
	}
	if err := guard.Check(target); err != nil {
		return err
	}
	req := tool.WriteScopeRequest{Path: abs, Dir: filepath.Dir(abs)}
	if checker, ok := approver.(tool.WriteScopeSessionChecker); ok && checker.WriteScopeSessionAllowed(ctx, req) {
		return nil
	}
	allow, reason, err := approver.ApproveWriteScope(ctx, req)
	if err != nil {
		return err
	}
	if !allow {
		if strings.TrimSpace(reason) == "" {
			reason = writeScopeDeclined
		}
		return tool.Refusal{Code: CodeWriteOutsideScope, Message: reason}
	}
	return nil
}

// admitsForQuestion is whether an admission refusal is only the write-scope one
// and execution can put it to a person. Anything else a write can be refused for
// (a network path, the session-data guard) is not something an answer fixes, and
// execution checks those again before it asks.
func admitsForQuestion(ctx context.Context, err error) bool {
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeWriteOutsideScope {
		return false
	}
	_, ok := tool.WriteScopeApproverFrom(ctx)
	return ok
}
