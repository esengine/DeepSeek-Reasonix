// BrowserCoordinator is the single lifecycle and routing entry point between
// the Reasonix desktop host and the Browser Companion child process. It owns
// the stopped -> starting -> ready -> crashed -> (disabled) state machine,
// spawns the companion lazily on first use, speaks the length-prefixed JSON
// frame protocol over the child's stdin/stdout, and bounds every call by
// request budget, timeout, and cancellation.
//
// The coordinator never starts the companion during app startup: cold-start
// latency of the main shell is unaffected by the browser.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/config"

	"reasonix/desktop/internal/browseripc"
)

// BrowserCoordinatorState is the host-visible lifecycle state.
type BrowserCoordinatorState string

const (
	browserStopped  BrowserCoordinatorState = "stopped"
	browserStarting BrowserCoordinatorState = "starting"
	browserReady    BrowserCoordinatorState = "ready"
	browserCrashed  BrowserCoordinatorState = "crashed"
	browserDisabled BrowserCoordinatorState = "disabled"
)

// Sentinel host errors. The companion may be absent (component not installed),
// disabled for this session after repeated failures, or simply not ready yet.
// These never cross the wire; they are translated into tool/Wails results.
var (
	ErrBrowserComponentMissing = errors.New("browser companion component is not installed")
	ErrBrowserDisabled         = errors.New("browser companion is disabled for this session")
	ErrBrowserNotReady         = errors.New("browser companion is not ready")
	ErrBrowserCrashed          = errors.New("browser companion crashed")
	ErrBrowserShuttingDown     = errors.New("browser companion is shutting down")
)

// browserCompanionEnvAllowlist is the complete environment passed to the child.
// Everything else is dropped, so API keys, tokens, and provider secrets that
// live in the host environment can never leak into the companion process.
var browserCompanionEnvAllowlist = []string{
	"PATH",
	"HOME",
	"TMPDIR",
	"TMP",
	"TEMP",
	"LANG",
	"LC_ALL",
	"LC_CTYPE",
	"LC_MESSAGES",
	"USER",
	"LOGNAME",
	"SHELL",
	"TERM",
	"NO_COLOR",
	// Linux/Wayland display plumbing so the Electron window can open.
	"DISPLAY",
	"WAYLAND_DISPLAY",
	"XDG_RUNTIME_DIR",
	"DBUS_SESSION_BUS_ADDRESS",
}

// browserBackoff is the minimum interval between spawn attempts, indexed by
// consecutive failure count.
var browserBackoff = []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second, 5 * time.Second}

const (
	browserMaxFailures        = 3
	browserFailureWindow      = 60 * time.Second
	browserStderrBufferLimit  = 64 * 1024
	browserComponentDirName   = "browser-components"
	browserCurrentManifest    = "current.json"
	browserComponentBinaryDir = "browser"
)

// BrowserCoordinatorView is the renderer-safe status surface for Wails.
// Slices are always non-nil so the frontend contract is [] never null.
type BrowserCoordinatorView struct {
	State                   BrowserCoordinatorState `json:"state"`
	ComponentVersion        string                  `json:"componentVersion"`
	ProtocolVersion         int                     `json:"protocolVersion"`
	ElectronVersion         string                  `json:"electronVersion"`
	ChromiumVersion         string                  `json:"chromiumVersion"`
	PID                     int                     `json:"pid"`
	LastError               string                  `json:"lastError,omitempty"`
	RecoveryAvailable       bool                    `json:"recoveryAvailable"`
	Retryable               bool                    `json:"retryable"`
	InstalledComponent      string                  `json:"installedComponent,omitempty"`
	PendingRequests         int                     `json:"pendingRequests"`
	Capabilities            []string                `json:"capabilities"`
	AgentBrowserToolEnabled bool                    `json:"agentBrowserToolEnabled"`
}

type pendingBrowserCall struct {
	reply    chan browseripc.Response
	deadline time.Time
	timer    *time.Timer
	// ownerEpoch is the deletion epoch captured when the request was
	// registered; the response path validates it before touching the mirror.
	ownerEpoch uint64
}

type browserCoordinatorOptions struct {
	// resolveBinary locates the companion executable. Defaults to the standard
	// user-level component directory layout.
	resolveBinary func() (string, error)
	// spawn is the exec wrapper; tests replace it to run fake companions.
	spawn func(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error)
	now   func() time.Time
	// responseTimeout bounds every request including the handshake.
	responseTimeout time.Duration
	// shutdownGrace is how long Close waits for a graceful exit before kill.
	shutdownGrace time.Duration
	// failureWindow/maxFailures define the disable rule (3 failures in 60s).
	failureWindow time.Duration
	maxFailures   int
	// events receives companion notifications (never nil after construction).
	events func(browseripc.Event)
	// onCrash fires when the companion process dies or violates the protocol.
	onCrash func()
	// stderr receives capped, redacted child stderr.
	stderr io.Writer
}

type browserCoordinator struct {
	opts browserCoordinatorOptions

	mu      sync.Mutex
	state   BrowserCoordinatorState
	hello   *browseripc.HelloResult
	lastErr error
	// consecutive failures and when the current streak started.
	failures       int
	failureStarted time.Time
	lastFailureAt  time.Time
	cmd            *exec.Cmd
	writer         io.WriteCloser
	// waitCh resolves exactly once per spawned process; every waiter (graceful
	// close, kill) consumes the same channel so exec.Cmd.Wait never runs twice.
	waitCh chan error
	// pending requests by requestId.
	pending map[string]*pendingBrowserCall
	// owner -> tabs mirror. This is the host's authoritative in-memory copy of
	// the companion's tab layout, persisted by browserstate (browser-state-v1.json)
	// and used to rehydrate a restarted companion.
	owners map[string]*browserOwnerState
	// companionGen is incremented on every successful handshake. Each owner
	// records the generation it was restored into (browserOwnerState.restoredGen),
	// so a companion crash+restart re-restores every owner into the new
	// process — the host mirror's tab IDs belong to the previous process and
	// must be remapped, never reused.
	companionGen uint64
	// restoreBarrier buffers companion events for owners whose restore is in
	// flight. The real companion emits navigation/tab.changed BEFORE the
	// tab.open response, so without a barrier those events would create and
	// mutate the owner ahead of the atomic restore commit (built from the
	// successful responses in persisted order). Buffered events are replayed
	// onto the committed mirror; entries are deleted when the owner commits.
	restoreBarrier map[string][]browseripc.Event
	// tombstonedOwners marks chats deleted (RemoveOwner) while their restore
	// was in flight. The restore commit must not resurrect a deleted chat's
	// tabs. The tombstone is cleared only by an explicit host-side reopen
	// (tab.open response); async companion events never clear it.
	tombstonedOwners map[string]bool
	// procToken identifies the current companion process; it is incremented on
	// every spawn. failClosed validates it so a stale process's death can
	// never tear down a newer generation, and failClosedGen makes the teardown
	// idempotent per generation (the crash callback fires exactly once).
	procToken     uint64
	failClosedGen uint64
	// ownerEpoch counts deletions per owner (RemoveOwner increments). Requests
	// capture the epoch at registration, re-check it under writeMu right
	// before the frame goes out, and responses validate it again — so a
	// request that passed the tombstone check before a deletion can never
	// reach the wire or resurrect the owner afterwards.
	ownerEpoch map[string]uint64
	// writeMu serializes complete frame writes to the child's stdin. A frame
	// is written as header + payload in two Write calls; concurrent writers
	// (restore replay, RemoveOwner cleanup, ordinary requests, cancel) would
	// interleave them and corrupt the wire. It guards the whole WriteRequest,
	// not individual Write calls.
	writeMu sync.Mutex
	// restorePassActive marks an in-flight restore pass. Owner commits during
	// the pass skip their individual persistence; the pass persists once at
	// the end (or not at all when the pass aborts).
	restorePassActive bool
	// eventSink is set once before the first call; tabsChanged fires on mirror
	// mutations so the persistence layer can write atomically.
	tabsChanged func()

	requestSeq atomic.Uint64
	closed     bool
}

