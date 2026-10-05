package config

import "strings"

// Continuation is how an endpoint carries a turn's context into the next one.
// The zero value defers to the provider's own vendor detection, which is what
// an entry nobody has characterised must keep doing.
type Continuation string

const (
	// ContinuationAuto leaves the answer to vendor detection.
	ContinuationAuto Continuation = ""
	// ContinuationStateful sends a reference to the previous response and only
	// the new turn, which is what keeps a prefix cache warm.
	ContinuationStateful Continuation = "stateful"
	// ContinuationStateless replays the whole history every turn. Relays that
	// answer the Responses protocol without implementing its stored state need
	// this, and they reject the reference rather than ignoring it.
	ContinuationStateless Continuation = "stateless"
)

// ParseContinuation resolves a caller-supplied value. Unknown spellings are
// refused rather than folded into auto: a request naming a mode nobody serves
// is a caller to correct, not an intent to guess at.
func ParseContinuation(s string) (Continuation, bool) {
	switch Continuation(strings.ToLower(strings.TrimSpace(s))) {
	case ContinuationAuto:
		return ContinuationAuto, true
	case ContinuationStateful:
		return ContinuationStateful, true
	case ContinuationStateless:
		return ContinuationStateless, true
	}
	return ContinuationAuto, false
}

// CanConfigureContinuation reports whether the choice exists for this entry,
// which the protocol catalog decides — the same way the thinking-parameter pin
// asks it.
func CanConfigureContinuation(e *ProviderEntry) bool {
	if e == nil {
		return false
	}
	p, ok := ProtocolFor(e.Kind)
	return ok && p.StatefulContinuation
}

// ContinuationOf reports the recorded choice. The legacy boolean answers only
// where no mode was written, which is the precedence the provider itself
// applies — stated once here so a panel and a request cannot disagree.
func ContinuationOf(e *ProviderEntry) Continuation {
	if e == nil {
		return ContinuationAuto
	}
	if mode, ok := ParseContinuation(e.ResponsesMode); ok && mode != ContinuationAuto {
		return mode
	}
	if e.ResponsesStateful != nil {
		if *e.ResponsesStateful {
			return ContinuationStateful
		}
		return ContinuationStateless
	}
	return ContinuationAuto
}

// SetContinuation records the choice. Returning to auto clears the legacy
// boolean with it: leaving that behind would answer for an entry the user just
// asked to stop answering for.
func SetContinuation(e *ProviderEntry, mode Continuation) {
	if e == nil {
		return
	}
	e.ResponsesStateful = nil
	if mode == ContinuationAuto {
		e.ResponsesMode = ""
		return
	}
	e.ResponsesMode = string(mode)
}
