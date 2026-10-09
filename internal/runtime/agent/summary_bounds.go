package agent

import (
	"fmt"
	"time"

	"reasonix/internal/contract/provider"
)

// SummaryIdleTimeout is how long a summary request may produce nothing before it
// is read as stalled. It sits one minute past provider.StreamIdleTimeout so a
// provider's own watchdog, which names its idle_timeout_seconds override, speaks
// first; a local model prefilling a long transcript is silent for minutes.
const SummaryIdleTimeout = provider.StreamIdleTimeout + time.Minute

// SummaryCeiling bounds one summary request however steadily it streams, so a
// model trickling a token a minute cannot hold the turn. Thirty minutes lets a
// 16K-token digest finish at about 9 tokens/s, far below any usable local model.
const SummaryCeiling = 30 * time.Minute

// SummaryLimits are the bounds one summary request runs under.
type SummaryLimits struct{ Idle, Ceiling time.Duration }

// SummaryBounds is what a summary request is held to; a test shortens it to run
// in milliseconds and restores it, and nothing else writes it.
var SummaryBounds = SummaryLimits{Idle: SummaryIdleTimeout, Ceiling: SummaryCeiling}

func summaryStalled(idle time.Duration) error {
	return fmt.Errorf("%w: no output for %s", errSummaryTimeout, idle)
}

func summaryCeilingHit(ceiling time.Duration) error {
	return fmt.Errorf("%w: still running after %s", errSummaryCeiling, ceiling)
}
