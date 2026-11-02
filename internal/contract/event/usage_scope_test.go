package event

import (
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

type usageScopeTestSink struct {
	AuditForwarder
	current any
	events  []Event
}

func (s *usageScopeTestSink) CaptureUsageScope() any { return s.current }
func (s *usageScopeTestSink) Emit(e Event)           { s.events = append(s.events, e) }

func TestUsageScopePreservesCapturedOwnerAndWire(t *testing.T) {
	old, next := make(chan struct{}), make(chan struct{})
	sink := &usageScopeTestSink{current: old}
	var quotes int
	owner := NewCostQuoteSink(sink, &QuoteContext{
		Now: func() time.Time {
			quotes++
			return time.Date(2026, 8, 19, 2, 0, 0, 0, time.UTC)
		},
		CatalogProviderForModel: func(string) string { return "deepseek" },
	})
	chain := Coalesce(Sync(owner), DefaultStreamDeltaWindow)
	bound := BindUsageScope(chain)
	if missing := MissingCapabilities(bound); len(missing) != 0 {
		t.Fatalf("bound wrapper drops %v", missing)
	}
	sink.current = next
	nested := BindUsageScope(bound)
	u := Event{Kind: Usage, UsageSource: UsageSourceSubagent, ModelRef: "deepseek/deepseek-v4-pro",
		Pricing: &provider.Pricing{CacheHit: .15, Input: 4.5, Output: 13.5, Currency: "CNY"},
		Usage:   &provider.Usage{PromptTokens: 1_000_000, CompletionTokens: 1_000_000, RequestCount: 1}}
	u.CostQuote = QuoteUsage(nested, u)
	if u.CostQuote == nil || u.CostQuote.Original.Amount != "36" || quotes != 1 {
		t.Fatalf("bound usage lost the quote owner: quote=%+v calls=%d", u.CostQuote, quotes)
	}
	nested.Emit(u)
	if len(sink.events) != 1 || UsageScope(sink.events[0]) != old {
		t.Fatal("nested capture adopted a later owner")
	}
	chain.Emit(u)
	if quotes != 1 || sink.events[0].CostQuote != u.CostQuote || sink.events[1].CostQuote != u.CostQuote {
		t.Fatal("scope binding changed or repeated the occurrence quote")
	}
	want, err := json.Marshal(sink.events[1])
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(sink.events[0])
	if err != nil || string(got) != string(want) {
		t.Fatalf("scope changed JSON: %s %v", got, err)
	}
	if UsageScope(u) != nil {
		t.Fatal("capture mutated the producer event")
	}
	var replay Event
	if err := json.Unmarshal(got, &replay); err != nil || UsageScope(replay) != nil {
		t.Fatal("replay acquired process-local ownership")
	}
	bound.Emit(Event{Kind: Notice, Text: "unchanged"})
	if len(sink.events) != 3 || UsageScope(sink.events[2]) != nil {
		t.Fatal("non-usage event acquired attribution")
	}
}

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
