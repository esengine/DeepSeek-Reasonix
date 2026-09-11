package control

import (
	"errors"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
)

func TestInboxExpectedSessionCannotSubmitOrConfirmReplacement(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first.jsonl"), filepath.Join(dir, "second.jsonl")
	c := New(Options{SessionDir: dir, SessionPath: first, Sink: event.Discard})
	defer c.Close()
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	request := InboxRequest{ExpectedSessionPath: first, Submit: "original", Idempotency: "original"}
	receipt, err := c.TryEnqueueFollowup(request)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, found, err := c.LookupInboxReceiptForSession(first, request.Idempotency)
	if err != nil || !found || confirmed.ItemID != receipt.ItemID {
		t.Fatalf("original confirmation = %+v, %v, %v", confirmed, found, err)
	}
	c.SetSessionPath(second)
	if err := c.SetInboxPaused(true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.TryEnqueueFollowup(request); !errors.Is(err, ErrInboxSessionChanged) {
		t.Fatalf("stale request = %v", err)
	}
	if _, _, err := c.LookupInboxReceiptForSession(first, request.Idempotency); !errors.Is(err, ErrInboxSessionChanged) {
		t.Fatalf("stale lookup = %v", err)
	}
	if got := c.InboxSnapshot(); got.SessionPath != second || len(got.Items) != 0 {
		t.Fatalf("replacement mutated: %+v", got)
	}
	request.ExpectedSessionPath = ""
	if _, err := c.TryEnqueueFollowup(request); err != nil {
		t.Fatalf("legacy request no longer works: %v", err)
	}
}
