package control

import (
	"log/slog"
	"time"

	"reasonix/internal/sessioninbox"
)

const maxInboxDispatchRetryAttempts = 3

type inboxDispatchResult int

const (
	inboxDispatchIdle inboxDispatchResult = iota
	inboxDispatchStarted
	inboxDispatchRetry
)

// endRotation releases the admission gate and republishes durable queue work.
func (c *Controller) endRotation() {
	c.mu.Lock()
	c.rotating = false
	c.mu.Unlock()
	c.maybeDispatchInbox()
}

// maybeDispatchInbox is a level-triggered kick, not a one-shot edge. Every
// caller publishes pending work before checking whether a dispatcher is live.
// The active dispatcher clears dispatching only while holding the same lock
// after observing no pending kick, so a completion, rotation release, or steer
// rejection can never disappear in the handoff window.
func (c *Controller) maybeDispatchInbox() {
	c.inbox.mu.Lock()
	c.inbox.dispatchPending = true
	if c.inbox.dispatching {
		c.inbox.mu.Unlock()
		return
	}
	c.inbox.dispatching = true
	c.inbox.mu.Unlock()

	for {
		c.inbox.mu.Lock()
		if !c.inbox.dispatchPending {
			c.inbox.dispatching = false
			c.inbox.mu.Unlock()
			return
		}
		c.inbox.dispatchPending = false
		c.inbox.mu.Unlock()

		switch c.dispatchInboxOnce() {
		case inboxDispatchRetry:
			c.scheduleInboxDispatchRetry()
		case inboxDispatchStarted, inboxDispatchIdle:
			c.resetInboxDispatchRetries()
		}
	}
}

// dispatchInboxOnce admits one FIFO item when every runtime gate is open. A
// started turn owns the next kick through finishGuardedTurn; this method never
// loops over multiple items while that turn is active.
func (c *Controller) dispatchInboxOnce() inboxDispatchResult {
	if c.PendingPrompt() {
		return inboxDispatchIdle
	}
	c.mu.Lock()
	busy := c.running || c.finishing || c.rotating || c.closed
	c.mu.Unlock()
	if busy {
		return inboxDispatchIdle
	}
	// Controllers without persistence cannot own a durable inbox. Rotation and
	// turn-completion hooks are shared with those controllers, so treat the
	// missing path as an empty queue instead of retrying a permanent condition.
	if c.SessionPath() == "" {
		return inboxDispatchIdle
	}
	st, err := c.ensureInbox()
	if err != nil {
		slog.Warn("controller: open inbox for dispatch", "err", err)
		return inboxDispatchRetry
	}
	meta, ok := st.NextQueued()
	c.inbox.mu.Lock()
	afterScan := c.inbox.afterDispatchScan
	beforeSubmit := c.inbox.beforeDispatchSubmit
	c.inbox.mu.Unlock()
	if afterScan != nil {
		afterScan(ok)
	}
	if !ok {
		return inboxDispatchIdle
	}
	if beforeSubmit != nil {
		if err := beforeSubmit(meta.ID); err != nil {
			slog.Warn("controller: inbox dispatch hook", "err", err, "id", meta.ID)
			return inboxDispatchRetry
		}
	}
	receipt, err := c.TrySubmitInboxItem(meta.ID)
	if err != nil {
		slog.Warn("controller: dispatch inbox item", "err", err, "id", meta.ID)
		return inboxDispatchRetry
	}
	if receipt.Disposition == sessioninbox.DispositionStarted {
		return inboxDispatchStarted
	}
	// A competing turn or rotation owns the next kick when its gate releases.
	return inboxDispatchIdle
}

func (c *Controller) scheduleInboxDispatchRetry() {
	c.inbox.mu.Lock()
	if c.inbox.dispatchRetryScheduled || c.inbox.dispatchRetryAttempts >= maxInboxDispatchRetryAttempts {
		c.inbox.mu.Unlock()
		return
	}
	attempt := c.inbox.dispatchRetryAttempts
	c.inbox.dispatchRetryAttempts++
	c.inbox.dispatchRetryScheduled = true
	schedule := c.inbox.scheduleDispatchRetry
	c.inbox.mu.Unlock()

	delay := [...]time.Duration{50 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond}[attempt]
	retry := func() {
		c.inbox.mu.Lock()
		c.inbox.dispatchRetryScheduled = false
		c.inbox.mu.Unlock()
		c.maybeDispatchInbox()
	}
	if schedule != nil {
		schedule(delay, retry)
		return
	}
	time.AfterFunc(delay, retry)
}

func (c *Controller) resetInboxDispatchRetries() {
	c.inbox.mu.Lock()
	c.inbox.dispatchRetryAttempts = 0
	c.inbox.mu.Unlock()
}
