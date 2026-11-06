package serve

import (
	"errors"
	"fmt"
	"log/slog"
)

// errRuntimeGone is Close's answer for an id the hub does not hold.
var errRuntimeGone = errors.New("no such runtime")

// Close retires a runtime: the conversation is persisted, the assembly torn
// down, and the session file released for another window to open.
func (h *Hub) Close(id string) error {
	h.mu.Lock()
	rt := h.runtimes[id]
	if rt == nil {
		h.mu.Unlock()
		return fmt.Errorf("%w: %s", errRuntimeGone, id)
	}
	delete(h.runtimes, id)
	h.order = removeString(h.order, id)
	h.mu.Unlock()
	defer h.treeChanged()
	defer h.muxes.paneClosed(id)

	if rt.Local() {
		rt.Server.preparations.close()
		rt.Server.terminals.CloseAll()
		rt.Server.bindMu.Lock()
		defer rt.Server.bindMu.Unlock()
	}
	if h.opts.OnClose != nil {
		h.opts.OnClose(rt)
	}
	if rt.stop != nil {
		rt.stop()
	}
	if rt.remote != nil {
		// Releasing the forward first would strand the far session's write lease.
		rt.closeFarRuntime()
		if rt.remote.release != nil {
			rt.remote.release()
		}
		return nil
	}
	if err := rt.Server.Controller().Snapshot(); err != nil {
		slog.Warn("serve: snapshot before closing runtime", "id", id, "err", err)
	}
	rt.Server.Controller().Close()
	if rt.leases != nil {
		rt.leases.Release()
	}
	return nil
}

// Shutdown closes every runtime, newest first.
func (h *Hub) Shutdown() {
	defer h.closeLegacyListings()
	for _, view := range h.List() {
		if err := h.Close(view.ID); err != nil {
			slog.Warn("serve: close runtime", "id", view.ID, "err", err)
		}
	}
}
