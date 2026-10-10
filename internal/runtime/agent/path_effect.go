package agent

import (
	"context"
	"os"
	"path/filepath"
	"reasonix/internal/runtime/writeclaim"
	"slices"
	"strings"

	"reasonix/internal/base/diff"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/checkpoint"
)

// A command the host cannot decompose statically — `rm x && ls`, a project
// script — is unknown, which is honest but final: the ledger then carries a
// mutation with no paths. Watching the call closes that gap without teaching
// the host any command's semantics. Only watched paths can be reported, so a
// call that changed something else stays unknown, never a wrong "nothing".
const observedPathLimit = 32

type pathState struct {
	exists  bool
	size    int64
	modTime int64
	mode    os.FileMode
}

// pathSnapshot is what the workspace looked like before a call. root is kept so
// the comparison can also find entries that did not exist to be watched — a
// build artifact appears under a name no tool ever passed to the host.
type pathSnapshot struct {
	root  string
	state map[string]watchedPath // keyed by evidence.NormalizePath
}

// watchedPath keeps the spelling the call used beside the state, because the map
// is keyed by the ledger's path identity: the same file reaches here from the
// ledger and from a directory read, and on Windows those differ in case only.
type watchedPath struct {
	path string
	pathState
}

func (before pathSnapshot) empty() bool { return len(before.state) == 0 && before.root == "" }

// snapshotPaths records the call's own targets, the paths this turn has already
// read or written, and the workspace's top level. Take it as the last step
// before the call runs, so hooks and approvals that touch the workspace stay
// outside what the call is credited with.
func snapshotPaths(ledger *evidence.Ledger, root string, targets []string) pathSnapshot {
	root = strings.TrimSpace(root)
	paths := append([]string(nil), targets...)
	paths = append(paths, ledger.TouchedPaths(observedPathLimit, false)...)
	paths = append(paths, workspaceTopLevel(root)...)
	paths = slices.DeleteFunc(paths, func(p string) bool { return !watchablePath(p, root) })
	if len(paths) == 0 && root == "" {
		return pathSnapshot{}
	}
	snap := pathSnapshot{root: root, state: make(map[string]watchedPath, len(paths))}
	for _, p := range paths {
		key := evidence.NormalizePath(p)
		if key == "" {
			continue
		}
		if _, seen := snap.state[key]; seen {
			continue
		}
		snap.state[key] = watchedPath{path: p, pathState: statePathOf(p, root)}
	}
	return snap
}

// workspaceTopLevel lists the workspace's immediate entries — one directory
// read, never recursive, so a repository with a large tree costs the same as an
// empty one.
func workspaceTopLevel(root string) []string {
	if root == "" {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) > observedPathLimit*4 {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, filepath.Join(root, e.Name()))
	}
	return out
}

func statePathOf(path, root string) pathState {
	if !watchablePath(path, root) {
		return pathState{}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return pathState{}
	}
	return pathState{exists: true, size: info.Size(), modTime: info.ModTime().UnixNano(), mode: info.Mode()}
}

// since compares the snapshot against the workspace as it is now, returning
// what the call changed and, of those, what it brought into existence.
func (before pathSnapshot) since() (affected, created []string) {
	for _, was := range before.state {
		if !watchablePath(was.path, before.root) {
			continue
		}
		now := statePathOf(was.path, before.root)
		if now == was.pathState {
			continue
		}
		affected = append(affected, was.path)
		if !was.exists && now.exists {
			created = append(created, was.path)
		}
	}
	for _, path := range workspaceTopLevel(before.root) {
		if _, watched := before.state[evidence.NormalizePath(path)]; watched {
			continue
		}
		affected = append(affected, path)
		created = append(created, path)
	}
	slices.Sort(affected)
	slices.Sort(created)
	return affected, created
}

