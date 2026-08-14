package control

import (
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

// turnFinalBoundary binds a semantic consumer to one foreground turn. A
// current result must be newer than this boundary; an older visible assistant
// message is never a substitute for an empty current result.
type turnFinalBoundary struct {
	session           *agent.Session
	startMessages     int
	messageLogVersion uint64
}

func (c *Controller) captureTurnFinalBoundary() turnFinalBoundary {
	if c == nil || c.executor == nil {
		return turnFinalBoundary{}
	}
	sess := c.executor.Session()
	if sess == nil {
		return turnFinalBoundary{}
	}
	startMessages, messageLogVersion := sess.MessageLogBoundary()
	return turnFinalBoundary{
		session:           sess,
		startMessages:     startMessages,
		messageLogVersion: messageLogVersion,
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
	msgs, ok := b.session.MessageRangeSince(b.startMessages, b.messageLogVersion)
	if !ok || c.executor.Session() != b.session {
		return nil
	}
	return msgs
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
