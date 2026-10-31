package team

import (
	"testing"

	"reasonix/internal/state/teamroster"
)

func TestRestartSettlesEveryUnfinishedExchangeOnce(t *testing.T) {
	h := newHarness(t, "Chief", "Ben")
	for _, state := range []teamroster.ExchangeState{teamroster.ExchangeQueued, teamroster.ExchangeDelivered, teamroster.ExchangeWaiting} {
		x, _, err := h.store.OpenExchange(t.Context(), teamroster.Exchange{From: h.id("Chief"), To: h.id("Ben"), ReplyTo: teamroster.ReplyToSender}, "work")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.store.SetExchangeState(t.Context(), x.ID, state, h.clock.now()); err != nil {
			t.Fatal(err)
		}
	}
	fresh := New(Options{Store: h.store, Panes: h.panes})
	for range 2 {
		if err := fresh.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	book, err := h.store.Exchanges()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range book {
		if x.State != teamroster.ExchangeInterrupted {
			t.Fatalf("unfinished exchange after restart: %+v", x)
		}
	}
	if got := len(h.inbox("Chief")); got != 3 {
		t.Fatalf("recovery replies = %d, want 3", got)
	}
}
