package agent

import "reasonix/internal/contract/tool"

type snipStrategy struct {
	head      int
	tail      int
	headChars int
	tailChars int
}

var (
	defaultReadOnlySnip      = snipStrategy{head: 80, tail: 12, headChars: 10000, tailChars: 2000}
	defaultSideEffectingSnip = snipStrategy{head: 40, tail: 40, headChars: 8000, tailChars: 8000}
)

func (a *contextWindow) snipStrategyFor(name string) snipStrategy {
	if a.svc.tools != nil {
		if t, ok := a.svc.tools.Get(name); ok {
			if h, ok := t.(tool.SnipHinter); ok {
				return snipStrategyFromHint(h.SnipHint())
			}
			if t.ReadOnly() {
				return defaultReadOnlySnip
			}
			return defaultSideEffectingSnip
		}
	}
	return defaultReadOnlySnip
}

func snipStrategyFromHint(h tool.SnipHint) snipStrategy {
	return snipStrategy{head: h.Head, tail: h.Tail, headChars: h.HeadChars, tailChars: h.TailChars}
}

// forFailure reweights a geometry toward the tail. A failed call's answer is at
// its end — the diagnostic, the stack, the exit status — whatever shape the
// tool carries when it succeeds. The caller knows the call failed; reading that
// back out of the body would be guessing at wording.
func (s snipStrategy) forFailure(cap int) snipStrategy {
	s.tailChars = max(s.tailChars, cap/3)
	if s.headChars+s.tailChars > cap-512 {
		s.headChars = cap - 512 - s.tailChars
	}
	return s
}
