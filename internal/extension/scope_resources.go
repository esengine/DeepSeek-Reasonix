package extension

import (
	"context"
	"fmt"
	"io"
)

// TrackMCPClient registers an MCP client/transport closer as a reversible effect.
func TrackMCPClient(scope EffectScope, serverName string, c io.Closer) error {
	if scope == nil || c == nil {
		return nil
	}
	return scope.Track(Effect{
		ID:    "mcp-client:" + serverName,
		Owner: "mcp",
		Class: Reversible,
		Dispose: func(context.Context) error {
			return c.Close()
		},
	})
}

// FuncCloser adapts a func() into io.Closer for TrackMCPClient call sites.
type FuncCloser func()

// Close implements io.Closer.
func (f FuncCloser) Close() error {
	if f != nil {
		f()
	}
	return nil
}

// TrackUIHub registers a UI hub binding. Dispose is a no-op close of the
// binding registration (hub itself may be reused across generations).
func TrackUIHub(scope EffectScope, generation uint64) error {
	if scope == nil {
		return nil
	}
	return scope.Track(Effect{
		ID:        fmt.Sprintf("ui-hub-gen-%d", generation),
		Owner:     "uihub",
		Component: "extension-ui",
		Class:     Reversible,
		Dispose:   func(context.Context) error { return nil },
	})
}

// TrackProviderStream registers a cancelable provider stream so Dispose waits
// for background delivery to finish (caller supplies wait).
func TrackProviderStream(scope EffectScope, streamID string, cancel func(), wait func(context.Context) error) error {
	if scope == nil {
		return nil
	}
	return scope.Track(Effect{
		ID:    "provider-stream:" + streamID,
		Owner: "providerext",
		Class: Cancelable,
		Dispose: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			if wait != nil {
				return wait(ctx)
			}
			return nil
		},
	})
}

// TrackWatcher registers a filesystem or config watcher.
func TrackWatcher(scope EffectScope, id string, stop func() error) error {
	if scope == nil || stop == nil {
		return nil
	}
	return scope.Track(Effect{
		ID:    "watcher:" + id,
		Owner: "watcher",
		Class: Reversible,
		Dispose: func(context.Context) error {
			return stop()
		},
	})
}

// TrackEventSubscription registers an event subscription teardown.
func TrackEventSubscription(scope EffectScope, id string, unsub func() error) error {
	if scope == nil || unsub == nil {
		return nil
	}
	return scope.Track(Effect{
		ID:    "event-sub:" + id,
		Owner: "events",
		Class: Reversible,
		Dispose: func(context.Context) error {
			return unsub()
		},
	})
}

// TrackBackgroundJob registers cancelable background work.
func TrackBackgroundJob(scope EffectScope, id string, cancel func(), wait func(context.Context) error) error {
	if scope == nil {
		return nil
	}
	return scope.Track(Effect{
		ID:    "bg:" + id,
		Owner: "background",
		Class: Cancelable,
		Dispose: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			if wait != nil {
				return wait(ctx)
			}
			return nil
		},
	})
}

// TrackControllerCleanup registers a controller cleanup callback.
func TrackControllerCleanup(scope EffectScope, id string, cleanup func() error) error {
	if scope == nil || cleanup == nil {
		return nil
	}
	return scope.Track(Effect{
		ID:    "controller-cleanup:" + id,
		Owner: "control",
		Class: Reversible,
		Dispose: func(context.Context) error {
			return cleanup()
		},
	})
}

// TrackIrreversible records an irreversible external action (message send,
// provider request submitted) for recovery assessment — dispose never claims
// rollback success.
func TrackIrreversible(scope EffectScope, id, owner string) error {
	if scope == nil {
		return nil
	}
	err := scope.Track(Effect{
		ID:    id,
		Owner: owner,
		Class: Irreversible,
		Dispose: func(context.Context) error {
			return nil
		},
	})
	if err == nil {
		RuntimeOwnerOrDefault(nil).Receipts.Record(EffectReceipt{
			ID:                 id,
			Owner:              owner,
			Class:              Irreversible,
			Generation:         scope.Generation(),
			CompensationStatus: "not_applicable",
		})
	}
	return err
}
