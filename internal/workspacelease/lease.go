// Package workspacelease serializes Delivery writers that target the same
// workspace. Readers never acquire a lease. Write-tool holds are released when
// the tool returns, with bounded retention for background jobs.
package workspacelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"reasonix/internal/filelock"
)

const backgroundGrace = 30 * time.Second

// WaitNotice is called once when an acquisition cannot complete immediately.
// It must return quickly and must not call back into Owner.
type WaitNotice func()

type ownerActivity struct {
	activeRuns int
	background int
}

type systemHold struct {
	refs    int
	scope   string
	keys    []string
	slots   []string
	release func()
}

type sharedSystemHold struct {
	refs    int
	release func()
}

type ownerLease struct {
	acquiring   bool
	waiting     bool
	targetScope string
	targetLabel string
	targetKeys  []string
	acquireDone chan struct{}
	changed     chan struct{}
	holds       map[uint64]*systemHold
	shared      map[string]*sharedSystemHold
	nextID      uint64
	legacy      []func()
	epoch       uint64
	graceTimer  *time.Timer
}

// Owner is one Delivery session's re-entrant workspace lease. One Owner may be
// shared by the root agent and its subagents; different sessions use different
// Owners even when they share a workspace.
type Owner struct {
	lockPath      string
	lockDir       string
	canonical     string
	compatibility string
	onWait        WaitNotice
	graceAfter    time.Duration

	mu       sync.Mutex
	activity ownerActivity
	lease    ownerLease
}

// State is a sanitized process-local snapshot used by Desktop. WaitingKeys are
// internal canonical identities; they are never copied into the Wails payload.
type State struct {
	Acquired    bool
	Waiting     bool
	Scope       string
	Label       string
	HeldScope   string
	HeldLabel   string
	HeldKeys    []string
	WaitingKeys []string
}

// State returns the current acquisition state without performing lease I/O.
func (o *Owner) State() State {
	if o == nil {
		return State{}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	heldScope, heldLabel := o.holdScopeLocked()
	heldKeys := o.heldKeysLocked()
	state := State{
		Acquired:  len(o.lease.holds) > 0,
		Waiting:   o.lease.waiting,
		Scope:     heldScope,
		Label:     heldLabel,
		HeldScope: heldScope,
		HeldLabel: heldLabel,
		HeldKeys:  heldKeys,
	}
	if o.lease.waiting {
		state.Scope = o.lease.targetScope
		state.Label = o.lease.targetLabel
		state.WaitingKeys = append([]string(nil), o.lease.targetKeys...)
	}
	return state
}

func (o *Owner) holdScopeLocked() (string, string) {
	keys := map[string]string{}
	for _, hold := range o.lease.holds {
		if hold.scope == "workspace" {
			return "workspace", ""
		}
		for _, key := range hold.keys {
			keys[key] = filepath.Base(key)
		}
	}
	switch len(keys) {
	case 0:
		return "", ""
	case 1:
		for _, label := range keys {
			return "file", label
		}
	default:
		return "files", fmt.Sprintf("%d files", len(keys))
	}
	return "", ""
}

// New returns a Delivery-session lease owner for workspaceRoot. lockDir is
// shared by Reasonix processes and remains outside the user's workspace.
func New(workspaceRoot, lockDir string, onWait WaitNotice) (*Owner, error) {
	canonical, compatibility, err := workspaceIdentities(workspaceRoot)
	if err != nil {
		return nil, err
	}
	lockDir = strings.TrimSpace(lockDir)
	if lockDir == "" {
		return nil, errors.New("workspace lease directory is unavailable")
	}
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, fmt.Errorf("create workspace lease directory: %w", err)
	}
	lockPath := workspaceLockPath(lockDir, compatibility)
	return &Owner{
		lockPath: lockPath, canonical: canonical, compatibility: compatibility,
		lockDir: lockDir,
		onWait:  onWait, graceAfter: backgroundGrace,
		lease: ownerLease{
			changed: make(chan struct{}), holds: map[uint64]*systemHold{},
			shared: map[string]*sharedSystemHold{},
		},
	}, nil
}

