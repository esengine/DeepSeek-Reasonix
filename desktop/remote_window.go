package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/config"
)

const (
	remoteWindowTicketArgPrefix = "--remote-window-ticket="
	remoteWindowHostArgPrefix   = "--remote-window-host="
	remoteWindowOwnerArgPrefix  = "--remote-window-owner="
	remoteWindowParentArgPrefix = "--remote-window-parent="
	remoteWindowTicketPrefix    = ".remote-window-"
	remoteWindowTicketTTL       = 2 * time.Minute
	remoteWindowTicketMaxBytes  = 16 * 1024
	remoteWindowInstancePrefix  = "com.reasonix.desktop.remote."
)

// remoteWindowLaunch is a one-shot handoff from the primary Reasonix process to
// a lightweight web-window child process. The URL carries the local tunnel token,
// so the descriptor lives in a mode-0600 ticket file instead of the process
// arguments. HostKey is the non-secret per-host digest used both to derive the
// child's Wails single-instance identity and to verify the argv host matches the
// ticket before the child consumes it.
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
// window spawn or navigation.
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

// remoteWindowTicketPath validates the ticket name and resolves it inside the
// Reasonix private state directory. Only the bare generated name is accepted —
// never a path, a traversal, or a foreign filename.
func remoteWindowTicketPath(ticket string) (string, error) {
	if ticket == "" || filepath.Base(ticket) != ticket || !strings.HasPrefix(ticket, remoteWindowTicketPrefix) {
		return "", fmt.Errorf("invalid remote window ticket")
	}
	dir := strings.TrimSpace(config.MemoryUserDir())
	if dir == "" {
		return "", fmt.Errorf("cannot resolve remote window state directory")
	}
	return filepath.Join(dir, ticket), nil
}

func writeRemoteWindowLaunch(launch remoteWindowLaunch) (string, error) {
	if !isSafeRemoteWindowURL(launch.URL) {
		return "", fmt.Errorf("remote window URL must use HTTP on loopback")
	}
	if strings.TrimSpace(launch.HostKey) == "" {
		return "", fmt.Errorf("remote window ticket missing host identity")
	}
	dir := config.MemoryUserDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create remote window state directory: %w", err)
	}
	f, err := os.CreateTemp(dir, remoteWindowTicketPrefix)
	if err != nil {
		return "", fmt.Errorf("create remote window ticket: %w", err)
	}
	path := f.Name()
	remove := true
	defer func() {
		_ = f.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return "", fmt.Errorf("secure remote window ticket: %w", err)
	}
	if err := json.NewEncoder(f).Encode(launch); err != nil {
		return "", fmt.Errorf("write remote window ticket: %w", err)
	}
	if err := f.Sync(); err != nil {
		return "", fmt.Errorf("sync remote window ticket: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close remote window ticket: %w", err)
	}
	remove = false
	return filepath.Base(path), nil
}

// consumeRemoteWindowLaunch reads and immediately deletes the ticket. A ticket
// is one-shot: whoever consumes it (the first window to win the per-host
// single-instance lock, or the existing window receiving a handoff) owns the
// navigation. The file must be a regular 0600 file within the size bound; on
// Unix, broader permissions or symlinks are rejected outright.
func consumeRemoteWindowLaunch(ticket string) (*remoteWindowLaunch, error) {
	path, err := remoteWindowTicketPath(ticket)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect remote window ticket: %w", err)
	}
	defer os.Remove(path)
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("remote window ticket is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > remoteWindowTicketMaxBytes {
		return nil, fmt.Errorf("remote window ticket has an invalid size")
	}
	// Strict TTL: the ticket must be consumed within remoteWindowTicketTTL of
	// being written. This bounds leftover token files even when the spawning
	// process died before its time.AfterFunc backstop could remove them.
	if time.Since(info.ModTime()) > remoteWindowTicketTTL {
		return nil, fmt.Errorf("remote window ticket has expired")
	}
	// Windows does not expose Unix owner/group permission bits through Stat;
	// CreateTemp still creates the file for the current user, while ACLs remain
	// governed by the private user state directory.
	if goruntime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("remote window ticket permissions are too broad")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read remote window ticket: %w", err)
	}
	var launch remoteWindowLaunch
	if err := json.Unmarshal(data, &launch); err != nil {
		return nil, fmt.Errorf("decode remote window ticket: %w", err)
	}
	if !isSafeRemoteWindowURL(launch.URL) {
		return nil, fmt.Errorf("remote window URL must use HTTP on loopback")
	}
	if strings.TrimSpace(launch.HostKey) == "" {
		return nil, fmt.Errorf("remote window ticket missing host identity")
	}
	return &launch, nil
}