// browserOwnerState mirrors one chat's tabs: stable tab order, active tab, and
// per-tab generation for stale-ref protection.
type browserOwnerState struct {
	ownerID   string
	tabs      []browserTabState
	activeTab string
	// restoredGen is the companionGen this mirror was restored into. A mirror
	// whose restoredGen is older than the current companionGen belongs to a
	// dead companion process and must be remapped on the next restore pass.
	restoredGen uint64
}

// browserTabState is the serializable per-tab mirror.
type browserTabState struct {
	tabID      string
	url        string
	title      string
	generation int64
}

func newBrowserCoordinator(opts browserCoordinatorOptions) *browserCoordinator {
	if opts.resolveBinary == nil {
		opts.resolveBinary = resolveBrowserComponentBinary
	}
	if opts.spawn == nil {
		opts.spawn = spawnBrowserCompanion
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.responseTimeout <= 0 {
		opts.responseTimeout = time.Duration(browseripc.ResponseTimeoutMs) * time.Millisecond
	}
	if opts.shutdownGrace <= 0 {
		opts.shutdownGrace = time.Duration(browseripc.ShutdownGraceMs) * time.Millisecond
	}
	if opts.failureWindow <= 0 {
		opts.failureWindow = browserFailureWindow
	}
	if opts.maxFailures <= 0 {
		opts.maxFailures = browserMaxFailures
	}
	if opts.events == nil {
		opts.events = func(browseripc.Event) {}
	}
	if opts.onCrash == nil {
		opts.onCrash = func() {}
	}
	if opts.stderr == nil {
		opts.stderr = io.Discard
	}
	return &browserCoordinator{
		opts:             opts,
		state:            browserStopped,
		pending:          make(map[string]*pendingBrowserCall),
		owners:           make(map[string]*browserOwnerState),
		restoreBarrier:   make(map[string][]browseripc.Event),
		tombstonedOwners: make(map[string]bool),
		ownerEpoch:       make(map[string]uint64),
		// procToken starts at 1 so the zero value never collides with
		// failClosedGen's "nothing torn down yet" sentinel.
		procToken: 1,
	}
}

// State reports the current lifecycle state.
func (b *browserCoordinator) State() BrowserCoordinatorState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Status returns the renderer-safe status snapshot.
func (b *browserCoordinator) Status() BrowserCoordinatorView {
	b.mu.Lock()
	defer b.mu.Unlock()
	installed := b.installedComponentVersionLocked()
	view := BrowserCoordinatorView{
		State:              b.state,
		ProtocolVersion:    browseripc.ProtocolVersion,
		InstalledComponent: installed,
		// The recovery entry is offered for crashed/disabled states and
		// whenever no component is installed at all (settings install/repair).
		RecoveryAvailable: b.state == browserDisabled || b.state == browserCrashed || installed == "",
		PendingRequests:   len(b.pending),
		Capabilities:      []string{},
	}
	if b.hello != nil {
		view.ComponentVersion = b.hello.ComponentVersion
		view.ElectronVersion = b.hello.ElectronVersion
		view.ChromiumVersion = b.hello.ChromiumVersion
		view.PID = b.hello.PID
		view.Capabilities = append([]string(nil), b.hello.Capabilities.Methods...)
	}
	if b.lastErr != nil {
		view.LastError = b.lastErr.Error()
	}
	view.Retryable = b.state == browserCrashed || b.state == browserStarting
	return view
}

// Call sends one request and waits for its response. ownerID is bound by the
// caller (the chat tab ID) — a model cannot choose or reach another owner's
// tabs because the host builds the ownerId, never the model.
func (b *browserCoordinator) Call(ctx context.Context, ownerID, method string, params, result any) error {
	// Lazily start on first use; never during app startup.
	if err := b.ensureReady(ctx); err != nil {
		return err
	}
	return b.callDirect(ctx, ownerID, method, params, result)
}

// callDirect sends a request without the lifecycle gate. It is used by the
// handshake and shutdown paths, which must talk to a just-spawned or shutting
// down process.
func (b *browserCoordinator) callDirect(ctx context.Context, ownerID, method string, params, result any) error {
	req := browseripc.Request{
		ProtocolVersion: browseripc.ProtocolVersion,
		RequestID:       b.nextRequestID(),
		OwnerID:         ownerID,
		Method:          method,
		Params:          mustBrowserParams(params),
	}
	if err := browseripc.ValidateRequest(req); err != nil {
		return fmt.Errorf("browseripc: %w", err)
	}

	// A request addressed to a deleted chat is stale: the deletion made it
	// invalid. Only tab.open (explicit reopen) and owner.remove (cleanup)
	// may still be sent to a tombstoned owner. The epoch captured here is
	// re-checked under writeMu before the frame goes out and again when the
	// response lands, so a deletion cannot slip in between.
	var epoch uint64
	if ownerID != "" {
		b.mu.Lock()
		if b.tombstonedOwners[ownerID] && req.Method != "tab.open" && req.Method != "owner.remove" {
			b.mu.Unlock()
			return fmt.Errorf("browser companion: owner %q was deleted", ownerID)
		}
		epoch = b.ownerEpoch[ownerID]
		b.mu.Unlock()
	}

	reply := make(chan browseripc.Response, 1)
	deadline := b.opts.now().Add(b.opts.responseTimeout)
	call := &pendingBrowserCall{reply: reply, deadline: deadline}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return ErrBrowserShuttingDown
	}
	if len(b.pending) >= browseripc.MaxPendingRequests {
		b.mu.Unlock()
		return fmt.Errorf("browser companion: pending request limit reached (%d)", browseripc.MaxPendingRequests)
	}
	call.ownerEpoch = epoch
	b.pending[req.RequestID] = call
	// The writer and its process token are captured together: a write error
	// must fail the generation the writer belongs to — never a newer one a
	// late EPIPE from an old pipe could otherwise hit.
	writer := b.writer
	token := b.procToken
	b.mu.Unlock()

	if writer == nil {
		b.mu.Lock()
		delete(b.pending, req.RequestID)
		b.mu.Unlock()
		return fmt.Errorf("%w: no writer (process died)", ErrBrowserCrashed)
	}
	if err := b.writeRequest(writer, req, epoch); err != nil {
		b.mu.Lock()
		delete(b.pending, req.RequestID)
		b.mu.Unlock()
		if errors.Is(err, errBrowserStaleOwner) {
			// A deletion landed between the epoch capture and the write:
			// this is a normal stale-request rejection, not a pipe failure.
			// The healthy companion must NOT be torn down for it.
			return err
		}
		// A real I/O failure means the child's pipe is broken. The reader
		// goroutine may not have noticed yet (EOF races the EPIPE); fail the
		// generation the captured writer belongs to synchronously so callers
		// see a process-level failure instead of a single-tab miss.
		b.failClosed(token, fmt.Errorf("write request: %w", err))
		return fmt.Errorf("browser companion: write request: %w", err)
	}

	call.timer = time.AfterFunc(time.Until(deadline), func() {
		b.deliver(req.RequestID, browseripc.Response{
			ProtocolVersion: browseripc.ProtocolVersion,
			RequestID:       req.RequestID,
			Error: &browseripc.RPCError{
				Code:    browseripc.CodeTimeout,
				Message: "request timed out",
			},
		})
	})

	select {
	case resp := <-reply:
		if call.timer != nil {
			call.timer.Stop()
		}
		return b.applyResponse(req, resp, result, call.ownerEpoch)
	case <-ctx.Done():
		call.timer.Stop()
		b.mu.Lock()
		delete(b.pending, req.RequestID)
		b.mu.Unlock()
		// Best-effort cancel so the companion drops the work too.
		cancelReq := browseripc.Request{
			ProtocolVersion: browseripc.ProtocolVersion,
			RequestID:       b.nextRequestID(),
			Method:          "request.cancel",
			Params:          mustBrowserParams(browseripc.CancelParams{RequestID: req.RequestID}),
		}
		_ = b.writeRequest(writer, cancelReq, 0)
		return fmt.Errorf("%w: %w", browseripcCodeError(browseripc.CodeCancelled, "request cancelled"), ctx.Err())
	}
}

