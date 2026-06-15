package config

import (
	"strings"
	"testing"
)

func TestIsMiniMaxEntry(t *testing.T) {
	for _, tc := range []struct {
		baseURL string
		want    bool
	}{
		{"https://api.minimaxi.com/v1", true},
		{"https://api.minimaxi.com", true},
		{"https://api.minimaxi.com/anthropic", true},
		// Subdomain variants of the canonical host.
		{"https://eu.minimaxi.com/v1", true},
		{"https://us.minimaxi.com/v1", true},
		// Bare apex (no api. prefix) is rejected: it would only match if
		// the user pointed their base_url at the apex domain, which is a
		// misconfiguration — not a path we want to silently accept.
		{"https://minimaxi.com/v1", false},
		{"https://minimaxi.com", false},
		// Other vendors must not match.
		{"https://api.deepseek.com", false},
		{"https://api.xiaomimimo.com/v1", false},
		{"https://api.minimax.example.com", false}, // wrong spelling — must not match
		{"", false},
	} {
		e := &ProviderEntry{Kind: "openai", BaseURL: tc.baseURL}
		if got := isMiniMaxEntry(e); got != tc.want {
			t.Errorf("baseURL=%q: isMiniMaxEntry=%v, want %v", tc.baseURL, got, tc.want)
		}
	}
}

func TestEffortCapabilityMiniMax(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}
	cap := EffortCapabilityForEntry(e)
	if !cap.Supported {
		t.Fatalf("M3 entry should expose /effort, got %+v", cap)
	}
	wantLevels := []string{"auto", "adaptive", "disabled"}
	if len(cap.Levels) != len(wantLevels) {
		t.Fatalf("levels = %v, want %v", cap.Levels, wantLevels)
	}
	for i, l := range wantLevels {
		if cap.Levels[i] != l {
			t.Errorf("levels[%d] = %q, want %q", i, cap.Levels[i], l)
		}
	}
	if cap.Default != "adaptive" {
		t.Errorf("default = %q, want adaptive (M3 ships with thinking on)", cap.Default)
	}
}

func TestNormalizeEffortMiniMax(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}
	cases := []struct {
		in, want string
	}{
		{"auto", ""}, // auto == "leave to provider default" == empty
		{"adaptive", "adaptive"},
		{"disabled", "disabled"},
		{"ADAPTIVE", "adaptive"}, // case-insensitive
		// Stale values from other vendors still resolve to a valid M3 level
		// rather than failing the /effort command.
		{"off", "disabled"}, // retired DeepSeek "no thinking" → M3 actually supports "disabled"
		{"low", "adaptive"},
		{"medium", "adaptive"},
		{"high", "adaptive"},
		{"xhigh", "disabled"},
		{"max", "disabled"},
	}
	for _, tc := range cases {
		got, err := NormalizeEffort(e, tc.in)
		if err != nil {
			t.Errorf("NormalizeEffort(%q) returned error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeEffort(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeEffortMiniMaxRejectsGarbage(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}
	// "turbo" and similar unrecognised inputs reach the MiniMax switch and
	// are rejected with the MiniMax-specific usage hint. Empty input is
	// rejected earlier with the generic hint; both are valid user-facing
	// errors. "off" is *not* in this list — it's a retired level we now
	// migrate to "adaptive" (tested in TestNormalizeEffortMiniMax above).
	cases := map[string]string{
		"turbo": "auto|adaptive|disabled",
		"":      "auto|<level>",
	}
	for in, wantHint := range cases {
		_, err := NormalizeEffort(e, in)
		if err == nil {
			t.Errorf("NormalizeEffort(%q) should be rejected", in)
			continue
		}
		if !strings.Contains(err.Error(), wantHint) {
			t.Errorf("NormalizeEffort(%q) error = %q, want it to mention %q", in, err.Error(), wantHint)
		}
	}
}

func TestEffectiveEffortMiniMax(t *testing.T) {
	e := &ProviderEntry{Kind: "openai", BaseURL: "https://api.minimaxi.com/v1", Model: "MiniMax-M3"}
	// unset → EffectiveEffort stays empty so the wire layer defaults to adaptive
	if got := EffectiveEffort(e); got != "" {
		t.Errorf("unset EffectiveEffort = %q, want \"\"", got)
	}
	// explicit value is preserved verbatim
	e.Effort = "disabled"
	if got := EffectiveEffort(e); got != "disabled" {
		t.Errorf("explicit EffectiveEffort = %q, want disabled", got)
	}
}
