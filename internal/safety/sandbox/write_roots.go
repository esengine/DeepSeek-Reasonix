package sandbox

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/base/hostwrite"
)

// Write-root drop codes: the identity of why a root a caller named was left
// out of a launch's writable set.
const (
	// WriteRootChangedCode: the root no longer is the directory the session
	// pinned (replaced, re-pointed or removed).
	WriteRootChangedCode = "sandbox.write_root_changed"
	// WriteRootRedirectedCode: the root resolves through a link that sits
	// where a confined command can write, so the command chooses its target.
	WriteRootRedirectedCode = "sandbox.write_root_redirected"
)

// WriteRootDrop is one root a launch did not make writable, and why.
type WriteRootDrop struct {
	Root string
	Code string
}

// WriteRootPins holds each configured write root's identity as the session
// established it. It is immutable once built, so specs copied across tools
// and sub-agents share one without locking.
type WriteRootPins struct {
	roots map[string]rootIdentity
}

type rootIdentity struct {
	path string      // symlink-resolved at pin time
	info os.FileInfo // nil when the root did not exist yet
}

// PinWriteRoots records the identity each root has now: its symlink-resolved
// path and, where it exists, the file it names.
func PinWriteRoots(roots []string) *WriteRootPins {
	return (*WriteRootPins)(nil).With(roots)
}

// With is p plus the identity each of roots has now; roots p already holds
// keep the identity p recorded.
func (p *WriteRootPins) With(roots []string) *WriteRootPins {
	out := &WriteRootPins{roots: map[string]rootIdentity{}}
	if p != nil {
		maps.Copy(out.roots, p.roots)
	}
	p = out
	for _, r := range roots {
		key, ok := rootKey(r)
		if !ok {
			continue
		}
		if _, dup := p.roots[key]; dup {
			continue
		}
		resolved, _ := resolveWithLinks(key)
		id := rootIdentity{path: resolved}
		if info, err := os.Stat(resolved); err == nil {
			id.info = info
		}
		p.roots[key] = id
	}
	return p
}

// hostWritePins holds the host directories every non-minimal launch may write
// (temp, toolchain caches), pinned the first time this process confines one.
// A confined command may replace such a directory's own entry, which a
// Seatbelt subpath covers, so its target has to stay the one pinned here. Only
// the path is held: a cache rebuilt in place is the same grant.
var hostWritePins = pinHostWriteDirs

var pinHostWriteDirs = sync.OnceValue(func() *WriteRootPins {
	p := PinWriteRoots(append([]string{"/dev"}, hostwrite.Dirs()...))
	for key, id := range p.roots {
		p.roots[key] = rootIdentity{path: id.path}
	}
	return p
})

func (p *WriteRootPins) lookup(key string) (rootIdentity, bool) {
	if p == nil {
		return rootIdentity{}, false
	}
	id, ok := p.roots[key]
	return id, ok
}

func (p *WriteRootPins) paths() []string {
	if p == nil {
		return nil
	}
	out := make([]string, 0, len(p.roots))
	for _, id := range p.roots {
		out = append(out, id.path)
	}
	return out
}

func rootKey(root string) (string, bool) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	return abs, true
}

// writeCandidate is one directory a backend would make writable.
type writeCandidate struct {
	key      string
	named    string
	resolved string
	links    []string
	exists   bool
	caller   bool
}

// writeRootPlan is the write surface one launch confines to: resolved paths
// in the order they were named, and the roots it refused.
type writeRootPlan struct {
	dirs    []string
	callers []string // the caller's roots among dirs
	dropped []WriteRootDrop
}

// planWriteRoots resolves the caller's roots and the backend's extra
// directories without trusting any link a confined command could have
// rewritten: a root reached through a link inside a writable directory, or no
// longer the directory its pin recorded, is dropped. Resolved paths are what
// the profile names, so a later swap cannot move a granted root.
func planWriteRoots(spec Spec, extras []string) writeRootPlan {
	var cands []writeCandidate
	seen := map[string]bool{}
	add := func(named string, caller bool) {
		key, ok := rootKey(named)
		if !ok || seen[key] {
			return
		}
		seen[key] = true
		resolved, links := resolveWithLinks(key)
		_, err := os.Lstat(resolved)
		cands = append(cands, writeCandidate{key: key, named: named, resolved: resolved, links: links, exists: err == nil, caller: caller})
	}
	for _, r := range spec.WriteRoots {
		add(r, true)
	}
	for _, r := range extras {
		add(r, false)
	}
	pins := []*WriteRootPins{spec.Pins, hostWritePins()}
	regions := append(spec.Pins.paths(), pins[1].paths()...)
	if dir := strings.TrimSpace(spec.SessionTemp); dir != "" {
		resolved, _ := resolveWithLinks(dir)
		regions = append(regions, resolved)
	}
	for _, c := range cands {
		if len(c.links) == 0 {
			regions = append(regions, c.resolved)
		}
	}
	// A linked root widens the regions only once it passed against the
	// unlinked ones, so a root re-pointed at / cannot refuse every other root.
	for _, c := range cands {
		if len(c.links) > 0 && c.refusal(pins, regions) == "" {
			regions = append(regions, c.resolved)
		}
	}
	var plan writeRootPlan
	kept := map[string]bool{}
	for _, c := range cands {
		if code := c.refusal(pins, regions); code != "" {
			plan.dropped = append(plan.dropped, WriteRootDrop{Root: c.named, Code: code})
			continue
		}
		if c.caller {
			plan.callers = append(plan.callers, c.resolved)
		}
		if !kept[fold(c.resolved)] {
			kept[fold(c.resolved)] = true
			plan.dirs = append(plan.dirs, c.resolved)
		}
	}
	return plan
}

// refusal names why c may not be granted. A link a writable directory's own
// entry could be, or one inside it, is the confined command's to re-point.
func (c writeCandidate) refusal(pins []*WriteRootPins, regions []string) string {
	for _, link := range c.links {
		for _, region := range regions {
			if pathWithin(fold(region), fold(link)) {
				return WriteRootRedirectedCode
			}
		}
	}
	var id rootIdentity
	pinned := false
	for _, p := range pins {
		if id, pinned = p.lookup(c.key); pinned {
			break
		}
	}
	if !pinned {
		return ""
	}
	if fold(id.path) != fold(c.resolved) {
		return WriteRootChangedCode
	}
	if id.info == nil {
		return ""
	}
	info, err := os.Stat(c.resolved)
	if err != nil || !os.SameFile(id.info, info) {
		return WriteRootChangedCode
	}
	return ""
}

// DroppedWriteRoots lists the roots a launch under spec would not make
// writable, with the reason for each.
func DroppedWriteRoots(spec Spec) []WriteRootDrop {
	if !spec.Enforce() {
		return nil
	}
	return planWriteRoots(spec, backendWriteDirs(spec)).dropped
}
