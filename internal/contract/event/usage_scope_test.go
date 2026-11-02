package event

import (
	"testing"
)

type usageScopeTestSink struct {
	AuditForwarder
	current any
	events  []Event
}

func (s *usageScopeTestSink) CaptureUsageScope() any { return s.current }
func (s *usageScopeTestSink) Emit(e Event)           { s.events = append(s.events, e) }

func TestUsageScopeKeepsEarlierEventOwner(t *testing.T) {
	sink := &usageScopeTestSink{current: "new"}
	BindUsageScope(sink).Emit(Event{Kind: Usage, usageScope: "old"})
	if UsageScope(sink.events[0]) != "old" {
		t.Fatal("wrapper overwrote earlier attribution")
	}
}

func TestUsageScopeWithoutCapability(t *testing.T) {
	if BindUsageScope(nil) != nil {
		t.Fatal("nil sink changed")
	}
	var typedNil *usageScopeTestSink
	if BindUsageScope(typedNil) != typedNil {
		t.Fatal("typed nil sink changed")
	}
	var calls int
	sink := FuncSink(func(e Event) {
		calls++
		if UsageScope(e) != nil {
			t.Fatal("unscoped sink gained attribution")
		}
	})
	BindUsageScope(sink).Emit(Event{Kind: Usage})
	if calls != 1 {
		t.Fatal("unscoped event dropped")
	}
}
