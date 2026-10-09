package control

import (
	"errors"
	"testing"
	"time"

	"reasonix/internal/state/sessioninbox"
)

// A queue held by an earlier stop takes a mid-turn message as a queued
// follow-up; refusing it would leave a queued copy of a line reported unsent.
func TestSteerIntoAPausedQueueIsHeldNotRefused(t *testing.T) {
	c, _, _, _, _ := steeringTurn(t)
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}

	rec, err := c.TryEnqueueAndSteer(InboxRequest{Submit: "mid turn words", Source: "test"})
	if err != nil {
		t.Fatalf("a message sent into a paused queue was refused: %v", err)
	}
	if rec.Disposition != sessioninbox.DispositionQueuedFollowup || !rec.Paused {
		t.Fatalf("receipt = %+v, want a held follow-up that says the queue is paused", rec)
	}
	snap := c.InboxSnapshot()
	if len(snap.Items) != 1 || snap.Items[0].State != sessioninbox.StateQueued || snap.Items[0].Intent != sessioninbox.IntentFollowup {
		t.Fatalf("queue = %+v, want exactly one queued follow-up", snap.Items)
	}
}

func TestSendNowStillRefusesWhilePaused(t *testing.T) {
	c, _, _, _, _ := steeringTurn(t)
	rec, err := c.EnqueueInbox(InboxRequest{Submit: "held", Source: "test", Intent: sessioninbox.IntentSteer})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TrySteerInboxItem(rec.ItemID); !errors.Is(err, sessioninbox.ErrPaused) {
		t.Fatalf("send now on a paused queue = %v, want ErrPaused", err)
	}
}

func TestRepeatedSendsIntoAPausedQueueEachHoldOneCopy(t *testing.T) {
	c, _, _, _, _ := steeringTurn(t)
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	stale, err := c.EnqueueInbox(InboxRequest{Submit: "stopped", Source: "test", Intent: sessioninbox.IntentSteer})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetState(stale.ItemID, sessioninbox.StateUncertain, "steer_unapplied"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"one", "two"} {
		if _, err := c.TryEnqueueAndSteer(InboxRequest{Submit: text, Source: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	snap := c.InboxSnapshot()
	if len(snap.Items) != 3 || !snap.Paused {
		t.Fatalf("queue = %+v, want three items and the pause kept", snap)
	}
	for _, it := range snap.Items {
		switch {
		case it.ID == stale.ItemID:
			if it.State != sessioninbox.StateUncertain {
				t.Fatalf("the stopped steer is %v, want it left uncertain", it.State)
			}
		case it.State != sessioninbox.StateQueued || it.Intent != sessioninbox.IntentFollowup:
			t.Fatalf("held item = %+v, want a queued follow-up", it)
		}
	}
}

func TestHoldingIntoAPausedQueueDecidesUnderTheAdmissionLock(t *testing.T) {
	c, _, _, _, _ := steeringTurn(t)
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	c.inbox.admissionMu.Lock()
	done := make(chan error, 1)
	go func() {
		_, err := c.TryEnqueueAndSteer(InboxRequest{Submit: "late", Source: "test"})
		done <- err
	}()
	select {
	case err := <-done:
		c.inbox.admissionMu.Unlock()
		t.Fatalf("the hold finished without the admission lock (err=%v)", err)
	case <-time.After(200 * time.Millisecond):
	}
	c.inbox.admissionMu.Unlock()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRetryOfUnappliedSteerQueuesItAndKeepsThePause(t *testing.T) {
	c, _, _, _, _ := steeringTurn(t)
	st, err := c.ensureInbox()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := c.EnqueueInbox(InboxRequest{Submit: "stopped", Source: "test", Intent: sessioninbox.IntentSteer})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStateCoded(rec.ItemID, sessioninbox.StateUncertain, sessioninbox.BlockSteerUnapplied, "unapplied"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	if err := c.RetryInboxItem(rec.ItemID); err != nil {
		t.Fatal(err)
	}
	snap := c.InboxSnapshot()
	if len(snap.Items) != 1 || snap.Items[0].State != sessioninbox.StateQueued || snap.Items[0].BlockCode != "" {
		t.Fatalf("item = %+v, want queued with the block cleared", snap.Items)
	}
	if !snap.Paused {
		t.Fatal("a retry must not release the pause that also holds the other entries")
	}
}
