package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/state/sessioninbox"
	"reasonix/internal/state/store"

	"reasonix/internal/base/testenv"
)

// corruptInboxBodies leaves one durable body on disk, then makes the blobs
// directory unreadable and the manifest undecodable, so the next open of the
// queue cannot say what it holds.
func corruptInboxBodies(t *testing.T, session string) {
	t.Helper()
	s, err := sessioninbox.Open(session, sessioninbox.Limits{})
	if err != nil {
		t.Fatalf("open a fresh inbox: %v", err)
	}
	if _, err := s.Enqueue(sessioninbox.EnqueueRequest{Envelope: sessioninbox.PromptEnvelope{SubmitText: "the user's lost prompt"}}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	s.Close()

	inboxDir := store.SessionInboxDir(session)
	blobs := filepath.Join(inboxDir, "blobs")
	if err := os.Rename(blobs, filepath.Join(inboxDir, "blobs-preserved")); err != nil {
		t.Fatalf("preserve blobs: %v", err)
	}
	if err := os.WriteFile(blobs, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("replace blobs with a file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inboxDir, "manifest.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("corrupt manifest: %v", err)
	}
}

// sinkEvents copies what the sink has seen; the sink appends from the store's
// own goroutine, so a test reads under its lock.
func sinkEvents(s *noticeSink) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]event.Event(nil), s.events...)
}

// noticeWindow is how long a test lets the opened queue's own goroutine report
// itself. It is short on purpose: the work has already been handed over, and a
// wait that outlives the binary's deadline reports a hang that never happened.
const noticeWindow = 300 * time.Millisecond

// settledEvents waits for the queue's open to report itself, then returns what
// the sink saw: rebindInbox emits from its own goroutine, so reading once right
// after the snapshot would race it.
func settledEvents(s *noticeSink) []event.Event {
	deadline := time.Now().Add(noticeWindow)
	for {
		got := sinkEvents(s)
		for _, e := range got {
			if e.Kind == event.Notice {
				return got
			}
		}
		if time.Now().After(deadline) {
			return got
		}
		time.Sleep(time.Millisecond)
	}
}

// noticesCoded returns the notices a code carries, in the order the sink saw
// them.
func noticesCoded(events []event.Event, code string) []event.Event {
	var out []event.Event
	for _, e := range events {
		if e.Code == code {
			out = append(out, e)
		}
	}
	return out
}

func newUnreadableInboxController(t *testing.T) (*Controller, *noticeSink) {
	t.Helper()
	dir := testenv.TempDir(t)
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	corruptInboxBodies(t, session)
	sink := &noticeSink{}
	c := New(Options{SessionDir: dir, SessionPath: session, Sink: sink})
	t.Cleanup(c.Close)
	return c, sink
}

// A queue whose bodies cannot be read is a case of its own: the user has to
// hear that the contents are unknown and the queue is paused for inspection,
// and must not hear the "recovered N" line, which claims contents it does not
// have.
func TestUnreadableInboxEmitsTheUnknownNoticeNotTheRecoveredOne(t *testing.T) {
	c, sink := newUnreadableInboxController(t)

	snap := c.InboxSnapshot()
	if !snap.Unrecovered || !snap.Paused {
		t.Fatalf("snapshot = %+v, want an unrecovered paused queue", snap)
	}
	if snap.Recovered || snap.RecoveredN != 0 || len(snap.Items) != 0 {
		t.Fatalf("snapshot = %+v, want no claimed recovery and no items", snap)
	}

	seen := settledEvents(sink)
	unknown := noticesCoded(seen, event.NoticeCodeInboxUnrecovered)
	if len(unknown) != 1 {
		t.Fatalf("unreadable queue emitted %d unknown notices, want 1 (notices: %v)", len(unknown), sink.notices())
	}
	if recovered := noticesCoded(seen, event.NoticeCodeInboxRecovered); len(recovered) != 0 {
		t.Fatalf("an unreadable queue announced a recovery: %+v", recovered)
	}
	ev := unknown[0]
	if ev.Kind != event.Notice || ev.Level != event.LevelWarn {
		t.Fatalf("unknown notice = %+v, want a warn notice", ev)
	}
	if !strings.Contains(ev.Text, "unknown") || !strings.Contains(ev.Text, "paused") {
		t.Fatalf("unknown notice text %q must say the contents are unknown and the queue is paused", ev.Text)
	}
	if p, ok := event.DecodeInboxUnrecovered(ev.Detail); !ok || p.Dir == "" {
		t.Fatalf("unknown notice detail = %q, want an InboxUnrecovered payload naming the queue directory", ev.Detail)
	}
	if !event.DetailIsPayload(event.NoticeCodeInboxUnrecovered) {
		t.Fatal("a sink that prints Text must not print the payload again")
	}
	if _, ok := event.DecodeInboxUnrecovered("not json"); ok {
		t.Fatal("a Detail that is not the payload must read as none")
	}
}

// The recovered notice keeps its own path: a readable salvage still says how
// many instructions came back and carries that count as its payload.
func TestReadableSalvageStillEmitsTheRecoveredNotice(t *testing.T) {
	dir := testenv.TempDir(t)
	session := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(session, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := sessioninbox.Open(session, sessioninbox.Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enqueue(sessioninbox.EnqueueRequest{Envelope: sessioninbox.PromptEnvelope{SubmitText: "orphan one"}}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if err := os.WriteFile(filepath.Join(store.SessionInboxDir(session), "manifest.json"), []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	sink := &noticeSink{}
	c := New(Options{SessionDir: dir, SessionPath: session, Sink: sink})
	t.Cleanup(c.Close)
	snap := c.InboxSnapshot()
	if snap.Unrecovered || !snap.Recovered || snap.RecoveredN != 1 {
		t.Fatalf("snapshot = %+v, want a readable salvage of 1", snap)
	}
	seen := settledEvents(sink)
	recovered := noticesCoded(seen, event.NoticeCodeInboxRecovered)
	unknown := noticesCoded(seen, event.NoticeCodeInboxUnrecovered)
	if len(recovered) != 1 || len(unknown) != 0 {
		t.Fatalf("salvage notices: recovered=%d unknown=%d, want 1 and 0 (notices: %v)", len(recovered), len(unknown), sink.notices())
	}
}