// HeldKeys returns the actual lock-domain identities currently held. An
// exclusive hold reports the workspace root; path holds report only files.
func (o *Owner) HeldKeys() []string {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.heldKeysLocked()
}

func (o *Owner) heldKeysLocked() []string {
	seen := map[string]bool{}
	for _, hold := range o.lease.holds {
		for _, key := range hold.keys {
			seen[key] = true
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// CanonicalWorkspace returns the stable identity used to key a workspace.
func CanonicalWorkspace(root string) (string, error) {
	canonical, _, err := workspaceIdentities(root)
	return canonical, err
}

func workspaceIdentities(root string) (canonical, compatibility string, err error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", "", errors.New("workspace root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve workspace root: %w", err)
	}
	abs = filepath.Clean(abs)
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = filepath.Clean(resolved)
	} else if !os.IsNotExist(resolveErr) {
		return "", "", fmt.Errorf("canonicalize workspace root: %w", resolveErr)
	}
	abs = nearestGitWorktreeRoot(abs)
	compatibility = compatibilityIdentityPath(abs)
	return normalizeIdentityPath(compatibility), compatibility, nil
}

func caseInsensitivePlatform() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}

func nearestGitWorktreeRoot(path string) string {
	start := path
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		start = filepath.Dir(path)
	}
	for current := start; ; current = filepath.Dir(current) {
		if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
	}
}

// BeginRun registers an agent run without taking a writer lease.
func (o *Owner) BeginRun() {
	if o == nil {
		return
	}
	o.mu.Lock()
	o.activity.activeRuns++
	o.cancelGraceLocked()
	o.mu.Unlock()
}

// EndRun drops leaked tool references after the final participating run.
func (o *Owner) EndRun() {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.activity.activeRuns > 0 {
		o.activity.activeRuns--
	}
	if o.activity.activeRuns == 0 {
		for _, hold := range o.lease.holds {
			hold.refs = 0
		}
		o.lease.legacy = nil
	}
	releases := o.collectInactiveLocked()
	o.mu.Unlock()
	runReleases(releases)
}

// AcquireWrite acquires a legacy workspace hold released by ReleaseWrite or
// EndRun. New call sites should prefer HoldWrite.
func (o *Owner) AcquireWrite(ctx context.Context) error {
	release, err := o.HoldWrite(ctx)
	if err == nil && o != nil {
		o.mu.Lock()
		o.lease.legacy = append(o.lease.legacy, release)
		o.mu.Unlock()
	}
	return err
}

// HoldWrite acquires an exclusive workspace hold. If this Owner already has
// path holds, it waits for them to finish instead of dropping their protection.
func (o *Owner) HoldWrite(ctx context.Context) (func(), error) {
	if o == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		o.mu.Lock()
		if id, hold := o.exclusiveHoldLocked(); hold != nil {
			hold.refs++
			o.cancelGraceLocked()
			o.mu.Unlock()
			return o.releaseHoldFunc(id), nil
		}
		if o.lease.acquiring {
			done := o.lease.acquireDone
			o.mu.Unlock()
			if err := waitForSignal(ctx, done); err != nil {
				return func() {}, err
			}
			continue
		}
		o.beginAcquisitionLocked("workspace", "", []string{o.canonical})
		for o.hasPathHoldsLocked() {
			if o.activity.background > 0 {
				o.armGraceLocked()
			}
			changed := o.lease.changed
			o.mu.Unlock()
			if err := waitForSignal(ctx, changed); err != nil {
				o.mu.Lock()
				o.finishAcquisitionLocked()
				o.mu.Unlock()
				return func() {}, err
			}
			o.mu.Lock()
		}
		o.mu.Unlock()

		notified := false
		release, err := o.acquireWorkspace(ctx, filelock.ModeExclusive, &notified)
		o.mu.Lock()
		var id uint64
		if err == nil {
			id = o.addHoldLocked(&systemHold{
				refs: 1, scope: "workspace", keys: []string{o.canonical}, release: release,
			})
		}
		o.finishAcquisitionLocked()
		releases := o.collectInactiveLocked()
		o.mu.Unlock()
		runReleases(releases)
		if err != nil {
			return func() {}, err
		}
		return o.releaseHoldFunc(id), nil
	}
}

