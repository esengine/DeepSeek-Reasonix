// Browser relay server — local WebSocket bridge for browser extension.
//
// The server binds to 127.0.0.1, generates a random token at startup, and
// waits for a browser extension (Chrome) to connect and authenticate. Once
// authorized, built-in browser_* tools can send CDP commands through it.
//
// Design goal: minimal upstream intrusion — one file, zero changes to existing
// Go code (App field + startup line are the only touch points).

package main

import (
	"context"
	"log"
	"sync"

	"reasonix/internal/browserrelay"
)

// browserRelayServer wraps browserrelay.Server for the desktop App lifecycle.
type browserRelayServer struct {
	mu     sync.Mutex
	server *browserrelay.Server
	addr   string
}

// newBrowserRelayServer creates an unstarted browser relay server.
func newBrowserRelayServer() *browserRelayServer {
	return &browserRelayServer{}
}

// Start begins the relay server on a random localhost port.
func (br *browserRelayServer) Start(ctx context.Context) {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.server != nil {
		return // already started
	}

	srv := browserrelay.NewServer()
	browserrelay.DefaultListenAddr = "127.0.0.1:23002"
	addr, err := srv.Start(ctx)
	if err != nil {
		log.Printf("browserrelay: failed to start server: %v", err)
		return
	}

	// Set the package-level default so built-in browser_* tools can use it.
	browserrelay.DefaultServer = srv

	br.server = srv
	br.addr = addr

	log.Printf("browserrelay: server started on %s (token: %s)",
		addr, srv.Token())
}

// Stop gracefully shuts down the relay server.
func (br *browserRelayServer) Stop() {
	br.mu.Lock()
	defer br.mu.Unlock()

	if br.server != nil {
		br.server.Stop()
		br.server = nil
		br.addr = ""
		browserrelay.DefaultServer = nil
	}
}

// Addr returns the server's listening address.
func (br *browserRelayServer) Addr() string {
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.addr
}

// Token returns the authentication token.
func (br *browserRelayServer) Token() string {
	br.mu.Lock()
	defer br.mu.Unlock()
	if br.server == nil {
		return ""
	}
	return br.server.Token()
}

// Status returns the current server status.
func (br *browserRelayServer) Status() browserrelay.Status {
	br.mu.Lock()
	defer br.mu.Unlock()
	if br.server == nil {
		return browserrelay.Status{}
	}
	return br.server.Status()
}

// ── Wails bindings (on App, for frontend) ──────────────────────────────────

// BrowserRelayStatus returns the relay server connection status for the frontend.
func (a *App) BrowserRelayStatus() browserrelay.Status {
	if a.browserRelay == nil {
		return browserrelay.Status{}
	}
	return a.browserRelay.Status()
}

// BrowserRelayAddr returns the relay server WebSocket address.
func (a *App) BrowserRelayAddr() string {
	if a.browserRelay == nil {
		return ""
	}
	return a.browserRelay.Addr()
}

// BrowserRelayToken returns the relay server authentication token.
func (a *App) BrowserRelayToken() string {
	if a.browserRelay == nil {
		return ""
	}
	return a.browserRelay.Token()
}
