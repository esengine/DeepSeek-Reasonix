package extension

import (
	"context"
	"sync/atomic"
	"testing"
)

type fakeMCP struct {
	id     string
	closed atomic.Bool
	calls  atomic.Int32
}

func (m *fakeMCP) ID() string { return m.id }
func (m *fakeMCP) Close(context.Context) error {
	m.closed.Store(true)
	return nil
}
func (m *fakeMCP) Call(context.Context, string, []byte) ([]byte, error) {
	m.calls.Add(1)
	return []byte(`{"ok":true}`), nil
}

func TestMCPProxyRollingReplace(t *testing.T) {
	p := NewMCPProxy("fs")
	a := &fakeMCP{id: "a"}
	b := &fakeMCP{id: "b"}
	if err := p.Replace(context.Background(), a, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Call(context.Background(), "read", nil); err != nil {
		t.Fatal(err)
	}
	if a.calls.Load() != 1 {
		t.Fatalf("calls = %d", a.calls.Load())
	}
	if err := p.Replace(context.Background(), b, 2); err != nil {
		t.Fatal(err)
	}
	if !a.closed.Load() {
		t.Fatal("previous MCP backend not drained")
	}
	if _, err := p.Call(context.Background(), "read", nil); err != nil {
		t.Fatal(err)
	}
	if b.calls.Load() != 1 || p.Generation() != 2 {
		t.Fatalf("backend b not active: calls=%d gen=%d", b.calls.Load(), p.Generation())
	}
}

func TestMCPProxyFailFastWithoutBackend(t *testing.T) {
	p := NewMCPProxy("empty")
	if _, err := p.Call(context.Background(), "x", nil); err == nil {
		t.Fatal("expected fail-fast")
	}
}
