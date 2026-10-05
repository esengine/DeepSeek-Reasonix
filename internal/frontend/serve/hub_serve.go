package serve

import (
	"context"
	"net"
)

// RunGraceful serves every runtime behind one listener, draining on ctx the way
// a single server does.
func (h *Hub) RunGraceful(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return h.RunGracefulListener(ctx, ln)
}

// RunGracefulListener is RunGraceful over a caller-supplied listener, for hosts
// that need the bound address before serving (--addr :0 with --port-file).
func (h *Hub) RunGracefulListener(ctx context.Context, ln net.Listener) error {
	h.auth.hosts.ListenOn(ln.Addr().String())
	return runGracefulListener(ctx, ln, h.Handler())
}

// AllowHost lets an auth-disabled hub answer requests addressed to host, such
// as the name a proxy in front of it forwards.
func (h *Hub) AllowHost(host string) { h.auth.hosts.Allow(host) }

// StartRecoveryGC sweeps redundant recovery branches for every open pane, and
// for each one opened later.
func (h *Hub) StartRecoveryGC(ctx context.Context) {
	h.mu.Lock()
	h.gcCtx = ctx
	h.mu.Unlock()
	for _, rt := range h.localRuntimes() {
		rt.Server.StartRecoveryGC(ctx)
	}
}

// EnableProviderSetupForListener opens the credential-writing setup surface on
// loopback only, for every runtime. Reported once for the hub: the panes share
// one listener, so they share its verdict.
func (h *Hub) EnableProviderSetupForListener(addr string) bool {
	if !isLoopbackHost(addr) {
		return false
	}
	h.mu.Lock()
	h.setupAddr = addr
	h.mu.Unlock()
	for _, rt := range h.localRuntimes() {
		rt.Server.EnableProviderSetupForListener(addr)
	}
	return true
}

// adoptHostDecisions applies to a newly opened runtime whatever the host
// already decided for the hub, so a pane opened an hour in is not quietly less
// capable than the one the window started with.
func (h *Hub) adoptHostDecisions(rt *Runtime) {
	h.mu.RLock()
	addr, gcCtx := h.setupAddr, h.gcCtx
	h.mu.RUnlock()
	if addr != "" {
		rt.Server.EnableProviderSetupForListener(addr)
	}
	if gcCtx != nil {
		// Its own cancel, so closing one pane stops that pane's sweep rather
		// than every pane's.
		ctx, cancel := context.WithCancel(gcCtx)
		rt.stop = cancel
		rt.Server.StartRecoveryGC(ctx)
	}
}
