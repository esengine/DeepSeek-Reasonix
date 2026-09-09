package control

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Fork branches the conversation at the start of turn into a NEW session file,
// preserving the current one as the branch point, and switches to the branch. Code
// is untouched (it's a conversation operation). Like a conversation rewind it needs
// the live boundary, so it is unavailable for resumed-session turns and refused
// while a turn runs. Returns the new session path.
func (c *Controller) Fork(turn int) (string, error) {
	return c.ForkNamed(turn, "")
}

func (c *Controller) ForkNamed(turn int, name string) (string, error) {
	return c.forkNamed(turn, name, true)
}

// ForkSession copies the conversation at the start of turn into a new session
// file without switching this controller to it. Desktop uses this to open the
// branch in a new tab while the source tab keeps its current transcript.
func (c *Controller) ForkSession(turn int, name string) (string, error) {
	return c.forkNamed(turn, name, false)
}

func (c *Controller) forkNamed(turn int, name string, switchToFork bool) (string, error) {
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return "", c.rewindFail(fmt.Errorf("cannot fork while a turn is running"))
		}
		return "", c.rewindFail(err)
	}
	defer c.endRotation()
	return c.forkNamedReady(turn, name, switchToFork, agent.HeadKindFork)
}

// forkNamedReady forks at turn's boundary: a schema-2 session that switches
// gets a new head in its own log (head id returned); every other case still
// creates a new session file (path returned).
func (c *Controller) forkNamedReady(turn int, name string, switchToFork bool, kind string) (string, error) {
	if c.executor == nil {
		return "", c.rewindFail(fmt.Errorf("checkpoints unavailable"))
	}
	if c.sessionDir == "" {
		return "", c.rewindFail(fmt.Errorf("fork needs session persistence, which is disabled"))
	}
	boundary, hasBound := c.checkpoints.boundary(turn)
	if !hasBound {
		return "", c.rewindFail(fmt.Errorf("fork unavailable for turn %d (resumed session)", turn))
	}
	if sess := c.headBranchSession(); sess != nil && switchToFork {
		return c.forkHeadReady(sess, turn, boundary, name, kind)
	}

	// Persist the current conversation first so the branch point survives, then
	// seed a fresh session with the messages up to the fork and switch to it.
	if err := c.Snapshot(); err != nil {
		slog.Warn("controller: pre-fork snapshot", "err", err)
	}
	parentPath := c.SessionPath()
	parentID := agent.BranchID(parentPath)
	src := c.executor.Session().Snapshot()
	if boundary > len(src) {
		boundary = len(src)
	}
	forked := append([]provider.Message(nil), src[:boundary]...)
	sess := agent.NewSession("")
	sess.Messages = forked

	newPath := agent.NewSessionPath(c.sessionDir, c.label)
	if err := sess.SaveIfAbsent(newPath); err != nil {
		return "", c.rewindFail(err)
	}
	if _, err := sess.CopyValidContextProjection(parentPath, newPath); err != nil {
		slog.Warn("controller: fork did not inherit context projection", "err", err)
	}
	forkPreview, forkTurns := agent.SessionPreviewFromMessages(forked)
	if err := agent.SaveBranchMeta(newPath, agent.BranchMeta{
		Name:             strings.TrimSpace(name),
		ParentID:         parentID,
		ForkTurn:         turn,
		ForkMessageIndex: boundary,
		Preview:          forkPreview,
		Turns:            forkTurns,
		SchemaVersion:    agent.BranchMetaCountsVersion,
	}); err != nil {
		return "", c.rewindFail(err)
	}
	if switchToFork {
		commitTransition, err := c.prepareSessionTransition(newPath, "fork", sess)
		if err != nil {
			return "", c.rewindFail(fmt.Errorf("bind fork session: %w", err))
		}
		// See snapshotMu: the swap must not interleave with an in-flight save.
		c.snapshotMu.Lock()
		commitTransition.publish()
		// Load the child sidecar when the covered prefix survived the fork. The
		// loader rebinds its lineage key without touching the parent's sidecar.
		c.bindExecutorProjection(newPath, true)
		c.ResetPlannerSession()
		c.rebindCheckpoints(newPath)
		// A historical fork rewinds before later failures, so it starts with no
		// active recovery event even though it inherits the session preference.
		c.loadRecoveryState(newPath)
		if c.guardianSess != nil {
			c.guardianSess.Reset()
		}
		// Switching into the fork is a new logical session for temporary files.
		c.rotateSessionTemp()
		c.snapshotMu.Unlock()
	}
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("forked conversation at turn %d into a new session", turn)})
	return newPath, nil
}

