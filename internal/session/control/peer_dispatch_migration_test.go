package control

import (
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/state/peermail"
)

func TestDispatchMigrationPreservesSigningAndReplayState(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "session.jsonl")
	old := New(Options{SessionPath: path})
	next := New(Options{SessionPath: path})
	t.Cleanup(old.Close)
	t.Cleanup(next.Close)
	old.MarkOpenedByDispatch("parent", pairSecret, 2)
	old.NoteDispatchedSession("child", pairSecret)
	in := peermail.Message{ID: "message", Seq: 7, Body: "task", From: peermail.Party{SessionID: "parent"}}
	in.Proof = peermail.Proof(pairSecret, in)
	if !old.dispatchPartner(in) {
		t.Fatal("initial signed message was refused")
	}
	next.InheritDispatchSession(old, path)
	if !next.OpenedByDispatch() || next.DispatchDepth() != 2 || next.dispatchOpenerKey() != "parent" {
		t.Fatal("replacement lost the dispatch depth or reply destination")
	}
	if next.dispatchPartner(in) {
		t.Fatal("replacement accepted a replay")
	}
	in.Seq++
	in.Proof = peermail.Proof(pairSecret, in)
	if !next.dispatchPartner(in) || old.dispatchPartner(in) {
		t.Fatal("controller generations did not share replay protection")
	}
	first := peermail.Message{To: peermail.Party{SessionID: "child"}}
	second := first
	old.signDispatch(&first)
	next.signDispatch(&second)
	if first.Seq != 1 || second.Seq != 2 || !peermail.ProofValid(pairSecret, second) {
		t.Fatalf("signing sequence restarted: first=%d second=%d", first.Seq, second.Seq)
	}
}

func TestDispatchMigrationDoesNotGrantAnotherSessionItsPairings(t *testing.T) {
	root := testenv.TempDir(t)
	old := New(Options{SessionPath: filepath.Join(root, "first.jsonl")})
	t.Cleanup(old.Close)
	old.MarkOpenedByDispatch("parent", pairSecret, 1)
	for _, path := range []string{"", filepath.Join(root, "second.jsonl")} {
		next := New(Options{SessionPath: path})
		t.Cleanup(next.Close)
		next.InheritDispatchSession(old, path)
		if next.OpenedByDispatch() || next.DispatchDepth() != 0 || next.dispatchOpenerKey() != "" {
			t.Fatalf("another session inherited a pairing: %q", path)
		}
	}
}
