package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/filelock"
	"reasonix/internal/fileutil"
	"reasonix/internal/repair"
)

const (
	desktopLifecycleSchemaVersion = 2
	desktopLifecycleRetention     = 30 * 24 * time.Hour
	maxDesktopLifecycleRecords    = 20
	desktopLifecycleReadRetries   = 12
	desktopLifecycleReadBackoff   = 20 * time.Millisecond
)

type desktopLifecycleState struct {
	SchemaVersion int    `json:"schemaVersion"`
	PID           int    `json:"pid"`
	RunID         string `json:"runId"`
	Version       string `json:"version,omitempty"`
	Channel       string `json:"channel,omitempty"`
	Phase         string `json:"phase"`
	StartedAt     string `json:"startedAt"`
	UpdatedAt     string `json:"updatedAt"`
}

type desktopLifecycleObservation struct {
	Version   string
	Channel   string
	Phase     string
	StartedAt string
	UpdatedAt string
}

type desktopLifecycleRuntime struct {
	previousRun  repair.PreviousRunObservation
	previousRuns []desktopLifecycleObservation
	tracker      *desktopLifecycleTracker
}

type desktopLifecycleTracker struct {
	mu           sync.Mutex
	dir          string
	path         string
	state        desktopLifecycleState
	now          func() time.Time
	processAlive func(int) bool
	updates      chan string
	writerStop   chan struct{}
	writerDone   chan struct{}
	writerOnce   sync.Once
	stopOnce     sync.Once
	writerActive bool
}

func newDesktopLifecycleTracker(root, appVersion, appChannel string) *desktopLifecycleTracker {
	now := time.Now().UTC()
	runID := newDesktopLifecycleRunID()
	dir := filepath.Join(root, "diagnostics", "lifecycle")
	return &desktopLifecycleTracker{
		dir:  dir,
		path: filepath.Join(dir, strconv.Itoa(os.Getpid())+"-"+runID+".json"),
		state: desktopLifecycleState{
			SchemaVersion: desktopLifecycleSchemaVersion,
			PID:           os.Getpid(),
			RunID:         runID,
			Version:       appVersion,
			Channel:       appChannel,
			Phase:         "starting",
			StartedAt:     now.Format(time.RFC3339Nano),
			UpdatedAt:     now.Format(time.RFC3339Nano),
		},
		now:          func() time.Time { return time.Now().UTC() },
		processAlive: desktopProcessAlive,
		updates:      make(chan string, 1),
		writerStop:   make(chan struct{}),
		writerDone:   make(chan struct{}),
	}
}

func prepareDesktopDiagnostics(app *App) {
	if app == nil || app.remoteWindowTicket != "" || version == "dev" {
		return
	}
	root := config.MemoryUserDir()
	if root == "" {
		return
	}
	diagnosticsDir := filepath.Join(root, "diagnostics")
	if err := os.MkdirAll(diagnosticsDir, 0o700); err != nil {
		return
	}
	release, err := filelock.TryAcquire(filepath.Join(diagnosticsDir, "primary.lock"))
	if err != nil {
		return
	}
	app.diagnosticsOwner = true
	app.diagnosticsOwnerRelease = release

	cfg, err := config.Load()
	if err != nil {
		return
	}
	app.diagnosticsConfigLoaded = true
	if !cfg.DesktopTelemetry() {
		return
	}
	app.diagnosticsTelemetry = true
	tracker := newDesktopLifecycleTracker(root, version, channel)
	if tracker.start() == nil {
		app.lifecycle.tracker = tracker
	}
}

func (a *App) releaseDesktopDiagnosticsOwnership() {
	if a == nil || a.diagnosticsOwnerRelease == nil {
		return
	}
	a.diagnosticsOwnerRelease()
	a.diagnosticsOwnerRelease = nil
	a.diagnosticsOwner = false
	a.diagnosticsConfigLoaded = false
	a.diagnosticsTelemetry = false
}

func initializeLifecycleDiagnostics(app *App) {
	if app == nil || app.remoteWindowTicket != "" || !app.diagnosticsOwner {
		return
	}
	if !app.diagnosticsConfigLoaded || version == "dev" {
		return
	}
	tracker := app.lifecycle.tracker
	if tracker == nil {
		tracker = newDesktopLifecycleTracker(config.MemoryUserDir(), version, channel)
	}
	enabled := app.diagnosticsTelemetry
	legacy := repair.NewStartupTracker("").ObservePreviousRun()
	if enabled {
		app.lifecycle.previousRun = legacy
	}
	app.lifecycle.previousRuns = tracker.consumePrevious(enabled)
	installWebKitProcessObserver(app, enabled)
	if enabled {
		refreshWebRuntimeContext()
	}
}

func (a *App) markDesktopHealthy() {
	a.startupReady.Store(true)
	a.lifecycle.tracker.markAsync("healthy")
}

func newDesktopLifecycleRunID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return hex.EncodeToString(raw[:])
	}
	return strconv.FormatInt(time.Now().UTC().UnixNano(), 16)
}

func (t *desktopLifecycleTracker) start() error {
	if t == nil || t.path == "" {
		return nil
	}
	if err := t.writeState(); err != nil {
		return err
	}
	t.writerOnce.Do(func() {
		t.mu.Lock()
		t.writerActive = true
		t.mu.Unlock()
		go t.runWriter()
	})
	return nil
}

func (t *desktopLifecycleTracker) runWriter() {
	defer close(t.writerDone)
	for {
		select {
		case phase := <-t.updates:
			t.mark(phase)
		case <-t.writerStop:
			select {
			case phase := <-t.updates:
				t.mark(phase)
			default:
			}
			return
		}
	}
}