func (c *Controller) CheckpointHasBoundary(turn int) bool {
	boundary, ok := c.checkpoints.boundary(turn)
	if !ok {
		return false
	}
	// After compaction or a head switch the key may point past the current
	// message log; treat those turns as "no boundary" so the UI can disable
	// the button. Len is lock-guarded for the frontend goroutines calling this.
	return boundary <= c.executor.Session().Len()
}

// Branch copies the current conversation into a child branch and switches to it.
// Unlike Fork, it branches at the current tip and does not require a checkpoint.
func (c *Controller) Branch(name string) (string, error) {
	if c.executor == nil {
		return "", c.rewindFail(fmt.Errorf("branch unavailable"))
	}
	if c.sessionDir == "" {
		return "", c.rewindFail(fmt.Errorf("branch needs session persistence, which is disabled"))
	}
	// Hold the rotation gate across the Snapshot and the switch below so a turn
	// cannot start mid-branch and then have its session replaced.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return "", c.rewindFail(fmt.Errorf("cannot branch while a turn is running"))
		}
		return "", c.rewindFail(err)
	}
	defer c.endRotation()
	if !c.executor.Session().HasContent() {
		return "", c.rewindFail(fmt.Errorf("nothing to branch yet"))
	}
	if err := c.Snapshot(); err != nil {
		return "", c.rewindFail(err)
	}
	if sess := c.headBranchSession(); sess != nil {
		return c.forkHeadReady(sess, -1, sess.Len(), name, agent.HeadKindFork)
	}
	parentPath := c.SessionPath()
	parentID := agent.BranchID(parentPath)
	src := c.executor.Session().Snapshot()
	branched := append([]provider.Message(nil), src...)
	sess := agent.NewSession("")
	sess.Messages = branched

	newPath := agent.NewSessionPath(c.sessionDir, c.label)
	if err := sess.SaveIfAbsent(newPath); err != nil {
		return "", c.rewindFail(err)
	}
	if _, err := sess.CopyValidContextProjection(parentPath, newPath); err != nil {
		slog.Warn("controller: branch did not inherit context projection", "err", err)
	}
	branchPreview, branchTurns := agent.SessionPreviewFromMessages(branched)
	if err := agent.SaveBranchMeta(newPath, agent.BranchMeta{
		Name:             strings.TrimSpace(name),
		ParentID:         parentID,
		ForkTurn:         -1,
		ForkMessageIndex: len(branched),
		Preview:          branchPreview,
		Turns:            branchTurns,
		SchemaVersion:    agent.BranchMetaCountsVersion,
	}); err != nil {
		return "", c.rewindFail(err)
	}
	commitTransition, err := c.prepareSessionTransition(newPath, "branch", sess)
	if err != nil {
		return "", c.rewindFail(fmt.Errorf("bind branch session: %w", err))
	}
	// See snapshotMu: the swap must not interleave with an in-flight save.
	c.snapshotMu.Lock()
	commitTransition.publish()
	c.bindExecutorProjection(newPath, true)
	c.ResetPlannerSession()
	c.rebindCheckpoints(newPath)
	if c.guardianSess != nil {
		c.guardianSess.Reset()
	}
	c.carryRecoveryState(newPath)
	c.rotateSessionTemp()
	c.snapshotMu.Unlock()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("created branch %s", agent.BranchID(newPath))})
	return newPath, nil
}

// Branches lists saved conversation branches in this controller's session dir.
func (c *Controller) Branches() ([]agent.BranchInfo, error) {
	if c.sessionDir == "" {
		return nil, fmt.Errorf("session persistence is disabled")
	}
	if err := c.Snapshot(); err != nil {
		return nil, err
	}
	branches, err := agent.ListBranches(c.sessionDir)
	if err != nil {
		return nil, err
	}
	return c.withHeadBranches(branches), nil
}

