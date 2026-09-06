package main

// Local takeover of a session held by a same-machine serve process — the
// desktop half of the single-writer handoff protocol (internal/serve's
// /ownership, /handoff, /external/frames, /reclaim and /mirror-end).
//
// The scenario: a remote desktop connected to this machine over SSH and left a
// resident serve holding session leases. The user now sits at THIS machine and
// opens the project locally. The tab lands in lease_blocked; instead of the
// dead-end banner it can now take the session over: Serve releases the lease,
// this window rebuilds on it, and the remote tab keeps watching through the
// frame mirror. When the remote side reclaims, this tab demotes itself to a
// read-only spectator fed by the same mirror in reverse.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/remote/bootstrap"
	"reasonix/internal/store"
)

// SessionTakeoverView is what the confirmation dialog is built from.
type SessionTakeoverView struct {
	Available      bool   `json:"available"`
	Reason         string `json:"reason,omitempty"`
	SessionPath    string `json:"sessionPath,omitempty"`
	Holder         string `json:"holder,omitempty"` // serve | external | other | free
	RemoteAttached bool   `json:"remoteAttached"`
	Running        bool   `json:"running"`
	Mirrored       bool   `json:"mirrored"`
	HolderPID      int    `json:"holderPid,omitempty"`
	HolderHost     string `json:"holderHost,omitempty"`
}

type takeoverGrant struct {
	SessionPath     string `json:"sessionPath"`
	MirrorID        string `json:"mirrorId"`
	HandoffID       string `json:"handoffId,omitempty"`
	ReturnHandoffID string `json:"returnHandoffId"`
	SourceWriterID  string `json:"sourceWriterId"`
	TargetWriterID  string `json:"targetWriterId"`
	Status          string `json:"status"`
}

// takeoverHandoffTimeout bounds the drain window for a wait-mode takeover.
const takeoverHandoffTimeout = 5 * time.Minute

// takeoverAfterGrantHookForTest deterministically pauses a takeover after
// Serve published its grant but before the desktop enters the rebuild
// transaction. Production leaves it nil.
var takeoverAfterGrantHookForTest func()

var takeoverFindTargetForTest func(context.Context, *App, string) (takeoverServeRecord, *http.Client, SessionTakeoverView, error)

// takeoverServeRecord is one resident serve discovered from this machine's
// remote state directory, reachable over loopback HTTP.
type takeoverServeRecord struct {
	slug  string
	state bootstrap.ServeState
	base  string
	token string
}

// discoverLocalTakeoverServes enumerates the serve state files under
// <Reasonix home>/remote. The bootstrap wrote them over SFTP; the takeover
// reads them locally because this machine is now where the user sits.
func discoverLocalTakeoverServes() []takeoverServeRecord {
	dir := config.RemoteStateDir()
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []takeoverServeRecord
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "serve-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		state, err := bootstrap.UnmarshalState(data)
		if err != nil || state.PID <= 0 {
			continue
		}
		if !takeoverProcessAlive(state.PID) {
			// Stale state from a long-dead serve. Probing its dead port only
			// burns handshakes (and can trip client/proxy rate limits).
			continue
		}
		slug := strings.TrimSuffix(strings.TrimPrefix(name, "serve-"), ".json")
		record := takeoverServeRecord{slug: slug, state: state}
		// The port file carries the real bound address; the state JSON is the
		// fallback for serves that predate it.
		addr := state.Addr
		if port, err := os.ReadFile(filepath.Join(dir, store.RemoteServePortName(slug))); err == nil {
			if trimmed := strings.TrimSpace(string(port)); trimmed != "" {
				addr = trimmed
			}
		}
		if addr == "" {
			continue
		}
		record.base = "http://" + addr
		if token, err := os.ReadFile(filepath.Join(dir, store.RemoteServeTokenName(slug))); err == nil {
			record.token = strings.TrimSpace(string(token))
		}
		if record.token == "" {
			continue
		}
		out = append(out, record)
	}
	return out
}

var discoverLocalTakeoverServesForMirror = discoverLocalTakeoverServes

// takeoverProcessAlive reports whether a pid is a live process on this host.
// The takeover protocol only ever talks to serves that are actually running;
// everything else is stale state.
func takeoverProcessAlive(pid int) bool {
	return desktopProcessAlive(pid)
}

func takeoverClient(ctx context.Context, record takeoverServeRecord) (*http.Client, error) {
	client, err := newServeHTTPClient(record.base)
	if err != nil {
		return nil, err
	}
	if err := serveHandshake(ctx, client, record.base, record.token); err != nil {
		return nil, err
	}
	return client, nil
}