// isSafeRemoteWindowURL accepts only plain HTTP on localhost or a loopback IP,
// with no userinfo, and nothing that could smuggle a file, script, or external
// destination through the webview navigation.
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

func remoteWindowNavigationJS(raw string) (string, error) {
	if !isSafeRemoteWindowURL(raw) {
		return "", fmt.Errorf("remote window URL must use HTTP on loopback")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return "", err
	}
	return "window.location.replace(" + string(encoded) + ");", nil
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

// remoteWindowHostKey derives the non-secret per-host identity used for the
// child window's Wails single-instance lock. It is scoped to the Reasonix home
// (so two isolated data homes can each open a window for the same host label)
// and contains no URL, token, or user data — only a digest. The child receives
// this digest in argv and validates it against the ticket before consuming.
func remoteWindowHostKey(hostID string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, singleInstanceIDPrefix+"|")
	_, _ = io.WriteString(h, strings.TrimSpace(config.ReasonixHomeDir())+"|")
	_, _ = io.WriteString(h, hostID)
	return hex.EncodeToString(h.Sum(nil)[:16])
}

func newRemoteWindowOwnerID() string {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		panic("generate remote window owner identity: " + err.Error())
	}
	return hex.EncodeToString(entropy[:])
}

func isRemoteWindowOwnerID(ownerID string) bool {
	if len(ownerID) != 32 {
		return false
	}
	_, err := hex.DecodeString(ownerID)
	return err == nil
}

// remoteWindowInstanceID is the owner-and-host Wails SingleInstanceLock
// identity. Different hosts proceed independently, while the same Desktop
// reuses its existing host window. A restarted Desktop has a new owner identity
// and therefore never adopts a child that its process registry cannot control.
func remoteWindowInstanceID(hostKey, ownerID string) string {
	digest := sha256.Sum256([]byte(hostKey + "|" + ownerID))
	return remoteWindowInstancePrefix + hex.EncodeToString(digest[:16])
}

// remoteWindowSingleInstanceLock wires the child process's owner-and-host lock.
// The second instance never reaches the webview: Wails hands its argv to the
// existing window's OnSecondInstanceLaunch and exits at the gate, so the new
// ticket is consumed exactly once, by the window that owns the host.
func remoteWindowSingleInstanceLock(app *App) *options.SingleInstanceLock {
	return &options.SingleInstanceLock{
		UniqueId: remoteWindowInstanceID(app.remoteWindowHostKey, app.remoteWindowOwnerID),
		OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
			app.secondInstanceRemoteWindow(data)
		},
	}
}

// ── Child process registry (main process) ──

// remoteWindowChild is one spawned web-window process. gen is bumped per spawn
// so a late Wait from an old child can never clear a newer registration.
type remoteWindowChild struct {
	gen  uint64
	pid  int
	proc *os.Process
}

// remoteWindowRegistry tracks, per host, the web-window child processes the
// main process spawned. The per-host value is a set: re-opening a host spawns
// a short-lived handoff process that exits at the Wails single-instance gate
// after passing its ticket to the still-running window, so only that
// handoff's own entry may be cleared by its Wait — the surviving window's
// entry must stay registered. Closing the window (user or terminal
// disconnect) releases only its registration; the remote Serve and the main
// process's SSH connection keep running. A real main-process quit terminates
// survivors.
type remoteWindowRegistry struct {
	mu       sync.Mutex
	children map[string][]remoteWindowChild // per host, one live window plus transient handoffs
	nextGen  uint64
}

func newRemoteWindowRegistry() *remoteWindowRegistry {
	return &remoteWindowRegistry{children: map[string][]remoteWindowChild{}}
}

// record registers proc for hostKey and returns its generation. Each spawn is
// a distinct entry; replacing the host's window never forgets a live one.
func (r *remoteWindowRegistry) record(hostKey string, proc *os.Process) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	gen := r.nextGen
	r.nextGen++
	r.children[hostKey] = append(r.children[hostKey], remoteWindowChild{gen: gen, pid: proc.Pid, proc: proc})
	return gen
}

// clearIf drops exactly the caller's own entry — the Wait result for one
// spawned process. A handoff process that exited at the single-instance gate
// clears only itself; the window it handed the ticket to stays registered.
func (r *remoteWindowRegistry) clearIf(hostKey string, gen uint64, pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entries := r.children[hostKey]
	for i, child := range entries {
		if child.gen == gen && child.pid == pid {
			r.children[hostKey] = append(entries[:i], entries[i+1:]...)
			if len(r.children[hostKey]) == 0 {
				delete(r.children, hostKey)
			}
			return
		}
	}
}

