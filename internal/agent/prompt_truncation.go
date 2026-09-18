package agent

import (
	"fmt"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

const (
	// truncationMinChars keeps short requests out of the detector: their token
	// counts round too coarsely to demonstrate a ceiling.
	truncationMinChars = 8192
	// truncationMinCeiling is the smallest prompt size that can be a real
	// context window. Below it the provider is reporting meaningless usage, not
	// serving a small window, and nothing should be inferred from the number.
	truncationMinCeiling = 512
	// truncationDensityFloor is the tokens-per-character density below which no
	// tokenizer operates. calibratedPromptTokens already refuses to learn from
	// an observation this dense; read as evidence, it names why.
	truncationDensityFloor = 0.05
	// truncationCalibratedDrop is the share of an established density below
	// which an observation cannot be the same tokenizer on the same wire.
	truncationCalibratedDrop = 0.5
)

// promptTruncation is one provider that accepted a request and silently dropped
// part of the prompt: the ceiling it appears to enforce, and whether the session
// has been told. Grouped behind a single atomic pointer so the guarded state
// gains no scalar.
type promptTruncation struct {
	promptCeiling int
	notified      bool
}

// promptTruncationCeiling reports the prompt ceiling a provider appears to have
// applied, or 0 when the reported size is consistent with the request that was
// sent.
//
// A truncating server answers HTTP 200 with no error field, so the only evidence
// is arithmetic: it reports far fewer prompt tokens than the characters on the
// wire can encode. Cache fields never deflate the count — CacheHitTokens is a
// subset of PromptTokens, not a deduction from it.
func promptTruncationCeiling(promptTokens int, shape requestCalibrationShape, cal *promptTokenCalibration) int {
	if promptTokens < truncationMinCeiling || shape.requestChars < truncationMinChars {
		return 0
	}
	density := float64(promptTokens) / float64(shape.requestChars)
	// A calibration this model established is the sharper comparison, but only
	// while it is itself plausible; otherwise fall through to the absolute floor
	// rather than letting a bad calibration suppress detection entirely.
	if cal != nil && cal.requestChars > 0 && cal.promptTokens > 0 {
		if known := float64(cal.promptTokens) / float64(cal.requestChars); known > truncationDensityFloor {
			if density < known*truncationCalibratedDrop {
				return promptTokens
			}
			return 0
		}
	}
	if density < truncationDensityFloor {
		return promptTokens
	}
	return 0
}

// notePromptTruncation warns once per session, on the turn the truncation
// happens: the turn whose answer the user is about to read is the one the
// explanation belongs to.
//
// It deliberately does not clamp the window to the ceiling. Feeding it to
// learnContextBudget makes every later prompt that cannot fit fail admission
// outright, turning a session that degrades into one that stops — a much larger
// behavior change than detection, and one that needs its own evidence. Warning
// without clamping also keeps the cost of a wrong reading at one line of text.
func (a *Agent) notePromptTruncation(promptCeiling int) {
	if a == nil || promptCeiling < truncationMinCeiling {
		return
	}
	// Claim the warning with CAS: concurrent turns share this state, and the
	// promise is one warning per session, not one per racing turn.
	prev := a.sess.output.truncation.Load()
	if prev != nil && prev.notified {
		return
	}
	claimed := &promptTruncation{promptCeiling: promptCeiling, notified: true}
	if !a.sess.output.truncation.CompareAndSwap(prev, claimed) {
		return
	}
	if a.svc.sink == nil {
		return
	}
	a.svc.sink.Emit(event.Event{
		Kind:   event.Notice,
		Code:   event.NoticeCodePromptTruncatedByServer,
		Level:  event.LevelWarn,
		Text:   i18n.M.PromptTruncatedByServerNotice,
		Detail: fmt.Sprintf("accepted_prompt_tokens=%d", promptCeiling),
	})
}
