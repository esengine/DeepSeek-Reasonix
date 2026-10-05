package config

import "testing"

// The window was only ever recorded per preset provider, so a source added by
// hand — the shape every relay has — resolved to nothing, and nothing is what
// turns compaction off. It is a fact about the model, so a gateway serving the
// same model inherits it.
func TestARelayInheritsTheWindowKnownForTheModel(t *testing.T) {
	cfg := &Config{Providers: []ProviderEntry{{
		Name: "relay", Kind: "openai",
		BaseURL: "https://relay.example.com/v1",
		Models:  []string{"deepseek-v4-pro"}, Default: "deepseek-v4-pro",
	}}}
	e, ok := cfg.ResolveModel("relay/deepseek-v4-pro")
	if !ok {
		t.Fatal("relay model did not resolve")
	}
	if e.ContextWindow != 1_000_000 {
		t.Fatalf("window = %d, want the model's own ceiling", e.ContextWindow)
	}
	if _, declared := ResolvedContextWindow(e); !declared {
		t.Fatal("the window resolved but reported itself as nobody's answer")
	}
}

// Everything the configuration says outranks it: the registry is the fallback
// for what nobody declared, not an override of what someone did.
func TestDeclaredWindowsOutrankTheRegistry(t *testing.T) {
	cfg := &Config{Providers: []ProviderEntry{{
		Name: "relay", Kind: "openai", BaseURL: "https://relay.example.com/v1",
		Models: []string{"deepseek-v4-pro"}, Default: "deepseek-v4-pro",
		ContextWindow: 128_000,
	}}}
	e, _ := cfg.ResolveModel("relay/deepseek-v4-pro")
	if e.ContextWindow != 128_000 {
		t.Fatalf("provider-wide window = %d, want the configured 128000", e.ContextWindow)
	}

	cfg.Providers[0].ContextWindow = 0
	cfg.Providers[0].ModelOverrides = map[string]ProviderModelOverride{
		"deepseek-v4-pro": {ContextWindow: 64_000},
	}
	e, _ = cfg.ResolveModel("relay/deepseek-v4-pro")
	if e.ContextWindow != 64_000 {
		t.Fatalf("per-model window = %d, want the configured 64000", e.ContextWindow)
	}
}

// A custom relay cannot always expose context metadata. In that case it gets a
// usable 160k baseline until the user supplies the model's actual limit.
func TestAnUnestablishedWindowUsesTheRelayFallback(t *testing.T) {
	cfg := &Config{Providers: []ProviderEntry{{
		Name: "relay", Kind: "openai", BaseURL: "https://relay.example.com/v1",
		Models: []string{"gpt-5.6-sol"}, Default: "gpt-5.6-sol",
	}}}
	e, ok := cfg.ResolveModel("relay/gpt-5.6-sol")
	if !ok {
		t.Fatal("model did not resolve")
	}
	if window, declared := ResolvedContextWindow(e); !declared || window != DefaultUnknownContextWindow {
		t.Fatalf("window = %d declared=%v, want fallback %d", window, declared, DefaultUnknownContextWindow)
	}
	// The effort ladder for the same model is established, so the two questions
	// stay independent: knowing one thing about a model is not knowing all.
	if !EffortCapabilityForEntry(e).Supported {
		t.Fatal("the measured effort ladder was lost")
	}
}
