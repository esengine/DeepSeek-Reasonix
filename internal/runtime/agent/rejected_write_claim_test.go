package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/builtin"
)

func TestRejectedParentWriteDoesNotRetainCrossSessionClaim(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "shared.txt")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	owner, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := workspacelease.New(root, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner.BeginRun()
	defer owner.EndRun()
	other.BeginRun()
	defer other.EndRun()
	sched := writeclaim.NewSubagentScheduler(0, 0)
	claim, err := writeclaim.NormalizeWritePaths(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	release, err := sched.Acquire(context.Background(), writeclaim.AcquireRequest{Writer: true, WritePaths: claim})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	reg := tool.NewRegistry()
	for _, writer := range builtin.ConfineWriters([]string{root}, builtin.SessionDataGuard{}, builtin.ManagedConfigPaths{}) {
		reg.Add(writer)
	}
	a := New(nil, reg, sessionstore.NewSession("sys"), Options{
		WorkspaceLease: owner, WriteScheduler: sched, WriteWorkspaceRoot: root, CheckTargetAccess: allowTestFileAccess,
	}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{
		ID: "parent-write", Name: "write_file", Arguments: string(mustJSON(t, map[string]string{"path": path, "content": "parent"})),
	})
	if out.refusalCode != writeclaim.CodeWritePathBusy {
		t.Fatalf("unexpected outcome: %+v", out)
	}
	release()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "original" {
		t.Fatalf("file changed: %q, %v", got, err)
	}
	if owner.State().Acquired {
		t.Fatal("a refused write retained an unused cross-session claim")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := other.AcquirePaths(ctx, []string{path}); err != nil {
		t.Fatalf("unexpected acquisition: %v", err)
	}
	owner.EndRun()
	if err := other.AcquirePaths(context.Background(), []string{path}); err != nil {
		t.Fatal(err)
	}

}
