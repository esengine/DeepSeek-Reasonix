package control

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/base/i18n"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/evidence"
)

// A turn that ends owing verification has not failed: the host read what is
// missing off its own receipts, so asking the user to press continue asks them
// to relay a message the host wrote. What bounds it is a round that moved
// nothing, plus a ceiling for a gap that changes shape without shrinking.
const (
	readinessStallRounds = 2
	// A list growing an item for each one it closes looks like progress by every
	// local measure and never ends; the ceiling hands it back instead.
	readinessMaxRounds = 8
)

// completionWaysOut is every action the completion guard lets through. It is one
// string because two host prompts state it, and a run told two different things
// about which door is open walks into the one that is shut.
const completionWaysOut = "Mark each finished item with complete_step — that is what advances the list; a todo_write that flips an item to completed is rejected. " +
	"If an item should no longer be done — the request changed, or it was superseded — send a todo_write without it. " +
	"If one turns on a decision only the user can make, put it to them with ask; not having heard back is not an answer to assume one from. " +
	"If one cannot go on until the user says something only they can say, call await_user naming it — the list is kept and this message stops. " +
	"If one cannot be done as specified, call conclude_blocked with the evidence for why."

// continueUntilReady runs the missing requirements as further turns. turnErr is
// the outcome of the turn just finished; the returned error is the outcome of
// the last one run, so a caller cannot tell whether one turn or four produced it.
func (o *turnOrchestrator) continueUntilReady(ctx context.Context, turnErr error) error {
	last, stall, rounds := "", 0, 0
	for {
		var readinessErr *agent.FinalReadinessError
		if !errors.As(turnErr, &readinessErr) {
			// Ready, or a failure that has nothing to do with readiness.
			return turnErr
		}
		// A round that moved nothing at all is the only reliable stall: the
		// category list stays identical while counts inside it move, and the
		// counts themselves rise when landed work reveals a new requirement.
		if sig := readinessErr.Signature; sig == last {
			stall++
			if stall >= readinessStallRounds {
				return turnErr
			}
		} else {
			last, stall = sig, 0
		}
		if rounds++; rounds > readinessMaxRounds {
			return turnErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		prompt := readinessContinuationPrompt(o.c.goalTodos(), readinessErr.Reason)
		if prompt == "" {
			return turnErr
		}
		// The continuation inherits the finished turn's receipts. Without that
		// the next run starts from an empty ledger and the gap reads as closed
		// because the record of it was dropped, not because it was filled.
		if o.c.executor == nil || !o.c.executor.PrepareReadinessContinuation() {
			return turnErr
		}
		o.c.noticeDetail(i18n.M.ReadinessContinuing, prompt)
		turnErr = o.runOrchestratedTurn(ctx, orchestratedTurn{input: prompt, raw: prompt, synthetic: true})
	}
}

// readinessContinuationPrompt states what the turn still owes. It is the host's
// own account, so it names the requirement rather than asking the model whether
// it agrees one is outstanding.
func readinessContinuationPrompt(todos []evidence.TodoItem, reason string) string {
	var parts []string
	if incomplete := evidence.IncompleteTodos(todos); len(incomplete) > 0 {
		var b strings.Builder
		b.WriteString("these tasks are still incomplete:")
		for _, t := range incomplete {
			fmt.Fprintf(&b, "\n  - %s (%s)", evidence.TodoCitation(t.StepID, t.Index, t.Content), t.Status)
		}
		parts = append(parts, b.String())
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		parts = append(parts, reason)
	}
	if len(parts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("This turn ended with work still outstanding:\n")
	for _, p := range parts {
		b.WriteString("- " + p + "\n")
	}
	b.WriteString("Do the remaining work, then verify it. " + completionWaysOut)
	return b.String()
}