// ReleaseWrite releases the most recent legacy AcquireWrite/AcquireWriteForPath.
func (o *Owner) ReleaseWrite() {
	if o == nil {
		return
	}
	o.mu.Lock()
	if len(o.lease.legacy) == 0 {
		o.mu.Unlock()
		return
	}
	last := len(o.lease.legacy) - 1
	release := o.lease.legacy[last]
	o.lease.legacy = o.lease.legacy[:last]
	o.mu.Unlock()
	release()
}

func (o *Owner) exclusiveHoldLocked() (uint64, *systemHold) {
	for id, hold := range o.lease.holds {
		if hold.scope == "workspace" {
			return id, hold
		}
	}
	return 0, nil
}

func (o *Owner) hasPathHoldsLocked() bool {
	for _, hold := range o.lease.holds {
		if hold.scope != "workspace" {
			return true
		}
	}
	return false
}

func (o *Owner) addHoldLocked(hold *systemHold) uint64 {
	o.lease.nextID++
	o.lease.holds[o.lease.nextID] = hold
	o.lease.epoch++
	o.cancelGraceLocked()
	o.signalChangedLocked()
	return o.lease.nextID
}

func (o *Owner) releaseHoldFunc(id uint64) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			o.mu.Lock()
			if hold := o.lease.holds[id]; hold != nil && hold.refs > 0 {
				hold.refs--
			}
			releases := o.collectInactiveLocked()
			o.signalChangedLocked()
			o.mu.Unlock()
			runReleases(releases)
		})
	}
}

// RetainUntil keeps completed tool holds alive for a background job.
func (o *Owner) RetainUntil(done <-chan struct{}) {
	if o == nil || done == nil {
		return
	}
	o.mu.Lock()
	if len(o.lease.holds) == 0 {
		o.mu.Unlock()
		return
	}
	o.activity.background++
	o.mu.Unlock()
	go func() {
		<-done
		o.mu.Lock()
		if o.activity.background > 0 {
			o.activity.background--
		}
		releases := o.collectInactiveLocked()
		o.mu.Unlock()
		runReleases(releases)
	}()
}

func (o *Owner) collectInactiveLocked() []func() {
	var inactive bool
	for _, hold := range o.lease.holds {
		if hold.refs == 0 {
			inactive = true
			break
		}
	}
	if !inactive {
		return nil
	}
	if o.activity.background > 0 {
		o.armGraceLocked()
		return nil
	}
	return o.takeInactiveLocked()
}

func (o *Owner) takeInactiveLocked() []func() {
	o.cancelGraceLocked()
	var releases []func()
	for id, hold := range o.lease.holds {
		if hold.refs != 0 {
			continue
		}
		delete(o.lease.holds, id)
		if hold.release != nil {
			releases = append(releases, hold.release)
		}
	}
	if len(releases) > 0 {
		o.signalChangedLocked()
	}
	return releases
}

func (o *Owner) armGraceLocked() {
	if o.graceAfter <= 0 || o.lease.graceTimer != nil {
		return
	}
	epoch := o.lease.epoch
	o.lease.graceTimer = time.AfterFunc(o.graceAfter, func() {
		o.mu.Lock()
		if o.lease.graceTimer == nil || o.lease.epoch != epoch {
			o.mu.Unlock()
			return
		}
		releases := o.takeInactiveLocked()
		o.mu.Unlock()
		runReleases(releases)
	})
}

