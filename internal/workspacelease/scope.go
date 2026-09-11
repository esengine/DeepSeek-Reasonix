package workspacelease

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/filelock"
)

// All workspaces share a fixed set of hashed path-lock files. Hash collisions
// conservatively serialize unrelated files without allowing inode growth to
// track every path ever written.
const pathLockStripes = 4096

// Hierarchy locks make overlapping workspace roots intersect without making a
// whole-workspace writer take every path stripe. They are bounded separately so
// historical directory names cannot create an unbounded set of lock files.
const treeLockStripes = 4096

type pathSpec struct {
	key           string
	compatibility string
	display       string
	slot          string
}

// AcquireWriteForPath takes a legacy file-scoped hold released by ReleaseWrite
// or EndRun. New call sites should prefer HoldWriteForPath(s).
func (o *Owner) AcquireWriteForPath(ctx context.Context, abs string) error {
	release, err := o.HoldWriteForPath(ctx, abs)
	if err == nil && o != nil {
		o.mu.Lock()
		o.lease.legacy = append(o.lease.legacy, release)
		o.mu.Unlock()
	}
	return err
}

// HoldWriteForPath acquires a file-scoped write hold.
func (o *Owner) HoldWriteForPath(ctx context.Context, abs string) (func(), error) {
	return o.HoldWriteForPaths(ctx, []string{abs})
}

