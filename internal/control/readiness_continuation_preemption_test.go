package control

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

func TestReadinessContinuationYieldsToUserWorkQueuedAtRunBoundary(t *testing.T) {
	var c *Controller
	var enqueueErr error
	enqueued := false
	sink := event.FuncSink(func(e event.Event) {
		if c == nil || enqueued || e.Kind != event.Notice || e.Text != i18n.M.ReadinessContinuing {
			return
		}
		enqueued = true
		_, enqueueErr = c.EnqueueInbox(InboxRequest{Submit: "new user work"})
	})
	c, prov := readinessContinuationController(t, [][]provider.Chunk{
		textTurn("I am preparing the implementation."),
	}, sink)
	c.SetSessionPath(filepath.Join(t.TempDir(), "session.jsonl"))
	if err := c.SetQualityFloor(QualityFloorStandard); err != nil {
		t.Fatalf("SetQualityFloor: %v", err)
	}

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(
		context.Background(), "update main.go", "update main.go", "",
	)
	if enqueueErr != nil {
		t.Fatalf("enqueue at continuation boundary: %v", enqueueErr)
	}
	if !enqueued {
		t.Fatal("continuation boundary notice was not observed")
	}
	var readinessErr *agent.FinalReadinessError
	if !errors.As(err, &readinessErr) || readinessErr.Attempts != 1 {
		t.Fatalf("continuation error = %v, want the original readiness failure", err)
	}
	if got := readinessSyntheticTurns(c); got != 0 {
		t.Fatalf("synthetic readiness turns = %d, want 0 after queued user work", got)
	}
	if prov.call != 1 {
		t.Fatalf("provider calls = %d, want only the original model turn", prov.call)
	}
	if !c.executor.PrepareFinalReadinessRecovery() {
		t.Fatal("yielded continuation consumed the manual recovery action")
	}
}