// decorateObservedPaths records what a call demonstrably did. Classification is
// only ever sharpened: a call observed to change nothing stays unknown, since
// it may have changed something outside the watched set.
func decorateObservedPaths(rec *evidence.Receipt, plan *toolCallPlan) {
	if rec == nil || plan == nil {
		return
	}
	if rec.Success {
		rec.CriteriaRewritten = plan.criteriaRewritten
		for _, path := range plan.declaredPaths {
			if !holdsPath(rec.Paths, path) {
				rec.Paths = append(rec.Paths, path)
			}
		}
	}
	if plan.pathsBefore.empty() || !rec.Success {
		return
	}
	affected, created := plan.pathsBefore.since()
	rec.Created = created
	if rec.MutationEvidence != evidence.MutationUnknown || len(affected) == 0 {
		return
	}
	rec.MutationEvidence = evidence.MutationProven
	for _, path := range affected {
		if !holdsPath(rec.Paths, path) {
			rec.Paths = append(rec.Paths, path)
		}
	}
}

// holdsPath asks whether the receipt already names this file, by the ledger's
// identity rather than by spelling — otherwise a receipt lists one Windows file
// twice and every count downstream reads one change as two.
func holdsPath(paths []string, want string) bool {
	key := evidence.NormalizePath(want)
	for _, p := range paths {
		if evidence.NormalizePath(p) == key {
			return true
		}
	}
	return false
}

// leftSomethingBehind reports whether a change still has anything on disk to
// verify. A path that is gone counts as cleaned up only if this turn created
// it: removing a file the turn made leaves the workspace as found, removing one
// it did not is the change. An unresolvable path looks exactly like a deleted
// one, so anything never watched appear is assumed to have survived.
func leftSomethingBehind(ledger *evidence.Ledger, r evidence.Receipt, root string) bool {
	if len(r.Paths) == 0 {
		return true
	}
	for _, path := range r.Paths {
		if statePathOf(path, root).exists || !ledger.CreatedInTurn(path) {
			return true
		}
	}
	return false
}

// touchedTheWorkspace reports whether a change landed in the work product. A
// probe written under $TMPDIR is outside it by construction, so a turn whose
// only write was a scratch file owes no verification of the workspace — the
// same reasoning the memory-write case already carries, read off the path.
func (a *Agent) touchedTheWorkspace(r evidence.Receipt) bool {
	if a.writeWorkspaceRoot == "" || len(r.Paths) == 0 {
		return true
	}
	return slices.ContainsFunc(r.Paths, a.pathInWorkspace)
}

// pathInWorkspace reports whether one path is part of the work product. A
// relative path is resolved against the workspace by every file tool, so it is
// inside by construction; only an absolute one can leave.
func (a *Agent) pathInWorkspace(path string) bool {
	root := a.writeWorkspaceRoot
	if root == "" {
		return true
	}
	if filepath.IsAbs(path) && !writeclaim.PathWithin(root, path) {
		return false
	}
	return !hostOwnedPath(root, path)
}

// mutationBaseline is what the turn's remaining obligations are measured from:
// the latest change still on disk. A turn whose only writes were scratch files
// and build artifacts it cleaned up has no baseline, and owes no verification
// of changes it kept none of.
func (a *Agent) mutationBaseline(delivery bool) (int, bool) {
	ledger := a.task.ledger
	survives := func(r evidence.Receipt) bool {
		return a.touchedTheWorkspace(r) && leftSomethingBehind(ledger, r, a.writeWorkspaceRoot)
	}
	if delivery {
		return ledger.LatestProvenMutationIndexFunc(survives)
	}
	return ledger.LatestSuccessfulWriterIndexFunc(survives)
}

// observeBeforeMutation captures preimages for Previewable writers and records
// explicit coverage gaps for bash / opaque MCP tools. Host-internal only.
func (a *Agent) observeBeforeMutation(ctx context.Context, plan *toolCallPlan) {
	if a == nil || plan == nil {
		return
	}
	toolName := plan.evidenceName
	if toolName == "" {
		toolName = plan.call.Name
	}
	// One preview serves every reader below. Which check the edit rewrites is
	// a property of the edit itself, so it is read here rather than inside any
	// one observer's branch — a build with no mutation observer still may not
	// move its own criteria silently.
	var change diff.Change
	if pv, ok := plan.execTool.(tool.Previewer); ok {
		if c, perr := pv.Preview(ctx, plan.execArgs); perr == nil {
			change = c
			plan.criteriaRewritten = evidence.RewrittenTestCriteria(c.Path, c.OldText, c.NewText)
			a.captureRewrittenCriteria(c, plan.criteriaRewritten)
		}
	}
	obs := a.svc.mutationObserver
	if obs != nil {
		if change.Path != "" {
			obs.BeforeMutationFromChange(change, toolName)
			plan.mutationPath = change.Path
			plan.mutationWitness = evidence.ChangedLines(change.OldText, change.NewText)
			return
		}
		if a.observeDeclaredPaths(plan, toolName) {
			return
		}
		// Non-previewable writers: record a coverage gap (do not guess paths).
		switch toolName {
		case "bash":
			obs.RecordGap(checkpoint.CoverageGap{Reason: checkpoint.GapBashSideEffect, Tool: toolName, Detail: "bash side effects are not path-tracked"})
		default:
			// MCP or other writers without Previewer.
			if !plan.readOnly {
				obs.RecordGap(checkpoint.CoverageGap{Reason: checkpoint.GapMCPExternal, Tool: toolName, Detail: "tool cannot describe local write paths"})
			}
		}
		return
	}
	// Legacy onPreEdit path.
	if a.svc.preEdit != nil && change.Path != "" {
		a.svc.preEdit(change)
		plan.mutationPath = change.Path
	}
}

