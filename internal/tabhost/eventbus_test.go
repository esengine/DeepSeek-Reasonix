package tabhost_test

import (
	"encoding/json"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/tabhost"
)

func TestEventBusPublishSubscribe(t *testing.T) {
	bus := tabhost.NewEventBus()
	ch, unsub := bus.Subscribe()
	defer unsub()
	if err := bus.PublishEvent("tab_1", "epoch-a", event.Event{Kind: event.Text, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	raw := <-ch
	var w tabhost.WireEvent
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	if w.TabID != "tab_1" || w.RuntimeEpoch != "epoch-a" || w.Kind != "text" || w.Text != "hi" {
		t.Fatalf("wire=%+v", w)
	}
}
