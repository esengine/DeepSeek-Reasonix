package openai

import (
	"testing"
	"time"

	"reasonix/internal/provider"
)

func TestNewStreamIdleTimeout(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		want  time.Duration
	}{
		{"configured", map[string]any{"stream_idle_timeout": 8 * time.Second}, 8 * time.Second},
		{"default", nil, defaultStreamIdleTimeout},
	}
	for _, tc := range cases {
		p, err := New(provider.Config{Name: "p", BaseURL: "https://example.com/v1", Model: "m", APIKey: "k", Extra: tc.extra})
		if err != nil {
			t.Fatalf("%s: New() error: %v", tc.name, err)
		}
		if got := p.(*client).idleTimeout; got != tc.want {
			t.Errorf("%s: idleTimeout = %v, want %v", tc.name, got, tc.want)
		}
	}
}
