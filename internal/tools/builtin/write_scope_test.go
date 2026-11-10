package builtin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

// Admission runs before a call is dispatched, so a write outside the scope would
// be stopped there and never reach the question. It is let through only where
// the question can actually be put at execution; everywhere else it stays refused.
func TestAdmissionLetsAWriteOutsideTheScopeThroughOnlyWhereSomeoneCanBeAsked(t *testing.T) {
	root := testenv.TempDir(t)
	outside := filepath.Join(testenv.TempDir(t), "wsl.conf")
	ws := Workspace{Dir: root, WriteRoots: []string{root}}
	writer := ws.Tools("write_file")[0]
	args := argsJSON(t, map[string]any{"path": outside, "content": "x\n"})
	check := ws.TargetAccessCheck()

	err := check(context.Background(), writer, args)
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeWriteOutsideScope {
		t.Fatalf("with nobody to ask, admission must refuse as before, got %v", err)
	}

	asking := tool.WithConfigWriteApprover(context.Background(), &stubScopeApprover{})
	if err := check(asking, writer, args); err != nil {
		t.Fatalf("with an approver on the context the question belongs to execution, got %v", err)
	}

	onlyConfig := tool.WithConfigWriteApprover(context.Background(), &stubConfigWriteApprover{allow: true})
	if err := check(onlyConfig, writer, args); err == nil {
		t.Fatal("an approver that only knows config writes must not admit a write outside the scope")
	}
}