func (o *Owner) cancelGraceLocked() {
	if o.lease.graceTimer != nil {
		o.lease.graceTimer.Stop()
		o.lease.graceTimer = nil
	}
}

func (o *Owner) beginAcquisitionLocked(scope, label string, keys []string) {
	o.lease.acquiring = true
	o.lease.acquireDone = make(chan struct{})
	o.lease.targetScope = scope
	o.lease.targetLabel = label
	o.lease.targetKeys = append([]string(nil), keys...)
	o.signalChangedLocked()
}

func (o *Owner) finishAcquisitionLocked() {
	o.lease.acquiring = false
	o.lease.waiting = false
	o.lease.targetScope = ""
	o.lease.targetLabel = ""
	o.lease.targetKeys = nil
	if o.lease.acquireDone != nil {
		close(o.lease.acquireDone)
		o.lease.acquireDone = nil
	}
	o.signalChangedLocked()
}

func (o *Owner) signalChangedLocked() {
	if o.lease.changed != nil {
		close(o.lease.changed)
	}
	o.lease.changed = make(chan struct{})
}

func (o *Owner) markWaiting() bool {
	o.mu.Lock()
	first := !o.lease.waiting
	o.lease.waiting = true
	o.signalChangedLocked()
	o.mu.Unlock()
	return first
}

func (o *Owner) acquireWorkspace(ctx context.Context, mode filelock.Mode, notified *bool) (func(), error) {
	roots := append(ancestorDirectories(o.canonical), ancestorDirectories(o.compatibility)...)
	compatRelease, err := o.acquireCompatibilityRoots(ctx, roots, mode, notified)
	if err != nil {
		return nil, err
	}
	if mode != filelock.ModeExclusive {
		return compatRelease, nil
	}
	treeRelease, err := o.acquireQueuedMode(ctx, o.treeLockPath(o.canonical), filelock.ModeExclusive, notified)
	if err != nil {
		compatRelease()
		return nil, err
	}
	return func() { runReleases([]func(){compatRelease, treeRelease}) }, nil
}

// acquireCompatibilityRoots keeps the original per-workspace lock protocol in
// the hierarchy. Previous Reasonix versions only know these exact lock files,
// so descendants take their ancestor locks shared while a whole-workspace
// writer takes its own root exclusively.
func (o *Owner) acquireCompatibilityRoots(
	ctx context.Context,
	roots []string,
	rootMode filelock.Mode,
	notified *bool,
) (func(), error) {
	roots = orderedWorkspaceRoots(roots)
	releases := make([]func(), 0, len(roots))
	for _, root := range roots {
		mode := filelock.ModeShared
		if normalizeIdentityPath(root) == o.canonical {
			mode = rootMode
		}
		release, err := o.acquireQueuedMode(ctx, workspaceLockPath(o.lockDir, root), mode, notified)
		if err != nil {
			runReleases(releases)
			return nil, err
		}
		releases = append(releases, release)
	}
	return func() { runReleases(releases) }, nil
}

func (o *Owner) acquireQueuedMode(ctx context.Context, lockPath string, mode filelock.Mode, notified *bool) (func(), error) {
	if mode == filelock.ModeShared {
		return o.acquireSharedDomain(ctx, lockPath, notified)
	}
	return o.acquireQueuedModeRaw(ctx, lockPath, mode, notified)
}

func (o *Owner) acquireSharedDomain(ctx context.Context, lockPath string, notified *bool) (func(), error) {
	lockPath = compatibilityIdentityPath(lockPath)
	o.mu.Lock()
	// Re-enter before the writer-priority queue: its writer waits for this Owner's
	// shared hold, so queueing the same Owner would deadlock. Other Owners still
	// queue because they have no entry here.
	if hold := o.lease.shared[lockPath]; hold != nil {
		hold.refs++
		o.mu.Unlock()
		return o.releaseSharedDomainFunc(lockPath, hold), nil
	}
	o.mu.Unlock()

	release, err := o.acquireQueuedModeRaw(ctx, lockPath, filelock.ModeShared, notified)
	if err != nil {
		return nil, err
	}
	hold := &sharedSystemHold{refs: 1, release: release}
	o.mu.Lock()
	o.lease.shared[lockPath] = hold
	o.mu.Unlock()
	return o.releaseSharedDomainFunc(lockPath, hold), nil
}

