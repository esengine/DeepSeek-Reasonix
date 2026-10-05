package control

import (
	"context"
	"errors"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessioninbox"
)

func TestStaleWriteAuthorityBlocksAsyncAdmission(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	sess := sessionstore.NewSession("sys")
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
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := c.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration()); err != nil {
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
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	sess := sessionstore.NewSession("sys")
	exec := agent.New(nil, nil, sess, agent.Options{}, event.Discard)
	c := New(Options{Executor: exec, SessionPath: path, Sink: event.Discard})
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := c.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	if err := c.Run(context.Background(), "must not run"); !errors.Is(err, sessionstore.ErrSessionWriteAuthorityStale) {
		t.Fatalf("Run error = %v, want stale authority", err)
	}
}

func TestStaleWriteAuthorityHoldsInboxWithoutRepeatedAdmission(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	sess := sessionstore.NewSession("sys")
	runner := &inboxDispatchRunner{inputs: make(chan string, 1)}
	events := make(chan event.Event, 16)
	c := New(Options{
		Executor:    agent.New(nil, nil, sess, agent.Options{}, event.Discard),
		Runner:      runner,
		SessionPath: path,
		Sink:        event.FuncSink(func(e event.Event) { events <- e }),
	})
	defer c.Close()
	lease, err := sessionstore.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if err := c.BindSessionWriteAuthority(lease); err != nil {
		t.Fatal(err)
	}
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentFollowup, Submit: "keep this line"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.IssueWriteAuthority(sessionstore.NextSessionWriteGeneration()); err != nil {
		t.Fatal(err)
	}
	got, err := c.TrySubmitInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != sessioninbox.DispositionRejectedClosed || !got.Paused {
		t.Fatalf("receipt = %+v", got)
	}
	snap := c.InboxSnapshot()
	if !snap.Paused || len(snap.Items) != 1 || snap.Items[0].State != sessioninbox.StateQueued {
		t.Fatalf("held inbox = %+v", snap)
	}
	select {
	case input := <-runner.inputs:
		t.Fatalf("read-only inbox item ran: %q", input)
	default:
	}
	deadline := time.After(25 * time.Millisecond)
	for {
		select {
		case e := <-events:
			if e.Kind == event.Notice && strings.Contains(e.Text, "no longer writable") {
				t.Fatalf("read-only queue emitted transcript notice: %+v", e)
			}
		case <-deadline:
			return
		}
	}
}
