package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// Tool-result bounding, used for a call's first-visible output and for the
// temporary transcript fed to the summarizer. The model-visible projection is
// never rewritten by these helpers.
const (
	snippedMarker = "[snipped tool result — "
	minPruneBytes = 1024
)

func snipToolResult(m provider.Message, archive string, strategy snipStrategy) string {
	if archive == "" {
		archive = "the canonical transcript"
	}
	lines := strings.Split(m.Content, "\n")
	if len(lines) <= strategy.head+strategy.tail {
		headChars := minInt(strategy.headChars, len(m.Content)/2)
		tailChars := minInt(strategy.tailChars, len(m.Content)/4)
		return fmt.Sprintf("%s%s, %d bytes; full original retained in %s; single large line truncated]\n%s\n[... %d bytes omitted ...]\n%s",
			snippedMarker, m.Name, len(m.Content), archive,
			firstRunes(m.Content, headChars),
			omittedBytes(m.Content, headChars, tailChars),
			lastRunes(m.Content, tailChars))
	}
	head := strings.Join(lines[:strategy.head], "\n")
	tail := strings.Join(lines[len(lines)-strategy.tail:], "\n")
	return fmt.Sprintf("%s%s, %d bytes; full original retained in %s; showing first %d lines and last %d lines]\n%s\n[... %d lines omitted ...]\n%s",
		snippedMarker, m.Name, len(m.Content), archive, strategy.head, strategy.tail,
		head, len(lines)-strategy.head-strategy.tail, tail)
}

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

func firstRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneBoundary(s, n) {
		n--
	}
	return s[:n]
}

func lastRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !isRuneBoundary(s, start) {
		start++
	}
	return s[start:]
}

func omittedBytes(s string, head, tail int) int {
	omitted := len(s) - head - tail
	if omitted < 0 {
		return 0
	}
	return omitted
}

func isRuneBoundary(s string, i int) bool {
	return i == 0 || i == len(s) || (i > 0 && i < len(s) && (s[i]&0xc0) != 0x80)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
