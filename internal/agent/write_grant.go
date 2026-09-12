// write_grant.go — what one delegated run may write, over the whole run.
package agent

import (
	"errors"
	"slices"
	"sync"
)

// ErrWriteFenceClosed reports a path the run may not write because widening
// its fence onto that path was not granted. A sentinel because the caller has
// somewhere to go with it — ask for a different path, or report what it needed
// — which "outside the declared write_paths" never gave it.
var ErrWriteFenceClosed = errors.New("write path is outside this run's granted paths")

// ErrWritePathBusy reports a path this run may write but another run is writing
// right now. Distinct from a closed fence because the answer is different: this
// one is worth coming back to.
var ErrWritePathBusy = errors.New("write path is in use by another run")

// WriteGrant is one delegated run's write scope: what it declared before
// starting, plus what a user granted while it ran. The two stay apart because
// only the declared set can be proven non-overlapping up front — nothing added
// later can join a proof against runs that already started. Their union is the
// answer to "was this run allowed here", which is what the audit compares to.
type WriteGrant struct {
	mu       sync.Mutex
	declared WritePathSet
	granted  []string
}

// NewWriteGrant opens a grant over the paths a run declared up front.
func NewWriteGrant(declared WritePathSet) *WriteGrant {
	return &WriteGrant{declared: declared}
}

// Declared returns the up-front claim, which is what scheduling reads. A path
// granted mid-run is deliberately absent: it was cleared against the runs that
// were live at the time, not against the ones that had yet to start.
func (g *WriteGrant) Declared() WritePathSet {
	if g == nil {
		return WritePathSet{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.declared
}

// Allows reports whether target is already inside this grant. It asks the same
// question the audit does, through the same resolution: a path compared as a
// string matches neither the symlink it arrived as nor the one it resolves to.
func (g *WriteGrant) Allows(target string) bool {
	return g.Scope().AllowsPath(target)
}

// Add records paths a user has granted. Callers must have obtained that answer
// and cleared the path against live claims first; this only writes it down.
func (g *WriteGrant) Add(paths ...string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range paths {
		// Stored resolved, like a declared path: the set treats each entry as a
		// root to test paths against, and a root that is still a symlink or a
		// traversal matches nothing that arrives resolved.
		abs, err := realPathForClaim(p)
		if err != nil {
			continue
		}
		if !g.declared.AllowsPath(abs) && !slices.Contains(g.granted, abs) {
			g.granted = append(g.granted, abs)
		}
	}
}

// Scope is everything this run was allowed to write. The audit compares
// observed mutations against this, so a path the user granted does not surface
// as a write that escaped its claim.
func (g *WriteGrant) Scope() WritePathSet {
	if g == nil {
		return WritePathSet{}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.granted) == 0 {
		return g.declared
	}
	out := g.declared
	out.Paths = append(append([]string(nil), g.declared.Paths...), g.granted...)
	return out
}

// Granted returns the paths taken on after the run started, in the order the
// user allowed them. Empty for a run that stayed inside what it declared.
func (g *WriteGrant) Granted() []string {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.granted...)
}