// close terminates every process registered for the host — the live window
// and any transient handoffs — and releases the registration immediately.
// Killing an already-exited handoff is a no-op error.
func (r *remoteWindowRegistry) close(hostKey string) {
	r.mu.Lock()
	entries := r.children[hostKey]
	delete(r.children, hostKey)
	r.mu.Unlock()
	for _, child := range entries {
		if child.proc != nil {
			_ = child.proc.Kill()
		}
	}
}

// has reports whether any child process is currently registered for hostKey —
// true while the live window (or a handoff still in flight) exists.
func (r *remoteWindowRegistry) has(hostKey string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.children[hostKey]
	return ok
}

// closeAll terminates every surviving child window. Used only on real main
// process shutdown — background (tray) close keeps windows and tunnels alive.
func (r *remoteWindowRegistry) closeAll() {
	r.mu.Lock()
	all := make([]*os.Process, 0)
	for _, entries := range r.children {
		for _, child := range entries {
			if child.proc != nil {
				all = append(all, child.proc)
			}
		}
	}
	r.children = map[string][]remoteWindowChild{}
	r.mu.Unlock()
	for _, proc := range all {
		_ = proc.Kill()
	}
}

// ── Spawn / open (main process) ──

// spawnRemoteWindow launches a fresh Reasonix child process for hostKey. Argv
// contains only the ticket name, non-secret host/owner identities, and owner
// PID; the URL and Serve token travel exclusively in the 0600 ticket. When a
// window already exists for this owner and host, the Wails single-instance lock
// routes the ticket to it and this process exits at the gate without showing UI.
func (a *App) spawnRemoteWindow(hostKey string, launch remoteWindowLaunch) error {
	ticket, err := writeRemoteWindowLaunch(launch)
	if err != nil {
		return err
	}
	path, _ := remoteWindowTicketPath(ticket)
	executable, err := os.Executable()
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("locate Reasonix executable: %w", err)
	}
	if !isRemoteWindowOwnerID(a.remoteWindowOwnerID) {
		_ = os.Remove(path)
		return fmt.Errorf("remote window owner identity is unavailable")
	}
	cmd := exec.Command(
		executable,
		remoteWindowTicketArgPrefix+ticket,
		remoteWindowHostArgPrefix+hostKey,
		remoteWindowOwnerArgPrefix+a.remoteWindowOwnerID,
		remoteWindowParentArgPrefix+strconv.Itoa(os.Getpid()),
	)
	if err := cmd.Start(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("start remote Reasonix window: %w", err)
	}
	gen := a.remoteWindows.record(hostKey, cmd.Process)
	// The child (or the existing window it hands off to) normally consumes the
	// ticket immediately. This bounds any leftover token file if every consumer
	// exits before reaching the ticket.
	time.AfterFunc(remoteWindowTicketTTL, func() { _ = os.Remove(path) })
	go func() {
		_ = cmd.Wait()
		a.remoteWindows.clearIf(hostKey, gen, cmd.Process.Pid)
	}()
	return nil
}

// watchRemoteWindowOwner closes a child window when the primary Desktop process
// that owns its SSH tunnel exits. The owner identity also scopes the Wails
// single-instance lock, so a restarted Desktop creates a fresh owned child
// instead of handing a ticket to an unregistered survivor from the old process.
func (a *App) watchRemoteWindowOwner(ctx context.Context) {
	pid := a.remoteWindowParentPID
	if pid <= 0 {
		return
	}
	a.goSafe("remoteWindowOwner", func() {
		if waitForRemoteWindowOwnerExit(ctx, pid) {
			runtime.Quit(ctx)
		}
	})
}

// openRemoteWindowForHost opens (or re-points) the host's web window at rawURL.
// The window open is deliberately the last step: the caller must already have
// a live Serve and loopback tunnel for the target workspace. A failure here is
// delivered to the caller while the Serve stays ready for the target
// workspace; the window can simply be opened again (the Serve is reused) and
// any previous window is left in place until then.
func (a *App) openRemoteWindowForHost(hostID, rawURL string) error {
	launch := remoteWindowLaunch{
		URL:     rawURL,
		Title:   remoteWindowTitle(hostID),
		HostKey: remoteWindowHostKey(hostID),
	}
	if a.remoteWindowOpener != nil {
		return a.remoteWindowOpener(launch)
	}
	return a.spawnRemoteWindow(launch.HostKey, launch)
}

// closeRemoteWindowForHost terminates the host's web window. Called on explicit
// disconnect, stop-server, host removal, and deterministic SSH failure.
func (a *App) closeRemoteWindowForHost(hostID string) {
	if a.remoteWindows == nil {
		return
	}
	a.remoteWindows.close(remoteWindowHostKey(hostID))
}

func (a *App) hasRemoteWindow(hostID string) bool {
	if a.remoteWindows == nil {
		return false
	}
	return a.remoteWindows.has(remoteWindowHostKey(hostID))
}