// errBrowserStaleOwner marks a request whose owner was deleted between epoch
// capture and the write gate. It is a normal rejection, never a pipe failure:
// callers must NOT fail the companion process for it.
var errBrowserStaleOwner = errors.New("browser companion: owner was deleted while the request was in flight")

// writeRequest writes one complete frame under the frame-level write lock so
// concurrent writers (restore replay, RemoveOwner cleanup, ordinary requests,
// cancels) can never interleave a header with another frame's payload. It
// re-validates the request's owner epoch under the lock: if the owner was
// deleted after the tombstone gate, the stale frame is dropped before it can
// reach the wire.
func (b *browserCoordinator) writeRequest(writer io.Writer, req browseripc.Request, epoch uint64) error {
	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	if req.OwnerID != "" {
		b.mu.Lock()
		current := b.ownerEpoch[req.OwnerID]
		b.mu.Unlock()
		if epoch != current {
			return fmt.Errorf("%w: owner %q", errBrowserStaleOwner, req.OwnerID)
		}
	}
	return browseripc.WriteRequest(writer, req)
}

// ownerDeleted reports whether the chat is under a deletion tombstone.
func (b *browserCoordinator) ownerDeleted(ownerID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.tombstonedOwners[ownerID]
}

// applyResponse routes a response into the caller's typed result.
func (b *browserCoordinator) applyResponse(req browseripc.Request, resp browseripc.Response, result any, epoch uint64) error {
	if err := browseripc.ValidateResponse(resp); err != nil {
		return fmt.Errorf("browser companion: invalid response: %w", err)
	}
	if resp.Error != nil {
		return browseripcCodeError(resp.Error.Code, resp.Error.Message)
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Result, result); err != nil {
		return fmt.Errorf("browser companion: decode %s result: %w", req.Method, err)
	}
	b.updateMirrorFromResponse(req, resp, epoch)
	return nil
}

// updateMirrorFromResponse applies a response to the owner/tab mirror. The
// epoch gate, the restore barrier, and the mutation run in ONE b.mu critical
// section (compare-and-apply): a deletion landing between a check and a
// mutation can no longer be undone by an in-flight response.
func (b *browserCoordinator) updateMirrorFromResponse(req browseripc.Request, resp browseripc.Response, epoch uint64) {
	switch req.Method {
	case "tab.open":
		var res browseripc.TabInfo
		if json.Unmarshal(resp.Result, &res) == nil && res.TabID != "" {
			b.mu.Lock()
			if !b.responseEpochCurrentLocked(req.OwnerID, epoch) || b.ownerBarrieredLocked(req.OwnerID) {
				b.mu.Unlock()
				return
			}
			// The epoch gate guarantees this tab.open was issued after the
			// latest deletion, so it is the explicit reopen: clear the
			// deletion tombstone.
			delete(b.tombstonedOwners, req.OwnerID)
			owner := b.ensureOwnerLocked(req.OwnerID)
			changed := applyTabObservationLocked(owner, res.TabID, res.URL, res.Title, res.Active, res.Generation)
			notify := b.tabsChanged
			b.mu.Unlock()
			b.notifyMirrorChange(notify, changed)
		}
	case "tab.navigate":
		var res browseripc.TabNavigateResult
		if json.Unmarshal(resp.Result, &res) == nil && res.TabID != "" {
			b.mu.Lock()
			if !b.responseEpochCurrentLocked(req.OwnerID, epoch) || b.ownerBarrieredLocked(req.OwnerID) {
				b.mu.Unlock()
				return
			}
			owner := b.ensureOwnerLocked(req.OwnerID)
			changed := applyTabObservationLocked(owner, res.TabID, res.URL, res.Title, res.Active, res.Generation)
			notify := b.tabsChanged
			b.mu.Unlock()
			b.notifyMirrorChange(notify, changed)
		}
	case "tab.activate":
		var p browseripc.TabRefParams
		if json.Unmarshal(req.Params, &p) == nil && p.TabID != "" {
			b.mu.Lock()
			if !b.responseEpochCurrentLocked(req.OwnerID, epoch) || b.ownerBarrieredLocked(req.OwnerID) {
				b.mu.Unlock()
				return
			}
			changed := false
			if owner := b.owners[req.OwnerID]; owner != nil && owner.activeTab != p.TabID {
				owner.activeTab = p.TabID
				changed = true
			}
			notify := b.tabsChanged
			b.mu.Unlock()
			b.notifyMirrorChange(notify, changed)
		}
	case "tab.close":
		var p browseripc.TabRefParams
		if json.Unmarshal(req.Params, &p) == nil && p.TabID != "" {
			b.mu.Lock()
			if !b.responseEpochCurrentLocked(req.OwnerID, epoch) || b.ownerBarrieredLocked(req.OwnerID) {
				b.mu.Unlock()
				return
			}
			changed := false
			if owner := b.owners[req.OwnerID]; owner != nil {
				for i, t := range owner.tabs {
					if t.tabID == p.TabID {
						owner.tabs = append(owner.tabs[:i], owner.tabs[i+1:]...)
						changed = true
						break
					}
				}
			}
			notify := b.tabsChanged
			b.mu.Unlock()
			b.notifyMirrorChange(notify, changed)
		}
	case "owner.remove":
		b.mu.Lock()
		_, existed := b.owners[req.OwnerID]
		delete(b.owners, req.OwnerID)
		notify := b.tabsChanged
		b.mu.Unlock()
		b.notifyMirrorChange(notify, existed)
	case "tab.list":
		var res browseripc.TabListResult
		if json.Unmarshal(resp.Result, &res) == nil {
			b.mu.Lock()
			if !b.responseEpochCurrentLocked(req.OwnerID, epoch) || b.ownerBarrieredLocked(req.OwnerID) {
				b.mu.Unlock()
				return
			}
			owner := b.ensureOwnerLocked(req.OwnerID)
			owner.tabs = make([]browserTabState, 0, len(res.Tabs))
			for _, t := range res.Tabs {
				owner.tabs = append(owner.tabs, browserTabState{tabID: t.TabID, url: t.URL, title: t.Title, generation: t.Generation})
				if t.Active {
					owner.activeTab = t.TabID
				}
			}
			notify := b.tabsChanged
			b.mu.Unlock()
			b.notifyMirrorChange(notify, true)
		}
	}
}