func takeoverOwnership(ctx context.Context, client *http.Client, base, sessionPath string) (SessionTakeoverView, error) {
	query := url.Values{"session": []string{sessionPath}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serveURL(base, "/ownership?"+query.Encode()), nil)
	if err != nil {
		return SessionTakeoverView{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return SessionTakeoverView{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return SessionTakeoverView{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return SessionTakeoverView{}, fmt.Errorf("serve /ownership: status %d", resp.StatusCode)
	}
	var view SessionTakeoverView
	if err := json.Unmarshal(body, &view); err != nil {
		return SessionTakeoverView{}, err
	}
	return view, nil
}

// findTakeoverTarget scans resident serves for one holding (or mirroring) the
// session, and returns a ready-to-use client for it.
func (a *App) findTakeoverTarget(ctx context.Context, sessionPath string) (takeoverServeRecord, *http.Client, SessionTakeoverView, error) {
	records := discoverLocalTakeoverServesForMirror()
	// Handshake failures back off: a serve whose token file was rotated by a
	// later bootstrap would otherwise burn a failed login every probe.
	records = a.serveProbesFresh(records)
	var lastErr error
	for _, record := range records {
		client, err := takeoverClient(ctx, record)
		if err != nil {
			a.noteServeProbeFailure(record.base)
			lastErr = err
			continue
		}
		view, err := takeoverOwnership(ctx, client, record.base, sessionPath)
		if err != nil {
			lastErr = err
			continue
		}
		if view.Holder == "serve" && !view.Mirrored {
			return record, client, view, nil
		}
	}
	if lastErr != nil {
		return takeoverServeRecord{}, nil, SessionTakeoverView{}, fmt.Errorf("no reachable local serve holds this session: %w", lastErr)
	}
	return takeoverServeRecord{}, nil, SessionTakeoverView{}, fmt.Errorf("no resident serve on this machine holds this session")
}

// QuerySessionTakeover reports whether the lease-blocked tab's session can be
// taken over from a local serve, plus the occupancy details the confirmation
// dialog shows (remote attached, turn running, holder identity).
func (a *App) QuerySessionTakeover(tabID string) (*SessionTakeoverView, error) {
	tab := a.tabByID(tabID)
	if tab == nil {
		return nil, fmt.Errorf("unknown tab")
	}
	path := strings.TrimSpace(tab.currentSessionPath())
	if path == "" {
		path = strings.TrimSpace(tab.SessionPath)
	}
	if path == "" {
		return &SessionTakeoverView{Available: false, Reason: "tab has no session"}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _, view, err := a.findTakeoverTarget(ctx, path)
	if err != nil {
		return &SessionTakeoverView{Available: false, Reason: err.Error(), SessionPath: path}, nil
	}
	view.Available = true
	view.SessionPath = path
	return &view, nil
}

// TakeoverSession performs the confirmed takeover: Serve releases the session
// (draining or cancelling its active turn per mode) and the tab's deferred
// rebuild picks the now-free lease up. mode is "wait" or "interrupt".
func (a *App) TakeoverSession(tabID, mode string) error {
	tab := a.tabByID(tabID)
	if tab == nil {
		return fmt.Errorf("unknown tab")
	}
	if a.takeoverTabState(tab) == takeoverTabUnavailable {
		return fmt.Errorf("tab is no longer waiting for a session lease")
	}
	path := strings.TrimSpace(tab.currentSessionPath())
	if path == "" {
		path = strings.TrimSpace(tab.SessionPath)
	}
	if path == "" {
		return fmt.Errorf("tab has no session")
	}
	if mode != "wait" && mode != "interrupt" {
		mode = "wait"
	}
	a.mu.RLock()
	sourceEpoch := a.runtimeEpochForTabLocked(tab)
	a.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), takeoverHandoffTimeout+30*time.Second)
	defer cancel()
	var record takeoverServeRecord
	var client *http.Client
	var view SessionTakeoverView
	var err error
	if takeoverFindTargetForTest != nil {
		record, client, view, err = takeoverFindTargetForTest(ctx, a, path)
	} else {
		record, client, view, err = a.findTakeoverTarget(ctx, path)
	}
	if err != nil {
		return err
	}
	if view.Mirrored || view.Holder != "serve" {
		return fmt.Errorf("serve no longer holds this session (%s)", view.Holder)
	}
	body, err := json.Marshal(map[string]any{
		"sessionPath":    path,
		"targetWriterId": agent.SessionWriterID(),
		"force":          true,
		"mode":           mode,
		"timeoutMs":      takeoverHandoffTimeout.Milliseconds(),
	})
	if err != nil {
		return err
	}
	resp, err := serveDo(ctx, client, http.MethodPost, serveURL(record.base, "/handoff"), body)
	if err != nil {
		return fmt.Errorf("takeover handoff: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("takeover handoff: %s", strings.TrimSpace(string(respBody)))
	}
	var grant takeoverGrant
	if err := json.Unmarshal(respBody, &grant); err != nil || grant.MirrorID == "" || grant.HandoffID == "" || grant.SourceWriterID == "" {
		return fmt.Errorf("takeover handoff: invalid grant")
	}
	if grant.TargetWriterID != agent.SessionWriterID() {
		a.endFailedTakeover(record, client, grant)
		return fmt.Errorf("takeover handoff: grant targets another runtime")
	}
	if sessionRuntimeKey(grant.SessionPath) != "" && sessionRuntimeKey(grant.SessionPath) != sessionRuntimeKey(path) {
		a.endFailedTakeover(record, client, grant)
		return fmt.Errorf("takeover handoff: grant targets another session")
	}
	if hook := takeoverAfterGrantHookForTest; hook != nil {
		hook()
	}

	// The handoff request must not hold runtimeRebuildMu because Serve may wait
	// for an active turn. Once the grant exists, however, validation, targeted
	// acquisition, lease/mirror installation and controller publication are one
	// serialized transaction. A concurrent tab rebuild therefore wins before
	// acquisition or waits until this transaction commits; it can never be
	// overwritten by a stale takeover.
	a.runtimeRebuildMu.Lock()
	defer a.runtimeRebuildMu.Unlock()
	switch state := a.takeoverTabStateAt(tab, sourceEpoch, path); {
	case state == takeoverTabUnavailable || tab.sessionLeaseRuntimeKey() != "":
		a.endFailedTakeover(record, client, grant)
		return fmt.Errorf("tab changed while taking over the session; retry")
	case state == takeoverTabLocalSpectator:
		return a.promoteLocalTakeoverSpectator(tab, path, sourceEpoch, record, client, grant)
	}
	lease, err := agent.TryAcquireSessionLeaseWithHandoff(path, grant.SourceWriterID, grant.HandoffID)
	if err != nil {
		a.endFailedTakeover(record, client, grant)
		return userFacingSessionLeaseError("", err)
	}
	oldLease := tab.swapSessionLease(lease)
	if oldLease != nil {
		tab.swapSessionLease(oldLease)
		if err := lease.ReleaseForHandoff(grant.SourceWriterID, grant.ReturnHandoffID); err != nil {
			// This cannot be installed on the changed tab; let a return-only
			// mirror retain and retry it without disturbing the old tab lease.
			key := sessionRuntimeKey(path)
			a.registerTakeoverMirror(key, tabID, path, record, client, grant)
			a.takeoverMirrorForKey(key).holdPendingReturn(lease)
			return fmt.Errorf("return takeover lease: %w", err)
		}
		a.endFailedTakeover(record, client, grant)
		return fmt.Errorf("tab already owns another session lease")
	}

	key := sessionRuntimeKey(path)
	a.registerTakeoverMirror(key, tabID, path, record, client, grant)
	previousStartupErr := tab.StartupErr
	previousLeaseHeld := tab.StartupErrLeaseHeld
	previousReady := tab.Ready
	err = a.rebuildStartupTabLocked(tab)
	if err == nil {
		ctrl := a.controllerForTab(tab)
		if ctrl == nil || sessionRuntimeKey(ctrl.SessionPath()) != key || tab.sessionLeaseRuntimeKey() != key {
			err = fmt.Errorf("session startup did not publish the handed-off controller")
		}
	}
	if err != nil {
		if returned := tab.takeSessionLease(); returned != nil {
			if m := a.takeoverMirrorForKey(key); m != nil {
				m.returnLeaseAfterFailedTakeover(returned)
			} else if releaseErr := returned.ReleaseForHandoff(grant.SourceWriterID, grant.ReturnHandoffID); releaseErr != nil {
				tab.adoptSessionLease(returned)
			}
		}
		a.mu.Lock()
		if a.tabs[tab.ID] == tab && !tab.removed && tab.Ctrl == nil {
			tab.StartupErr = previousStartupErr
			tab.StartupErrLeaseHeld = previousLeaseHeld
			tab.Ready = previousReady
			a.setSessionRuntimePhaseLocked(tab, sessionRuntimeLeaseBlocked, &sessionLeaseBusyError{})
			a.saveTabsLocked()
		}
		a.mu.Unlock()
		return err
	}
	a.setTabReadOnly(tabID, false)
	a.clearDeferredRebuild(tabID)
	return nil
}

func (a *App) endFailedTakeover(record takeoverServeRecord, client *http.Client, grant takeoverGrant) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{"sessionPath": grant.SessionPath, "mirrorId": grant.MirrorID})
	resp, err := serveDo(ctx, client, http.MethodPost, serveURL(record.base, "/mirror-end"), payload)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

// takeoverMirror forwards one local tab's events to the serve that used to own
// the session, so the remote tab keeps rendering, and watches the heartbeat
// responses for a reclaim request. One mirror per session key.
type takeoverMirror struct {
	app         *App
	key         string
	sessionPath string

	// sendMu serializes every binding user and writer: HTTP forwarding,
	// re-adoption, grant replacement, reverse reservation, and mirror-end.
	// Code holding sendMu may take mu; the inverse order is forbidden.
	sendMu          sync.Mutex
	mu              sync.Mutex
	tabID           string
	sink            *tabEventSink
	client          *http.Client
	record          takeoverServeRecord
	grant           takeoverGrant
	bindingRevision uint64
	queue           eventwire.MirrorQueue
	pendingReturn   *agent.SessionLease
	returnNextTry   time.Time
	returnBackoff   time.Duration
	releaseHandoff  func(*agent.SessionLease, string, string) error

	reclaimRequested    atomic.Bool
	returned            atomic.Bool
	stopping            atomic.Bool
	consecutiveFailures int32
	stop                chan struct{}
	done                chan struct{}
	wake                chan struct{}
	stopOnce            sync.Once
	detachOnce          sync.Once
}

const (
	takeoverMirrorMaxQueue   = eventwire.MirrorBatchMaxFrames
	takeoverMirrorFlushEvery = 120 * time.Millisecond
	takeoverMirrorHeartbeat  = 5 * time.Second
)

// adoptSessionFromLocalServe announces a directly-opened local session to the
// resident serve on this machine. Without a handoff there is no mirrored
// entry, so the remote side could neither watch it live nor reclaim it — it
// only saw the raw 409 lease refusal from serve's /resume. Adoption registers
// the mirror (and its frame forwarder), which restores both: the remote tab
// can spectator-attach, and its take-back works through /reclaim.
//
// Skipped when the serve itself holds the session (the /handoff takeover
// flow governs), when the serve is unreachable, or when the session is
// already mirrored by another writer.

// serveProbeBackoffWindow suppresses probing a serve whose handshake failed
// recently - typically a token file rotated by a later bootstrap - so a
// polling loop cannot hammer it with failed logins.
const serveProbeBackoffWindow = 60 * time.Second

func (a *App) serveProbesFresh(records []takeoverServeRecord) []takeoverServeRecord {
	a.serveProbeMu.Lock()
	defer a.serveProbeMu.Unlock()
	if a.serveProbeUntil == nil {
		return records
	}
	now := time.Now()
	out := records[:0]
	for _, record := range records {
		if until := a.serveProbeUntil[record.base]; until.After(now) {
			continue
		}
		out = append(out, record)
	}
	return out
}

func (a *App) noteServeProbeFailure(base string) {
	a.serveProbeMu.Lock()
	if a.serveProbeUntil == nil {
		a.serveProbeUntil = map[string]time.Time{}
	}
	a.serveProbeUntil[base] = time.Now().Add(serveProbeBackoffWindow)
	a.serveProbeMu.Unlock()
}

// pathWithinDir reports whether child is inside dir (canonical path prefix).
func pathWithinDir(child, dir string) bool {
	child = strings.TrimRight(filepath.Clean(child), string(filepath.Separator)) + string(filepath.Separator)
	dir = strings.TrimRight(filepath.Clean(dir), string(filepath.Separator)) + string(filepath.Separator)
	return strings.HasPrefix(strings.ToLower(child), strings.ToLower(dir))
}

func (a *App) registerTakeoverMirror(key, tabID, sessionPath string, record takeoverServeRecord, client *http.Client, grant takeoverGrant) {
	if key == "" {
		return
	}
	var m *takeoverMirror
	for {
		a.takeoverMu.Lock()
		if a.takeoverMirrors == nil {
			a.takeoverMirrors = map[string]*takeoverMirror{}
		}
		m = a.takeoverMirrors[key]
		if m == nil || m.returned.Load() || m.stopping.Load() {
			m = newTakeoverMirror(a, key, tabID, sessionPath, nil, record, client, grant)
			a.takeoverMirrors[key] = m
			a.takeoverMu.Unlock()
			go m.run(client, record)
			break
		}
		a.takeoverMu.Unlock()
		m.sendMu.Lock()
		a.takeoverMu.Lock()
		current := a.takeoverMirrors[key] == m
		a.takeoverMu.Unlock()
		if !current || m.returned.Load() || m.stopping.Load() {
			m.sendMu.Unlock()
			continue
		}
		m.mu.Lock()
		m.tabID = tabID
		m.record = record
		m.client = client
		m.grant = grant
		m.bindingRevision++
		m.consecutiveFailures = 0
		m.mu.Unlock()
		m.sendMu.Unlock()
		break
	}
	a.attachTakeoverMirror(tabID, sessionPath)
}

// attachTakeoverMirror points the tab's current sink at its mirror, if one is
// registered for the session. Called after every successful (re)bind so a
// deferred rebuild or a later session switch keeps the wiring true.
func (a *App) attachTakeoverMirror(tabID, sessionPath string) {
	key := sessionRuntimeKey(sessionPath)
	if key == "" {
		return
	}
	a.takeoverMu.Lock()
	m := a.takeoverMirrors[key]
	a.takeoverMu.Unlock()
	if m == nil {
		return
	}
	a.mu.RLock()
	tab := a.tabByIDLocked(tabID)
	var sink *tabEventSink
	if tab != nil {
		sink = tab.sink
	}
	a.mu.RUnlock()
	if tab == nil || sink == nil {
		return
	}
	m.mu.Lock()
	m.tabID = tabID
	m.sink = sink
	m.mu.Unlock()
	sink.setTakeoverMirror(m)
}

func (a *App) takeoverMirrorForKey(key string) *takeoverMirror {
	if key == "" {
		return nil
	}
	a.takeoverMu.Lock()
	defer a.takeoverMu.Unlock()
	return a.takeoverMirrors[key]
}

// stopTakeoverMirrors halts every mirror's forwarding loop. The registry
// entries stay: endTakeoverMirrors still needs them after controller teardown
// has released the leases to tell Serve the writers are gone.
func (a *App) stopTakeoverMirrors() {
	for _, m := range a.snapshotTakeoverMirrors() {
		m.stopLoop()
	}
}

// endTakeoverMirrors is the shutdown epilogue: every lease is released by now,
// so Serve's mirror-end immediately hands the sessions back to their remote
// tabs instead of waiting out the stale-mirror timeout.
func (a *App) endTakeoverMirrors() {
	for _, m := range a.snapshotTakeoverMirrors() {
		if m.finalizePendingReturn() {
			m.mirrorEnd()
		}
		m.detach()
	}
}

func (a *App) snapshotTakeoverMirrors() []*takeoverMirror {
	a.takeoverMu.Lock()
	mirrors := make([]*takeoverMirror, 0, len(a.takeoverMirrors))
	for _, m := range a.takeoverMirrors {
		mirrors = append(mirrors, m)
	}
	a.takeoverMu.Unlock()
	return mirrors
}

// returnTakeoverLeaseForShutdown publishes the reverse reservation while the
// tab still owns its lease. The controller snapshot has already completed in
// shutdownBody; a failed reservation leaves the lease installed so the normal
// release fallback can still make progress.
func (a *App) returnTakeoverLeaseForShutdown(tab *WorkspaceTab) bool {
	if tab == nil {
		return false
	}
	path := strings.TrimSpace(tab.currentSessionPath())
	m := a.takeoverMirrorForKey(sessionRuntimeKey(path))
	if m == nil {
		return false
	}
	_, _, _, grant := m.snapshotClient()
	if grant.SourceWriterID == "" || grant.ReturnHandoffID == "" {
		return false
	}
	lease := tab.takeSessionLease()
	if lease == nil {
		return false
	}
	if err := lease.ReleaseForHandoff(grant.SourceWriterID, grant.ReturnHandoffID); err != nil {
		tab.adoptSessionLease(lease)
		slog.Warn("desktop: reserve takeover lease return during shutdown", "session", path, "err", err)
		return false
	}
	return true
}

// forwardEvent enqueues one local event for the mirror. Called from the tab
// sink's Emit; must never block the agent loop.
func (m *takeoverMirror) forwardEvent(e event.Event) {
	if m == nil {
		return
	}
	wired := eventwire.ToWire(e)
	m.mu.Lock()
	m.queue.Push(wired)
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// holdPendingReturn transfers a failed takeover target into the mirror. The
// tab's prior state stays intact while this independent lease fences out third
// writers until its reverse reservation is durably published.
func (m *takeoverMirror) holdPendingReturn(lease *agent.SessionLease) {
	if m == nil || lease == nil {
		return
	}
	m.mu.Lock()
	if m.pendingReturn != nil && m.pendingReturn != lease {
		m.mu.Unlock()
		lease.Release()
		return
	}
	m.pendingReturn = lease
	m.returnBackoff = 200 * time.Millisecond
	m.returnNextTry = time.Now()
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *takeoverMirror) returnLeaseAfterFailedTakeover(lease *agent.SessionLease) {
	m.holdPendingReturn(lease)
}

// retryPendingReturn reports true only when it published a pending reverse
// reservation. The run loop then ends the mirror from its own goroutine,
// avoiding stop/join self-deadlock.
func (m *takeoverMirror) retryPendingReturn(force bool) bool {
	if m == nil {
		return false
	}
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	m.mu.Lock()
	lease := m.pendingReturn
	grant := m.grant
	nextTry := m.returnNextTry
	release := m.releaseHandoff
	m.mu.Unlock()
	if lease == nil || (!force && time.Now().Before(nextTry)) {
		return false
	}
	var err error
	if release != nil {
		err = release(lease, grant.SourceWriterID, grant.ReturnHandoffID)
	} else {
		err = lease.ReleaseForHandoff(grant.SourceWriterID, grant.ReturnHandoffID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pendingReturn != lease {
		return false
	}
	if err == nil {
		m.pendingReturn = nil
		m.returnBackoff = 0
		m.returnNextTry = time.Time{}
		// Fence grant replacement before the run loop sends mirror-end. A new
		// registration will create its own mirror instead of rotating the
		// generation whose target lease was just returned.
		m.returned.Store(true)
		return true
	}
	if m.returnBackoff <= 0 {
		m.returnBackoff = 200 * time.Millisecond
	} else {
		m.returnBackoff = min(m.returnBackoff*2, 5*time.Second)
	}
	m.returnNextTry = time.Now().Add(m.returnBackoff)
	return false
}

// finalizePendingReturn runs at final app teardown. A reservation that still
// cannot be written keeps its lease until this point, then releases the OS
// lock without sending mirror-end; Serve will recover it as a vanished writer.
func (m *takeoverMirror) finalizePendingReturn() bool {
	if m == nil {
		return true
	}
	if m.retryPendingReturn(true) {
		return true
	}
	m.mu.Lock()
	lease := m.pendingReturn
	m.pendingReturn = nil
	m.mu.Unlock()
	if lease != nil {
		lease.Release()
		return false
	}
	return true
}

func (m *takeoverMirror) snapshotClient() (*http.Client, takeoverServeRecord, string, takeoverGrant) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client, m.record, m.tabID, m.grant
}

func (m *takeoverMirror) snapshotBinding() (*http.Client, takeoverServeRecord, string, takeoverGrant, uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client, m.record, m.tabID, m.grant, m.bindingRevision
}

func (m *takeoverMirror) bindingCurrent(client *http.Client, grant takeoverGrant, revision uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.client == client && m.grant.MirrorID == grant.MirrorID && m.bindingRevision == revision
}

func (m *takeoverMirror) run(initialClient *http.Client, initialRecord takeoverServeRecord) {
	defer close(m.done)
	m.mu.Lock()
	if m.client == nil {
		m.client = initialClient
		m.record = initialRecord
	}
	m.mu.Unlock()

	flushTimer := time.NewTimer(time.Hour)
	if !flushTimer.Stop() {
		<-flushTimer.C
	}
	heartbeat := time.NewTicker(takeoverMirrorHeartbeat)
	retryReturn := time.NewTicker(250 * time.Millisecond)
	defer flushTimer.Stop()
	defer heartbeat.Stop()
	defer retryReturn.Stop()
	flushArmed := false
	for {
		select {
		case <-m.stop:
			m.flushOnce(context.Background())
			return
		case <-m.wake:
			if !flushArmed {
				flushTimer.Reset(takeoverMirrorFlushEvery)
				flushArmed = true
			}
			continue
		case <-flushTimer.C:
			flushArmed = false
			if !m.pushOnce(false) {
				return
			}
		case <-heartbeat.C:
			if !m.pushOnce(true) {
				return
			}
		case <-retryReturn.C:
			if m.retryPendingReturn(false) {
				m.detach()
				m.mirrorEnd()
				return
			}
		}
		// A mirror whose tab is gone entirely (closed, not detached) ends
		// itself so Serve can hand the session back to the remote side.
		if !m.app.takeoverTabLive(m.sessionPath) {
			m.detach()
			m.mirrorEnd()
			return
		}
	}
}

func (m *takeoverMirror) pushOnce(heartbeat bool) bool {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	return m.pushOnceLocked(heartbeat)
}

func (m *takeoverMirror) pushOnceLocked(heartbeat bool) bool {
	if m.returned.Load() {
		return false
	}
	client, record, _, grant, revision := m.snapshotBinding()
	if client == nil || grant.MirrorID == "" {
		return true
	}
	frames := m.drainQueue()
	if len(frames) == 0 && !heartbeat {
		return true
	}
	marshal := func(batch []eventwire.Event) ([]byte, error) {
		return json.Marshal(map[string]any{
			"sessionPath": m.sessionPath, "mirrorId": grant.MirrorID, "frames": batch,
		})
	}
	batch, remainder, payload, err := eventwire.MarshalMirrorBatch(frames, eventwire.MirrorBatchMaxBytes, marshal)
	if err == nil && len(batch) == 0 && len(frames) > 0 && len(remainder) > 0 {
		remainder = remainder[1:]
	}
	m.requeue(remainder)
	if err != nil {
		m.requeue(batch)
		return true
	}
	if len(batch) == 0 && len(frames) > 0 {
		m.wakeIfQueued()
		if !heartbeat {
			return true
		}
		payload, _ = marshal(nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	resp, err := serveDo(ctx, client, http.MethodPost, serveURL(record.base, "/external/frames"), payload)
	if err != nil {
		cancel()
		if !m.bindingCurrent(client, grant, revision) {
			return true
		}
		m.requeue(batch)
		return m.retryAdoptOrDemote(client, grant, revision)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	cancel()
	if !m.bindingCurrent(client, grant, revision) {
		return true
	}
	switch resp.StatusCode {
	case http.StatusOK:
		m.mu.Lock()
		if m.bindingRevision == revision {
			m.consecutiveFailures = 0
		}
		m.mu.Unlock()
		var out struct {
			ReclaimRequested bool   `json:"reclaimRequested"`
			ReclaimMode      string `json:"reclaimMode"`
		}
		if json.Unmarshal(body, &out) == nil && out.ReclaimRequested {
			m.requestDemote(out.ReclaimMode)
		}
		m.wakeIfQueued()
		return true
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict:
		slog.Info("desktop: mirror generation rejected — attempting re-adopt",
			"session", m.sessionPath)
		m.requeue(batch)
		return m.retryAdoptOrDemote(client, grant, revision)
	default:
		m.requeue(batch)
		m.mu.Lock()
		if m.bindingRevision == revision {
			m.consecutiveFailures++
		}
		failures := m.consecutiveFailures
		m.mu.Unlock()
		if failures < 3 {
			return true
		}
		return m.retryAdoptOrDemote(client, grant, revision)
	}
}

// retryAdoptOrDemote attempts to re-establish the mirror with fresh serve
// credentials (the serve may have restarted with a new token). If the serve
// already owns the session (reclaim completed or another writer took over),
// demotes this tab to read-only and releases the lease so the remote side
// can proceed. Returns true if re-adopted (caller continues the loop).
func (m *takeoverMirror) retryAdoptOrDemote(oldClient *http.Client, oldGrant takeoverGrant, revision uint64) bool {
	if !m.bindingCurrent(oldClient, oldGrant, revision) {
		return true
	}
	conflicted := false
	records := discoverLocalTakeoverServesForMirror()
	for _, record := range records {
		if !pathWithinDir(m.sessionPath, config.ProjectSessionDir(record.state.Workspace)) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		client, err := takeoverClient(ctx, record)
		if err != nil {
			cancel()
			continue
		}
		body, bodyErr := json.Marshal(map[string]string{"sessionPath": m.sessionPath, "writerId": agent.SessionWriterID()})
		if bodyErr != nil {
			cancel()
			continue
		}
		resp, respErr := serveDo(ctx, client, http.MethodPost, serveURL(record.base, "/adopt"), body)
		cancel()
		if respErr != nil {
			continue
		}
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			var grant takeoverGrant
			if json.Unmarshal(respBody, &grant) != nil || grant.MirrorID == "" || grant.ReturnHandoffID == "" || grant.SourceWriterID == "" ||
				grant.TargetWriterID != agent.SessionWriterID() || sessionRuntimeKey(grant.SessionPath) != sessionRuntimeKey(m.sessionPath) {
				continue
			}
			// Re-adopted: swap in the fresh client and keep mirroring.
			m.mu.Lock()
			if m.client == oldClient && m.grant.MirrorID == oldGrant.MirrorID && m.bindingRevision == revision {
				m.client = client
				m.record = record
				m.grant = grant
				m.bindingRevision++
				m.consecutiveFailures = 0
			}
			m.mu.Unlock()
			slog.Info("desktop: mirror re-adopted with fresh credentials",
				"session", m.sessionPath, "base", record.base)
			return true
		}
		if resp.StatusCode == http.StatusConflict {
			conflicted = true
			continue
		}
		// Other statuses: try next record.
	}
	if conflicted && m.bindingCurrent(oldClient, oldGrant, revision) {
		slog.Info("desktop: serve holds session — demoting to release lease",
			"session", m.sessionPath)
		m.requestDemote("")
		return false
	}
	// The Serve may be restarting or its state/token files may not have become
	// visible yet. Keep the bounded queue and retry on the next heartbeat.
	slog.Warn("desktop: mirror re-adopt unavailable; retaining local writer and bounded queue",
		"session", m.sessionPath)
	return true
}

func (m *takeoverMirror) drainQueue() []eventwire.Event {
	m.mu.Lock()
	frames := m.queue.Take(takeoverMirrorMaxQueue)
	m.mu.Unlock()
	return frames
}

func (m *takeoverMirror) requeue(frames []eventwire.Event) {
	if len(frames) == 0 {
		return
	}
	m.mu.Lock()
	m.queue.Prepend(frames)
	m.mu.Unlock()
}

func (m *takeoverMirror) wakeIfQueued() {
	m.mu.Lock()
	pending := m.queue.Len() > 0
	m.mu.Unlock()
	if pending {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
}

func (m *takeoverMirror) flushOnce(ctx context.Context) {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	m.flushOnceLocked(ctx)
}

func (m *takeoverMirror) flushOnceLocked(ctx context.Context) {
	if m.returned.Load() {
		return
	}
	client, record, _, grant := m.snapshotClient()
	if client == nil || grant.MirrorID == "" {
		return
	}
	frames := m.drainQueue()
	if len(frames) == 0 {
		return
	}
	marshal := func(batch []eventwire.Event) ([]byte, error) {
		return json.Marshal(map[string]any{"sessionPath": m.sessionPath, "mirrorId": grant.MirrorID, "frames": batch})
	}
	batch, _, payload, err := eventwire.MarshalMirrorBatch(frames, eventwire.MirrorBatchMaxBytes, marshal)
	if err != nil {
		return
	}
	if len(batch) == 0 {
		return
	}
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := serveDo(flushCtx, client, http.MethodPost, serveURL(record.base, "/external/frames"), payload)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// takeoverTabLive reports whether the mirrored session still has a live
// desktop runtime (visible tab or detached background runtime).
func (a *App) takeoverTabLive(sessionPath string) bool {
	return a.sessionParentLive(sessionPath)
}

// tabHoldingSession returns the live tab currently owning the session path,
// using the same liveness notion as sessionParentLive. Mirrors outlive tab
// rebuilds, so lease-affecting actions must resolve the tab through the
// session rather than a snapshotted tab ID.
func (a *App) tabHoldingSession(sessionPath string) *WorkspaceTab {
	key := sessionRuntimeKey(sessionPath)
	if a == nil || key == "" {
		return nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	holding := func(tab *WorkspaceTab) bool {
		if tab == nil {
			return false
		}
		if sessionRuntimeKey(tab.SessionPath) == key {
			return true
		}
		return tab.Ctrl != nil && sessionRuntimeKey(tab.Ctrl.SessionPath()) == key
	}
	for _, tab := range a.tabs {
		if holding(tab) {
			return tab
		}
	}
	for _, tab := range a.detachedSessions {
		if holding(tab) {
			return tab
		}
	}
	return nil
}

// requestDemote reacts to a remote reclaim: this tab loses speaking rights.
// The demotion itself is passive — flip the tab read-only, release the lease,
// tell the user, and let Serve resume ownership.
func (m *takeoverMirror) requestDemote(mode string) {
	if !m.reclaimRequested.CompareAndSwap(false, true) {
		return
	}
	m.app.emitTakeoverNotice(m, event.LevelWarn, "session_reclaim_requested",
		"The remote side is taking this session back; this window is now read-only.")
	go m.demote(mode == string(handoffModeInterruptLocal))
}

const handoffModeInterruptLocal = "interrupt"

func (m *takeoverMirror) demote(interrupt bool) {
	a := m.app
	tab := a.tabByID(m.tabIDSnapshot())
	if tab == nil {
		// Tab IDs rotate on rebuilds and app restarts while a reclaim is in
		// flight; the session path is the stable handle. Without this
		// fallback the demote skipped the read-only flip and the lease
		// release, and the remote reclaim waited on a writer that had
		// already forgotten it was one.
		tab = a.tabHoldingSession(m.sessionPath)
	}
	var sink *tabEventSink
	if tab != nil {
		a.mu.RLock()
		sink = tab.sink
		a.mu.RUnlock()
		// Block new submits before waiting for the current turn to drain.
		a.setTabReadOnly(tab.ID, true)
	}
	m.emitNoticeSink(sink, event.LevelWarn, "session_taken_over_local",
		"This session was taken back by the remote side. This window is a read-only spectator.")
	if interrupt && tab != nil && tab.Ctrl != nil {
		// The remote asked to cancel an in-flight local turn; with the tab
		// read-only the admission gate refuses new input, cancel what is
		// already running so the drain completes quickly.
		tab.Ctrl.Cancel()
	}
	if tab == nil || tab.Ctrl == nil {
		return
	}
	deadline := time.Now().Add(takeoverHandoffTimeout)
	for controllerHasActiveRuntimeWork(tab.Ctrl) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if controllerHasActiveRuntimeWork(tab.Ctrl) {
		a.setTabReadOnly(tab.ID, false)
		return
	}
	if err := tab.Ctrl.Snapshot(); err != nil {
		slog.Warn("desktop: snapshot before returning takeover", "session", m.sessionPath, "err", err)
		a.setTabReadOnly(tab.ID, false)
		return
	}
	if err := m.returnLeaseForDemotion(tab); err != nil {
		a.setTabReadOnly(tab.ID, false)
		return
	}
	a.markLocalTakeoverSpectator(tab)
	m.stopAndFinalize(false)
	m.startSpectate(tab, sink)
}

// returnLeaseForDemotion waits for any in-flight sender/re-adoption, then uses
// one stable binding for the reverse reservation and mirror-end. returned is
// published before releasing sendMu, so the forwarding loop cannot issue a
// later request from the retired generation while demote stops and joins it.
func (m *takeoverMirror) returnLeaseForDemotion(tab *WorkspaceTab) error {
	if m == nil || tab == nil {
		return fmt.Errorf("takeover return target unavailable")
	}
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	client, record, _, grant := m.snapshotClient()
	if client == nil || grant.MirrorID == "" || grant.SourceWriterID == "" || grant.ReturnHandoffID == "" {
		return fmt.Errorf("takeover return binding unavailable")
	}
	lease := tab.takeSessionLease()
	if lease == nil {
		return fmt.Errorf("takeover session lease unavailable")
	}
	if err := lease.ReleaseForHandoff(grant.SourceWriterID, grant.ReturnHandoffID); err != nil {
		tab.adoptSessionLease(lease)
		return err
	}
	m.returned.Store(true)
	m.mirrorEndLocked(client, record, grant)
	return nil
}

// emitTakeoverNotice surfaces a takeover lifecycle change as a notice frame on
// the tab's event channel (best effort; the sink may not exist yet).
func (a *App) emitTakeoverNotice(m *takeoverMirror, level event.Level, code, text string) {
	m.mu.Lock()
	sink := m.sink
	m.mu.Unlock()
	m.emitNoticeSink(sink, level, code, text)
}

func (m *takeoverMirror) emitNoticeSink(sink *tabEventSink, level event.Level, code, text string) {
	if sink == nil {
		return
	}
	tabID, _ := sink.binding()
	e := event.Event{Kind: event.Notice, Level: level, Code: code, Text: text, SessionPath: m.sessionPath}
	sink.emitRuntimeEvent(eventChannel, toWireTabWithSubmission(e, tabID, sink.runtimeEpochSnapshot(), "", 0, sink.sessionGenerationSnapshot()))
}

// mirrorEnd tells Serve the writer is gone so the remote side resumes without
// waiting for the stale-mirror timeout. Best effort.
func (m *takeoverMirror) mirrorEnd() {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	m.mu.Lock()
	pending := m.pendingReturn != nil
	m.mu.Unlock()
	if pending {
		return
	}
	client, record, _, grant := m.snapshotClient()
	if client == nil || grant.MirrorID == "" {
		return
	}
	m.mirrorEndLocked(client, record, grant)
}

func (m *takeoverMirror) mirrorEndLocked(client *http.Client, record takeoverServeRecord, grant takeoverGrant) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	payload, _ := json.Marshal(map[string]string{"sessionPath": m.sessionPath, "mirrorId": grant.MirrorID})
	resp, err := serveDo(ctx, client, http.MethodPost, serveURL(record.base, "/mirror-end"), payload)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

// stopLoop halts the forwarding goroutine. Idempotent.
func (m *takeoverMirror) stopLoop() {
	m.stopping.Store(true)
	m.stopOnce.Do(func() { close(m.stop) })
	<-m.done
}

// detach removes the mirror from the app registry and clears the sink hook.
func (m *takeoverMirror) detach() {
	m.detachOnce.Do(func() {
		m.app.takeoverMu.Lock()
		if m.app.takeoverMirrors[m.key] == m {
			delete(m.app.takeoverMirrors, m.key)
		}
		m.app.takeoverMu.Unlock()
		if sink := m.currentSink(); sink != nil {
			sink.setTakeoverMirror(nil)
		}
	})
}

// shutdown stops the forwarding loop and deregisters the mirror; when
// notifyServe is set (the mirror ended without a demotion, e.g. its tab
// closed) Serve is told the writer is gone once it can accept that.
func (m *takeoverMirror) stopAndFinalize(notifyServe bool) {
	m.stopLoop()
	m.detach()
	if notifyServe {
		m.mirrorEnd()
	}
}

func (m *takeoverMirror) currentSink() *tabEventSink {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sink
}

// startSpectate keeps the demoted tab rendering by streaming Serve's frames
// (the remote side is the writer again) into the tab's event channel. The
// frames are the same wire contract the local reducer already consumes.
func (m *takeoverMirror) startSpectate(tab *WorkspaceTab, sink *tabEventSink) {
	client, record, _, _ := m.snapshotClient()
	if tab == nil || sink == nil || client == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithCancel(m.app.ctx)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, serveURL(record.base, "/events?all=1"), nil)
		if err != nil {
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		canonical := agent.CanonicalSessionPath(m.sessionPath)
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
		for scanner.Scan() {
			select {
			case <-m.app.ctx.Done():
				return
			default:
			}
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var frame eventwire.Event
			if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame) != nil {
				continue
			}
			if frame.SessionPath != canonical {
				continue
			}
			sink.emitRuntimeEvent(eventChannel, wireEventTab{Event: frame, TabID: m.tabIDSnapshot()})
		}
	}()
}

func (m *takeoverMirror) tabIDSnapshot() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tabID
}
