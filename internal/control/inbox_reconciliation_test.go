package control

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/tool"
)

func TestSteerEventFollowsDurableConsumedTransition(t *testing.T) {
	dir := t.TempDir()
	prov := &inboxSteerProvider{started: make(chan struct{}), release: make(chan struct{})}
	exec := agent.New(prov, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard)
	observed := make(chan sessioninbox.InboxState, 1)
	done := make(chan struct{})
	var c *Controller
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Steer && c != nil {
			state := sessioninbox.InboxState("")
			for _, item := range c.InboxSnapshot().Items {
				if item.ID == e.ItemID {
					state = item.State
					break
				}
			}
			observed <- state
		}
		if e.Kind == event.TurnDone {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})
	c = New(Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        sink,
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	t.Cleanup(func() {
		c.Close()
		c.autosaveWG.Wait()
	})
	c.Submit("initial turn")
	select {
	case <-prov.started:
	case <-time.After(time.Second):
		t.Fatal("initial provider turn did not start")
	}
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "durable steer"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.TrySteerInboxItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	close(prov.release)
	select {
	case state := <-observed:
		if state != sessioninbox.StateSteerConsumed {
			t.Fatalf("state at steer event = %q, want %q", state, sessioninbox.StateSteerConsumed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for steer event")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for turn completion")
	}
}

func TestCancelWithInboxItemsResultRestoresOnlyUnconsumedItems(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	accepted, err := c.EnqueueInbox(InboxRequest{Submit: "accepted", Source: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := c.EnqueueInbox(InboxRequest{Submit: "consumed", Source: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(accepted.ItemID, sessioninbox.StateSteerAccepted, ""); err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(consumed.ItemID, sessioninbox.StateSteerConsumed, ""); err != nil {
		t.Fatal(err)
	}

	result, err := c.CancelWithInboxItemsResult([]string{accepted.ItemID, consumed.ItemID}, "desktop")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DiscardedItemIDs) != 1 || result.DiscardedItemIDs[0] != accepted.ItemID {
		t.Fatalf("discarded ids = %v", result.DiscardedItemIDs)
	}
	items := c.InboxSnapshot().Items
	if len(items) != 1 || items[0].ID != consumed.ItemID {
		t.Fatalf("remaining items = %+v", items)
	}
}

func TestDeleteInboxItemDoesNotOverwriteConsumedSteer(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	_ = os.WriteFile(session, []byte("{}\n"), 0o644)
	c := New(Options{SessionPath: session, SessionDir: dir, Sink: event.Discard})
	rec, err := c.EnqueueInbox(InboxRequest{Intent: sessioninbox.IntentSteer, Submit: "consumed"})
	if err != nil {
		t.Fatal(err)
	}
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(rec.ItemID, sessioninbox.StateSteerAccepted, ""); err != nil {
		t.Fatal(err)
	}
	c.inbox.mu.Lock()
	c.inbox.trackActive(rec.ItemID)
	c.inbox.mu.Unlock()
	if err := st.MarkSteerConsumed(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteInboxItem(rec.ItemID); !errors.Is(err, sessioninbox.ErrInvalidState) {
		t.Fatalf("delete consumed steer = %v, want ErrInvalidState", err)
	}
	meta, _, err := c.ReadInboxItem(rec.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	if meta.State != sessioninbox.StateSteerConsumed {
		t.Fatalf("state = %q, want %q", meta.State, sessioninbox.StateSteerConsumed)
	}
}

type inboxChangedCapture struct {
	changed chan sessioninbox.InboxSnapshot
}

func (s *inboxChangedCapture) Emit(event.Event) {}

func (s *inboxChangedCapture) InboxChanged(snap sessioninbox.InboxSnapshot) {
	s.changed <- snap
}

func TestInboxStoreChangesReachOptionalSink(t *testing.T) {
	dir := t.TempDir()
	sink := &inboxChangedCapture{changed: make(chan sessioninbox.InboxSnapshot, 1)}
	c := New(Options{
		SessionPath: filepath.Join(dir, "s.jsonl"),
		SessionDir:  dir,
		Sink:        sink,
	})
	rec, err := c.EnqueueInbox(InboxRequest{Submit: "notify", Source: "desktop"})
	if err != nil {
		t.Fatal(err)
	}
	awaitState := func(want sessioninbox.InboxState) sessioninbox.InboxSnapshot {
		t.Helper()
		select {
		case snap := <-sink.changed:
			if len(snap.Items) != 1 || snap.Items[0].State != want {
				t.Fatalf("notification = %+v, want state %q", snap.Items, want)
			}
			return snap
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %q notification", want)
			return sessioninbox.InboxSnapshot{}
		}
	}
	queued := awaitState(sessioninbox.StateQueued)
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(rec.ItemID, sessioninbox.StateSteerAccepted, ""); err != nil {
		t.Fatal(err)
	}
	accepted := awaitState(sessioninbox.StateSteerAccepted)
	if err := st.MarkSteerConsumed(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	consumed := awaitState(sessioninbox.StateSteerConsumed)
	if !(queued.Revision < accepted.Revision && accepted.Revision < consumed.Revision) {
		t.Fatalf("revisions did not increase: %d, %d, %d", queued.Revision, accepted.Revision, consumed.Revision)
	}
}
