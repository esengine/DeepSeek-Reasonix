package control

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// ErrBranchUnavailable is a branch the conversation cannot be cut for: a turn it
// has no checkpoint boundary for, or one compaction has moved past.
var ErrBranchUnavailable = errors.New("control: this point cannot be branched")

// ErrBranchStale is a cut whose recorded boundary no longer names the turn that
// was asked for. It is an ErrBranchUnavailable: the answer is the same, only the
// reason is typed.
var ErrBranchStale = fmt.Errorf("%w: the recorded boundary no longer names that turn", ErrBranchUnavailable)

// BranchAt copies the conversation as it stood at the end of turn into a new
// session file and leaves this controller on its own session, so a frontend can
// open the branch beside the source. Persisted checkpoints restore these
// boundaries when the session is reopened.
func (c *Controller) BranchAt(turn int, name string) (string, error) {
	return c.branchAt(&turn, name, nil)
}

// A nil turn selects the current tip. The optional switch runs under the same
// rotation gate as creation, so a new turn cannot slip between the two.
func (c *Controller) branchAt(turn *int, name string, switchTo func(*sessionstore.Session, string)) (string, error) {
	if c.executor == nil || c.sessionDir == "" {
		return "", fmt.Errorf("%w: sessions are not persisted here", ErrBranchUnavailable)
	}
	if err := c.beginRotation(); err != nil {
		if errors.Is(err, errTurnRunningRotation) {
			if turn != nil {
				return c.branchClosedTurn(*turn, name)
			}
			return "", fmt.Errorf("cannot branch while a turn is running: %w", ErrTurnRunning)
		}
		return "", err
	}
	defer c.endRotation()
	if c.hasUnfinishedSessionJobs(c.SessionPath()) {
		return "", ErrTurnRunning
	}
	if turn == nil && !c.executor.Session().HasContent() {
		return "", fmt.Errorf("%w: nothing to branch yet", ErrBranchUnavailable)
	}
	if err := c.Snapshot(); err != nil {
		return "", err
	}
	src := c.executor.Session().Snapshot()
	end := len(src)
	if turn != nil {
		var ok bool
		end, ok = c.checkpoints.endOfTurn(*turn, len(src))
		if !ok {
			return "", fmt.Errorf("%w: turn %d has no live boundary", ErrBranchUnavailable, *turn)
		}
		if end > len(src) {
			return "", fmt.Errorf("%w: the conversation was compacted past turn %d", ErrBranchUnavailable, *turn)
		}
	}
	_, path, err := c.createBranchPrefixInEnvironment(src[:end], name, switchTo == nil)
	if err != nil {
		return "", err
	}
	if switchTo != nil {
		// The creation lease has been released; do not adopt its stale authority.
		loaded, err := sessionstore.LoadSession(path)
		if err != nil {
			return "", err
		}
		switchTo(loaded, path)
	}
	return path, nil
}

// branchClosedTurn cuts a turn that is already closed while a later one runs.
// The running turn is partial (a tool call may have no result yet), so only a
// cut whose end is the start of the next recorded turn, and whose message there
// is that turn's own prompt, is a conversation boundary. The latest turn, whose
// end is still moving, is refused; a boundary that no longer matches is stale.
func (c *Controller) branchClosedTurn(turn int, name string) (string, error) {
	if c.executor == nil || c.sessionDir == "" {
		return "", fmt.Errorf("%w: sessions are not persisted here", ErrBranchUnavailable)
	}
	src := c.executor.Session().Snapshot()
	end, ok := c.checkpoints.endOfTurn(turn, len(src))
	if !ok || end >= len(src) {
		return "", fmt.Errorf("cannot branch this point while a turn is running: %w", ErrTurnRunning)
	}
	if !c.checkpoints.namesUserMessage(end, src[end]) {
		return "", ErrBranchStale
	}
	_, path, err := c.createBranchPrefixInEnvironment(src[:end], name, true)
	return path, err
}

// createBranchPrefix is the one writer for idle, closed-turn and tip branches.
func (c *Controller) createBranchPrefixInEnvironment(src []provider.Message, name string, independent bool) (*sessionstore.Session, string, error) {
	child := sessionstore.NewSession("")
	child.Messages = append([]provider.Message(nil), src...)
	path := sessionstore.NewSessionPath(c.sessionDir, c.label)
	name = strings.TrimSpace(name)
	preview, turns := sessionstore.SessionPreviewFromMessages(child.Messages)
	meta := sessionstore.BranchMeta{
		Name: name, ParentID: sessionstore.BranchID(c.SessionPath()), WorkspaceRoot: c.workspaceRoot,
		Preview: preview, Turns: turns, SchemaVersion: sessionstore.BranchMetaCountsVersion,
		Model: c.ModelRef(), AgentPreset: c.AgentPreset(), Mode: "agent",
	}
	if c.PlanMode() {
		meta.Mode = "plan"
	}
	if err := saveConversationBranch(path, child, meta, func() error {
		if c.rootEnvironment != nil {
			if independent {
				fork, err := c.rootEnvironment.Fork(context.Background(), c.SessionPath(), path, child.WriteAuthority())
				if err != nil {
					return err
				}
				fork.Close()
			} else {
				if err := c.rootEnvironment.AttachSession(context.Background(), c.SessionPath(), path, child.WriteAuthority()); err != nil {
					return err
				}
			}
		}
		if err := c.checkpoints.storeRef().CopyConversationPrefixTo(ckptDir(path), sessionstore.BranchID(path), len(src)); err != nil {
			return err
		}
		if name != "" {
			return sessionstore.RenameSession(path, name)
		}
		return nil
	}); err != nil {
		return nil, "", err
	}
	return child, path, nil
}
