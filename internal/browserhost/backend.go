// Package browserhost is the generation-scoped bridge between Extension
// Protocol v2 host/browser/* RPCs and a frontend-owned browser backend.
//
// The desktop App provides a tab-bound Backend that talks to the global
// BrowserCoordinator / Chromium companion. CLI, serve, ACP, and TUI leave
// BrowserHost nil so the dependency graph never advertises
// reasonix/browser/companion and browser-dependent plugins stay Inactive.
package browserhost

import (
	"context"

	"reasonix/internal/extension/protocol"
)

// Backend is the restricted browser surface a frontend binds to one chat.
// Implementations must not expose Close, Cookie, permission mutation,
// download, screenshot, or arbitrary JavaScript.
type Backend interface {
	List(ctx context.Context) ([]protocol.BrowserTab, error)
	Open(ctx context.Context, p protocol.BrowserTabOpenParams) (protocol.BrowserTab, error)
	Snapshot(ctx context.Context, p protocol.BrowserTabSnapshotParams) (protocol.BrowserTabSnapshotResult, error)
	Wait(ctx context.Context, p protocol.BrowserTabWaitParams) (protocol.BrowserTab, error)
	Act(ctx context.Context, p protocol.BrowserTabActParams) (protocol.BrowserTab, error)
}
