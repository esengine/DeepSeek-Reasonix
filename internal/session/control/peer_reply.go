package control

import (
	"log/slog"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/state/peermail"
)

// peerReplyHeadroom keeps the framing and the cut text under the message cap.
const peerReplyHeadroom = 1 << 10

// dispatchOpenerKey is the key of the session that opened this one by a
// dispatch, or "" when none did.
func (c *Controller) dispatchOpenerKey() string {
	c.dispatch.mu.Lock()
	defer c.dispatch.mu.Unlock()
	for key, pair := range c.dispatch.pairs {
		if pair.opener {
			return key
		}
	}
	return ""
}

// replyOwed reports whether a task from the session that opened this one is
// among the items the finishing turn ran, and returns that session's key. Only
// the opener is answered: a reply travels from the session that was handed the
// task to the one that handed it, and is itself never answered, so two sessions
// cannot keep each other talking.
func (c *Controller) replyOwed() string {
	opener := c.dispatchOpenerKey()
	if opener == "" {
		return ""
	}
	c.inbox.mu.Lock()
	ids := c.inbox.activeIDs()
	st := c.inbox.store
	c.inbox.mu.Unlock()
	if st == nil {
		return ""
	}
	for _, id := range ids {
		if meta, _, err := st.ReadItem(id); err == nil && meta.Source == peerSourcePrefix+opener {
			return opener
		}
	}
	return ""
}

// replyToOpener sends the finished turn's outcome back to the session that
// handed this one the task. The user's dispatch switch is their consent for the
// replies of the sessions it opened; with it off nothing is sent.
func (c *Controller) replyToOpener(opener string, done event.Event) {
	if opener == "" || !config.LoadForEdit(config.UserConfigPath()).PeersDispatch() {
		return
	}
	if _, err := c.SendPeerMessage(opener, c.replyBody(done)); err != nil {
		slog.Warn("controller: reply to the session that dispatched this one", "err", err)
	}
}

// replyBody is the host's own account of how the turn ended, with the session's
// last written answer when it finished. Which of the three it is comes from the
// turn's outcome fields, not from the answer's words.
func (c *Controller) replyBody(done event.Event) string {
	switch {
	case done.Cancelled:
		return "Reply from the session you handed a task to: its user stopped it before it finished the task."
	case done.Err != nil:
		return "Reply from the session you handed a task to: its turn ended with an error and the task is not finished."
	}
	answer := strings.TrimSpace(lastAssistantText(c.History()))
	if answer == "" {
		return "Reply from the session you handed a task to: it finished the turn without writing an answer."
	}
	if limit := peermail.MaxBodyBytes - peerReplyHeadroom; len(answer) > limit {
		answer = strings.ToValidUTF8(answer[:limit], "")
		answer += "\n[cut: the answer was longer than a message can carry]"
	}
	return "Reply from the session you handed a task to, with its final answer:\n\n" + answer
}
