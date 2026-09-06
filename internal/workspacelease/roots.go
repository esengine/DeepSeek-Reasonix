package workspacelease

import (
	"context"
	"sort"
	"strings"
	"sync"

	"reasonix/internal/filelock"
)

type rootLockDomain struct {
	path string
	mode filelock.Mode
}

// HoldWriteRoots acquires one exclusive hold spanning several workspace roots.
// It preserves the legacy ancestor locks and coalesces tree-stripe collisions
// before acquisition, so a group cannot wait for a stripe it already owns.
// Callers must not hold separate leases for these roots while acquiring a group.
func HoldWriteRoots(ctx context.Context, lockDir string, roots ...string) (func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	owner, domains, err := rootLockDomains(lockDir, roots)
	if err != nil {
		return nil, err
	}
	releases := make([]func(), 0, len(domains))
	notified := false
	for _, domain := range domains {
		if err := ctx.Err(); err != nil {
			runReleases(releases)
			return nil, err
		}
		release, err := owner.acquireQueuedMode(ctx, domain.path, domain.mode, &notified)
		if err != nil {
			runReleases(releases)
			return nil, err
		}
		releases = append(releases, release)
	}
	if err := ctx.Err(); err != nil {
		runReleases(releases)
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { runReleases(releases) }) }, nil
}

func rootLockDomains(lockDir string, roots []string) (*Owner, []rootLockDomain, error) {
	lockDir = strings.TrimSpace(lockDir)
	var coordinator *Owner
	var compatibilityRoots []string
	exclusive := map[string]bool{}
	trees := map[string]bool{}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		owner, err := New(root, lockDir, nil)
		if err != nil {
			return nil, nil, err
		}
		if coordinator == nil {
			coordinator = owner
		}
		exclusive[owner.canonical] = true
		compatibilityRoots = append(compatibilityRoots, ancestorDirectories(owner.canonical)...)
		compatibilityRoots = append(compatibilityRoots, ancestorDirectories(owner.compatibility)...)
		trees[owner.treeLockPath(owner.canonical)] = true
	}
	// Match the single-root protocol: all compatibility ancestors first, then
	// tree stripes. Promote an ancestor requested by this group to exclusive.
	var domains []rootLockDomain
	for _, root := range orderedWorkspaceRoots(compatibilityRoots) {
		mode := filelock.ModeShared
		if exclusive[normalizeIdentityPath(root)] {
			mode = filelock.ModeExclusive
		}
		domains = append(domains, rootLockDomain{workspaceLockPath(lockDir, root), mode})
	}
	paths := make([]string, 0, len(trees))
	for path := range trees {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		domains = append(domains, rootLockDomain{path, filelock.ModeExclusive})
	}
	return coordinator, domains, nil
}
