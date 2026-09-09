package provider

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestOpenCodeGoHeadersScopedStableAndPrivate(t *testing.T) {
	for _, base := range []string{"https://opencode.ai/zen/go", "https://opencode.ai/zen/go/v1"} {
		ctx := WithCacheSession(context.Background(), "/private/path/to/session.jsonl")
		var previous string
		for i := range 3 {
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, base+"/responses", nil)
			ApplyOpenCodeGoHeaders(req, base, NewClientIdentityHeaders())
			id := req.Header.Get("x-opencode-session")
			if id == "" || strings.Contains(id, "private") || i > 0 && id != previous {
				t.Fatalf("unstable/private session ID: %q", id)
			}
			if req.Header.Get("User-Agent") != "Reasonix" {
				t.Fatal("missing client identity")
			}
			previous = id
		}
		child, _ := http.NewRequestWithContext(WithCacheSession(ctx, "child"), http.MethodPost, base, nil)
		ApplyOpenCodeGoHeaders(child, base, NewClientIdentityHeaders())
		if child.Header.Get("x-opencode-session") == previous {
			t.Fatal("child retained parent identity")
		}
	}
	for _, base := range []string{"https://opencode.ai/zen/v1", "https://opencode.ai.evil/zen/go/v1", "http://opencode.ai/zen/go/v1", "https://opencode.ai/zen/go/v1?x=1", "https://api.deepseek.com", "https://opencode.ai:8443/zen/go"} {
		req, _ := http.NewRequest(http.MethodPost, base, nil)
		ApplyOpenCodeGoHeaders(req, base, NewClientIdentityHeaders())
		if len(req.Header) != 0 {
			t.Fatalf("headers leaked to %s", base)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, "https://opencode.ai/zen/go/v1/responses", nil)
	req.Header.Set("User-Agent", "Reasonix/custom")
	req.Header.Set("x-opencode-session", "explicit-session")
	ApplyOpenCodeGoHeaders(req, "https://opencode.ai/zen/go/v1", NewClientIdentityHeaders())
	if req.Header.Get("User-Agent") != "Reasonix/custom" || req.Header.Get("x-opencode-session") != "explicit-session" {
		t.Fatal("overwrote explicit client header")
	}
}
