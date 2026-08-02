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
		{"https://api.deepseek.com", 60 * time.Minute},
		{"https://api.anthropic.com", 5 * time.Minute},
		{"https://unknown.example.com/v1", 10 * time.Minute},
		{"", 10 * time.Minute},
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
	if got := e2.EffectiveCacheTTL(); got != 60*time.Minute {
		t.Fatalf("DeepSeek default = %v, want 60m", got)
	}
}

func TestEffectiveCacheTTLOverride(t *testing.T) {
	e := &ProviderEntry{BaseURL: "https://api.deepseek.com", CacheTTLMinutes: 30}
	if got := e.EffectiveCacheTTL(); got != 30*time.Minute {
		t.Fatalf("override = %v, want 30m", got)
	}
	// Zero falls through to vendor default.
	e2 := &ProviderEntry{BaseURL: "https://api.deepseek.com", CacheTTLMinutes: 0}
	if got := e2.EffectiveCacheTTL(); got != 60*time.Minute {
		t.Fatalf("zero override = %v, want 60m (vendor default)", got)
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
		{"", ""},
	}
	for _, tc := range cases {
		if got := detectCacheVendor(tc.url); got != tc.want {
			t.Errorf("detectCacheVendor(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}
