package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// Tool-result helpers serve first-visible bounding and summary fold input.
// Automatic prune/snip projections are gone; the public APIs are no-ops.
const (
	snippedMarker = "[snipped tool result — "
	prunedMarker  = "[elided tool result — "
	minPruneBytes = 1024
)

type toolResultMaintenanceMode int

const (
	toolResultSnip toolResultMaintenanceMode = iota
	toolResultPrune
)

// PruneStats reports one maintenance pass.
type PruneStats struct {
	Results    int
	SavedChars int
	Archive    string
	Mode       toolResultMaintenanceMode
	InputHash  string
	Force      bool
}

// SnipStaleToolResults is a no-op: automatic prune/snip projections are gone.
func (a *Agent) SnipStaleToolResults() (PruneStats, error) {
	return PruneStats{Mode: toolResultSnip}, nil
}

// PruneStaleToolResults is a no-op: automatic prune/snip projections are gone.
func (a *Agent) PruneStaleToolResults() (PruneStats, error) {
	return PruneStats{Mode: toolResultPrune}, nil
}

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

func (a *Agent) snipStrategyFor(name string) snipStrategy {
	if a.tools != nil {
		if t, ok := a.tools.Get(name); ok {
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
