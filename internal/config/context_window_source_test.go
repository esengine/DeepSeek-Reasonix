package config

import (
	"path/filepath"
	"testing"
)

func TestEffectiveContextWindowSourceLegacy(t *testing.T) {
	if got := EffectiveContextWindowSource(&ProviderEntry{ContextWindow: 128_000}); got != ContextWindowSourceExplicit {
		t.Fatalf("positive legacy window source = %q, want explicit", got)
	}
	if got := EffectiveContextWindowSource(&ProviderEntry{}); got != ContextWindowSourceDefault {
		t.Fatalf("zero window source = %q, want default", got)
	}
	if got := EffectiveContextWindowSource(&ProviderEntry{ContextWindow: 128_000, ContextWindowSource: ContextWindowSourceDefault}); got != ContextWindowSourceDefault {
		t.Fatalf("default-marked window source = %q, want default", got)
	}
	if got := EffectiveContextWindowSource(nil); got != ContextWindowSourceDefault {
		t.Fatalf("nil entry source = %q, want default", got)
	}
}

func TestLearnedWindowStoreRoundTripAndDownward(t *testing.T) {
	path := filepath.Join(t.TempDir(), "learned.json")
	s, err := LoadLearnedWindowStorePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Update("https://example.com/v1/", "model-a", LearnedWindow{WindowTokens: 100_000, CompletionBudget: 8_000}); !ok {
		t.Fatal("first update rejected")
	}
	if got := s.Get("HTTPS://EXAMPLE.COM/v1", "model-a"); got.WindowTokens != 100_000 || got.CompletionBudget != 8_000 {
		t.Fatalf("normalised key lookup = %+v", got)
	}
	if _, ok := s.Update("https://example.com/v1", "model-a", LearnedWindow{WindowTokens: 120_000}); ok {
		t.Fatal("upward window update must be rejected")
	}
	if _, ok := s.Update("https://example.com/v1", "model-a", LearnedWindow{WindowTokens: 90_000, CompletionBudget: 4_000}); !ok {
		t.Fatal("downward update rejected")
	}
	if got := s.Get("https://example.com/v1", "model-a"); got.WindowTokens != 90_000 || got.CompletionBudget != 8_000 {
		t.Fatalf("downward update kept stale completion = %+v", got)
	}
	if got := s.Get("https://example.com/v1", "model-b"); got != (LearnedWindow{}) {
		t.Fatalf("model isolation broken: %+v", got)
	}
	reloaded, err := LoadLearnedWindowStorePath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Get("https://example.com/v1", "model-a"); got.WindowTokens != 90_000 {
		t.Fatalf("reloaded window = %+v", got)
	}
}
