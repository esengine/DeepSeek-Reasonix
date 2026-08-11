package agent

import (
	"strings"

	"reasonix/internal/event"
)

// verdict is the escalation ladder every run signal speaks. A round reduces its
// signals to the strongest one, so a land raised beside an advise still lands.
type verdict int

const (
	verdictContinue verdict = iota
	verdictAdvise
	verdictRedirect
	verdictLand
)

// intervention is one signal's decision for a round. Guidance reaches the model
// as environment feedback on the round tail rather than as a synthesized user
// turn: the host is not the user, and a fake user turn competes with real user
// text when compaction decides what to keep.
type intervention struct {
	verdict  verdict
	guidance string
	notice   *event.Event
	// stuckReason is what this signal contributes to a Goal structural-stuck
	// pause. Ordinary turns carry it and ignore it.
	stuckReason string
}

func (i intervention) fired() bool {
	return i.verdict != verdictContinue || i.guidance != "" || i.notice != nil
}

// applyInterventions folds a round's signals into one outcome: the strongest
// verdict, every notice, and all guidance appended to the round tail in signal
// order. Signals used to write that slot directly from outcomes[0].output, so
// two firing in the same round silently dropped the earlier one's guidance.
func (a *Agent) applyInterventions(results []string, outcomes []toolOutcome, ivs ...intervention) verdict {
	strongest := verdictContinue
	guidance := make([]string, 0, len(ivs))
	for _, iv := range ivs {
		if !iv.fired() {
			continue
		}
		if iv.verdict > strongest {
			strongest = iv.verdict
		}
		if iv.notice != nil {
			a.sink.Emit(*iv.notice)
		}
		if iv.guidance != "" {
			guidance = append(guidance, iv.guidance)
		}
	}
	if len(guidance) == 0 || len(results) == 0 || len(outcomes) == 0 {
		return strongest
	}
	results[0] = outcomes[0].output + "\n\n" + strings.Join(guidance, "\n\n")
	return strongest
}

// noticeFor builds the frontend notice a signal emits alongside its guidance.
func noticeFor(code string, level event.Level, text, detail string) *event.Event {
	return &event.Event{Kind: event.Notice, Level: level, Code: code, Text: text, Detail: detail}
}
