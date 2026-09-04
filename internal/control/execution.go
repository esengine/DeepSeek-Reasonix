package control

import (
	"context"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/execjournal"
)

// beginTurn marks the turn and hands back a context carrying its identity, so
// every path that starts one gives its tools the same durable turn identity —
// the only thing a delegation opened mid-turn can be recorded against.
func (c *Controller) beginTurn(ctx context.Context, startMessageIndex int, preserveUser bool) (context.Context, agent.InFlightTurnMeta) {
	marker := c.markInFlightTurn(startMessageIndex, preserveUser)
	return agent.WithTurnIdentity(ctx, marker.ID), marker
}

// A delegation opened mid-turn is a fact no later state can re-derive: its turn
// is appended only when it ends, so a process dying inside one leaves the
// request and every item it opened equally absent.

// InterruptedExecutions returns the delegations this session recorded and never
// settled, with no owner left in this process: durable evidence that work was
// opened and cut, never a handle to resume it. Entry.Interruption says whether
// it had reached a slot, which decides what may be half-finished — never
// whether anything may be restarted.
func (c *Controller) InterruptedExecutions() []execjournal.Entry {
	if c == nil {
		return nil
	}
	return execjournal.Interrupted(c.SessionPath())
}

// ExecutionHistory is every delegation this session opened, in the order it
// opened them. It states what the orchestration declared, never what a child
// produced: the sub-agent store owns that, and a second copy is a second
// authority to reconcile.
func ExecutionHistory(sessionPath string) []execjournal.Entry {
	return execjournal.History(sessionPath)
}

// interruptedExecutionContext is what the host owes the next request about
// delegations that did not finish. It states provenance, not a continuation:
// the runs are gone, and nothing they were about to do was carried out by
// their disappearance.
func (c *Controller) interruptedExecutionContext() []string {
	interrupted := c.InterruptedExecutions()
	if len(interrupted) == 0 {
		return nil
	}
	return []string{interruptedExecutionBlock(interrupted)}
}

// interruptedExecutionBlock renders them for the model. Every entry is named,
// because a block that says work was cut without saying which leaves the model
// to guess at scope — and a guess about half-finished work is worse than the
// silence it replaced.
func interruptedExecutionBlock(interrupted []execjournal.Entry) string {
	var b strings.Builder
	b.WriteString("<interrupted-execution>\n")
	b.WriteString("A previous run opened delegated work and ended before that work was settled.\n")
	for _, item := range interrupted {
		b.WriteString("\n- " + item.ID)
		if name := strings.TrimSpace(item.Name); name != "" {
			b.WriteString(": " + name)
		}
		if item.Grant != "" {
			b.WriteString(" (" + item.Grant + ")")
		}
		b.WriteString(" — " + item.Interruption())
		if up := item.DependsOn; len(up) > 0 && !item.Started() {
			b.WriteString(", which declared it may not start before ")
			b.WriteString(strings.Join(up, ", "))
		}
		if !item.Started() && item.Queued() {
			b.WriteString("; it was ready and the scheduler first refused it: " + item.Cause)
		}
	}
	b.WriteString("\n\nNone can be resumed and none were restarted. One marked ")
	b.WriteString(execjournal.InterruptedBeforeStart)
	b.WriteString(" never reached a slot, so nothing it would have done was done. One marked ")
	b.WriteString(execjournal.InterruptedDuringExecution)
	b.WriteString(" was executing: whatever it had already written stands, and whatever it had ")
	b.WriteString("not is simply undone. Treat this as context for what may be half-finished, ")
	b.WriteString("not as work to continue: anything still wanted has to be delegated again.\n")
	b.WriteString("</interrupted-execution>")
	return b.String()
}
