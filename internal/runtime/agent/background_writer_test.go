package agent

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/tools/jobs"
)

func startBlockedJob(t *testing.T, m *jobs.Manager, session string) (release func()) {
	t.Helper()
	gate := make(chan struct{})
	m.StartForSession(session, "bash", "watch", func(ctx context.Context, _ io.Writer) (string, error) {
		select {
		case <-gate:
		case <-ctx.Done():
		}
		return "", nil
	})
	var once sync.Once
	release = func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return release
}

func TestRunningBackgroundJobIsAnUnseenWriter(t *testing.T) {
	steps := stepsByName(t, "code", "test-pass", "doc")
	t.Run("running job keeps prose after a passing check owed", func(t *testing.T) {
		m := jobs.NewManager(event.Discard)
		startBlockedJob(t, m, "s1")
		got := proseVerdictOf(t, proseWorkspace(t), steps, func(a *Agent) {
			a.svc.jobs, a.turn.jobSession = m, "s1"
		})
		if !got.owed() {
			t.Error("prose after a passing check was waived while a background job still runs")
		}
	})
	t.Run("finished job is not a writer", func(t *testing.T) {
		m := jobs.NewManager(event.Discard)
		release := startBlockedJob(t, m, "s1")
		release()
		deadline := time.Now().Add(5 * time.Second)
		for m.HasUnfinishedForSession("s1") && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		got := proseVerdictOf(t, proseWorkspace(t), steps, func(a *Agent) {
			a.svc.jobs, a.turn.jobSession = m, "s1"
		})
		if got.owed() {
			t.Error("a finished job still blocked the prose waiver")
		}
	})
	t.Run("another session's job is not this turn's writer", func(t *testing.T) {
		m := jobs.NewManager(event.Discard)
		startBlockedJob(t, m, "other")
		got := proseVerdictOf(t, proseWorkspace(t), steps, func(a *Agent) {
			a.svc.jobs, a.turn.jobSession = m, "s1"
		})
		if got.owed() {
			t.Error("a job owned by another session blocked the prose waiver")
		}
	})
}
