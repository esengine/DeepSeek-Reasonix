package usage

import (
	"testing"

	"reasonix/internal/provider"
)

func TestTracker(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := NewTracker(tmpDir)

	usage1 := &provider.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CacheHitTokens:   30,
		CacheMissTokens:  70,
		ReasoningTokens:  20,
	}
	pricing := &provider.Pricing{
		CacheHit: 0.5,
		Input:    1.0,
		Output:   2.0,
		Currency: "USD",
	}

	tracker.Record("session-1", usage1, pricing, "executor", "model-a")

	su, ok := tracker.Get("session-1")
	if !ok {
		t.Fatal("should find session-1")
	}
	if su.PromptTokens != 100 {
		t.Errorf("expected 100 prompt tokens, got %d", su.PromptTokens)
	}
	if su.CompletionTokens != 50 {
		t.Errorf("expected 50 completion tokens, got %d", su.CompletionTokens)
	}
	if su.TotalTokens != 150 {
		t.Errorf("expected 150 total tokens, got %d", su.TotalTokens)
	}
	if su.Turns != 1 {
		t.Errorf("expected 1 turn, got %d", su.Turns)
	}
	if su.Currency != "$" {
		t.Errorf("expected currency '$', got %q", su.Currency)
	}
	expectedCost := (30*0.5 + 70*1.0 + 50*2.0) / 1e6
	if su.Cost != expectedCost {
		t.Errorf("expected cost %f, got %f", expectedCost, su.Cost)
	}

	usage2 := &provider.Usage{
		PromptTokens:     200,
		CompletionTokens: 100,
		TotalTokens:      300,
		CacheHitTokens:   100,
		CacheMissTokens:  100,
	}
	tracker.Record("session-1", usage2, pricing, "planner", "model-a")

	su, _ = tracker.Get("session-1")
	if su.PromptTokens != 300 {
		t.Errorf("expected 300 prompt tokens after 2 calls, got %d", su.PromptTokens)
	}
	if su.Turns != 2 {
		t.Errorf("expected 2 turns, got %d", su.Turns)
	}
	if len(su.Breakdown) != 2 {
		t.Errorf("expected 2 breakdown entries, got %d", len(su.Breakdown))
	}

	tracker.RecordCodeChanges("session-1", 10, 5)
	su, _ = tracker.Get("session-1")
	if su.CodeAdded != 10 {
		t.Errorf("expected 10 lines added, got %d", su.CodeAdded)
	}
	if su.CodeRemoved != 5 {
		t.Errorf("expected 5 lines removed, got %d", su.CodeRemoved)
	}

	tracker.Record("session-2", usage1, pricing, "executor", "model-b")

	if tracker.TotalTokens() != 600 {
		t.Errorf("expected 600 total tokens, got %d", tracker.TotalTokens())
	}

	recent := tracker.ListRecent(10)
	if len(recent) != 2 {
		t.Errorf("expected 2 recent sessions, got %d", len(recent))
	}

	summary := tracker.Summary()
	if summary == "" {
		t.Error("expected non-empty summary")
	}

	formatted := tracker.FormatSessionUsage("session-1")
	if formatted == "" {
		t.Error("expected non-empty formatted usage")
	}
}

func TestTracker_NilPricing(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := NewTracker(tmpDir)

	usage := &provider.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}

	tracker.Record("session-1", usage, nil, "executor", "model-a")

	su, ok := tracker.Get("session-1")
	if !ok {
		t.Fatal("should find session-1")
	}
	if su.Cost != 0 {
		t.Errorf("expected 0 cost with nil pricing, got %f", su.Cost)
	}
}

func TestTracker_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	tracker := NewTracker(tmpDir)

	_, ok := tracker.Get("nonexistent")
	if ok {
		t.Error("should not find nonexistent session")
	}
}
