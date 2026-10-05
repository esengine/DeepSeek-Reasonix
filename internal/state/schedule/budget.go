package schedule

import (
	"fmt"
	"time"
)

const (
	Day  = 24 * time.Hour
	Week = 7 * Day
)

// Outcome is what a supervisor reports when a run ends. Clean means the usage
// stream ran to the end, so Observed is the whole extent; anything else (a
// kill, a crash, a cut stream) leaves the extent unknown.
type Outcome struct {
	State    RunState
	Observed int64
	Clean    bool
}

// ChargeFor is the tokens a settled run counts against every window. An
// unknown extent is charged at the full per-run cap. Observed usage above the
// cap is charged as observed, so the window never reads lower than what was seen.
func ChargeFor(perRunCap int64, o Outcome) int64 {
	if !o.Clean || o.State == RunInterrupted {
		return max(perRunCap, o.Observed)
	}
	return max(o.Observed, 0)
}

type usage struct {
	runs   int64
	tokens int64
}

func windowUsage(runs []Run, now time.Time, span time.Duration, scheduleID string) usage {
	var u usage
	from := now.Add(-span)
	for _, r := range runs {
		if r.State == RunSkipped || r.ClaimedAt.Before(from) {
			continue
		}
		if scheduleID != "" && r.ScheduleID != scheduleID {
			continue
		}
		u.runs++
		u.tokens += r.Charged
	}
	return u
}

// Admit reserves the schedule's full per-run cap and checks that every window
// still fits with it counted. The same rule serves the ticker and a manual
// tick, so neither can start what the other would refuse.
func Admit(p Policy, m Manifest, s Schedule, now time.Time) error {
	if m.PausedAll {
		return fmt.Errorf("%w (%s)", ErrPausedAll, m.PausedReason)
	}
	switch s.Status {
	case StatusActive:
	case StatusExpired:
		return ErrExpired
	default:
		return fmt.Errorf("%w: %s", ErrNotActive, s.Status)
	}
	if !now.Before(s.ExpiresAt) {
		return ErrExpired
	}
	if err := p.fits(s.Budget); err != nil {
		return fmt.Errorf("%w: %w: %w", ErrBudget, ErrPolicyTightened, err)
	}
	if s.Trigger.Kind == TriggerEvery && time.Duration(s.Trigger.EverySec)*time.Second < p.MinInterval() {
		return fmt.Errorf("%w: %w: %ds < %s", ErrIntervalBelowFloor, ErrPolicyTightened, s.Trigger.EverySec, p.MinInterval())
	}
	for _, r := range m.Runs {
		if r.inFlight() {
			return ErrConcurrency
		}
	}
	reserve := s.Budget.PerRunTokens
	own := windowUsage(m.Runs, now, Day, s.ID)
	all := windowUsage(m.Runs, now, Day, "")
	week := windowUsage(m.Runs, now, Week, "")
	switch {
	case own.runs+1 > s.Budget.MaxRunsPerDay:
		return fmt.Errorf("%w: schedule run count for 24h", ErrBudget)
	case all.runs+1 > p.MaxRunsGlobalDay:
		return fmt.Errorf("%w: global run count for 24h", ErrBudget)
	case all.tokens+reserve > p.GlobalTokensDay:
		return fmt.Errorf("%w: global tokens for 24h", ErrBudget)
	case week.tokens+reserve > p.GlobalTokensWeek:
		return fmt.Errorf("%w: global tokens for 7d", ErrBudget)
	case s.SpentLifetimeTokens+reserve > s.Budget.LifetimeTokens:
		return fmt.Errorf("%w: schedule lifetime tokens", ErrBudget)
	}
	return nil
}
