package control

import (
	"errors"
	"path/filepath"
	"reasonix/internal/state/sessionstore"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessioninbox"
)

func steeringTurn(t *testing.T) (*Controller, *agent.Agent, *sessionstore.Session, *inboxSteerProvider, chan event.Event) {
	t.Helper()
	dir := testenv.TempDir(t)
	prov := newInboxSteerProvider()
	sess := sessionstore.NewSession("sys")
	exec := agent.New(prov, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	sink, done, _ := collectSink()
	c := New(Options{
		Runner:      exec,
		Executor:    exec,
		Sink:        sink,
		SessionDir:  dir,
		SessionPath: filepath.Join(dir, "s.jsonl"),
	})
	// The first round waits on release, so a test that fails before releasing it
	// would leave this wait, and with it the whole package, blocked forever.
	t.Cleanup(func() { prov.unblock(); c.autosaveWG.Wait() })
	c.Submit("initial turn")
	// Opening a turn is a chain of fsyncs; on a loaded Windows runner that alone
	// can pass a second. A turn that ends without reaching the model says so.
	select {
	case <-prov.started:
	case e := <-done:
		t.Fatalf("the turn ended before it reached the provider: %+v", e)
	case <-time.After(10 * time.Second):
		t.Fatal("the turn never started")
	}
	return c, exec, sess, prov, done
}

func acceptedSteer(t *testing.T, c *Controller, text string) string {
	t.Helper()
	rec, err := c.TryEnqueueAndSteer(InboxRequest{Submit: text, Source: "test"})
	if err != nil {
		t.Fatalf("queue %q: %v", text, err)
	}
	if rec.Disposition != sessioninbox.DispositionSteerAccepted {
		t.Fatalf("queue %q = %q, want it accepted as a steer", text, rec.Disposition)
	}
	return rec.ItemID
}

// A queued line is the one thing on screen that has not happened yet, which is
// what makes it the only one that can still be taken back. The cancel has to
// reach both halves: the executor's queue, so nothing reads it, and the durable
// item, so nothing replays it after the turn.
func TestCancellingAQueuedSteerNeverReachesTheModel(t *testing.T) {
	c, _, sess, prov, done := steeringTurn(t)
	acceptedSteer(t, c, "keep this one")
	taken := acceptedSteer(t, c, "take this one back")

	if err := c.DeleteInboxItem(taken); err != nil {
		t.Fatalf("cancel a queued steer: %v", err)
	}

	prov.unblock()
	waitForDoneWithin(t, done, 60*time.Second)

	kept, cancelled := 0, 0
	for _, m := range sess.Snapshot() {
		kept += strings.Count(m.Content, "keep this one")
		cancelled += strings.Count(m.Content, "take this one back")
	}
	if kept != 1 || cancelled != 0 {
		t.Fatalf("transcript carries the kept line %d times and the cancelled one %d, want 1 and 0", kept, cancelled)
	}
	if items := c.InboxSnapshot().Items; len(items) != 0 {
		t.Fatalf("queue is not empty after the turn: %+v", items)
	}
}

// Once the run loop has taken the entry the words are on their way, and the
// answer to "take it back" stops being "done". DropSteer stands in for the
// loop's own take here: it leaves exactly the state that does — gone from the
// queue, still accepted on disk — which is the state the refusal is about.
func TestCancellingAfterTheTurnReadItSaysSo(t *testing.T) {
	c, exec, _, prov, done := steeringTurn(t)
	id := acceptedSteer(t, c, "too late for this one")
	if !exec.DropSteer(id) {
		t.Fatal("the accepted steer was not in the executor's queue")
	}

	if err := c.DeleteInboxItem(id); !errors.Is(err, ErrSteerApplied) {
		t.Fatalf("cancel after the read = %v, want ErrSteerApplied", err)
	}

	prov.unblock()
	waitForDoneWithin(t, done, 60*time.Second)
}