func (o *Owner) releaseSharedDomainFunc(lockPath string, target *sharedSystemHold) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			var release func()
			o.mu.Lock()
			if hold := o.lease.shared[lockPath]; hold == target {
				hold.refs--
				if hold.refs == 0 {
					delete(o.lease.shared, lockPath)
					release = hold.release
				}
			}
			o.mu.Unlock()
			if release != nil {
				release()
			}
		})
	}
}

func (o *Owner) acquireQueuedModeRaw(ctx context.Context, lockPath string, mode filelock.Mode, notified *bool) (func(), error) {
	queueRelease, err := o.acquireMode(ctx, lockPath+".queue", filelock.ModeExclusive, notified)
	if err != nil {
		return nil, err
	}
	release, err := o.acquireMode(ctx, lockPath, mode, notified)
	queueRelease()
	return release, err
}

func workspaceLockPath(lockDir, canonical string) string {
	canonical = compatibilityIdentityPath(canonical)
	sum := sha256.Sum256([]byte(canonical))
	return filepath.Join(lockDir, hex.EncodeToString(sum[:])+".lock")
}

func ancestorDirectories(path string) []string {
	path = compatibilityIdentityPath(path)
	reversed := []string{path}
	for current := path; ; {
		parent := compatibilityIdentityPath(filepath.Dir(current))
		if parent == current {
			break
		}
		reversed = append(reversed, parent)
		current = parent
	}
	out := make([]string, len(reversed))
	for i := range reversed {
		out[len(reversed)-1-i] = reversed[i]
	}
	return out
}

func orderedWorkspaceRoots(roots []string) []string {
	seen := make(map[string]bool, len(roots))
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		root = compatibilityIdentityPath(root)
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		out = append(out, root)
	}
	sort.Slice(out, func(i, j int) bool {
		leftDepth := len(ancestorDirectories(out[i]))
		rightDepth := len(ancestorDirectories(out[j]))
		if leftDepth == rightDepth {
			return out[i] < out[j]
		}
		return leftDepth < rightDepth
	})
	return out
}

// compatibilityIdentityPath preserves the exact path bytes used by the
// previous workspace-lock protocol. New path and hierarchy stripes use
// normalizeIdentityPath instead, so macOS aliases still share those domains.
func compatibilityIdentityPath(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(filepath.ToSlash(path))
	}
	return path
}

func normalizeIdentityPath(path string) string {
	path = filepath.Clean(path)
	if caseInsensitivePlatform() {
		path = strings.ToLower(filepath.ToSlash(path))
	}
	return path
}

func (o *Owner) acquireMode(ctx context.Context, path string, mode filelock.Mode, notified *bool) (func(), error) {
	release, err := filelock.TryAcquireMode(path, mode)
	if err == nil {
		return release, nil
	}
	if !errors.Is(err, filelock.ErrHeld) {
		return nil, fmt.Errorf("acquire workspace write lease: %w", err)
	}
	if o.markWaiting() && !*notified {
		*notified = true
		if o.onWait != nil {
			o.onWait()
		}
	}
	release, err = filelock.AcquireMode(ctx, path, mode)
	if err != nil {
		return nil, fmt.Errorf("acquire workspace write lease: %w", err)
	}
	return release, nil
}

func waitForSignal(ctx context.Context, signal <-chan struct{}) error {
	select {
	case <-signal:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func runReleases(releases []func()) {
	for _, release := range slices.Backward(releases) {
		release()
	}
}
