package jobs

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
)

// blockedRun keeps a job Running until the returned release func is called.
func blockedRun() (func(ctx context.Context, out io.Writer) (string, error), func(), <-chan struct{}) {
	started := make(chan struct{})
	release := make(chan struct{})
	once := sync.Once{}
	return func(ctx context.Context, out io.Writer) (string, error) {
		close(started)
		<-release
		return "done", nil
	}, func() { once.Do(func() { close(release) }) }, started
}

func TestSteerJobForwardsToRunningSubagent(t *testing.T) {
	m := NewManager(event.Discard)
	run, release, started := blockedRun()
	j := m.StartForSession("sess-1", "task", "sub", run)
	<-started

	got := make(chan string, 1)
	j.SetSteer(func(text string) bool { got <- text; return true })

	if ok := m.SteerJob("sess-1", j.ID, "use tests first"); !ok {
		t.Fatal("SteerJob on a running subagent with steer callback must return true")
	}
	select {
	case s := <-got:
		if s != "use tests first" {
			t.Errorf("steer text = %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("steer callback never invoked")
	}
	release()
}

func TestSteerJobFallsBackWhenNoSteer(t *testing.T) {
	m := NewManager(event.Discard)
	run, release, started := blockedRun()
	j := m.StartForSession("sess-1", "bash", "cmd", run) // not a subagent: no steer set
	<-started
	if ok := m.SteerJob("sess-1", j.ID, "hello"); ok {
		t.Fatal("SteerJob without steer callback must return false")
	}
	release()
}

func TestSteerJobFallsBackAfterCompletion(t *testing.T) {
	m := NewManager(event.Discard)
	run, release, started := blockedRun()
	j := m.StartForSession("sess-1", "task", "sub", run)
	<-started
	var received bool
	j.SetSteer(func(string) bool { received = true; return true })
	release()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, st, ok := m.OutputForSession("sess-1", j.ID)
		if ok && st == Done {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if ok := m.SteerJob("sess-1", j.ID, "late"); ok {
		t.Fatal("SteerJob after completion must return false")
	}
	if received {
		t.Fatal("completed job must not accept steers (callback cleared)")
	}
}

func TestSteerJobUnknownJob(t *testing.T) {
	m := NewManager(event.Discard)
	if ok := m.SteerJob("sess-1", "no-such-job", "x"); ok {
		t.Fatal("SteerJob for unknown job must return false")
	}
}

func TestSteerJobSessionScoped(t *testing.T) {
	m := NewManager(event.Discard)
	run, release, started := blockedRun()
	j := m.StartForSession("sess-1", "task", "sub", run)
	<-started
	j.SetSteer(func(string) bool { return true })
	// Wrong session must not see the job.
	if ok := m.SteerJob("sess-2", j.ID, "x"); ok {
		t.Fatal("SteerJob from another session must return false")
	}
	release()
}

var _ = context.Background // keep import stable
