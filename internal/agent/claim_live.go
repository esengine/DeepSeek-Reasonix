package agent

// liveClaim is one active writer slot: the declared capability bound plus the
// paths actually reserved at runtime.
type liveClaim struct {
	id       int64
	writer   bool
	declared WritePathSet
	realized []string
	opaque   bool
	// callerParentClaim is the parent claim this claim runs inside, which it
	// does not conflict with.
	callerParentClaim int64
}

func (c liveClaim) reservation() WritePathSet {
	if c.opaque {
		return wholeReservation(c.declared.WorkspaceRoot)
	}
	if len(c.realized) > 0 {
		return fileReservation(c.declared.WorkspaceRoot, c.realized)
	}
	if c.declared.WholeWorkspace {
		return c.declared
	}
	if c.dirOnlyDeclared() {
		return WritePathSet{}
	}
	return c.declared
}

func (c liveClaim) dirOnlyDeclared() bool {
	if c.declared.WholeWorkspace || len(c.declared.Paths) == 0 {
		return false
	}
	for i := range c.declared.Paths {
		if c.declared.kindAt(i) != pathKindDir {
			return false
		}
	}
	return true
}

func wholeReservation(root string) WritePathSet {
	return WritePathSet{WholeWorkspace: true, WorkspaceRoot: root}
}

func fileReservation(root string, paths []string) WritePathSet {
	out := WritePathSet{WorkspaceRoot: root, Paths: append([]string(nil), paths...)}
	out.Kinds = make([]pathKind, len(paths))
	return out
}

func mergeRealized(existing []string, add WritePathSet) []string {
	capacity := max(len(existing), len(add.Paths))
	seen := make(map[string]bool, capacity)
	out := make([]string, 0, capacity)
	for _, p := range existing {
		key := foldPathKey(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	for _, p := range add.Paths {
		key := foldPathKey(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

// canStartIncomingLocked keeps a queued whole-workspace writer ahead of later
// writers. Directory claims have an empty reservation before their first write,
// so canStartLocked alone would otherwise let a steady stream bypass it.
func (s *SubagentScheduler) canStartIncomingLocked(req AcquireRequest) (bool, string) {
	if req.Writer && !s.holdsParentClaimLocked(req.callerParentClaim) {
		for _, waiter := range s.waiters {
			if waiter.req.Writer && waiter.req.WritePaths.WholeWorkspace {
				return false, "queued whole-workspace writer has priority"
			}
		}
	}
	return s.canStartLocked(req)
}

// parentWriteClaim is a write claim a depth-0 tool call holds for its Execute.
// Work that call delegates runs inside it: every queued writer that overlaps is
// blocked by the claim until Execute returns, so the delegate may not wait on
// the claim or queue behind such a writer.
type parentWriteClaim struct {
	id    int64
	paths WritePathSet
}

func (s *SubagentScheduler) holdsParentClaimLocked(id int64) bool {
	if id == 0 {
		return false
	}
	for _, parent := range s.parentClaims {
		if parent.id == id {
			return true
		}
	}
	return false
}
