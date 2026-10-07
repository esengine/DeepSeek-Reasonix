package control

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

var ErrForkBoundary = errors.New("the fork checkpoint is stale or has no completed reply")
var ErrForkBusy = errors.New("cannot fork while the session is busy")

func (c *Controller) ForkableTurns() map[int]bool {
	ready := make(map[int]bool)
	msgs, cps := c.History(), c.Checkpoints()
	for i, cp := range cps {
		end := len(msgs)
		if i+1 < len(cps) {
			end = cps[i+1].MsgIndex
		}
		_, ready[cp.Turn] = finalForkReply(msgs, cp.MsgIndex, end)
	}
	return ready
}

func finalForkReply(msgs []provider.Message, start, end int) (int, bool) {
	if start < 0 || start >= end || end > len(msgs) || msgs[start].Role != provider.RoleUser {
		return 0, false
	}
	last := -1
	for i := start + 1; i < end; i++ {
		if msgs[i].Role == provider.RoleAssistant {
			last = i
		}
	}
	if last < 0 {
		return 0, false
	}
	m := msgs[last]
	return last, strings.TrimSpace(m.Content) != "" && len(m.ToolCalls) == 0 && !m.LocalOnly && m.InterruptedTurn == nil
}

// ForkTurn copies through a completed turn's final reply without switching the
// source controller. The checkpoint stamp rejects a replaced turn at the same index.
func (c *Controller) ForkTurn(sessionPath string, turn, msgIndex int, stamp string) (string, error) {
	if c.executor == nil || c.sessionDir == "" || sessionPath == "" {
		return "", ErrForkBoundary
	}
	if err := c.beginRotation(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrForkBusy, err)
	}
	defer c.endRotation()
	if c.hasUnfinishedSessionJobs(sessionPath) {
		return "", ErrForkBusy
	}
	if err := c.Snapshot(); err != nil {
		return "", err
	}
	if c.SessionPath() != sessionPath {
		return "", ErrForkBoundary
	}
	matched := false
	end := c.HistoryLen()
	for _, cp := range c.Checkpoints() {
		if cp.Turn == turn && cp.MsgIndex == msgIndex && cp.Time.Format(time.RFC3339Nano) == stamp {
			matched = true
		}
		if cp.MsgIndex > msgIndex && cp.MsgIndex < end {
			end = cp.MsgIndex
		}
	}
	msgs := c.History()
	last, ready := finalForkReply(msgs, msgIndex, end)
	if !matched || !ready {
		return "", ErrForkBoundary
	}
	child := sessionstore.NewSession("")
	child.Messages = append([]provider.Message(nil), msgs[:last+1]...)
	path := sessionstore.NewSessionPath(c.sessionDir, c.label)
	preview, turns := sessionstore.SessionPreviewFromMessages(child.Messages)
	meta := sessionstore.BranchMeta{
		ParentID: sessionstore.BranchID(sessionPath), WorkspaceRoot: c.workspaceRoot,
		Preview: preview, Turns: turns, SchemaVersion: sessionstore.BranchMetaCountsVersion,
	}
	meta.Model, meta.AgentPreset = c.ModelRef(), c.AgentPreset()
	meta.Mode = "agent"
	if c.PlanMode() {
		meta.Mode = "plan"
	}
	if err := saveFork(path, child, meta, func() error {
		return c.checkpoints.storeRef().CopyConversationPrefixTo(ckptDir(path), sessionstore.BranchID(path), last+1)
	}); err != nil {
		return "", err
	}
	return path, nil
}
