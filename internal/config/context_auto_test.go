package config

import "testing"

func TestAutoContextWindow(t *testing.T) {
	cases := []struct {
		model string
		want  int
	}{
		{"qwen/qwen3.7-plus", 1_000_000}, // provider prefix stripped, Qwen commercial 1M tier
		{"qwen3-max", 262_144},           // 256K tier
		{"deepseek-v4-flash", 1_000_000}, // official V4 line
		{"deepseek-chat", 131_072},       // older DeepSeek fallback
		{"gpt-5", 272_000},
		{"gpt-4o", 131_072},
		{"claude-3.7-sonnet", 200_000},
		{"glm-5", 202_752},
		{"minimax-m3", 1_000_000},
		{"kimi-k2", 262_144},
		{"seed-oss", 524_288},
		{"gemini-2.5-pro", 1_000_000},
		{"unknown-vendor-model", 0}, // no pattern → 0, caller keeps its own default
	}
	for _, tc := range cases {
		if got := AutoContextWindow(tc.model); got != tc.want {
			t.Errorf("AutoContextWindow(%q) = %d, want %d", tc.model, got, tc.want)
		}
	}
}

func TestNormalizeModelName(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"qwen/qwen3.7-plus", "qwen3.7-plus"},
		{"org/model", "model"},
		{"  DeepSeek-V4-Flash  ", "deepseek-v4-flash"}, // lowercase + trim
		{"nemotron-3-nano:30b", "nemotron-3-nano"},     // ollama tag stripped
		{"model|tag", "model"},                         // pipe suffix stripped
		{"ollama/mistral:7b", "mistral"},               // prefix then tag
		{"plain-model", "plain-model"},
		// Edge cases: combined separators, empty input, trailing/leading tags.
		{"foo|bar:baz", "foo"}, // pipe wins, then colon would apply to the remainder
		{"foo:bar|baz", "foo"}, // pipe first yields "foo:bar", colon then strips to "foo"
		{"foo|", "foo"},        // trailing pipe
		{"foo:", "foo"},        // trailing colon
		{":30b", ""},           // tag-only input collapses to empty
		{"", ""},               // empty input passes through
	}
	for _, tc := range cases {
		if got := normalizeModelName(tc.in); got != tc.want {
			t.Errorf("normalizeModelName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveModelAutoContextWindow(t *testing.T) {
	cfg := &Config{
		Providers: []ProviderEntry{
			{
				Name:      "no-budget",
				Kind:      "openai",
				Model:     "deepseek-v4-flash",
				Models:    []string{"deepseek-v4-flash", "deepseek-chat", "mystery-model"},
				Default:   "deepseek-v4-flash",
				BaseURL:   "https://api.deepseek.com",
				APIKeyEnv: "DEEPSEEK_API_KEY",
			},
			{
				Name:          "explicit-budget",
				Kind:          "openai",
				Model:         "gpt-4o",
				Models:        []string{"gpt-4o"},
				Default:       "gpt-4o",
				BaseURL:       "https://api.openai.com",
				APIKeyEnv:     "OPENAI_API_KEY",
				ContextWindow: 999_999, // user-set budget must win over inference
			},
			{
				Name:      "override-budget",
				Kind:      "openai",
				Model:     "deepseek-v4-flash",
				Models:    []string{"deepseek-v4-flash"},
				Default:   "deepseek-v4-flash",
				BaseURL:   "https://api.deepseek.com",
				APIKeyEnv: "DEEPSEEK_API_KEY",
				ModelOverrides: map[string]ProviderModelOverride{
					"deepseek-v4-flash": {ContextWindow: 42}, // override wins over the 1M inference
				},
			},
		},
	}

	// Provider with no explicit budget: inference fills the window.
	e, ok := cfg.ResolveModel("no-budget")
	if !ok {
		t.Fatal("ResolveModel(no-budget) failed")
	}
	if got, want := e.ContextWindow, 1_000_000; got != want {
		t.Errorf("no-budget ContextWindow = %d, want %d (inferred)", got, want)
	}

	// Explicit budget is preserved, not overwritten.
	e, ok = cfg.ResolveModel("explicit-budget")
	if !ok {
		t.Fatal("ResolveModel(explicit-budget) failed")
	}
	if got, want := e.ContextWindow, 999_999; got != want {
		t.Errorf("explicit-budget ContextWindow = %d, want %d (user-set preserved)", got, want)
	}

	// A per-model override beats inference: the 1M pattern matches, but the
	// override's budget is applied first and must not be clobbered.
	e, ok = cfg.ResolveModel("override-budget")
	if !ok {
		t.Fatal("ResolveModel(override-budget) failed")
	}
	if got, want := e.ContextWindow, 42; got != want {
		t.Errorf("override-budget ContextWindow = %d, want %d (override wins)", got, want)
	}

	// Unmatched model keeps 0 = compaction disabled (conservative).
	e, ok = cfg.ResolveModel("no-budget/mystery-model")
	if !ok {
		t.Fatal("ResolveModel(no-budget/mystery-model) failed")
	}
	if got := e.ContextWindow; got != 0 {
		t.Errorf("unknown model ContextWindow = %d, want 0 (no pattern)", got)
	}
}
