package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"reasonix/internal/config"
)

// remoteWindowLaunch is one open-or-repoint request for a host's remote Serve
// window. Under the Electron shell the window is a BrowserWindow the shell
// owns; HostKey is the non-secret per-host digest that keys it.
type remoteWindowLaunch struct {
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	HostKey string `json:"hostKey,omitempty"`
}

// remoteWindowLifecycleRegistry linearizes window/Serve lifecycle operations
// per host while allowing different hosts to proceed independently. begin
// advances the host generation before waiting for the mutex: a later explicit
// action or SSH status event can therefore supersede an older operation that is
// still blocked in EnsureServer. Entries intentionally live for the App process
// lifetime; their cardinality is bounded by host identities used in that run.
type remoteWindowLifecycleRegistry struct {
	hosts sync.Map // map[string]*remoteWindowHostLifecycle
}

type remoteWindowHostLifecycle struct {
	mu         sync.Mutex
	generation atomic.Uint64
}

type remoteWindowHostOperation struct {
	host       *remoteWindowHostLifecycle
	generation uint64
}

func (r *remoteWindowLifecycleRegistry) begin(hostKey string) remoteWindowHostOperation {
	value, _ := r.hosts.LoadOrStore(hostKey, &remoteWindowHostLifecycle{})
	host := value.(*remoteWindowHostLifecycle)
	return remoteWindowHostOperation{host: host, generation: host.generation.Add(1)}
}

// run executes fn only while this operation is still the newest request for
// the host. fn may re-check current after a slow boundary before committing a
// window open or navigation.
func (op remoteWindowHostOperation) run(fn func(current func() bool) error) error {
	if op.host == nil {
		return nil
	}
	op.host.mu.Lock()
	defer op.host.mu.Unlock()
	current := func() bool { return op.host.generation.Load() == op.generation }
	if !current() {
		return nil
	}
	return fn(current)
}

func (a *App) beginRemoteWindowHostOperation(hostID string) remoteWindowHostOperation {
	return a.remoteWindowLifecycles.begin(remoteWindowHostKey(hostID))
}

// isSafeRemoteWindowURL accepts only plain HTTP on localhost or a loopback IP,
// with no userinfo, and nothing that could smuggle a file, script, or external
// destination through the shell window navigation.
func isSafeRemoteWindowURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil {
		return false
	}
	host := strings.TrimSpace(u.Hostname())
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func remoteWindowTitle(hostID string) string {
	hostID = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, hostID))
	runes := []rune(hostID)
	if len(runes) > 80 {
		hostID = string(runes[:80]) + "…"
	}
	if hostID == "" {
		hostID = "Remote"
	}
	return "Reasonix [SSH: " + hostID + "]"
}

// remoteWindowHostKey derives the non-secret per-host identity that keys the
// shell's BrowserWindow. It is scoped to the Reasonix home (so two isolated
// data homes can each open a window for the same host label) and contains no
// URL, token, or user data — only a digest.
func remoteWindowHostKey(hostID string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, singleInstanceIDPrefix+"|")
	_, _ = io.WriteString(h, strings.TrimSpace(config.ReasonixHomeDir())+"|")
	_, _ = io.WriteString(h, hostID)
	return hex.EncodeToString(h.Sum(nil)[:16])
}

// remoteWindowRegistry records which workspace each host's window is showing,
// so a reconnect refresh or a per-workspace stop can act on the right serve.
type remoteWindowRegistry struct {
	mu         sync.Mutex
	workspaces map[string]string // hostKey → workspace the window currently shows
}

func newRemoteWindowRegistry() *remoteWindowRegistry {
	return &remoteWindowRegistry{workspaces: map[string]string{}}
}

func (r *remoteWindowRegistry) setWorkspace(hostKey, workspace string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaces[hostKey] = workspace
}

// workspaceFor returns the workspace the host's window was last opened on
// ("" when unknown).
func (r *remoteWindowRegistry) workspaceFor(hostKey string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.workspaces[hostKey]
}

func (r *remoteWindowRegistry) forget(hostKey string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.workspaces, hostKey)
}

func (r *remoteWindowRegistry) forgetAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workspaces = map[string]string{}
}

// openRemoteWindowForHost opens (or re-points) the host's web window at rawURL.
// The window open is deliberately the last step: the caller must already have
// a live Serve and loopback tunnel for the target workspace. A failure here is
// delivered to the caller while the Serve stays ready for the target
// workspace; the window can simply be opened again (the Serve is reused) and
// any previous window is left in place until then.
func (a *App) openRemoteWindowForHost(hostID, workspace, rawURL string) error {
	hostKey := remoteWindowHostKey(hostID)
	if a.remoteWindows != nil {
		a.remoteWindows.setWorkspace(hostKey, workspace)
	}
	launch := remoteWindowLaunch{
		URL:     rawURL,
		Title:   remoteWindowTitle(hostID),
		HostKey: hostKey,
	}
	if !isSafeRemoteWindowURL(launch.URL) {
		return fmt.Errorf("remote window URL must use HTTP on loopback")
	}
	if a.remoteWindowOpener != nil {
		return a.remoteWindowOpener(launch)
	}
	if a.hostMode() {
		return a.hostShell.openRemoteWindow(launch)
	}
	return fmt.Errorf("remote windows require the Electron desktop shell")
}

// remoteWindowWorkspace reports which workspace the host's web window is
// currently showing ("" when no window or pre-tracking open).
func (a *App) remoteWindowWorkspace(hostID string) string {
	if a.remoteWindows == nil {
		return ""
	}
	return a.remoteWindows.workspaceFor(remoteWindowHostKey(hostID))
}

// closeRemoteWindowForHost closes the host's web window. Called on explicit
// disconnect, stop-server, host removal, and deterministic SSH failure.
func (a *App) closeRemoteWindowForHost(hostID string) {
	hostKey := remoteWindowHostKey(hostID)
	if a.hostMode() {
		a.hostShell.closeRemoteWindow(hostKey)
	}
	if a.remoteWindows == nil {
		return
	}
	a.remoteWindows.forget(hostKey)
}

func (a *App) hasRemoteWindow(hostID string) bool {
	if a.hostMode() {
		return a.hostShell.hasRemoteWindow(remoteWindowHostKey(hostID))
	}
	return false
}

func (a *App) closeAllRemoteWindows() {
	if a.hostMode() {
		a.hostShell.closeAllRemoteWindows()
	}
	if a.remoteWindows == nil {
		return
	}
	a.remoteWindows.forgetAll()
}
