package teamroster

import (
	"context"
	"testing"
)

// A message the user sent from a window that does not hold the team waits in
// the outbox until the holder acknowledges its delivery.
func TestTheOutboxHoldsWhatTheUserSentUntilItIsAcknowledged(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	ctx := context.Background()
	ada := newAgentID()
	if _, err := s.QueueUserMessage(ctx, ada, "write the weekly report"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.QueueUserMessage(ctx, ada, "   "); err == nil {
		t.Fatal("an empty message was queued")
	}
	got, err := s.PendingUserMessages(ctx)
	if err != nil || len(got) != 1 || got[0].To != ada || got[0].Text != "write the weekly report" || got[0].ID == "" {
		t.Fatalf("taken = %+v, %v", got, err)
	}
	if again, _ := s.PendingUserMessages(ctx); len(again) != 1 {
		t.Fatalf("reading the queue lost unacknowledged messages: %+v", again)
	}
	newer, err := s.QueueUserMessage(ctx, ada, "A newer request")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AckUserMessage(ctx, got[0].ID); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.PendingUserMessages(ctx); len(again) != 1 || again[0].ID != newer.ID {
		t.Fatalf("acknowledgement lost a newer request: %+v", again)
	}
}
