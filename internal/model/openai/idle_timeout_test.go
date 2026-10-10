package openai

import (
	"net/http"
	"testing"
	"time"

	"reasonix/internal/contract/provider"
)

// A configured idle_timeout_seconds must reach both windows the default bounds:
// the transport's pre-header deadline and the read loop's inter-event watchdog.
func TestIdleTimeoutOverrideReachesClientAndTransport(t *testing.T) {
	p, err := New(provider.Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4", APIKey: "k",
		Extra: map[string]any{provider.IdleTimeoutSecondsKey: 8},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if c.idleTimeout != 8*time.Second {
		t.Errorf("client.idleTimeout = %v, want 8s", c.idleTimeout)
	}
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.http.Transport)
	}
	if tr.ResponseHeaderTimeout != 8*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 8s", tr.ResponseHeaderTimeout)
	}
}

// Without the key the client keeps the built-in default, unchanged.
func TestIdleTimeoutDefaultsWhenUnset(t *testing.T) {
	p, err := New(provider.Config{Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4", APIKey: "k"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := p.(*client)
	if c.idleTimeout != provider.StreamIdleTimeout {
		t.Errorf("client.idleTimeout = %v, want %v", c.idleTimeout, provider.StreamIdleTimeout)
	}
	tr, ok := c.http.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", c.http.Transport)
	}
	if tr.ResponseHeaderTimeout != provider.StreamIdleTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", tr.ResponseHeaderTimeout, provider.StreamIdleTimeout)
	}
}

// The retry notice tells the user how long one attempt waits, so the send
// options must carry the same window the transport enforces.
func TestSendOptsCarryTheHeaderWindow(t *testing.T) {
	p, err := New(provider.Config{
		Name: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4", APIKey: "k",
		Extra: map[string]any{provider.IdleTimeoutSecondsKey: 8},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := p.(*client).sendOpts().HeaderTimeout; got != 8*time.Second {
		t.Fatalf("HeaderTimeout = %v, want 8s", got)
	}
}
