package control

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
	"reasonix/internal/testenv"
)

func seedTranscript(t *testing.T, path string, text string) {
	t.Helper()
	s := agent.NewSession("sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: text})
	if err := s.SaveSnapshot(path); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// Reading a conversation another window is writing is not what the lease
// protects against, so it attaches rather than being refused.
func TestAttachReadsASessionAnotherRuntimeHolds(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := filepath.Join(testenv.TempDir(t), "held.jsonl")
	seedTranscript(t, path, "what the other window is writing")

	holder, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	defer holder.Release()

	k := NewSessionLeaseKeeper()
	defer k.Release()
	writable, err := k.Attach(path)
	if err != nil {
		t.Fatalf("Attach on a held session = %v, want it to attach", err)
	}
	if writable {
		t.Error("attached writable to a session a live holder is writing")
	}
	if got := k.HeldPath(); got != "" {
		t.Errorf("keeper holds %q, want nothing — the lease is the holder's", got)
	}
}

// The whole point of the lease is that two windows cannot write the same
// transcript back over each other. Opening read-only must not buy that back:
// with no lease there is no authority, and every save has to fail closed.
func TestReadOnlyAttachmentCannotWriteTheTranscript(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := filepath.Join(testenv.TempDir(t), "guarded.jsonl")
	seedTranscript(t, path, "the content that must survive")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	holder, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	defer holder.Release()

	k := NewSessionLeaseKeeper()
	defer k.Release()
	writable, err := k.Attach(path)
	if err != nil || writable {
		t.Fatalf("Attach = (%v, %v), want (false, nil)", writable, err)
	}

	// What a read-only pane holds: a loaded transcript, and no authority. This
	// is the state Attach leaves the controller in, reproduced directly so the
	// assertion is about the save path rather than about controller wiring.
	sess := agent.NewSession("sys")
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "an edit this window must not persist"})
	sess.RequireWriteAuthority()

	if err := sess.SaveSnapshot(path); !errors.Is(err, agent.ErrSessionWriteAuthorityMissing) {
		t.Fatalf("SaveSnapshot without authority = %v, want ErrSessionWriteAuthorityMissing", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Error("a read-only attachment wrote the transcript; the holder's work would be gone")
	}
}

// A conversation nobody is writing attaches the way it always did.
func TestAttachTakesTheLeaseWhenItIsFree(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	path := filepath.Join(testenv.TempDir(t), "free.jsonl")
	seedTranscript(t, path, "nobody is here")

	k := NewSessionLeaseKeeper()
	defer k.Release()
	writable, err := k.Attach(path)
	if err != nil || !writable {
		t.Fatalf("Attach on a free session = (%v, %v), want (true, nil)", writable, err)
	}
	if got, want := k.HeldPath(), agent.CanonicalSessionPath(path); got != want {
		t.Errorf("held %q, want %q", got, want)
	}
}

// Moving off a writable session onto one someone else holds has to let the
// first lease go, or this window keeps a transcript it is no longer showing.
func TestAttachReleasesThePreviousLeaseWhenTheNextIsHeld(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	dir := testenv.TempDir(t)
	mine, theirs := filepath.Join(dir, "mine.jsonl"), filepath.Join(dir, "theirs.jsonl")
	seedTranscript(t, mine, "mine")
	seedTranscript(t, theirs, "theirs")

	holder, err := agent.TryAcquireSessionLease(theirs)
	if err != nil {
		t.Fatalf("holder: %v", err)
	}
	defer holder.Release()

	k := NewSessionLeaseKeeper()
	defer k.Release()
	if writable, err := k.Attach(mine); err != nil || !writable {
		t.Fatalf("Attach(mine) = (%v, %v)", writable, err)
	}
	if writable, err := k.Attach(theirs); err != nil || writable {
		t.Fatalf("Attach(theirs) = (%v, %v), want (false, nil)", writable, err)
	}
	if got := k.HeldPath(); got != "" {
		t.Errorf("still holding %q after moving to a session held elsewhere", got)
	}
	// And the one it let go of is free for whoever wants it next.
	got, err := agent.TryAcquireSessionLease(mine)
	if err != nil {
		t.Fatalf("the released session is still locked: %v", err)
	}
	got.Release()
}
