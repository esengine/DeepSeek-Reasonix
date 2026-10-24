package agent

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/base/hostwrite"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/sessiontemp"
)

// outsideObservedWrites names the files this turn wrote where nothing the host
// watches can speak for them. A write there is neither a probe nor the work
// product: the host saw the call succeed and can say nothing about what it left.
func (a *Agent) outsideObservedWrites() []string {
	if a == nil || a.task.ledger == nil || a.writeWorkspaceRoot == "" {
		return nil
	}
	var outside []string
	for _, r := range a.task.ledger.Receipts() {
		if !r.Success || !(r.Write || r.Mutation) {
			continue
		}
		for _, p := range r.Paths {
			if p == "" || slices.Contains(r.HostOwned, p) || a.pathInWorkspace(p) || underScratchOrCache(p) {
				continue
			}
			if !slices.Contains(outside, p) {
				outside = append(outside, p)
			}
		}
	}
	return a.relativeUnique(outside)
}

// underScratchOrCache reports a path the host disposes of on its own: inside the
// directory this session was handed for temporary work, or inside a toolchain
// cache. A shared temporary root is neither — a person who asked for a file
// there asked for something to keep, and nothing the host watches says it
// arrived.
func underScratchOrCache(path string) bool {
	return disposable(path, os.TempDir(), hostwrite.Caches())
}

// disposable is the rule above with the host's roots passed in, so its edges are
// decidable without an environment that happens to sit where the rule looks.
func disposable(path, tempRoot string, caches []string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	target := filepath.Clean(path)
	if insideSessionScratch(target, tempRoot) {
		return true
	}
	for _, dir := range caches {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		clean := filepath.Clean(dir)
		if !writeclaim.PathWithin(clean, target) {
			continue
		}
		// An executable installed under a cache is the user's, not the cache's:
		// `go install` lands in ~/go/bin, which is what the turn was asked for.
		if rel, err := filepath.Rel(clean, target); err == nil && strings.HasPrefix(filepath.ToSlash(rel), "bin/") {
			return false
		}
		return true
	}
	return false
}

// insideSessionScratch reports a path under a directory sessiontemp creates:
// directly beneath the temporary root, named with the scratch prefix. Only the
// first component below that root is read, so a folder deeper in the tree cannot
// earn an exemption by spelling the prefix.
func insideSessionScratch(path, tempRoot string) bool {
	if tempRoot == "" {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(tempRoot), path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	return strings.HasPrefix(first, sessiontemp.Prefix())
}