// responseEpochCurrentLocked reports whether the response's captured epoch
// still matches the owner's deletion epoch. Callers hold b.mu.
func (b *browserCoordinator) responseEpochCurrentLocked(ownerID string, epoch uint64) bool {
	if ownerID == "" {
		return true
	}
	return b.ownerEpoch[ownerID] == epoch
}

// ownerBarrieredLocked reports whether the owner's restore is in flight.
// Callers hold b.mu.
func (b *browserCoordinator) ownerBarrieredLocked(ownerID string) bool {
	_, barriered := b.restoreBarrier[ownerID]
	return barriered
}

// ensureOwnerLocked returns the owner's mirror, creating it when absent.
// Callers hold b.mu.
func (b *browserCoordinator) ensureOwnerLocked(ownerID string) *browserOwnerState {
	owner := b.owners[ownerID]
	if owner == nil {
		owner = &browserOwnerState{ownerID: ownerID, restoredGen: b.companionGen}
		b.owners[ownerID] = owner
	}
	return owner
}

// applyTabObservationLocked mutates one tab observation onto the owner mirror
// with field-level idempotency. Callers hold b.mu; returns whether anything
// changed (drives the persistence notification).
func applyTabObservationLocked(owner *browserOwnerState, tabID, url, title string, active bool, generation int64) bool {
	if owner == nil {
		return false
	}
	changed := false
	found := false
	for i := range owner.tabs {
		if owner.tabs[i].tabID == tabID {
			if url != "" && owner.tabs[i].url != url {
				owner.tabs[i].url = url
				changed = true
			}
			if title != "" && owner.tabs[i].title != title {
				owner.tabs[i].title = title
				changed = true
			}
			if generation > 0 && owner.tabs[i].generation != generation {
				owner.tabs[i].generation = generation
				changed = true
			}
			found = true
			break
		}
	}
	if !found {
		owner.tabs = append(owner.tabs, browserTabState{tabID: tabID, url: url, title: title, generation: generation})
		changed = true
	}
	if active && owner.activeTab != tabID {
		owner.activeTab = tabID
		changed = true
	}
	return changed
}

// notifyMirrorChange fires the persistence notification when a mutation
// actually changed the mirror.
func (b *browserCoordinator) notifyMirrorChange(notify func(), changed bool) {
	if changed && notify != nil {
		notify()
	}
}

func (b *browserCoordinator) setActiveTabMirror(ownerID, tabID string) {
	b.mu.Lock()
	if owner := b.owners[ownerID]; owner != nil && owner.activeTab != tabID {
		owner.activeTab = tabID
		changed := b.tabsChanged
		b.mu.Unlock()
		if changed != nil {
			changed()
		}
		return
	}
	b.mu.Unlock()
}

