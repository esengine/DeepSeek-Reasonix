package builtin

import (
	"context"
	"errors"
	"os"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessiontemp"
	"reasonix/internal/tools/jobs"
)

func TestBashRejectedBackgroundStartReleasesTemporaryLease(t *testing.T) {
	temps := sessiontemp.NewWithRoot(t.TempDir())
	temps.Retain()
	defer temps.Release()
	jm := jobs.NewManager(event.Discard)
	jm.Close()
	ctx := jobs.WithSession(jobs.WithManager(t.Context(), jm), "session")
	b := bash{sb: sandbox.Spec{Mode: "off"}, workDir: t.TempDir(), sessionTemp: temps}
	out, err := b.ExecuteDetailed(ctx, argsJSON(t, map[string]any{"command": "echo ready", "run_in_background": true}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("closed manager accepted command: %+v %v", out, err)
	}
	if out.Execution.State != tool.ShellStateNotRun || out.Execution.MutationRisk != tool.ShellMutationNotStarted {
		t.Fatalf("unstarted command reported as executed: %+v", out.Execution)
	}
	dir := temps.Dir()
	temps.Rotate()
	if dir == "" {
		t.Fatal("temporary lease was never acquired")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("unstarted command retained temporary generation: %v", err)
	}
}
