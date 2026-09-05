// jobs_bridge.go — a terminal background job becomes a turn the session runs.
package control

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/jobs"
	"reasonix/internal/sessioninbox"
	"reasonix/internal/taskmonitor"
)

// hostContinuationSource labels inbox items the runtime authored for itself.
const hostContinuationSource = "host:background-job"

// observeJobs attaches what watches the background-job table: the task-store
// recorder, and this controller as the completion observer. The recorder
// swallows its own failures, and resolves the session id lazily because the
// session path is only fixed once the first turn begins.
func (c *Controller) observeJobs(taskStore taskmonitor.WriteStore) {
	if c.jobs == nil {
		return
	}
	if c.workspaceRoot != "" {
		if taskStore == nil {
			taskStore = taskmonitor.NewFileStore(filepath.Join(".reasonix", "tasks"))
		}
		c.jobs.SetTaskRecorder(taskmonitor.NewTaskRecorder(taskStore, c.workspaceRoot,
			func() string { return c.parentSessionID() }))
	}
	// The manager keeps its own note either way, so a controller that cannot
	// take an event costs nothing.
	c.jobs.SetCompletionObserver(c)
}

// OnJobCompletion implements jobs.CompletionObserver: a terminal background job
// gives its session a turn instead of waiting for the user's next message. An
// error means the event never reached the durable queue, and the manager's own
// note carries it into the next real turn instead.
func (c *Controller) OnJobCompletion(_ context.Context, ev jobs.CompletionEvent) error {
	if c == nil {
		return fmt.Errorf("control: no controller for job %s", ev.JobID)
	}
	// Jobs owned by a session that is not the active one keep their queued note
	// until that session is active again — the same rule the drain path applies.
	if session := strings.TrimSpace(ev.SessionID); session != "" && session != c.parentSessionID() {
		return fmt.Errorf("control: job %s belongs to another session", ev.JobID)
	}
	// A run with no persisted session (headless `run`, e2ebench) has no durable
	// inbox to own the continuation, and must degrade rather than fail.
	if c.SessionPath() == "" {
		return fmt.Errorf("control: session has no durable inbox")
	}
	// A burst is one interruption, not one per job: thirty failing jobs queued
	// thirty turns. While a continuation is still unread, later completions stay
	// in the manager's note and ride that turn's <background-jobs> block.
	if c.hostContinuationPending() {
		return fmt.Errorf("control: job %s folds into a queued continuation", ev.JobID)
	}
	display, submit := backgroundCompletionTexts(ev)
	if submit == "" {
		return fmt.Errorf("control: job %s produced no continuation", ev.JobID)
	}
	_, err := c.EnqueueHostContinuation(InboxRequest{
		Display:     display,
		Submit:      submit,
		Source:      hostContinuationSource,
		Idempotency: ev.ID,
	})
	return err
}

// hostContinuationPending reports whether a runtime-authored continuation is
// still waiting for a turn to read it. Consumed and running items are past
// that point, so a completion arriving after one of those opens its own turn.
func (c *Controller) hostContinuationPending() bool {
	for _, it := range c.InboxSnapshot().Items {
		if it.Source != hostContinuationSource {
			continue
		}
		switch it.State {
		case sessioninbox.StateQueued, sessioninbox.StateSteerAccepted,
			sessioninbox.StateBlocked, sessioninbox.StateUncertain:
			return true
		}
	}
	return false
}

// EnqueueHostContinuation durably queues runtime-authored work and delivers it
// into the live turn, or as the next one. Which of the two is never decided by
// reading whether a turn is running — the turn can end in that gap — but by the
// steer attempt itself, whose refusal converts the item and kicks the
// dispatcher. A nil error means durable, not read.
func (c *Controller) EnqueueHostContinuation(req InboxRequest) (sessioninbox.InboxReceipt, error) {
	req.Intent = sessioninbox.IntentSteer
	req.Origin = sessioninbox.OriginHost
	rec, err := c.EnqueueInbox(req)
	if err != nil {
		return rec, err
	}
	if rec.Idempotent {
		return rec, nil
	}
	steered, steerErr := c.TrySteerInboxItem(rec.ItemID)
	if steerErr != nil {
		// Durable and still queued: delivery is the dispatcher's now.
		c.maybeDispatchInbox()
		return rec, nil
	}
	return steered, nil
}

// backgroundCompletionTexts is the host's own account of what finished, in the
// two forms it is read in. The model gets the tagged block; the queue row and
// the transcript get the fact alone, because Display is persisted as the turn's
// RawContent — one string for both is how <background-jobs> markup reached the
// pending-queue rows as if the user had typed it.
func backgroundCompletionTexts(ev jobs.CompletionEvent) (display, submit string) {
	tag := ev.JobID
	if ev.Label != "" {
		tag = fmt.Sprintf("%s (%s)", ev.JobID, ev.Label)
	}
	if tag == "" {
		return "", ""
	}
	display = fmt.Sprintf("%s — %s", tag, ev.Status)
	var b strings.Builder
	b.WriteString("<background-jobs>\n")
	b.WriteString(display + "\n")
	b.WriteString("</background-jobs>\n\n")
	b.WriteString("A background job you started has finished. Read its result with wait or bash_output, then continue the work it was part of. Do not redo what it already did, and end the turn if nothing is left to do.")
	return display, b.String()
}