// restoreBrowserState rehydrates the host mirror from browser-state-v1.json
// after a successful handshake and asks the companion to recreate the tabs.
// Restoration is best-effort: a failure leaves the chat without browser tabs
// rather than failing the companion start.
//
// The companion assigns fresh tab IDs on every start, so the persisted IDs
// are mapped oldID -> newID from the tab.open responses. The mirror is built
// atomically from the successful responses (persisted order, live IDs), then
// the active tab is activated under its mapped ID. Publishing old IDs first
// would duplicate tabs: navigation events would append the new IDs next to
// the stale ones, and every restart would grow the set.
//
// A restore barrier buffers navigation/tab.changed events while the tabs are
// opened (the real companion emits them BEFORE the tab.open response) and
// suppresses every other mirror mutation for the owner, so the commit owns the
// tab list: events can never create the owner ahead of it or persist partial
// mirrors. Buffered events are replayed onto the committed mirror, and the
// whole restore persists exactly once.
func (b *browserCoordinator) restoreBrowserState() {
	state := loadBrowserStateFile()
	if len(state.Owners) == 0 {
		return
	}
	// No restore pass may overlap another: start with a clean slate so a
	// barrier left over from an aborted pass (companion death mid-restore)
	// cannot buffer events for the new process.
	b.mu.Lock()
	b.restoreBarrier = make(map[string][]browseripc.Event)
	b.restorePassActive = true
	b.mu.Unlock()
	// passFailed marks a process-level abort. The whole pass persists exactly
	// once at the end (all published owners together), and not at all when it
	// aborts. A pass that published nothing (every owner deleted mid-restore)
	// skips the final write: RemoveOwner's own sync already persisted the
	// deletions.
	passFailed := false
	publishedAny := false
	defer func() {
		b.mu.Lock()
		b.restorePassActive = false
		notify := b.tabsChanged
		b.mu.Unlock()
		if notify != nil && !passFailed && publishedAny {
			notify()
		}
	}()
	for ownerID, owner := range state.Owners {
		b.mu.Lock()
		if existing, ok := b.owners[ownerID]; ok && existing.restoredGen == b.companionGen {
			// This owner already lives in the current companion process; no
			// remap needed (guards against duplicate restore passes).
			b.mu.Unlock()
			continue
		}
		if b.tombstonedOwners[ownerID] {
			// The chat was deleted while a restore was in flight; never
			// resurrect it.
			b.mu.Unlock()
			continue
		}
		// Raise the barrier: events and responses for this owner are deferred
		// until the mirror is committed below.
		b.restoreBarrier[ownerID] = nil
		b.mu.Unlock()

		type restoredTab struct {
			oldID      string
			tabID      string
			url        string
			title      string
			generation int64
		}
		var restored []restoredTab
		for _, t := range owner.Tabs {
			if b.ownerDeleted(ownerID) {
				// The chat was deleted while this restore was in flight. Stop
				// opening tabs (the deletion's owner.remove may already be in
				// the pipe) and issue a final owner.remove so tabs created so
				// far are cleaned up too.
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
				var cleanupRes json.RawMessage
				_ = b.callDirect(cleanupCtx, ownerID, "owner.remove", browseripc.OwnerParams{OwnerID: ownerID}, &cleanupRes)
				cleanupCancel()
				b.mu.Lock()
				delete(b.restoreBarrier, ownerID)
				b.mu.Unlock()
				restored = nil
				break
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			var res browseripc.TabInfo
			err := b.callDirect(ctx, ownerID, "tab.open", browseripc.TabOpenParams{
				OwnerID: ownerID, URL: t.URL, Disposition: browseripc.DispositionBackground,
			}, &res)
			cancel()
			if err != nil || res.TabID == "" {
				if b.processGone() {
					// The companion died mid-restore: abort the WHOLE pass.
					// Publishing a partial or empty mirror would overwrite
					// the intact desired state on disk. The startup path
					// fails instead, and the next attempt re-reads the
					// untouched state file.
					passFailed = true
					b.mu.Lock()
					delete(b.restoreBarrier, ownerID)
					b.mu.Unlock()
					return
				}
				// Single-tab failure only: the tab is omitted from the
				// restored mirror rather than half-registered.
				continue
			}
			restored = append(restored, restoredTab{oldID: t.ID, tabID: res.TabID, url: res.URL, title: res.Title, generation: res.Generation})
		}

		// Map the persisted active tab onto a live ID.
		newActive := ""
		if owner.ActiveTab != "" {
			for _, r := range restored {
				if r.oldID == owner.ActiveTab {
					newActive = r.tabID
					break
				}
			}
		}

		// Commit: build the mirror from the successful responses, replay the
		// buffered events onto it, and publish. The publish REPLACES any
		// mirror from a previous companion process. Persistence is deferred to
		// the end of the pass so all owners land in one atomic write (or none
		// at all when the pass aborts).
		b.mu.Lock()
		buffered := b.restoreBarrier[ownerID]
		delete(b.restoreBarrier, ownerID)
		if b.tombstonedOwners[ownerID] {
			// The chat was deleted while this restore was in flight; the
			// already-sent tab.open calls may have created companion-side
			// tabs, but the host must not record or persist them.
			b.mu.Unlock()
			continue
		}
		mirror := &browserOwnerState{ownerID: ownerID, activeTab: newActive, restoredGen: b.companionGen}
		for _, r := range restored {
			mirror.tabs = append(mirror.tabs, browserTabState{tabID: r.tabID, url: r.url, title: r.title, generation: r.generation})
		}
		applyRestoreEvents(mirror, buffered)
		b.owners[ownerID] = mirror
		delete(b.tombstonedOwners, ownerID)
		publishedAny = true
		changed := b.tabsChanged
		passActive := b.restorePassActive
		b.mu.Unlock()
		if changed != nil && !passActive {
			changed()
		}

		// Activate the persisted active tab under its mapped live ID. The
		// barrier is gone, so the activate response follows the normal mirror
		// path (idempotent: the commit already recorded the same active tab).
		if newActive != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			var res json.RawMessage
			err := b.callDirect(ctx, ownerID, "tab.activate", browseripc.TabRefParams{OwnerID: ownerID, TabID: newActive}, &res)
			cancel()
			if err != nil && b.processGone() {
				// The final activation hit a process-level failure (e.g.
				// EPIPE): the whole pass must not persist a mirror that
				// belongs to a dead process.
				passFailed = true
			}
		}
	}
}

// processGone reports whether the companion process is no longer usable: its
// state left "starting" (processDead fired) or the writer pipe disappeared.
func (b *browserCoordinator) processGone() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state != browserStarting || b.writer == nil
}

// applyRestoreEvents replays events buffered during an owner's restore onto
// the committed mirror. Only fields for tabs that actually restored are
// applied; events for unknown tab IDs (tabs whose tab.open failed) are
// dropped, so the mirror never grows beyond the successful responses.
func applyRestoreEvents(mirror *browserOwnerState, events []browseripc.Event) {
	for _, ev := range events {
		var data browseripc.TabChangedEventData
		if json.Unmarshal(ev.Event.Data, &data) == nil && data.TabID != "" {
			for i := range mirror.tabs {
				if mirror.tabs[i].tabID == data.TabID {
					if data.URL != "" {
						mirror.tabs[i].url = data.URL
					}
					if data.Title != "" {
						mirror.tabs[i].title = data.Title
					}
					if data.Generation > 0 {
						mirror.tabs[i].generation = data.Generation
					}
					break
				}
			}
		}
	}
}

// deliver resolves a pending request from the reader goroutine. It is the only
// writer to the reply channels, so a late timer cannot race a real response.
func (b *browserCoordinator) deliver(requestID string, resp browseripc.Response) {
	b.mu.Lock()
	call, ok := b.pending[requestID]
	if ok {
		delete(b.pending, requestID)
	}
	b.mu.Unlock()
	if ok {
		call.reply <- resp
	}
}

func (b *browserCoordinator) nextRequestID() string {
	return fmt.Sprintf("r-%d", b.requestSeq.Add(1))
}

