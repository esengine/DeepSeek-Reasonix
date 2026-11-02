package team

import "testing"

func TestUserOutboxSurvivesAReceiverOpenFailure(t *testing.T) {
	h := newHarness(t, "Chief", "Ada")
	seat := SeatRef{Agent: h.id("Ada")}
	h.panes.broke = map[SeatRef]bool{seat: true}
	if _, err := h.store.QueueUserMessage(t.Context(), seat.Agent, "Keep this request until delivered"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.Tick(t.Context()); err == nil {
		t.Fatal("receiver failure was not reported")
	}
	h.panes.broke[seat] = false
	h.svc = New(Options{Store: h.store, Panes: h.panes, Now: h.clock.now})
	for range 2 {
		if err := h.svc.Tick(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.inbox("Ada"); len(got) != 1 || got[0].Text != "Keep this request until delivered" {
		t.Fatalf("outbox lost or duplicated the user's request: %+v", got)
	}
}