// observeAfterMutation records the after fingerprint when a concrete path was
// known before execution, regardless of tool success or failure.
func (a *Agent) observeAfterMutation(plan *toolCallPlan) {
	if a == nil || plan == nil || a.svc.mutationObserver == nil {
		return
	}
	toolName := plan.evidenceName
	if toolName == "" {
		toolName = plan.call.Name
	}
	if plan.mutationPath != "" {
		a.svc.mutationObserver.AfterMutation(plan.mutationPath, toolName)
	}
	for _, p := range plan.declaredPaths {
		a.svc.mutationObserver.AfterMutation(p, toolName)
	}
}

// workspaceScanLimit bounds the walk. Past it the scan reports itself
// incomplete and nothing is concluded from it, so a very large workspace keeps
// the behaviour it has today instead of paying for an answer it cannot trust.
const workspaceScanLimit = 50_000

// scanCancelCheckEvery bounds how much walking a stop waits on. Checking every
// entry would put an atomic load in the hot loop for a wait nobody notices.
const scanCancelCheckEvery = 512

// workspaceScan is every file under the workspace at one moment. complete is
// false when the walk was cut short, which is the only honest answer a partial
// scan can give: "changed nothing" cannot be read off a tree half-looked-at.
type workspaceScan struct {
	state    map[string]pathState
	complete bool
	// overLimit separates the one incompleteness that will not change on the
	// next call: a workspace with more files than the walk may hold is that
	// size again, while a vanished file or a denied directory is not.
	overLimit bool
}

// scanWorkspace walks the whole tree rather than an ignore-filtered part of it:
// npm install and a build write exactly where ignore rules point away, so a
// filtered walk would report the biggest writes there are as no change at all.
func scanWorkspace(ctx context.Context, root string) workspaceScan {
	return scanWorkspaceTo(ctx, root, workspaceScanLimit)
}

// scanLimit is the walk bound this agent runs under: its configured limit,
// never above workspaceScanLimit, which is also the default.
func (a *Agent) scanLimit() int {
	if a.workspaceScanLimit > 0 {
		return min(a.workspaceScanLimit, workspaceScanLimit)
	}
	return workspaceScanLimit
}

// unchanged reports whether the workspace is byte-for-byte as this scan found
// it. Both scans must be complete; either one short of that proves nothing.
func (before workspaceScan) unchanged(after workspaceScan) bool {
	if !before.complete || !after.complete || len(before.state) != len(after.state) {
		return false
	}
	for path, was := range before.state {
		if now, ok := after.state[path]; !ok || now != was {
			return false
		}
	}
	return true
}

