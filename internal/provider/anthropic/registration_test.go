package anthropic

import (
	"context"
	"reasonix/internal/provider"
	"testing"
)

func TestRegistered(t *testing.T) {
	p, err := provider.New("anthropic", provider.Config{Model: "claude-opus-4-8", Name: "claude"})
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}
	if p.Name() != "claude" {
		t.Fatalf("name = %q", p.Name())
	}
	// Missing model is rejected.
	if _, err := provider.New("anthropic", provider.Config{}); err == nil {
		t.Fatal("expected error for missing model")
	}
	_ = context.Background()
}
