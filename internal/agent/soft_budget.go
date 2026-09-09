package agent

import (
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/i18n"
)

const (
	readonlySoftBudgetRounds = 10
	softBudgetHardFollowup   = 2
	softBudgetHistorySamples = 5
	softBudgetHistoryWindow  = 21
)

var readonlySoftBudgetHistory = struct {
	sync.Mutex
	byKey map[string][]time.Duration
}{byKey: map[string][]time.Duration{}}

func (a *Agent) applySoftBudget(outcomes []toolOutcome) intervention {
	if a == nil {
		return intervention{}
	}
	limit := a.task.budget.limit
	if limit.Tokens > 0 || limit.Wall > 0 || limit.Cost > 0 {
		return intervention{}
	}
	if !a.readonlySoftBudgetApplies(outcomes) {
		return intervention{}
	}
	rounds := a.turn.budget.rounds
	elapsed := a.turn.budget.elapsed()
	median := rollingReadonlySoftBudgetMedian(a.softBudgetHistoryKey())
	timeExceeded := median > 0 && elapsed >= 2*median
	if rounds < readonlySoftBudgetRounds && !timeExceeded {
		return intervention{}
	}
	if a.turn.loop.markSoftBudgetNudged(rounds) {
		if a.capabilityAudit != nil {
			a.capabilityAudit.RecordLoopGuard("soft_budget")
		}
		trigger := fmt.Sprintf("%d main-model rounds", rounds)
		if timeExceeded {
			trigger = fmt.Sprintf("%s elapsed (2x rolling median %s)", elapsed.Round(time.Second), median.Round(time.Second))
		}
		return intervention{
			verdict:  verdictRedirect,
			guidance: "Host budget check: this read-only planning/analysis task exceeded its soft round or elapsed-time budget. Stop expanding scope. Summarize the evidence you already have, state the remaining real blocker if any, and do not open new exploration paths unless the user asks.",
			notice:   noticeFor(event.NoticeCodeLoopGuard, event.LevelInfo, i18n.M.SoftBudgetConverge, "soft budget after "+trigger),
		}
	}
	if nudgedAt := a.turn.loop.softBudgetNudgedAt(); nudgedAt > 0 && rounds >= nudgedAt+softBudgetHardFollowup {
		return intervention{
			verdict:  verdictRedirect,
			guidance: "Host budget check: two further rounds passed after the convergence nudge. Output the current result or name exactly one real blocker. Do not continue exploring.",
		}
	}
	return intervention{}
}

func (a *Agent) readonlySoftBudgetApplies(outcomes []toolOutcome) bool {
	if a.planMode.Load() {
		return true
	}
	for _, outcome := range outcomes {
		if outcome.workspaceMutation != nil || (outcome.resolved && !outcome.resolvedReadOnly) {
			a.turn.softBudgetMutation = true
			return false
		}
	}
	if a.task.ledger == nil {
		return true
	}
	for _, rec := range a.task.ledger.Receipts() {
		if rec.Write {
			a.turn.softBudgetMutation = true
			return false
		}
	}
	return true
}

func (a *Agent) softBudgetHistoryKey() string {
	if a == nil {
		return ""
	}
	kind := "analysis"
	if a.planMode.Load() {
		kind = "plan"
	} else if a.readOnlyExecution {
		kind = "read_only_agent"
	}
	return strings.TrimSpace(a.modelRef) + "|" + kind
}

func recordReadonlySoftBudgetDuration(key string, duration time.Duration) {
	if key == "" || duration <= 0 {
		return
	}
	readonlySoftBudgetHistory.Lock()
	defer readonlySoftBudgetHistory.Unlock()
	values := append(readonlySoftBudgetHistory.byKey[key], duration)
	if len(values) > softBudgetHistoryWindow {
		values = append([]time.Duration(nil), values[len(values)-softBudgetHistoryWindow:]...)
	}
	readonlySoftBudgetHistory.byKey[key] = values
}

func rollingReadonlySoftBudgetMedian(key string) time.Duration {
	readonlySoftBudgetHistory.Lock()
	values := append([]time.Duration(nil), readonlySoftBudgetHistory.byKey[key]...)
	readonlySoftBudgetHistory.Unlock()
	if len(values) < softBudgetHistorySamples {
		return 0
	}
	slices.Sort(values)
	return values[len(values)/2]
}

func (a *Agent) recordReadonlySoftBudgetSample(state *turnRuntime, runErr error) {
	if a == nil || state == nil || runErr != nil || state.softBudgetMutation || !state.usedAnyTool || state.budget.rounds == 0 {
		return
	}
	limit := a.task.budget.limit
	if limit.Tokens > 0 || limit.Wall > 0 || limit.Cost > 0 {
		return
	}
	recordReadonlySoftBudgetDuration(a.softBudgetHistoryKey(), state.budget.elapsed())
}
