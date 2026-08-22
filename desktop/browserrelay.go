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
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
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

	// Never log the bearer token — captured stdout/diagnostics could otherwise
	// authenticate to the localhost relay. The settings page fetches the full
	// token through the Wails binding instead.
	log.Printf("browserrelay: server started on %s (token %s…)",
		addr, srv.Token()[:min(8, len(srv.Token()))])
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

// RotateToken generates a new authentication token, persisting it and dropping
// the current extension connection.
func (br *browserRelayServer) RotateToken() (string, error) {
	br.mu.Lock()
	defer br.mu.Unlock()
	if br.server == nil {
		return "", errors.New("relay server not running")
	}
	return br.server.RotateToken()
}

// extensionDirPath returns the local browser extension directory. It checks
// REASONIX_EXTENSION_PATH, then the source layout (dev: repo root) and the
// bundled layout (release: beside the executable or its resources dir), and
// returns the first candidate that actually exists. Empty when none does —
// the settings page then tells the user to set REASONIX_EXTENSION_PATH.
func extensionDirPath() string {
	if p := os.Getenv("REASONIX_EXTENSION_PATH"); p != "" {
		return p
	}
	var candidates []string
	// Source layout (dev): desktop/browserrelay.go → repo root/extensions.
	if _, file, _, ok := runtime.Caller(0); ok {
		root := filepath.Dir(filepath.Dir(file))
		candidates = append(candidates, filepath.Join(root, "extensions", "chrome-extension"))
	}
	// Bundled layout (release): beside the executable or its resources dir.
	if exe, err := os.Executable(); err == nil {
		binDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(binDir, "extensions", "chrome-extension"),
			// macOS bundle: Reasonix.app/Contents/Resources/extensions/... (the
			// binary lives in Contents/MacOS). Check both casings — APFS is
			// case-insensitive by default but the standard layout uses "Resources".
			filepath.Join(binDir, "..", "Resources", "extensions", "chrome-extension"),
			filepath.Join(binDir, "..", "resources", "extensions", "chrome-extension"),
		)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	return ""
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

// RelayTabInfo mirrors the extension's attached-tab list entry.
type RelayTabInfo struct {
	TabID  int    `json:"tabId"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// ListTabs queries the extension for the tabs currently attached to it.
func (br *browserRelayServer) ListTabs() ([]RelayTabInfo, error) {
	br.mu.Lock()
	defer br.mu.Unlock()
	if br.server == nil {
		return nil, errors.New("relay server not running")
	}
	raw, err := br.server.SendTabCommand(context.Background(), "list_attached", nil)
	if err != nil {
		return nil, err
	}
	var tabs []RelayTabInfo
	if err := json.Unmarshal(raw, &tabs); err != nil {
		return nil, err
	}
	return tabs, nil
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

// BrowserRelayAttachedTabs returns the tabs currently attached to the relay
// server, queried live from the extension. Empty on error (e.g. disconnected).
// Always returns a non-nil slice so Wails serializes it as [] (not null).
func (a *App) BrowserRelayAttachedTabs() []RelayTabInfo {
	if a.browserRelay == nil {
		return []RelayTabInfo{}
	}
	tabs, err := a.browserRelay.ListTabs()
	if err != nil || tabs == nil {
		return []RelayTabInfo{}
	}
	return tabs
}

// BrowserRelayRotateToken regenerates the relay auth token. The extension's
// current session is dropped and must re-authenticate with the new token.
func (a *App) BrowserRelayRotateToken() string {
	if a.browserRelay == nil {
		return ""
	}
	token, err := a.browserRelay.RotateToken()
	if err != nil {
		return ""
	}
	return token
}

// BrowserRelayExtensionPath returns the local path of the browser extension
// directory, used for manual install instructions.
func (a *App) BrowserRelayExtensionPath() string {
	return extensionDirPath()
}
