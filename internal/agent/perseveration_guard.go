package agent

import (
	"bytes"
	"context"
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

// abortOnPerseveration builds the terminal for a stream the guard cut short. It
// preserves what was produced and marks the result so the turn settles with a
// warning instead of retrying.
func abortOnPerseveration(collect func(string, error) streamedTurn, finishReasoning func() (string, string)) streamedTurn {
	stored, _ := finishReasoning()
	out := collect(stored, nil)
	out.perseverationAborted = true
	return out
}

// perseverationRetryMessage is the host nudge appended before a retry. It is
// deliberately not the previous prompt verbatim: re-sending the exact request
// would reproduce the loop, while this marker both tells the user why the
// attempt restarted and steers the model off the repeated block. attempt is the
// consecutive-retry ordinal (1 for the first retry), so raising the retry limit
// needs no message change.
func perseverationRetryMessage(attempt int) string {
	return fmt.Sprintf("\n[retrying (%d) avoiding perseveration]\n", attempt)
}

// defaultPerseverationRetries is the nudge-and-retry budget when the caller
// does not set Options.MaxPerseverationRetries.
const defaultPerseverationRetries = 1

// resolvePerseverationRetries maps the optional option onto the effective retry
// budget: nil keeps the default, and a negative value is clamped to 0 (no retry).
func resolvePerseverationRetries(configured *int) int {
	if configured == nil {
		return defaultPerseverationRetries
	}
	return max(*configured, 0)
}

// handlePerseverationAbort decides what happens after the guard ends a stream.
// While the consecutive-strike count is below the retry budget (no successful
// provider response since the last strike) it appends the nudge and continues
// the loop; once the budget is spent it stops and waits for the user instead of
// retrying into the same trap. It returns cont=true to keep the tool loop
// running.
func (a *Agent) handlePerseverationAbort(ctx context.Context) (cont bool, err error) {
	if a.sess.perseverationStrikes < a.perseverationMaxRetries {
		a.sess.perseverationStrikes++
		if err := a.appendCommittedMessages(ctx, "perseveration-retry",
			HostGeneratedUserMessage(a.withTurnPreferences(perseverationRetryMessage(a.sess.perseverationStrikes)))); err != nil {
			return false, err
		}
		return true, nil
	}
	a.svc.sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn,
		Code: event.NoticeCodePerseverationLoop, Text: i18n.M.PerseverationLoop})
	return false, nil
}

// perseverationGuard detects perseveration (= mindless repetition): a
// degenerate generation loop where the model emits a short block of text or
// reasoning over and over ("Let me write. Hmm. OK.") without ever calling a tool
// or stopping. No other guard observes that — the tool-round guards require
// tool calls and the turn-liveness watchdog only measures silence, so a chatty
// loop looks healthy while it burns the entire output budget.
//
// The guard watches the rolling tail of one streamed channel and reports once
// when a short block repeats enough times to be unmistakable. It matches only
// byte-identical repeats: near-loops with per-iteration variation are left to
// the provider's own finish_reason=repetition_truncation, which this keeps from
// false-positiving on ordinary repeated structure (tables, separators, fixtures).
type perseverationGuard struct {
	tail       []byte
	window     int // bytes of history examined for periodicity
	minPeriod  int // shortest block that can count as a loop unit
	maxPeriod  int // longest block that can count as a loop unit
	minRepeats int // identical repeats required before firing
	minSpan    int // minimum total bytes covered by the repeats
	fired      bool
}

func newPerseverationGuard() *perseverationGuard {
	return &perseverationGuard{
		window:     8192,
		minPeriod:  4,
		maxPeriod:  256,
		minRepeats: 8,
		minSpan:    160,
	}
}

// observe appends one streamed delta and reports whether the accumulated tail
// is now a short block repeated enough times to be degenerate. It reports true
// at most once, so a single abort decision is made per stream.
func (g *perseverationGuard) observe(delta string) bool {
	if g == nil || g.fired || delta == "" {
		return false
	}
	g.tail = append(g.tail, delta...)
	if len(g.tail) > g.window {
		g.tail = append(g.tail[:0], g.tail[len(g.tail)-g.window:]...)
	}
	if g.degenerate() {
		g.fired = true
		return true
	}
	return false
}

// degenerate reports whether the tail ends with the same block repeated at
// least minRepeats times for some period in [minPeriod, maxPeriod]. The
// smallest qualifying period wins, so a unit that is itself periodic is caught
// at its fundamental period.
func (g *perseverationGuard) degenerate() bool {
	n := len(g.tail)
	limit := min(g.maxPeriod, n/g.minRepeats)
	for period := g.minPeriod; period <= limit; period++ {
		repeats := g.trailingRepeats(period)
		if repeats < g.minRepeats || period*repeats < g.minSpan {
			continue
		}
		if meaningfulPerseveration(g.tail[n-period:]) {
			return true
		}
	}
	return false
}

// trailingRepeats counts how many times the final period-length block repeats
// contiguously at the end of the tail.
func (g *perseverationGuard) trailingRepeats(period int) int {
	n := len(g.tail)
	block := g.tail[n-period:]
	count := 1
	for i := n - 2*period; i >= 0; i -= period {
		if !bytes.Equal(g.tail[i:i+period], block) {
			break
		}
		count++
	}
	return count
}

// meaningfulPerseveration filters out blocks that would false-positive on
// legitimate output: whitespace runs, alignment padding, or a single repeated
// character. A real degenerate loop repeats words and punctuation, so require
// some non-space bytes and at least two letters or digits.
func meaningfulPerseveration(block []byte) bool {
	nonSpace, alnum := 0, 0
	for _, b := range block {
		switch {
		case b == ' ' || b == '\t' || b == '\r' || b == '\n':
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b >= 0x80:
			nonSpace++
			alnum++
		default:
			nonSpace++
		}
	}
	return nonSpace >= 4 && alnum >= 2
}