// ensureReady returns when the companion is ready or an actionable error.
func (b *browserCoordinator) ensureReady(ctx context.Context) error {
	b.mu.Lock()
	switch b.state {
	case browserReady:
		b.mu.Unlock()
		return nil
	case browserDisabled:
		err := b.lastErr
		b.mu.Unlock()
		if err == nil {
			err = ErrBrowserDisabled
		}
		return err
	case browserStarting:
		// Another goroutine is starting the companion; a request racing the
		// start returns not_ready instead of queueing behind an unbounded wait.
		b.mu.Unlock()
		return fmt.Errorf("%w: start in progress", ErrBrowserNotReady)
	case browserStopped, browserCrashed:
		// Fall through to attempt a (re)start.
	default:
		err := fmt.Errorf("browser companion: unexpected state %q", b.state)
		b.mu.Unlock()
		return err
	}
	if b.closed {
		b.mu.Unlock()
		return ErrBrowserShuttingDown
	}
	b.state = browserStarting
	b.mu.Unlock()

	err := b.startLocked(ctx)
	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		if b.writer == nil {
			// The reader goroutine killed the process during the handshake
			// (protocol violation, oversized frame): do not declare ready.
			err = fmt.Errorf("%w: process died during handshake", ErrBrowserCrashed)
			b.recordFailureLocked(err)
			return err
		}
		b.failures = 0
		b.failureStarted = time.Time{}
		b.lastFailureAt = time.Time{}
		b.state = browserReady
		return nil
	}
	if errors.Is(err, ErrBrowserNotReady) {
		// Backoff fast-fail: not a spawn attempt, must not count toward the
		// disable threshold. Restore a state that permits the next retry and
		// carry the underlying cause (e.g. component missing) so callers can
		// show the right recovery entry instead of a generic "retry".
		b.state = browserStopped
		if b.lastErr != nil {
			return fmt.Errorf("%w: %w", err, b.lastErr)
		}
		return err
	}
	b.recordFailureLocked(err)
	return err
}

// recordFailureLocked updates the disable bookkeeping. Callers hold b.mu.
func (b *browserCoordinator) recordFailureLocked(err error) {
	now := b.opts.now()
	b.lastErr = err
	if b.failureStarted.IsZero() || now.Sub(b.failureStarted) > b.opts.failureWindow {
		b.failureStarted = now
		b.failures = 0
	}
	b.failures++
	b.lastFailureAt = now
	b.state = browserCrashed
	if b.failures >= b.opts.maxFailures {
		b.state = browserDisabled
		err = fmt.Errorf("%w: %d failures in %s", ErrBrowserDisabled, b.failures, b.opts.failureWindow)
		b.lastErr = err
	}
}

// ResetRecovery re-arms a crashed/disabled coordinator after an explicit
// install-or-repair action. Ready/starting processes are left untouched.
func (b *browserCoordinator) ResetRecovery() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != browserDisabled && b.state != browserCrashed {
		return
	}
	b.failures = 0
	b.failureStarted = time.Time{}
	b.lastFailureAt = time.Time{}
	b.state = browserStopped
	b.lastErr = nil
}

// startLocked spawns and handshakes with the companion. The caller does not
// hold b.mu; the spawn and handshake run outside the lock.
func (b *browserCoordinator) startLocked(ctx context.Context) error {
	b.mu.Lock()
	backoff := b.backoffForAttemptLocked()
	lastFailureAt := b.lastFailureAt
	b.mu.Unlock()
	if backoff > 0 {
		if wait := backoff - b.opts.now().Sub(lastFailureAt); wait > 0 {
			// Still inside the backoff window: fail fast instead of blocking a
			// tool call on an artificial sleep.
			return fmt.Errorf("%w: retry in %s", ErrBrowserNotReady, wait.Round(time.Millisecond))
		}
	}

	path, err := b.opts.resolveBinary()
	if err != nil {
		b.mu.Lock()
		b.lastErr = err
		b.mu.Unlock()
		return fmt.Errorf("%w: %w", ErrBrowserComponentMissing, err)
	}
	env := allowlistedBrowserCompanionEnv()
	cmd, writer, stdout, stderr, err := b.opts.spawn(ctx, path, env)
	if err != nil {
		return fmt.Errorf("spawn browser companion: %w", err)
	}
	b.mu.Lock()
	// Each spawn gets a fresh process token; failClosed validates it so a
	// stale process's death can never tear down a newer generation.
	b.procToken++
	token := b.procToken
	b.cmd = cmd
	b.writer = writer
	waitCh := make(chan error, 1)
	b.waitCh = waitCh
	b.mu.Unlock()
	go func() { waitCh <- cmd.Wait() }()
	go b.readStderr(stderr)
	// The reader must run before the handshake so the hello reply is routed
	// into the pending request instead of blocking the pipe.
	go b.readLoop(token, bufio.NewReaderSize(stdout, 64*1024))

	// Handshake: the companion must answer hello with its identity before any
	// other method is accepted.
	hsCtx, cancel := context.WithTimeout(ctx, b.opts.responseTimeout)
	defer cancel()
	var hello browseripc.HelloResult
	if err := b.callDirect(hsCtx, "", "hello", browseripc.HelloParams{
		HostName:    "reasonix-desktop",
		HostVersion: version,
	}, &hello); err != nil {
		_ = b.killProcess()
		return fmt.Errorf("browser companion handshake: %w", err)
	}
	if hello.ProtocolVersion != browseripc.ProtocolVersion {
		_ = b.killProcess()
		return fmt.Errorf("browser companion protocol %d != host %d", hello.ProtocolVersion, browseripc.ProtocolVersion)
	}
	if hello.Capabilities.MaxProtocolVersion < browseripc.ProtocolVersion {
		_ = b.killProcess()
		return fmt.Errorf("browser companion max protocol %d < host %d", hello.Capabilities.MaxProtocolVersion, browseripc.ProtocolVersion)
	}
	b.mu.Lock()
	b.hello = &hello
	// A new companion process: every owner mirror that predates this
	// generation holds dead tab IDs and must be remapped by the restore pass.
	b.companionGen++
	b.mu.Unlock()
	// After every successful start, rehydrate the persisted per-chat tab state.
	// This is also the companion-recovery path: a crash is followed by a
	// restart that recreates the tabs from browser-state-v1.json.
	b.restoreBrowserState()
	return nil
}

// backoffForAttemptLocked returns the minimum wait before the next spawn
// attempt. Callers hold b.mu.
func (b *browserCoordinator) backoffForAttemptLocked() time.Duration {
	if b.failures == 0 || b.lastFailureAt.IsZero() {
		return 0
	}
	idx := b.failures
	if idx >= len(browserBackoff) {
		idx = len(browserBackoff) - 1
	}
	return browserBackoff[idx]
}