// changed names every file that differs between the two scans. ok is false when
// either scan fell short, because a walk that stopped cannot say what it missed
// — and a partial answer here would be scope the host never established.
func (before workspaceScan) changed(after workspaceScan) (paths []string, ok bool) {
	if !before.complete || !after.complete {
		return nil, false
	}
	for path, was := range before.state {
		if now, seen := after.state[path]; !seen || now != was {
			paths = append(paths, path)
		}
	}
	for path := range after.state {
		if _, seen := before.state[path]; !seen {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths, true
}

// settleUnchangedWorkspace answers by observation what no reading of a command
// can: whether it changed anything. A call the host could not classify — sed
// through its own script language, a wrapper, a path built from a variable —
// stays a mutation unless the workspace it ran against is exactly as it was.
func (a *Agent) settleUnchangedWorkspace(ctx context.Context, rec *evidence.Receipt, plan *toolCallPlan) {
	if rec == nil || plan == nil || !rec.Success {
		return
	}
	// The watched paths may already have proven a change, but only the walk can
	// say those were all of it.
	watchedProof := rec.MutationEvidence == evidence.MutationProven
	if rec.MutationEvidence != evidence.MutationUnknown && !watchedProof {
		return
	}
	// Nothing to compare against is not a reason to walk: a call that took no
	// before-scan can only reach changed() to be told so, and that walk costs
	// what one that answers costs.
	if !plan.scanBefore.complete || !a.mayAttributeObserved(ctx) {
		return
	}
	after := scanWorkspaceTo(ctx, a.observeRoot, a.scanLimit())
	changed, ok := plan.scanBefore.changed(after)
	if !ok {
		return
	}
	whole := true
	if scope := scopedGrant(ctx); scope != nil && len(changed) > 0 {
		// Inside a run's own declared paths a change is taken as that run's:
		// the claims are proven disjoint up front and writer tools are fenced
		// to them. Outside them it is someone else's, so it says nothing here.
		changed = slices.DeleteFunc(changed, func(p string) bool { return !scope.Allows(p) })
		whole = false
		if len(changed) == 0 {
			return
		}
	}
	if len(changed) == 0 {
		if !watchedProof {
			rec.Mutation = false
			rec.MutationEvidence = ""
		}
		return
	}
	// The walk answered what the command would not: these files and no others.
	// That is scope established by observation, which is the only thing that
	// can establish it — a check run afterwards speaks for state, not effect.
	rec.MutationEvidence = evidence.MutationProven
	for _, path := range changed[:min(len(changed), observedPathLimit)] {
		if !holdsPath(rec.Paths, path) {
			rec.Paths = append(rec.Paths, path)
		}
		if _, existed := plan.scanBefore.state[path]; !existed && !holdsPath(rec.Created, path) {
			rec.Created = append(rec.Created, path)
		}
	}
	rec.PathsComplete = whole && len(changed) < observedPathLimit
}

// scanBeforeUnprovenCall takes the whole-workspace scan only for a call the
// host could not classify. A proven writer already reports its paths, and a
// proven reader has nothing to settle, so neither pays for the walk.
func (a *Agent) scanBeforeUnprovenCall(ctx context.Context, plan *toolCallPlan) workspaceScan {
	if evidence.ToolCallMutationClassWith(plan.evidenceName, plan.evidenceArgs, plan.facts()) != evidence.MutationUnknown {
		return workspaceScan{}
	}
	// A backgrounded job's receipt is written when it starts, so the workspace
	// it will write to still looks untouched. It never settles.
	if isBackgroundTaskCall(string(plan.evidenceArgs)) {
		return workspaceScan{}
	}
	// A workspace already found past the limit is past it again, and the walk
	// that says so settles nothing -- the whole cost, on exactly the trees
	// where it buys least.
	if a.task.workspaceOverScanLimit() {
		return workspaceScan{}
	}
	if !a.mayAttributeObserved(ctx) {
		return workspaceScan{}
	}
	scan := scanWorkspaceTo(ctx, a.observeRoot, a.scanLimit())
	if scan.overLimit {
		a.task.noteWorkspaceOverScanLimit()
	}
	return scan
}

// mayAttributeObserved says whether what a walk sees change can be charged to
// this agent's call: when it holds a declared write scope to filter by, or
// when no other background writer is running beside it.
func (a *Agent) mayAttributeObserved(ctx context.Context) bool {
	return scopedGrant(ctx) != nil || !a.svc.mutationObserver.HasOtherActiveWriters()
}

// scopedGrant is the running child's write grant when it names paths; a
// whole-workspace grant filters nothing, so it is not one.
func scopedGrant(ctx context.Context) *writeclaim.WriteGrant {
	g := writeclaim.SubagentWriteGrant(ctx)
	if g == nil || g.Declared().WholeWorkspace || g.Scope().Empty() {
		return nil
	}
	return g
}
