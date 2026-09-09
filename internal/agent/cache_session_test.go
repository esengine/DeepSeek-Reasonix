package agent

import (
	"context"
	"net/http"
	"reasonix/internal/provider"
	"strings"
	"testing"
)

func TestProviderCacheSessionLifecycle(t *testing.T) {
	makeAgent := func() *Agent { a := &Agent{}; a.sess.conversation = NewSession(""); return a }
	id := func(a *Agent, ctx context.Context) string {
		req, _ := http.NewRequestWithContext(a.withProviderCacheSession(ctx), http.MethodPost, "https://opencode.ai/zen/go/v1/responses", nil)
		provider.ApplyOpenCodeGoHeaders(req, "https://opencode.ai/zen/go/v1", provider.NewClientIdentityHeaders())
		return req.Header.Get("x-opencode-session")
	}
	a := makeAgent()
	ctx := context.Background()
	first := id(a, ctx)
	if first != id(a, ctx) {
		t.Fatal("ephemeral conversation changed between turns")
	}
	if id(makeAgent(), a.withProviderCacheSession(ctx)) == first {
		t.Fatal("child reused parent session")
	}
	a.SetSessionPath("/private/root/conversation.jsonl")
	bound := id(a, ctx)
	b := makeAgent()
	b.SetSessionPath(a.SessionPath())
	if bound != id(b, ctx) || strings.Contains(bound, "private") {
		t.Fatal("resume identity changed or exposed local path")
	}
	b.SetSessionPath("/private/root/new-session.jsonl")
	if bound == id(b, ctx) {
		t.Fatal("new session reused old identity")
	}
}
