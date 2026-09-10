package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// hostRemoteWindows tracks the remote Serve windows the Electron shell owns on
// this process's behalf. Under the shell there is no child process per host:
// the shell keeps one BrowserWindow per host key and reports its closure.
type hostRemoteWindows struct {
	mu   sync.Mutex
	open map[string]bool
}

func (b *hostShellBridge) remoteWindows() *hostRemoteWindows {
	b.remoteMu.Lock()
	defer b.remoteMu.Unlock()
	if b.remote == nil {
		b.remote = &hostRemoteWindows{open: map[string]bool{}}
	}
	return b.remote
}

func (b *hostShellBridge) openRemoteWindow(launch remoteWindowLaunch) error {
	if launch.HostKey == "" {
		return fmt.Errorf("remote window host key is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcHostWindowTimeout)
	defer cancel()
	params := map[string]string{"hostKey": launch.HostKey, "url": launch.URL, "title": launch.Title}
	if err := b.server.Request(ctx, "host/remoteWindow.open", params, nil); err != nil {
		return fmt.Errorf("open remote window: %w", err)
	}
	windows := b.remoteWindows()
	windows.mu.Lock()
	windows.open[launch.HostKey] = true
	windows.mu.Unlock()
	return nil
}

func (b *hostShellBridge) closeRemoteWindow(hostKey string) {
	windows := b.remoteWindows()
	windows.mu.Lock()
	present := windows.open[hostKey]
	delete(windows.open, hostKey)
	windows.mu.Unlock()
	if !present {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), rpcHostWindowTimeout)
	defer cancel()
	_ = b.server.Request(ctx, "host/remoteWindow.close", map[string]string{"hostKey": hostKey}, nil)
}

func (b *hostShellBridge) hasRemoteWindow(hostKey string) bool {
	windows := b.remoteWindows()
	windows.mu.Lock()
	defer windows.mu.Unlock()
	return windows.open[hostKey]
}

func (b *hostShellBridge) closeAllRemoteWindows() {
	windows := b.remoteWindows()
	windows.mu.Lock()
	keys := make([]string, 0, len(windows.open))
	for key := range windows.open {
		keys = append(keys, key)
	}
	windows.mu.Unlock()
	for _, key := range keys {
		b.closeRemoteWindow(key)
	}
}

// remoteWindowClosed records a window the user closed in the shell so a later
// open creates a fresh window instead of navigating a closed one.
func (b *hostShellBridge) remoteWindowClosed(payload json.RawMessage) {
	var closed struct {
		HostKey string `json:"hostKey"`
	}
	if err := json.Unmarshal(payload, &closed); err != nil || closed.HostKey == "" {
		return
	}
	windows := b.remoteWindows()
	windows.mu.Lock()
	delete(windows.open, closed.HostKey)
	windows.mu.Unlock()
}
