package config

import "testing"

func TestDefaultRetiredAutoPlanCompatibilityOff(t *testing.T) {
	if got := Default().Agent.AutoPlan; got != "off" {
		t.Fatalf("default auto_plan = %q, want off", got)
	}
}

func TestDefaultReasoningLanguageAuto(t *testing.T) {
	if got := Default().ReasoningLanguage(); got != "auto" {
		t.Fatalf("default reasoning_language = %q, want auto", got)
	}
}

func TestDefaultDeepSeekUsesAnthropicWebSearch(t *testing.T) {
	cfg := Default()
	for _, name := range []string{"deepseek-flash", "deepseek-pro"} {
		provider, ok := cfg.Provider(name)
		if !ok {
			t.Fatalf("default provider %q is missing", name)
		}
		if provider.Kind != "anthropic" || provider.BaseURL != deepSeekAnthropicBaseURL {
			t.Fatalf("default provider %q = kind %q base_url %q, want anthropic %q", name, provider.Kind, provider.BaseURL, deepSeekAnthropicBaseURL)
		}
		if !EffectiveWebSearch(provider) {
			t.Fatalf("default provider %q web_search = false, want true", name)
		}
	}
}

func TestDefaultDesktopAppearanceAutoGraphite(t *testing.T) {
	cfg := Default()
	if got := cfg.DesktopTheme(); got != "auto" {
		t.Fatalf("default desktop theme = %q, want auto", got)
	}
	if got := cfg.DesktopThemeStyle(); got != "" {
		t.Fatalf("default desktop theme style = %q, want empty so frontend resolves graphite", got)
	}
	if got := cfg.DesktopTerminalTheme(); got != "auto" {
		t.Fatalf("default desktop terminal theme = %q, want auto", got)
	}
}

func TestDefaultDesktopMetricsOn(t *testing.T) {
	cfg := Default()
	if !cfg.DesktopMetrics() {
		t.Fatal("default desktop metrics = false, want true")
	}
	disabled := false
	cfg.Desktop.Metrics = &disabled
	if cfg.DesktopMetrics() {
		t.Fatal("desktop metrics explicit false = true, want false")
	}
}
