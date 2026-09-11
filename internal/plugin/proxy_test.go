package plugin

import (
	"context"
	"testing"
)

func TestServerProxyReplaceDrainsPrevious(t *testing.T) {
	p := newServerProxy("fs")
	// Clients need a name for Host bookkeeping; close is best-effort on nil conn.
	a := &Client{name: "fs"}
	b := &Client{name: "fs"}
	if err := p.replace(context.Background(), a, 1); err != nil {
		t.Fatal(err)
	}
	if p.client() != a {
		t.Fatal("active not a")
	}
	if err := p.replace(context.Background(), b, 2); err != nil {
		t.Fatal(err)
	}
	if p.client() != b || p.generation != 2 {
		t.Fatal("active not b")
	}
}

func TestHostReplaceServerBackend(t *testing.T) {
	h := &Host{}
	a := &Client{name: "demo"}
	b := &Client{name: "demo"}
	if err := h.ReplaceServerBackend(context.Background(), "demo", a, 1); err != nil {
		t.Fatal(err)
	}
	if h.client("demo") != a {
		t.Fatal("client should be a")
	}
	if err := h.ReplaceServerBackend(context.Background(), "demo", b, 2); err != nil {
		t.Fatal(err)
	}
	if h.client("demo") != b {
		t.Fatal("client should be b after replace")
	}
	h.Close()
	if h.client("demo") != nil {
		t.Fatal("closed host should not resolve clients")
	}
}