func (a *App) closeAllRemoteWindows() {
	if a.remoteWindows == nil {
		return
	}
	a.remoteWindows.closeAll()
}

// ── Child process (web window) ──

// remoteWindowAssetMiddleware replaces the primary frontend with a blank dark
// shell while the web window boots, so the child (which exposes no Wails
// bindings) never loads the local app. The shell then navigates to the Serve
// URL. The main process passes this middleware through untouched.
func (a *App) remoteWindowAssetMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if a.remoteWindowTicket == "" || (r.URL.Path != "/" && r.URL.Path != "/index.html") {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
			_, _ = w.Write([]byte(`<!doctype html><html><head><meta charset="utf-8"><style>html{background:#1a1a2e}</style></head><body></body></html>`))
		})
	}
}

// consumeInitialRemoteWindowLaunch consumes the child process's initial ticket
// at most once. WebKit invokes OnDomReady for both the embedded blank shell and
// the remote Serve page loaded by that shell; the second callback must not try
// to consume the already-deleted one-shot ticket and close a healthy window.
func (a *App) consumeInitialRemoteWindowLaunch() (*remoteWindowLaunch, bool, error) {
	a.remoteWindowMu.Lock()
	if a.remoteWindowTicketConsumed {
		a.remoteWindowMu.Unlock()
		return nil, false, nil
	}
	a.remoteWindowTicketConsumed = true
	a.remoteWindowMu.Unlock()

	launch, err := consumeRemoteWindowLaunch(a.remoteWindowTicket)
	return launch, true, err
}

// domReadyRemoteWindow consumes the launch ticket (guarded by the per-host
// single-instance gate, so the second instance never reaches this point) and
// navigates the blank shell to the Serve URL. If a handoff already applied a
// newer ticket before the first domReady, the initial ticket is discarded, not
// applied on top of it. Later domReady callbacks from the remote page are no-ops.
func (a *App) domReadyRemoteWindow() {
	launch, first, err := a.consumeInitialRemoteWindowLaunch()
	if !first {
		return
	}
	if err != nil {
		slog.Warn("remote window: reject launch ticket", "err", err)
		runtime.Quit(a.ctx)
		return
	}
	if launch.HostKey != a.remoteWindowHostKey {
		slog.Warn("remote window: ticket host does not match window identity")
		runtime.Quit(a.ctx)
		return
	}
	a.remoteWindowMu.Lock()
	if a.remoteWindow == nil {
		a.applyRemoteWindowLaunchLocked(launch, true)
	}
	a.remoteWindowMu.Unlock()
	runtime.WindowCenter(a.ctx)
	runtime.WindowShow(a.ctx)
}

// secondInstanceRemoteWindow is the existing window's side of the per-host
// single-instance handoff: a second open for the same host arrives as this
// window's argv. It consumes the new ticket, updates the title, navigates to
// the new URL, and restores + focuses the window. Tickets from another host
// identity are rejected.
func (a *App) secondInstanceRemoteWindow(data options.SecondInstanceData) {
	ticket := ""
	for _, arg := range data.Args {
		if after, ok := strings.CutPrefix(arg, remoteWindowTicketArgPrefix); ok {
			ticket = after
			break
		}
	}
	if ticket == "" {
		// A second launch without a ticket (e.g. a launcher invocation): just
		// bring the existing remote window forward.
		runtime.WindowCenter(a.ctx)
		runtime.WindowShow(a.ctx)
		return
	}
	launch, err := consumeRemoteWindowLaunch(ticket)
	if err != nil {
		slog.Warn("remote window: reject handoff ticket", "err", err)
		return
	}
	a.remoteWindowMu.Lock()
	if launch.HostKey != a.remoteWindowHostKey {
		a.remoteWindowMu.Unlock()
		slog.Warn("remote window: handoff host does not match window identity")
		return
	}
	a.applyRemoteWindowLaunchLocked(launch, false)
	a.remoteWindowMu.Unlock()
}

// applyRemoteWindowLaunchLocked sets the title, navigates the shell to the new
// URL, and restores + focuses the window. The caller holds a.remoteWindowMu so
// a handoff arriving before domReady cannot be overridden by the initial
// ticket, and vice versa.
func (a *App) applyRemoteWindowLaunchLocked(launch *remoteWindowLaunch, initial bool) {
	if launch.Title != "" {
		runtime.WindowSetTitle(a.ctx, launch.Title)
	}
	if js, err := remoteWindowNavigationJS(launch.URL); err == nil {
		runtime.WindowExecJS(a.ctx, js)
	}
	if !initial && runtime.WindowIsMinimised(a.ctx) {
		runtime.WindowUnminimise(a.ctx)
	}
	runtime.WindowCenter(a.ctx)
	runtime.WindowShow(a.ctx)
	a.remoteWindow = launch
}
