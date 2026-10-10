package boot

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestEffectApprovalReceivesAdmittedPreview(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeUserConfig(t, "[sandbox]\nbash = \"off\"\n")
	writeFile(t, dir, "note.txt", "status=draft\n")
	call := provider.ToolCall{ID: "approval-edit", Name: "edit_file", Arguments: `{"path":"note.txt","old_string":"draft","new_string":"ready"}`}
	rec := &postureProvider{calls: []provider.ToolCall{call}}
	postureRegister.Do(func() {
		provider.Register("boot-posture", func(provider.Config) (provider.Provider, error) {
			postureMu.Lock()
			defer postureMu.Unlock()
			return postureCurrent, nil
		})
	})
	postureMu.Lock()
	postureCurrent = rec
	postureMu.Unlock()
	writeFile(t, dir, "reasonix.toml", `default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-posture"
model = "x"
`)
	approveWorkspace(t, dir)
	var ctrl *control.Controller
	var ready sync.WaitGroup
	ready.Add(1)
	var mu sync.Mutex
	latestDiff, approvalDiff := "", ""
	approvals := 0
	sink := event.FuncSink(func(e event.Event) {
		mu.Lock()
		if e.Kind == event.ToolDispatch && e.Tool.ID == call.ID {
			latestDiff = e.Tool.Diff
		}
		if e.Kind == event.ApprovalRequest {
			approvalDiff = latestDiff
			approvals++
		}
		mu.Unlock()
		if e.Kind == event.ApprovalRequest {
			id := e.Approval.ID
			go func() { ready.Wait(); ctrl.Approve(id, false, false, false) }()
		}
	})
	var err error
	ctrl, err = Build(context.Background(), Options{Sink: sink, SessionDir: filepath.Join(dir, "sessions")})
	ready.Done()
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.SetFreshSessionPath(sessionstore.NewSessionPath(ctrl.SessionDir(), ctrl.Label()))
	ctrl.SetToolApprovalMode(control.ToolApprovalAsk)
	ctrl.EnableInteractiveApproval()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := ctrl.Run(ctx, "edit note.txt"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if approvals != 1 || !strings.Contains(approvalDiff, "-status=draft") || !strings.Contains(approvalDiff, "+status=ready") {
		t.Fatalf("real approval had no file diff: approvals=%d diff=%q", approvals, approvalDiff)
	}
}