func (c *Controller) SwitchBranch(ref string) (agent.BranchInfo, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("usage: /switch <branch id|name>"))
	}
	// Hold the rotation gate across the branch listing/load and the switch so a
	// turn cannot start between the check and the SetSession below.
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("cannot switch branches while a turn is running"))
		}
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	defer c.endRotation()
	branches, err := c.Branches()
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	match, err := resolveBranch(branches, ref)
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	if !agent.IsVisibleSession(match.Path) {
		return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("branch %q not found", ref))
	}
	if match.HeadID != "" && agent.CanonicalSessionPath(match.Path) == agent.CanonicalSessionPath(c.SessionPath()) {
		return c.switchHeadInPlace(match)
	}
	loaded, err := agent.LoadSession(match.Path)
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	if match.HeadID != "" {
		if err := loaded.SwitchHead(match.Path, match.HeadID); err != nil {
			return agent.BranchInfo{}, c.rewindFail(err)
		}
	}
	commitTransition, err := c.prepareSessionTransition(match.Path, "switch", loaded)
	if err != nil {
		return agent.BranchInfo{}, c.rewindFail(fmt.Errorf("bind switched session: %w", err))
	}
	// See snapshotMu: the swap must not interleave with an in-flight save.
	c.snapshotMu.Lock()
	commitTransition.publish()
	c.bindExecutorProjection(match.Path, true)
	c.ResetPlannerSession()
	c.rebindCheckpoints(match.Path)
	c.restoreTerminalGoalTodos(match.Path)
	c.loadGuardianSession()
	c.loadRecoveryState(match.Path)
	c.rotateSessionTemp()
	c.snapshotMu.Unlock()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("switched to branch %s", branchDisplayName(match))})
	return match, nil
}

// ResolveBranchRef resolves a /switch-style branch reference (id, unique
// prefix, name, or path) against a branch listing, using the same matching
// rules as SwitchBranch. Frontends use it to learn the target session path
// before switching — e.g. to move their session lease first.
func ResolveBranchRef(branches []agent.BranchInfo, ref string) (agent.BranchInfo, error) {
	return resolveBranch(branches, strings.TrimSpace(ref))
}

func resolveBranch(branches []agent.BranchInfo, ref string) (agent.BranchInfo, error) {
	refLower := strings.ToLower(ref)
	var matches []agent.BranchInfo
	for _, b := range branches {
		nameLower := strings.ToLower(strings.TrimSpace(b.Name))
		switch {
		case b.ID == ref || strings.EqualFold(b.ID, ref):
			return b, nil
		case b.HeadID != "" && b.HeadID == ref:
			return b, nil
		case b.Name != "" && nameLower == refLower:
			matches = append(matches, b)
		case strings.HasPrefix(strings.ToLower(b.ID), refLower):
			matches = append(matches, b)
		case strings.HasPrefix(strings.ToLower(shortBranchID(b.ID)), refLower):
			matches = append(matches, b)
		case b.Path == ref:
			return b, nil
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return agent.BranchInfo{}, fmt.Errorf("branch %q is ambiguous", ref)
	}
	return agent.BranchInfo{}, fmt.Errorf("branch %q not found", ref)
}

func branchDisplayName(b agent.BranchInfo) string {
	if strings.TrimSpace(b.Name) != "" {
		return fmt.Sprintf("%s (%s)", b.Name, b.ID)
	}
	return b.ID
}

// forkHeadReady is the schema-2 fork: a new head starts at the message before
// boundary and this controller moves onto it without changing session path;
// checkpoint turns past the boundary stay hidden by CheckpointHasBoundary.
func (c *Controller) forkHeadReady(sess *agent.Session, turn, boundary int, name, kind string) (string, error) {
	if err := c.Snapshot(); err != nil {
		slog.Warn("controller: pre-fork snapshot", "err", err)
	}
	path := c.SessionPath()
	src := sess.Snapshot()
	if boundary > len(src) {
		boundary = len(src)
	}
	from := ""
	if boundary > 0 {
		from = src[boundary-1].ID
	}
	c.snapshotMu.Lock()
	head, err := sess.ForkHead(path, from, kind, name)
	if err != nil {
		c.snapshotMu.Unlock()
		return "", c.rewindFail(fmt.Errorf("fork head: %w", err))
	}
	c.afterHeadSwitch(path)
	c.snapshotMu.Unlock()
	if turn >= 0 {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("forked conversation at turn %d into a new version", turn)})
	} else {
		c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
			Text: fmt.Sprintf("created branch %s", head)})
	}
	return head, nil
}

