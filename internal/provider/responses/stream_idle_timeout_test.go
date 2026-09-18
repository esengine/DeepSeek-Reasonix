package responses

import (
	"testing"
	"time"
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
		p := New(Config{Name: "p", BaseURL: "https://example.com", Model: "m", APIKey: "k", Extra: tc.extra})
		if got := p.(*client).idleTimeout; got != tc.want {
			t.Errorf("%s: idleTimeout = %v, want %v", tc.name, got, tc.want)
		}
	}
}
