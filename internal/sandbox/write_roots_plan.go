//go:build !windows

package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// WriteRootRedirectedCode identifies a writable directory a launch left out
// because it resolves through a link inside a writable directory, where a
// confined command can re-point it.
const WriteRootRedirectedCode = "sandbox.write_root_redirected"

type writeCandidate struct {
	key      string
	resolved string
	links    []string
	caller   bool
}

// writeRootPlan is the write surface one launch confines to: resolved paths
// in the order they were named, the caller's among them, and what it refused.
type writeRootPlan struct {
	dirs    []string
	callers []string
	refused []string
}

// planWriteRoots resolves the caller's roots and the backend's extra
// directories without following any link a confined command could have
// rewritten. A backend names the resolved paths, so a later swap cannot move
// a granted root.
func planWriteRoots(roots, extras []string, sessionTemp string) writeRootPlan {
	var cands []writeCandidate
	seen := map[string]bool{}
	add := func(named string, caller bool) {
		named = strings.TrimSpace(named)
		if named == "" {
			return
		}
		abs, err := filepath.Abs(named)
		if err != nil || seen[abs] {
			return
		}
		seen[abs] = true
		resolved, links := resolveWithLinks(abs)
		cands = append(cands, writeCandidate{key: abs, resolved: resolved, links: links, caller: caller})
	}
	for _, r := range roots {
		add(r, true)
	}
	for _, r := range extras {
		add(r, false)
	}
	regions := hostWritePins().paths()
	if dir := strings.TrimSpace(sessionTemp); dir != "" {
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
		if len(c.links) > 0 && !c.redirected(regions) {
			regions = append(regions, c.resolved)
		}
	}
	var plan writeRootPlan
	kept := map[string]bool{}
	for _, c := range cands {
		if c.redirected(regions) {
			plan.refused = append(plan.refused, c.resolved)
			continue
		}
		if c.caller {
			plan.callers = append(plan.callers, c.resolved)
		}
		if !kept[foldPath(c.resolved)] {
			kept[foldPath(c.resolved)] = true
			plan.dirs = append(plan.dirs, c.resolved)
		}
	}
	return plan
}

// redirected reports whether c is not what it was, or resolves through a link
// a writable directory's own entry could be, or one inside it.
func (c writeCandidate) redirected(regions []string) bool {
	for _, link := range c.links {
		for _, region := range regions {
			if PathWithin(foldPath(region), foldPath(link)) {
				return true
			}
		}
	}
	return hostWritePins().changed(c.key, c.resolved)
}

// hostPins holds each host write directory's identity when first seen.
type hostPins map[string]hostIdentity

type hostIdentity struct {
	path string
	info os.FileInfo // nil when the directory did not exist
}

// hostWritePins pins the host write directories the first time this process
// confines a launch. A Seatbelt subpath covers the directory's own entry, so a
// confined command could otherwise leave a link where a cache was. Only the
// path is held: a cache rebuilt in place is the same grant.
var hostWritePins = pinHostWriteDirs

var pinHostWriteDirs = sync.OnceValue(func() hostPins {
	pins := hostPins{}
	for _, d := range append([]string{"/dev"}, hostWriteDirs()...) {
		abs, err := filepath.Abs(strings.TrimSpace(d))
		if err != nil || d == "" {
			continue
		}
		resolved, _ := resolveWithLinks(abs)
		pins[abs] = hostIdentity{path: resolved}
	}
	return pins
})

func (p hostPins) paths() []string {
	out := make([]string, 0, len(p))
	for _, id := range p {
		out = append(out, id.path)
	}
	return out
}

func (p hostPins) changed(key, resolved string) bool {
	id, ok := p[key]
	if !ok {
		return false
	}
	if foldPath(id.path) != foldPath(resolved) {
		return true
	}
	if id.info == nil {
		return false
	}
	info, err := os.Stat(resolved)
	return err != nil || !os.SameFile(id.info, info)
}

func foldPath(p string) string {
	if runtime.GOOS == "darwin" {
		return strings.ToLower(p)
	}
	return p
}
