package agent

import (
	"testing"
)

// Nothing was rewritten, so the fold's fingerprint cannot have changed. Taking
// it again would re-serialise every message behind the fold, on a path the
// status gauge alone walks several times a turn.
func TestProjectionCheckReusesThePrefixFingerprint(t *testing.T) {
	a := longSession(200)
	a.window().modelVisibleMessages()
	first := a.sess.win.coveredHash.Load()
	if first == nil {
		t.Fatal("no fingerprint was memoised")
	}

	a.window().modelVisibleMessages()

	if a.sess.win.coveredHash.Load() != first {
		t.Fatal("the fingerprint was recomputed although nothing was rewritten")
	}
}

// The memo is an optimisation of a safety check, so it has to lose to a real
// rewrite: a projection built over edited history must stop validating.
func TestRewrittenPrefixInvalidatesTheProjection(t *testing.T) {
	a := longSession(200)
	if visible := len(a.window().modelVisibleMessages()); visible > 8 {
		t.Fatalf("fixture projection is not in force: %d visible messages", visible)
	}

	msgs, _, _ := a.sess.conversation.SnapshotWithVersion()
	msgs[1].Content = "the user asked for something else entirely"
	a.sess.conversation.Rewrite(msgs, "test rewrite")

	if visible := len(a.window().modelVisibleMessages()); visible != len(msgs) {
		t.Fatalf("a rewritten prefix must drop the projection; visible = %d of %d", visible, len(msgs))
	}
}

// A memo keyed on the rewrite counter alone would answer for the wrong length
// once a second projection covers a different prefix.
func TestFingerprintMemoIsKeyedOnTheCoveredLength(t *testing.T) {
	a := longSession(50)
	msgs, _, rewriteVersion := a.sess.conversation.SnapshotWithVersion()
	hasher := a.window().prefixHasher(rewriteVersion)

	short, long := hasher(msgs, 10), hasher(msgs, 40)

	if short == long {
		t.Fatal("two different covered lengths produced one fingerprint")
	}
	if again := hasher(msgs, 10); again != short {
		t.Fatalf("re-asking for the same length changed the answer: %q vs %q", again, short)
	}
}