// markAsync keeps normal startup and DOM-ready paths free from diagnostic I/O.
// The single-slot queue is last-state-wins because lifecycle phases are
// monotonic and only the newest pending phase is useful.
func (t *desktopLifecycleTracker) markAsync(phase string) {
	if t == nil || strings.TrimSpace(phase) == "" {
		return
	}
	select {
	case <-t.writerStop:
		return
	default:
	}
	select {
	case t.updates <- phase:
		return
	default:
	}
	select {
	case <-t.updates:
	default:
	}
	select {
	case t.updates <- phase:
	default:
	}
}

func (t *desktopLifecycleTracker) stopWriter() {
	if t == nil {
		return
	}
	t.mu.Lock()
	active := t.writerActive
	t.mu.Unlock()
	if !active {
		return
	}
	t.stopOnce.Do(func() { close(t.writerStop) })
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-t.writerDone:
	case <-timer.C:
	}
}

func (t *desktopLifecycleTracker) mark(phase string) {
	if t == nil || t.path == "" || strings.TrimSpace(phase) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.state.Phase = phase
	t.state.UpdatedAt = t.now().Format(time.RFC3339Nano)
	_ = t.writeStateLocked()
}

func (t *desktopLifecycleTracker) clean() {
	if t == nil || t.path == "" {
		return
	}
	t.stopWriter()
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = os.Remove(t.path)
}

func (t *desktopLifecycleTracker) writeState() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.writeStateLocked()
}

func (t *desktopLifecycleTracker) writeStateLocked() error {
	body, err := json.Marshal(t.state)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(t.path, body, 0o600)
}

// consumePrevious atomically owns every dead per-process record before
// returning it. When emit is false (telemetry opt-out), records are consumed
// without exposing their contents to the reporting path.
func (t *desktopLifecycleTracker) consumePrevious(emit bool) []desktopLifecycleObservation {
	if t == nil || t.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(t.dir)
	if err != nil {
		return nil
	}
	now := t.now()
	observations := make([]desktopLifecycleObservation, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(t.dir, entry.Name())
		state, readErr := readDesktopLifecycleState(path)
		if readErr != nil {
			if info, statErr := entry.Info(); statErr == nil && now.Sub(info.ModTime()) > desktopLifecycleRetention {
				_ = os.Remove(path)
			}
			continue
		}
		// A newer Desktop may own a lifecycle schema this version cannot safely
		// interpret. Preserve it verbatim so a downgrade never consumes or prunes
		// future-format evidence.
		if state.SchemaVersion != desktopLifecycleSchemaVersion {
			continue
		}
		if state.PID <= 0 || state.Phase == "" {
			if info, statErr := entry.Info(); statErr == nil && now.Sub(info.ModTime()) > desktopLifecycleRetention {
				_ = os.Remove(path)
			}
			continue
		}
		if t.processAlive(state.PID) {
			continue
		}
		claimed := path + ".claimed-" + t.state.RunID
		// Windows fails a bare rename with a sharing violation while any other
		// instance still holds the record open, so without the retry every
		// claimant can lose the same race and the evidence is dropped by all.
		if err := fileutil.ClaimRename(path, claimed); err != nil {
			continue
		}
		state, readErr = readClaimedLifecycleState(claimed)
		if readErr != nil || state.SchemaVersion != desktopLifecycleSchemaVersion {
			// The file changed between inspection and claim. Put it back when
			// possible instead of deleting data that may belong to another schema.
			_ = os.Rename(claimed, path)
			continue
		}
		_ = os.Remove(claimed)
		if !emit {
			continue
		}
		observations = append(observations, desktopLifecycleObservation{
			Version: state.Version, Channel: state.Channel, Phase: state.Phase,
			StartedAt: state.StartedAt, UpdatedAt: state.UpdatedAt,
		})
	}
	t.pruneRecords()
	return observations
}

// readClaimedLifecycleState re-reads a just-claimed record. Winning the rename
// does not make the file readable yet: an instance that was inspecting it still
// holds a handle, and Windows answers with a sharing violation until it drops.
// Only the open is retried — content this build cannot parse is a definite
// answer about the record, not a lock waiting to clear.
func readClaimedLifecycleState(path string) (desktopLifecycleState, error) {
	var body []byte
	var err error
	for attempt := range desktopLifecycleReadRetries {
		if body, err = os.ReadFile(path); err == nil || os.IsNotExist(err) {
			break
		}
		time.Sleep(time.Duration(attempt+1) * desktopLifecycleReadBackoff)
	}
	if err != nil {
		return desktopLifecycleState{}, err
	}
	var state desktopLifecycleState
	err = json.Unmarshal(body, &state)
	return state, err
}

func readDesktopLifecycleState(path string) (desktopLifecycleState, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return desktopLifecycleState{}, err
	}
	var state desktopLifecycleState
	if err := json.Unmarshal(body, &state); err != nil {
		return desktopLifecycleState{}, err
	}
	return state, nil
}

func (t *desktopLifecycleTracker) pruneRecords() {
	entries, err := os.ReadDir(t.dir)
	if err != nil || len(entries) <= maxDesktopLifecycleRecords {
		return
	}
	type candidate struct {
		path string
		at   time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(t.dir, entry.Name())
		state, err := readDesktopLifecycleState(path)
		if err != nil || state.SchemaVersion != desktopLifecycleSchemaVersion || t.processAlive(state.PID) {
			continue
		}
		if info, err := entry.Info(); err == nil {
			candidates = append(candidates, candidate{path: path, at: info.ModTime()})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at.Before(candidates[j].at) })
	for len(candidates) > maxDesktopLifecycleRecords {
		_ = os.Remove(candidates[0].path)
		candidates = candidates[1:]
	}
}
