package config

import (
	"testing"
	"time"
)

func TestDefaultCacheTTL(t *testing.T) {
	cases := []struct {
		url  string
		want time.Duration
	}{
		{"https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", 5 * time.Minute},
		{"https://dashscope.aliyuncs.com/compatible-mode/v1", 5 * time.Minute},
		{"https://api.deepseek.com", 24 * time.Hour},
		{"https://api.anthropic.com", 5 * time.Minute},
		{"https://unknown.example.com/v1", 24 * time.Hour},
		{"", 24 * time.Hour},
	}
	for _, tc := range cases {
		if got := DefaultCacheTTL(tc.url); got != tc.want {
			t.Errorf("DefaultCacheTTL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestEffectiveCacheTTLVendorDefault(t *testing.T) {
	e := &ProviderEntry{BaseURL: "https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"}
	if got := e.EffectiveCacheTTL(); got != 5*time.Minute {
		t.Fatalf("DashScope default = %v, want 5m", got)
	}
	e2 := &ProviderEntry{BaseURL: "https://api.deepseek.com"}
	if got := e2.EffectiveCacheTTL(); got != 24*time.Hour {
		t.Fatalf("DeepSeek default = %v, want 24h (legacy)", got)
	}
}

func TestEffectiveCacheTTLOverride(t *testing.T) {
	e := &ProviderEntry{BaseURL: "https://api.deepseek.com", CacheTTLMinutes: 30}
	if got := e.EffectiveCacheTTL(); got != 30*time.Minute {
		t.Fatalf("override = %v, want 30m", got)
	}
	// Zero falls through to vendor default.
	e2 := &ProviderEntry{BaseURL: "https://api.deepseek.com", CacheTTLMinutes: 0}
	if got := e2.EffectiveCacheTTL(); got != 24*time.Hour {
		t.Fatalf("zero override = %v, want 24h (vendor default)", got)
	}
}

func TestDetectCacheVendor(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1", "dashscope"},
		{"https://dashscope.aliyuncs.com/api/v1", "dashscope"},
		{"https://api.deepseek.com", "deepseek"},
		{"https://api.anthropic.com", "anthropic"},
		{"https://openrouter.ai/api/v1", ""},
		{"https://dashscope.aliyuncs.com.attacker.example/v1", ""},
		{"https://example.com/?u=api.anthropic.com", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := detectCacheVendor(tc.url); got != tc.want {
			t.Errorf("detectCacheVendor(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}