// switchHeadInPlace moves the live session onto another head of its own log.
func (c *Controller) switchHeadInPlace(match agent.BranchInfo) (agent.BranchInfo, error) {
	sess := c.executor.Session()
	path := c.SessionPath()
	c.snapshotMu.Lock()
	if err := sess.SwitchHead(path, match.HeadID); err != nil {
		c.snapshotMu.Unlock()
		return agent.BranchInfo{}, c.rewindFail(err)
	}
	c.afterHeadSwitch(path)
	c.snapshotMu.Unlock()
	c.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelInfo,
		Text: fmt.Sprintf("switched to branch %s", branchDisplayName(match))})
	return match, nil
}

// afterHeadSwitch re-derives the per-transcript runtime state after the
// session moved to another head of the same log. Callers hold snapshotMu.
func (c *Controller) afterHeadSwitch(path string) {
	c.bindExecutorProjection(path, true)
	c.ResetPlannerSession()
	if c.guardianSess != nil {
		c.guardianSess.Reset()
	}
	c.rotateSessionTemp()
	c.emitHeadEvents()
	// Same path, different transcript: serve and remote clients rebind on this
	// barrier exactly as they do for a resume; local desktop tabs learn the
	// head in the desktop PR.
	c.sink.Emit(event.Event{Kind: event.SessionChanged, SessionPath: path, SessionReset: true})
}

// withHeadBranches lists the heads of the current schema-2 log as branches:
// the main head keeps the file's identity so the tree stays rooted at the
// log, and every other head hangs under its parent head.
func (c *Controller) withHeadBranches(branches []agent.BranchInfo) []agent.BranchInfo {
	sess := c.headBranchSession()
	if sess == nil {
		return branches
	}
	path := c.SessionPath()
	heads, err := agent.ListSessionHeads(path)
	if err != nil || len(heads) == 0 {
		return branches
	}
	fileID := agent.BranchID(path)
	headID := func(id string) string {
		if id == agent.SessionMainHead || id == "" {
			return fileID
		}
		return id
	}
	var file agent.BranchInfo
	out := make([]agent.BranchInfo, 0, len(branches)+len(heads))
	for _, b := range branches {
		if agent.CanonicalSessionPath(b.Path) == agent.CanonicalSessionPath(path) {
			file = b
			continue
		}
		out = append(out, b)
	}
	for _, h := range heads {
		if h.Retired {
			continue
		}
		info := file
		info.Path = path
		info.HeadID, info.HeadKind = h.ID, h.Kind
		info.ID = headID(h.ID)
		info.Turns, info.Preview = h.Turns, h.Preview
		if h.ID != agent.SessionMainHead {
			info.Name = h.Name
			info.ParentID = headID(h.ParentHead)
			info.ForkTurn, info.ForkMessageIndex = -1, 0
			info.CreatedAt = h.CreatedAt
		}
		out = append(out, info)
	}
	return out
}

// sessionHeadPolicy groups the frontend's choice between in-log heads and
// separate session files for branch operations.
type sessionHeadPolicy struct {
	fileBranchesOnly bool
}

// headBranchSession returns the session when branch operations may create
// heads inside its schema-2 log, nil when the frontend asked for files.
func (c *Controller) headBranchSession() *agent.Session {
	if c.headPolicy.fileBranchesOnly {
		return nil
	}
	return c.loggedTurnSession()
}

// SessionHead reports the schema-2 head the live session is on; ok is false
// for schema-1 sessions, whose branches are still separate files.
func (c *Controller) SessionHead() (agent.HeadRef, bool) {
	if c == nil || c.executor == nil || c.executor.Session() == nil {
		return agent.HeadRef{}, false
	}
	return c.executor.Session().Head()
}
