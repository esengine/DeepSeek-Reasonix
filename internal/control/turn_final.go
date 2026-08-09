package control

import (
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// turnFinalBoundary binds a semantic consumer to one foreground turn. A
// current result must be newer than this boundary; an older visible assistant
// message is never a substitute for an empty current result.
type turnFinalBoundary struct {
	session        *agent.Session
	startMessages  int
	rewriteVersion int
	startedAt      int64
}

func (c *Controller) captureTurnFinalBoundary() turnFinalBoundary {
	if c == nil || c.executor == nil || c.executor.Session() == nil {
		return turnFinalBoundary{}
	}
	sess := c.executor.Session()
	return turnFinalBoundary{
		session:        sess,
		startMessages:  sess.Len(),
		rewriteVersion: sess.RewriteVersion(),
		startedAt:      time.Now().UnixMilli(),
	}
}

func (b turnFinalBoundary) currentVisibleFinal(c *Controller) string {
	return terminalVisibleAssistant(b.currentTurnMessages(c))
}

// currentAssistantText preserves the Stop hook's historical ability to inspect
// a partial/tool-preamble answer, but constrains the search to this turn. Plan
// and Goal must use currentVisibleFinal's stricter terminal contract instead.
func (b turnFinalBoundary) currentAssistantText(c *Controller) string {
	msgs := b.currentTurnMessages(c)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleAssistant && strings.TrimSpace(msgs[i].Content) != "" {
			return msgs[i].Content
		}
	}
	return ""
}

func (b turnFinalBoundary) currentTurnMessages(c *Controller) []provider.Message {
	if b.session == nil || c == nil || c.executor == nil || c.executor.Session() != b.session {
		return nil
	}
	if b.session.RewriteVersion() == b.rewriteVersion {
		end := b.session.Len()
		if b.startMessages < 0 || b.startMessages >= end {
			return nil
		}
		msgs := b.session.MessageRange(b.startMessages, end)
		// A rewrite between the version check and range read invalidates the
		// numeric boundary. Fall through to the rewrite-safe anchor below.
		if b.session.RewriteVersion() == b.rewriteVersion && c.executor.Session() == b.session {
			return msgs
		}
	}

	// A rewrite can invalidate the numeric boundary. Compaction preserves the
	// recent user turn, so re-anchor only to a user message created after this
	// turn began. If that identity cannot be proved, fail closed.
	msgs := b.session.Snapshot()
	if c.executor.Session() != b.session {
		return nil
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == provider.RoleUser && msgs[i].CreatedAt >= b.startedAt {
			return msgs[i+1:]
		}
	}
	return nil
}

func terminalVisibleAssistant(msgs []provider.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	last := msgs[len(msgs)-1]
	if last.Role != provider.RoleAssistant || last.LocalOnly || len(last.ToolCalls) > 0 || strings.TrimSpace(last.Content) == "" {
		return ""
	}
	return last.Content
}
