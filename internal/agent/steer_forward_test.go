package agent

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/jobs"
)

func TestHasForwardMarker(t *testing.T) {
	pos := []string{"→ 用测试驱动重写 parser", "注入：换个方向", "inject: focus on errors", "告诉子智能体检查边界", "-> check the edge cases", "Steer subagent to use pnpm"}
	neg := []string{"继续之前的任务", "这个报错怎么解决", "→", "", "inject", "subagent"}
	for _, s := range pos {
		if !HasForwardMarker(s) {
			t.Errorf("HasForwardMarker(%q) = false, want true", s)
		}
	}
	for _, s := range neg {
		if HasForwardMarker(s) {
			t.Errorf("HasForwardMarker(%q) = true, want false", s)
		}
	}
}

func TestSteerForwardRoutesToLatestRunningSubagent(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	f := NewSteerForwarder(m)

	// First subagent job (older).
	older, releaseOlder := startBlockedJob(t, m, "sess-1")
	defer releaseOlder()
	time.Sleep(20 * time.Millisecond)
	// Second, newer subagent job — the forward target.
	newer, releaseNewer := startBlockedJob(t, m, "sess-1")
	defer releaseNewer()

	got := make(chan string, 4)
	newer.SetSteer(func(s string) bool { got <- s; return true })
	older.SetSteer(func(s string) bool { return true })

	if !f.Forward("sess-1", "→ 改成异步重试") {
		t.Fatal("Forward with marker + running subagent must return true")
	}
	select {
	case s := <-got:
		if s != "改成异步重试" {
			t.Errorf("forwarded text = %q, want %q (marker stripped)", s, "改成异步重试")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("steer never reached the newer subagent")
	}
}

func TestSteerForwardFallsBackWithoutMarker(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	f := NewSteerForwarder(m)
	_, release := startBlockedJob(t, m, "sess-1")
	defer release()
	if f.Forward("sess-1", "普通消息不转发") {
		t.Fatal("no marker must never forward")
	}
}

func TestSteerForwardFallsBackWithoutTarget(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	f := NewSteerForwarder(m)
	if f.Forward("sess-1", "→ 没有运行中的子智能体") {
		t.Fatal("marker but no running subagent must fall back")
	}
}

func TestSteerForwardSkipsNonTaskJobs(t *testing.T) {
	m := jobs.NewManager(event.Discard)
	f := NewSteerForwarder(m)
	// A running bash job is not a subagent — never a forward target.
	run, release, started := blockedJobRun()
	m.StartForSession("sess-1", "bash", "cmd", run)
	<-started
	defer release()
	if f.Forward("sess-1", "→ 只有 bash job 在跑") {
		t.Fatal("running bash job must not be a steer target")
	}
}

func TestSteerForwardNilManager(t *testing.T) {
	f := NewSteerForwarder(nil)
	if f.Forward("sess-1", "→ 无 manager") {
		t.Fatal("nil manager must not forward")
	}
}

// --- helpers ---

func blockedJobRun() (func(ctx context.Context, out io.Writer) (string, error), func(), <-chan struct{}) {
	started := make(chan struct{})
	release := make(chan struct{})
	once := sync.Once{}
	return func(ctx context.Context, out io.Writer) (string, error) {
		close(started)
		<-release
		return "done", nil
	}, func() { once.Do(func() { close(release) }) }, started
}

func startBlockedJob(t *testing.T, m *jobs.Manager, session string) (*jobs.Job, func()) {
	t.Helper()
	run, release, started := blockedJobRun()
	j := m.StartForSession(session, "task", "sub", run)
	<-started
	t.Cleanup(func() { j.SetSteer(nil) })
	return j, release
}
