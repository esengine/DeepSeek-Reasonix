package control

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
)

func TestStaleWriteAuthorityBlocksAsyncAdmission(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	events := make(chan event.Event, 1)
	c := New(Options{
		Executor:    exec,
		SessionPath: path,
		Sink: event.FuncSink(func(e event.Event) {
			select {
			case events <- e:
			default:
			}
		}),
	})
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := c.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.IssueWriteAuthority(agent.NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	ran := false
	if got := c.runGuarded(func(context.Context) error { ran = true; return nil }); got != turnDroppedWriteAuthority {
		t.Fatalf("admission = %v, want turnDroppedWriteAuthority", got)
	}
	if ran {
		t.Fatal("turn body ran with stale authority")
	}
	e := <-events
	if e.Kind != event.Notice || e.Level != event.LevelWarn || !strings.Contains(e.Text, "reopen") {
		t.Fatalf("notice = %+v", e)
	}
}

func TestStaleWriteAuthorityBlocksSynchronousRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	sess := agent.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionPath: path, Sink: event.Discard})
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := c.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.IssueWriteAuthority(agent.NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background(), "must not run"); !errors.Is(err, agent.ErrSessionWriteAuthorityStale) {
		t.Fatalf("Run error = %v, want stale authority", err)
	}
}
