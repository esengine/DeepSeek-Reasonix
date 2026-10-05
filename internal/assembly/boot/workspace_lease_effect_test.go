package boot

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/workspacelease"
)

// A process a session leaves running — a dev server, a watcher — used to hold
// the workspace writer lease until it exited, which for a dev server is never:
// the next session's first write waited behind it for something that was never
// going to end. The lease ends with the run that took it, and this asserts it
// through the real session assembly rather than the lease alone.
func TestBackgroundJobDoesNotPinTheWorkspaceForTheNextSession(t *testing.T) {
	home := robustTempDir(t)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	root := robustTempDir(t)

	session, err := startSessionRuntime(Options{SessionDir: robustTempDir(t)}, config.Default(), root, event.Discard)
	if err != nil {
		t.Fatalf("startSessionRuntime: %v", err)
	}
	running := make(chan struct{})
	defer session.jobs.Close()
	defer close(running)

	session.lease.BeginRun()
	if err := session.lease.AcquireWrite(context.Background()); err != nil {
		t.Fatalf("first write of the turn: %v", err)
	}
	session.jobs.Start("bash", "npm run dev", func(ctx context.Context, _ io.Writer) (string, error) {
		select {
		case <-running:
		case <-ctx.Done():
		}
		return "", nil
	})
	session.lease.EndRun()

	next, err := workspacelease.New(root, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatalf("next session lease: %v", err)
	}
	next.BeginRun()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := next.AcquireWrite(ctx); err != nil {
		t.Fatalf("a background job pinned the workspace for the next session: %v", err)
	}
	next.EndRun()
}

// The wait a session is told about is the wait it can act on: the open is a
// warning because the turn is stopped for the length of it, and every close
// carries how long that was.
func TestWorkspaceLeaseNoticePairsOpenWithClose(t *testing.T) {
	open := workspaceLeaseNotice(workspacelease.Wait{Outcome: workspacelease.WaitBegan})
	if open.Level != event.LevelWarn || open.Code != event.NoticeCodeWorkspaceLease {
		t.Fatalf("open notice = %+v, want a warning coded %q", open, event.NoticeCodeWorkspaceLease)
	}
	for _, tc := range []struct {
		outcome workspacelease.WaitOutcome
		code    string
	}{
		{workspacelease.WaitAcquired, event.NoticeCodeWorkspaceLeaseResumed},
		{workspacelease.WaitAbandoned, event.NoticeCodeWorkspaceLeaseAbandoned},
	} {
		got := workspaceLeaseNotice(workspacelease.Wait{Outcome: tc.outcome, Elapsed: 3200 * time.Millisecond})
		if got.Code != tc.code {
			t.Fatalf("close code = %q, want %q", got.Code, tc.code)
		}
		if got.Level != event.LevelInfo {
			t.Fatalf("close level = %v, want info", got.Level)
		}
		if !strings.Contains(got.Detail, "3.2s") {
			t.Fatalf("close detail = %q, want the measured wait in it", got.Detail)
		}
	}
}
