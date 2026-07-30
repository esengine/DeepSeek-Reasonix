package remote

import (
	"context"
	"testing"
)

// TestHopAuthDropsTargetCredentials pins the ProxyJump security fix: a jump
// host must never be handed the target's stored password/passphrase closures.
func TestHopAuthDropsTargetCredentials(t *testing.T) {
	targetAuth := &AuthOptions{
		Password:     func() (string, error) { return "target-secret", nil },
		Passphrase:   func() (string, error) { return "target-key-pass", nil },
		SecretPrompt: func(_ context.Context, _ SecretKind, _, _ string) (string, error) { return "", nil },
		DisableAgent: true,
	}
	cfg := dialConfig{auth: targetAuth}
	hop := ResolvedHost{Name: "bastion", HostName: "10.0.0.1", Port: 22, User: "jump"}

	hopAuth := cfg.hopAuthFor(hop)
	if hopAuth.Password != nil {
		t.Error("jump host auth carries the target's Password closure")
	}
	if hopAuth.Passphrase != nil {
		t.Error("jump host auth carries the target's Passphrase closure")
	}
	if hopAuth.SecretPrompt == nil {
		t.Error("jump host auth should still allow interactive prompting")
	}
	if !hopAuth.DisableAgent {
		t.Error("jump host auth should inherit DisableAgent")
	}
}

// TestClientHopAuthIsPersistentAndPerHost pins that the Client's per-hop auth is
// credential-free, cached per hop (so reconnects don't re-prompt), and distinct
// between hops (so one hop's secret is never reused for another).
func TestClientHopAuthIsPersistentAndPerHost(t *testing.T) {
	c, err := New(Options{
		Host: ResolvedHost{HostName: "target", Port: 22, User: "u"},
		Auth: AuthOptions{Password: func() (string, error) { return "target-secret", nil }},
	})
	if err != nil {
		t.Fatal(err)
	}
	h1 := ResolvedHost{HostName: "hop1", Port: 22, User: "u"}
	h2 := ResolvedHost{HostName: "hop2", Port: 22, User: "u"}

	a1 := c.hopAuthFor(h1)
	if a1.Password != nil || a1.Passphrase != nil {
		t.Fatal("hop auth carries target credentials")
	}
	if c.hopAuthFor(h1) != a1 {
		t.Error("hop auth not cached: reconnect would re-prompt for jump secrets")
	}
	if c.hopAuthFor(h2) == a1 {
		t.Error("distinct hops share one auth: a hop's secret could be reused for another")
	}
}
