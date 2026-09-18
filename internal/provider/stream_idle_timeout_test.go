package provider

import (
	"testing"
	"time"
)

func TestStreamIdleTimeoutFromExtra(t *testing.T) {
	cases := []struct {
		name  string
		extra map[string]any
		want  time.Duration
	}{
		{"nil extra", nil, 0},
		{"unset", map[string]any{}, 0},
		{"positive", map[string]any{"stream_idle_timeout": 8 * time.Second}, 8 * time.Second},
		{"zero", map[string]any{"stream_idle_timeout": time.Duration(0)}, 0},
		{"negative", map[string]any{"stream_idle_timeout": -time.Second}, 0},
		{"wrong type", map[string]any{"stream_idle_timeout": 8}, 0},
	}
	for _, tc := range cases {
		if got := StreamIdleTimeout(Config{Extra: tc.extra}); got != tc.want {
			t.Errorf("%s: StreamIdleTimeout() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