// readLoop routes child frames: responses into pending calls, events into the
// sink. A protocol violation (oversized frame) or process death marks the
// coordinator crashed and fails every pending call. The reader serves exactly
// one process generation: the token is validated before AND after the blocking
// read, and again at the event commit, so frames from a dead generation can
// never reach a newer process's state.
func (b *browserCoordinator) readLoop(token uint64, r io.Reader) {
	for {
		// Pre-read check: if a newer process was spawned while this reader
		// still held buffered frames, stop immediately.
		if token != b.currentProcToken() {
			return
		}
		payload, err := browseripc.ReadFrame(r, browseripc.FrameMaxBytes)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, browseripc.ErrFrameTooLarge) {
				b.processDead(token, err)
				return
			}
			b.processDead(token, err)
			return
		}
		// Post-read re-check: a newer process may have been installed while
		// this reader was blocked inside ReadFrame; the frame just read
		// belongs to the dead generation.
		if token != b.currentProcToken() {
			return
		}
		var envelope struct {
			RequestID string                `json:"requestId"`
			Result    json.RawMessage       `json:"result"`
			Error     *browseripc.RPCError  `json:"error"`
			Event     *browseripc.EventBody `json:"event"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			b.processDead(token, fmt.Errorf("browser companion: malformed frame: %w", err))
			return
		}
		switch {
		case envelope.Event != nil:
			ev := browseripc.Event{ProtocolVersion: browseripc.ProtocolVersion, Event: *envelope.Event}
			if err := browseripc.ValidateEvent(ev); err != nil {
				// Unknown future events are ignored, not fatal: the companion
				// may advertise a superset when the host is older.
				continue
			}
			b.handleEvent(token, ev)
		case envelope.RequestID != "":
			resp := browseripc.Response{
				ProtocolVersion: browseripc.ProtocolVersion,
				RequestID:       envelope.RequestID,
				Result:          envelope.Result,
				Error:           envelope.Error,
			}
			b.deliver(envelope.RequestID, resp)
		default:
			b.processDead(token, fmt.Errorf("browser companion: unaddressable frame"))
			return
		}
	}
}

// handleEvent updates the owner/tab mirror and forwards to the sink. Events
// for an owner whose restore is in flight are buffered instead of mutating the
// mirror: the restore commit owns that owner's tab list.
//
// The reader's process token is validated under b.mu before the mirror
// mutation: an event delivered by a stale reader (a newer process was
// installed while the old reader was blocked) is dropped entirely — it must
// not pollute the new generation's mirror or reach the sink.
func (b *browserCoordinator) handleEvent(token uint64, ev browseripc.Event) {
	// Unified token gate for EVERY event: an event delivered by a stale
	// reader must not reach the sink or the mirror — permission prompts,
	// downloads, takeover notices, and renderer crashes from a dead
	// generation are just as stale as tab observations.
	b.mu.Lock()
	stale := token != b.procToken
	b.mu.Unlock()
	if stale {
		return
	}
	switch ev.Event.Name {
	case "tab.changed", "navigation":
		var data browseripc.TabChangedEventData
		if json.Unmarshal(ev.Event.Data, &data) == nil && data.TabID != "" {
			b.mu.Lock()
			if token != b.procToken {
				// Stale generation: drop the event completely.
				b.mu.Unlock()
				return
			}
			if barrier, ok := b.restoreBarrier[data.OwnerID]; ok {
				b.restoreBarrier[data.OwnerID] = append(barrier, ev)
				b.mu.Unlock()
				// Still forward to the sink; only the mirror is deferred.
				b.opts.events(ev)
				return
			}
			owner := b.owners[data.OwnerID]
			if owner == nil {
				if b.tombstonedOwners[data.OwnerID] {
					// Late event for a deleted chat: drop it.
					b.mu.Unlock()
					return
				}
				owner = &browserOwnerState{ownerID: data.OwnerID, restoredGen: b.companionGen}
				b.owners[data.OwnerID] = owner
			}
			changed := applyTabObservationLocked(owner, data.TabID, data.URL, data.Title, data.Active, data.Generation)
			notifyFn := b.tabsChanged
			b.mu.Unlock()
			if changed && notifyFn != nil {
				notifyFn()
			}
			b.opts.events(ev)
			return
		}
	}
	b.opts.events(ev)
}

// updateTabMirror applies one tab observation (events and generic paths).
// It is field-level idempotent: when the observed values equal the mirror's
// (the real companion re-emits tab.changed on every activation), no
// persistence is triggered — the restore commit's single-write guarantee
// depends on this.
//
// A tombstoned owner (deleted chat) is never recreated here: asynchronous
// companion events that arrive after the deletion (did-navigate/tab.changed
// still in flight) must not resurrect it. Only an explicit host-side reopen
// (a tab.open response) clears the tombstone.
func (b *browserCoordinator) updateTabMirror(ownerID, tabID, url, title string, active bool, generation int64) {
	b.mu.Lock()
	owner := b.owners[ownerID]
	if owner == nil {
		if b.tombstonedOwners[ownerID] {
			// Late event for a deleted chat: drop it.
			b.mu.Unlock()
			return
		}
		owner = &browserOwnerState{ownerID: ownerID, restoredGen: b.companionGen}
		b.owners[ownerID] = owner
	}
	changed := applyTabObservationLocked(owner, tabID, url, title, active, generation)
	notifyFn := b.tabsChanged
	b.mu.Unlock()
	if changed && notifyFn != nil {
		notifyFn()
	}
}

// RemoveOwner drops one chat's tabs from the mirror and asks the companion to
// close them. Shared cookies/login state in the Chromium profile is untouched.
func (b *browserCoordinator) RemoveOwner(ctx context.Context, ownerID string) error {
	// The deletion is one atomic step under writeMu -> mu: owner removal,
	// tombstone, and the epoch bump must be visible to the write gate and the
	// response path at the same instant, or a response that saw the old epoch
	// could undo the deletion. The restore barrier is deliberately NOT
	// deleted here: while a restore is in flight, its in-queue tab.open
	// responses must stay suppressed so they cannot recreate the owner (and
	// clear the tombstone) before the commit consumes the barrier.
	b.writeMu.Lock()
	b.mu.Lock()
	delete(b.owners, ownerID)
	b.tombstonedOwners[ownerID] = true
	b.ownerEpoch[ownerID]++
	changed := b.tabsChanged
	state := b.state
	b.mu.Unlock()
	b.writeMu.Unlock()
	if changed != nil {
		changed()
	}
	// Best-effort companion cleanup; a missing companion must not fail chat
	// deletion. While a restore is in flight (state starting) the companion
	// may already hold tabs created by the restore replay — send owner.remove
	// directly (callDirect, not Call, which would bounce off the starting
	// state) so those orphan tabs are cleaned up too.
	if state == browserReady || state == browserStarting {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		var res json.RawMessage
		_ = b.callDirect(cctx, ownerID, "owner.remove", browseripc.OwnerParams{OwnerID: ownerID}, &res)
	}
	return nil
}

// Close shuts the companion down gracefully: window.close, then up to
// shutdownGraceMs of wait, then a hard kill. Idempotent.
func (b *browserCoordinator) Close() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	state := b.state
	cmd := b.cmd
	writer := b.writer
	b.mu.Unlock()

	if state != browserReady {
		if cmd != nil {
			_ = b.killProcess()
		}
		return
	}
	// Graceful: ask the companion to persist tab state, close its window, and
	// exit. window.close is the companion's shutdown entry point.
	ctx, cancel := context.WithTimeout(context.Background(), b.opts.shutdownGrace)
	defer cancel()
	_ = b.Call(ctx, "", "window.close", struct{}{}, nil)
	_ = writer.Close()

	b.mu.Lock()
	waitCh := b.waitCh
	b.mu.Unlock()
	select {
	case <-waitCh:
	case <-time.After(b.opts.shutdownGrace):
		_ = b.killProcess()
	}
}

// killProcess terminates the child and waits for it on the shared wait
// channel. Multiple callers may race here (graceful close timeout and the
// reader goroutine); the channel guarantees exec.Cmd.Wait runs exactly once.
func (b *browserCoordinator) killProcess() error {
	b.mu.Lock()
	cmd := b.cmd
	b.cmd = nil
	waitCh := b.waitCh
	if b.writer != nil {
		_ = b.writer.Close()
		b.writer = nil
	}
	b.mu.Unlock()
	if cmd == nil {
		return nil
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if waitCh != nil {
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

// failClosed tears down the companion after death, a protocol violation, or a
// broken write pipe, and fails every pending call with the crash code. It is
// safe to call from both the reader goroutine (EOF / protocol violation) and
// any write path (EPIPE): whichever notices first fails the current companion
// generation synchronously, never waiting on the other's scheduling.
//
// The token is the process token of the dying generation. A stale token (an
// older process's death arriving after a restart) is ignored entirely, so a
// late EOF can never clear a newer process's writer or pending calls. Within
// one generation the teardown is idempotent: the crash callback and the
// pending teardown run exactly once.
func (b *browserCoordinator) failClosed(token uint64, err error) {
	b.mu.Lock()
	if token != b.procToken {
		// Death of an older generation: the current process is newer and
		// unaffected. Ignore it completely.
		b.mu.Unlock()
		return
	}
	if b.failClosedGen == token {
		// Already torn down for this generation.
		b.mu.Unlock()
		return
	}
	b.failClosedGen = token
	b.state = browserCrashed
	b.hello = nil
	b.lastErr = err
	pending := b.pending
	b.pending = make(map[string]*pendingBrowserCall)
	writer := b.writer
	cmd := b.cmd
	waitCh := b.waitCh
	b.writer = nil
	b.cmd = nil
	b.waitCh = nil
	b.mu.Unlock()
	if writer != nil {
		_ = writer.Close()
	}
	// Kill the captured process directly: the fields were cleared above, so a
	// later killProcess() would see nil. A misbehaving companion must not
	// linger as a zombie holding the pipes.
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if waitCh != nil {
		select {
		case <-waitCh:
		case <-time.After(2 * time.Second):
		}
	}
	crashResp := browseripc.Response{
		ProtocolVersion: browseripc.ProtocolVersion,
		Error: &browseripc.RPCError{
			Code:    browseripc.CodeCrashed,
			Message: "browser companion process died",
		},
	}
	for id, call := range pending {
		resp := crashResp
		resp.RequestID = id
		call.reply <- resp
	}
	b.opts.onCrash()
}

// currentProcToken returns the token of the current companion process.
func (b *browserCoordinator) currentProcToken() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.procToken
}

// processDead is the reader-goroutine entry into failClosed, carrying the
// process token of the reader that observed the death.
func (b *browserCoordinator) processDead(token uint64, err error) {
	b.failClosed(token, err)
}

// readStderr copies capped child stderr into the diagnostic writer.
func (b *browserCoordinator) readStderr(r io.Reader) {
	buf := make([]byte, 4096)
	var kept int
	for {
		n, err := r.Read(buf)
		if n > 0 {
			kept += n
			if kept <= browserStderrBufferLimit {
				_, _ = b.opts.stderr.Write(buf[:n])
			}
		}
		if err != nil {
			return
		}
	}
}

// ---- spawn and resolution ----

func spawnBrowserCompanion(ctx context.Context, path string, env []string) (*exec.Cmd, io.WriteCloser, io.Reader, io.Reader, error) {
	cmd := exec.CommandContext(ctx, path)
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, nil, nil, err
	}
	return cmd, stdin, stdout, stderr, nil
}

// allowlistedBrowserCompanionEnv builds the child environment from the
// allowlist plus a marker. Secrets in the host environment are never copied.
func allowlistedBrowserCompanionEnv() []string {
	env := []string{"REASONIX_BROWSER_COMPANION=1"}
	for _, key := range browserCompanionEnvAllowlist {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// resolveBrowserComponentBinary locates the installed companion executable:
//
//	<home>/browser-components/current.json -> <version dir>/browser/<binary>
//
// The REASONIX_BROWSER_COMPANION_BIN env var overrides resolution for local
// development. Production installs arrive through the signed update manifest;
// until a component is present this returns ErrBrowserComponentMissing.
func resolveBrowserComponentBinary() (string, error) {
	if override := os.Getenv("REASONIX_BROWSER_COMPANION_BIN"); override != "" {
		if st, err := os.Stat(override); err == nil && !st.IsDir() {
			return override, nil
		}
		return "", fmt.Errorf("REASONIX_BROWSER_COMPANION_BIN %q is not a file", override)
	}
	dir := filepath.Join(config.ReasonixHomeDir(), browserComponentDirName)
	manifest, err := os.ReadFile(filepath.Join(dir, browserCurrentManifest))
	if err != nil {
		return "", fmt.Errorf("no component manifest: %w", err)
	}
	var current struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(manifest, &current); err != nil || current.Version == "" {
		return "", fmt.Errorf("invalid component manifest: %w", err)
	}
	bin := browserComponentBinaryName()
	path := filepath.Join(dir, current.Version, browserComponentBinaryDir, bin)
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return "", fmt.Errorf("component %s binary %s missing", current.Version, path)
	}
	return path, nil
}

func browserComponentBinaryName() string {
	return browserComponentBinaryNameFor(runtime.GOOS)
}

func browserComponentBinaryNameFor(goos string) string {
	switch goos {
	case "windows":
		return "reasonix-browser-companion.exe"
	case "darwin":
		return filepath.Join("Reasonix Browser.app", "Contents", "MacOS", "Electron")
	default:
		return "reasonix-browser-companion"
	}
}

// installedComponentVersionLocked reads the manifest; b.mu must be held.
func (b *browserCoordinator) installedComponentVersionLocked() string {
	manifest, err := os.ReadFile(filepath.Join(config.ReasonixHomeDir(), browserComponentDirName, browserCurrentManifest))
	if err != nil {
		return ""
	}
	var current struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(manifest, &current) != nil {
		return ""
	}
	return current.Version
}

func mustBrowserParams(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("browseripc: marshal params: %v", err))
	}
	return b
}

// browseripcCodeError maps a wire error code into an error carrying the code.
type browserCodeError struct {
	code    browseripc.ErrorCode
	message string
}

func (e *browserCodeError) Error() string {
	if e.message == "" {
		return fmt.Sprintf("browser companion: %s", e.code)
	}
	return fmt.Sprintf("browser companion: %s: %s", e.code, e.message)
}

func browseripcCodeError(code browseripc.ErrorCode, message string) error {
	return &browserCodeError{code: code, message: message}
}