// HoldWriteForPaths acquires one atomic, stably ordered file-scoped hold. All
// paths are canonicalized and de-duplicated before any system lock is taken.
func (o *Owner) HoldWriteForPaths(ctx context.Context, paths []string) (func(), error) {
	if o == nil {
		return func() {}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	specs, err := o.pathSpecs(paths)
	if err != nil || len(specs) == 0 {
		return o.HoldWrite(ctx)
	}
	keys, slots := specKeys(specs), specSlots(specs)
	compatibilityRoots := o.compatibilityRoots(specs)
	treeSlots := o.pathTreeSlots(specs)
	scope, label := pathScope(specs)
	for {
		o.mu.Lock()
		if id, hold := o.exclusiveHoldLocked(); hold != nil {
			hold.refs++
			o.cancelGraceLocked()
			o.mu.Unlock()
			return o.releaseHoldFunc(id), nil
		}
		if id, hold := o.coveringPathHoldLocked(keys); hold != nil {
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
		o.beginAcquisitionLocked(scope, label, keys)
		for !o.pathOrderAllowedLocked(slots) {
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
		release, err := o.acquirePathSystem(ctx, compatibilityRoots, treeSlots, slots, &notified)
		o.mu.Lock()
		var id uint64
		if err == nil {
			id = o.addHoldLocked(&systemHold{
				refs: 1, scope: scope, keys: keys, slots: slots, release: release,
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

func (o *Owner) pathSpecs(paths []string) ([]pathSpec, error) {
	seen := map[string]bool{}
	specs := make([]pathSpec, 0, len(paths))
	for _, path := range paths {
		compatibility, display, err := canonicalFilePath(path)
		key := normalizeIdentityPath(compatibility)
		if err != nil || key == "" || !canonicalContains(o.canonical, key) {
			if err == nil {
				err = errors.New("path is outside the workspace")
			}
			return nil, err
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		specs = append(specs, pathSpec{
			key: key, compatibility: compatibility,
			display: display, slot: o.pathLockPath(key),
		})
	}
	sort.Slice(specs, func(i, j int) bool {
		if specs[i].slot == specs[j].slot {
			return specs[i].key < specs[j].key
		}
		return specs[i].slot < specs[j].slot
	})
	return specs, nil
}

func specKeys(specs []pathSpec) []string {
	keys := make([]string, 0, len(specs))
	for _, spec := range specs {
		keys = append(keys, spec.key)
	}
	return keys
}

func specSlots(specs []pathSpec) []string {
	var slots []string
	for _, spec := range specs {
		if len(slots) == 0 || slots[len(slots)-1] != spec.slot {
			slots = append(slots, spec.slot)
		}
	}
	return slots
}

func pathScope(specs []pathSpec) (string, string) {
	if len(specs) == 1 {
		return "file", specs[0].display
	}
	return "files", fmt.Sprintf("%d files", len(specs))
}

func (o *Owner) coveringPathHoldLocked(keys []string) (uint64, *systemHold) {
	for id, hold := range o.lease.holds {
		if hold.scope == "workspace" || len(hold.keys) < len(keys) {
			continue
		}
		held := make(map[string]bool, len(hold.keys))
		for _, key := range hold.keys {
			held[key] = true
		}
		covered := true
		for _, key := range keys {
			if !held[key] {
				covered = false
				break
			}
		}
		if covered {
			return id, hold
		}
	}
	return 0, nil
}

func (o *Owner) pathOrderAllowedLocked(slots []string) bool {
	if len(slots) == 0 {
		return true
	}
	var maxHeld string
	for _, hold := range o.lease.holds {
		for _, slot := range hold.slots {
			if slot > maxHeld {
				maxHeld = slot
			}
		}
	}
	return maxHeld == "" || slots[0] > maxHeld
}

func (o *Owner) acquirePathSystem(
	ctx context.Context,
	compatibilityRoots, treeSlots, slots []string,
	notified *bool,
) (func(), error) {
	parentRelease, err := o.acquireCompatibilityRoots(ctx, compatibilityRoots, filelock.ModeShared, notified)
	if err != nil {
		return nil, err
	}
	releases := []func(){parentRelease}
	for _, slot := range treeSlots {
		release, acquireErr := o.acquireQueuedMode(ctx, slot, filelock.ModeShared, notified)
		if acquireErr != nil {
			runReleases(releases)
			return nil, acquireErr
		}
		releases = append(releases, release)
	}
	for _, slot := range slots {
		release, acquireErr := o.acquireMode(ctx, slot, filelock.ModeExclusive, notified)
		if acquireErr != nil {
			runReleases(releases)
			return nil, acquireErr
		}
		releases = append(releases, release)
	}
	return func() { runReleases(releases) }, nil
}

func (o *Owner) pathLockPath(key string) string {
	return stripedLockPath(o.lockDir, "path", key, pathLockStripes)
}

func (o *Owner) treeLockPath(key string) string {
	return stripedLockPath(o.lockDir, "tree", key, treeLockStripes)
}

func stripedLockPath(lockDir, prefix, key string, stripes int) string {
	sum := sha256.Sum256([]byte(key))
	stripe := (int(sum[0])<<8 | int(sum[1])) % stripes
	return filepath.Join(lockDir, fmt.Sprintf("%s-%03x.lock", prefix, stripe))
}

func (o *Owner) compatibilityRoots(specs []pathSpec) []string {
	roots := append(ancestorDirectories(o.canonical), ancestorDirectories(o.compatibility)...)
	for _, spec := range specs {
		for _, dir := range compatibilityPathChain(o.compatibility, filepath.Dir(spec.compatibility)) {
			if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
				roots = append(roots, dir, normalizeIdentityPath(dir))
			}
		}
	}
	return orderedWorkspaceRoots(roots)
}

func compatibilityPathChain(root, target string) []string {
	root = compatibilityIdentityPath(root)
	target = compatibilityIdentityPath(target)
	if !canonicalContains(root, target) {
		return []string{root}
	}
	out := []string{root}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." {
		return out
	}
	current := root
	for part := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		current = compatibilityIdentityPath(filepath.Join(current, part))
		out = append(out, current)
	}
	return out
}

func (o *Owner) pathTreeSlots(specs []pathSpec) []string {
	seen := map[string]bool{}
	var slots []string
	for _, spec := range specs {
		for _, identity := range pathChain(o.canonical, spec.key) {
			slot := o.treeLockPath(identity)
			if seen[slot] {
				continue
			}
			seen[slot] = true
			slots = append(slots, slot)
		}
	}
	sort.Strings(slots)
	return slots
}

func pathChain(root, target string) []string {
	root, target = normalizeIdentityPath(root), normalizeIdentityPath(target)
	if !canonicalContains(root, target) {
		return []string{root}
	}
	out := []string{root}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." {
		return out
	}
	current := root
	for part := range strings.SplitSeq(filepath.ToSlash(rel), "/") {
		if part == "" || part == "." {
			continue
		}
		current = normalizeIdentityPath(filepath.Join(current, part))
		out = append(out, current)
	}
	return out
}

func canonicalFileKey(abs string) (key, display string, err error) {
	abs, display, err = canonicalFilePath(abs)
	if err != nil {
		return "", "", err
	}
	return normalizeIdentityPath(abs), display, nil
}

func canonicalFilePath(abs string) (canonical, display string, err error) {
	abs = strings.TrimSpace(abs)
	if abs == "" {
		return "", "", errors.New("path is empty")
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", "", err
	}
	abs = filepath.Clean(abs)
	cur, tail := abs, ""
	for {
		if resolved, resolveErr := filepath.EvalSymlinks(cur); resolveErr == nil {
			abs = filepath.Join(resolved, tail)
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
	display = filepath.Base(abs)
	return compatibilityIdentityPath(abs), display, nil
}

func canonicalContains(root, path string) bool {
	root, path = normalizeIdentityPath(root), normalizeIdentityPath(path)
	if root == "" || path == "" {
		return false
	}
	if root == path {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// LeaseStatesOverlap reports whether a waiting process-local lease can be held
// by the candidate. File keys are authoritative even when the two tabs opened
// different, overlapping workspace roots; workspace scopes are matched against
// the concrete keys when they are available.
func LeaseStatesOverlap(waitingRoot string, waiting State, holderRoot string, holder State) bool {
	holderScope := holder.HeldScope
	if holderScope == "" {
		holderScope = holder.Scope
	}
	waitingKeys, holderKeys := waiting.WaitingKeys, holder.HeldKeys
	if waiting.Scope == "workspace" {
		if holderScope == "workspace" || len(holderKeys) == 0 {
			return workspaceRootsOverlap(waitingRoot, holderRoot)
		}
		return anyKeyWithin(waitingRoot, holderKeys)
	}
	if holderScope == "workspace" {
		if len(waitingKeys) == 0 {
			return workspaceRootsOverlap(waitingRoot, holderRoot)
		}
		return anyKeyWithin(holderRoot, waitingKeys)
	}
	if len(waitingKeys) > 0 && len(holderKeys) > 0 {
		return keysIntersect(waitingKeys, holderKeys)
	}
	// Older process-local reporters do not carry keys. Root containment keeps
	// their conservative behavior for overlapping workspaces.
	return workspaceRootsOverlap(waitingRoot, holderRoot)
}

func keysIntersect(left, right []string) bool {
	seen := make(map[string]bool, len(left))
	for _, key := range left {
		if key != "" {
			seen[key] = true
		}
	}
	for _, key := range right {
		if key != "" && seen[key] {
			return true
		}
	}
	return false
}

func anyKeyWithin(root string, keys []string) bool {
	for _, key := range keys {
		if canonicalContains(root, key) {
			return true
		}
	}
	return false
}

func workspaceRootsOverlap(left, right string) bool {
	return canonicalContains(left, right) || canonicalContains(right, left)
}
