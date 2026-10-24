package plugin

import (
	"context"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestDeferredHandshakePersistsBeforeLazyObserverPublishes(t *testing.T) {
	redirectCache(t)
	host := NewHost()
	t.Cleanup(host.Close)
	spec := helperSpec()
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(context.Canceled)
	tools := LazyToolset(spec, nil, host, tool.NewRegistry(), ctx, false)
	spawn := tools[0].(*lazyTool).shared
	if _, err := host.EnsureConnectedWithLifecycle(ctx, t.Context(), spec, spawn.generation); err != nil {
		t.Fatal(err)
	}
	if !host.HasClientForSpec(spec) {
		t.Fatal("handshake did not publish its client")
	}
	cancel(context.Canceled)
	host.Close()
	cached, ok := LoadCachedSchemaForSpec(spec)
	if !ok || len(cached.Tools) != 2 {
		t.Fatalf("completed handshake lost its schema when the lazy observer was cancelled: %+v", cached)
	}
}
