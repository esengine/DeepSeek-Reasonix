package control

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

func TestNoProgressQuotaByBudgetClass(t *testing.T) {
	cases := map[string]int{budgetClassResearch: 10, budgetClassWrite: 6, budgetClassSimple: defaultNoProgressLimit}
	for class, want := range cases {
		if got := noProgressQuota(class); got != want {
			t.Fatalf("noProgressQuota(%q) = %d, want %d", class, got, want)
		}
	}
	if got := resolvedNoProgressLimit(defaultNoProgressLimit, budgetClassResearch); got != 10 {
		t.Fatalf("legacy research limit = %d, want 10", got)
	}
	if got := resolvedNoProgressLimit(7, budgetClassWrite); got != 7 {
		t.Fatalf("custom write limit = %d, want 7", got)
	}
}

func TestMergeGoalProgressEvidenceIsNovelAndBounded(t *testing.T) {
	observed := make([]string, maxGoalProgressEvidence+25)
	for i := range observed {
		observed[i] = fmt.Sprintf("sig-%03d", i)
	}
	got, progressed := mergeGoalProgressEvidence([]string{"old"}, observed)
	if !progressed || len(got) != maxGoalProgressEvidence {
		t.Fatalf("merge = progressed:%v len:%d, want true/%d", progressed, len(got), maxGoalProgressEvidence)
	}
	if repeat, advanced := mergeGoalProgressEvidence(got, got); advanced || len(repeat) != maxGoalProgressEvidence {
		t.Fatalf("exact repeat = advanced:%v len:%d", advanced, len(repeat))
	}
}

// legacyTurnQuota reproduces the retired class-derived turn quota. Fixtures
// below only ever needed "some plausible number of turns"; the runtime no
// longer derives one.
func legacyTurnQuota(class string) int {
	switch class {
	case budgetClassResearch:
		return 40
	case budgetClassWrite:
		return 20
	default:
		return 10
	}
}

func TestTurnCountNeverPausesAndResumeClearsLegacyBudget(t *testing.T) {
	newMachine := func() *goalMachine {
		g := &goalMachine{goal: "fix the parser", status: GoalStatusRunning}
		g.budgetClass = budgetClassWrite
		g.turnsLimit = legacyTurnQuota(budgetClassWrite)
		g.noProgressLimit = noProgressQuota(g.budgetClass)
		return g
	}
	in := func(report *goalTurnReport, progress ...string) goalAdvanceInput {
		return goalAdvanceInput{report: report, progressEvidence: progress}
	}

	// Turn count is not a resource. A goal that keeps reporting progress runs
	// as long as it keeps making it; a ceiling now lives on spend, and only
	// when the user sets one.
	t.Run("turn count never pauses", func(t *testing.T) {
		g := newMachine()
		for i := range g.turnsLimit * 3 {
			res := g.advance(in(&goalTurnReport{status: GoalStatusRunning, reason: "keep going"}, "sig-"+fmt.Sprint(i)))
			if !res.cont {
				t.Fatalf("goal paused at turn %d on turn count alone", i+1)
			}
		}
		if g.status != GoalStatusRunning || g.stopCause != "" {
			t.Fatalf("machine = (%q, %q), want it still running", g.status, g.stopCause)
		}
	})

	t.Run("token usage never pauses", func(t *testing.T) {
		g := newMachine()
		g.tokensUsed = 900_000
		if res := g.advance(in(&goalTurnReport{status: GoalStatusRunning}, "new-evidence")); !res.cont {
			t.Fatal("token usage must not pause the goal")
		}
	})

	t.Run("no-progress is observational", func(t *testing.T) {
		g := newMachine()
		for range g.noProgressLimit {
			if res := g.advance(in(&goalTurnReport{status: GoalStatusRunning})); !res.cont {
				t.Fatalf("legacy 4/6/10 metadata paused Goal: %+v", g)
			}
		}
		if g.status != GoalStatusRunning || g.stopCause != "" || g.noProgressTurns != g.noProgressLimit {
			t.Fatalf("unexpected observational runtime: %+v", g)
		}
	})

	t.Run("host-verifiable progress resets no-progress", func(t *testing.T) {
		g := newMachine()
		for range g.noProgressLimit - 1 {
			g.advance(in(&goalTurnReport{status: GoalStatusRunning}))
		}
		if res := g.advance(in(&goalTurnReport{status: GoalStatusRunning}, "new-evidence")); !res.cont || g.noProgressTurns != 0 {
			t.Fatalf("progress did not reset observational streak: %+v", g)
		}
	})

	t.Run("exact repeated evidence does not reset no-progress", func(t *testing.T) {
		g := newMachine()
		g.advance(in(&goalTurnReport{status: GoalStatusRunning}, "same-read"))
		for range g.noProgressLimit {
			g.advance(in(&goalTurnReport{status: GoalStatusRunning}, "same-read"))
		}
		if g.status != GoalStatusRunning || g.stopCause != "" || g.noProgressTurns != g.noProgressLimit {
			t.Fatalf("repeated evidence changed runtime: %+v", g)
		}
	})

	t.Run("research accepts new read evidence but keeps a larger synthesis budget", func(t *testing.T) {
		g := &goalMachine{goal: "research", status: GoalStatusRunning, budgetClass: budgetClassResearch,
			turnsLimit: legacyTurnQuota(budgetClassResearch), noProgressLimit: noProgressQuota(budgetClassResearch)}
		for i := range 8 {
			if res := g.advance(in(&goalTurnReport{status: GoalStatusRunning}, "read-"+fmt.Sprint(i))); !res.cont {
				t.Fatalf("new research evidence paused at turn %d", i+1)
			}
		}
		if g.noProgressTurns != 0 || g.noProgressLimit != 10 {
			t.Fatalf("research runtime = no-progress %d/%d", g.noProgressTurns, g.noProgressLimit)
		}
	})

	// An old sidecar paused on the retired quota resumes by clearing it, not
	// by being granted more turns.
	t.Run("resume clears a legacy turn budget", func(t *testing.T) {
		g := newMachine()
		g.turnsUsed = g.turnsLimit
		g.pauseFor(stopCauseBudgetTurns, "turn budget exhausted", nil)
		_, _, _, resumed, extended := g.resume(nil)
		if !resumed || !extended || g.turnsLimit != 0 || g.budgetExtensions != 1 {
			t.Fatalf("legacy budget resume failed: %+v", g)
		}
		g.pauseFor(stopCauseManual, "user paused", nil)
		_, _, _, resumed, extended = g.resume(nil)
		if !resumed || extended {
			t.Fatalf("manual resume treated as a budget extension: %+v", g)
		}
	})
}

func TestWireContinueDoesNotKeepExactRepeatedEvidenceAlive(t *testing.T) {
	g := &goalMachine{goal: "repeat", status: GoalStatusRunning, budgetClass: budgetClassSimple,
		turnsLimit: legacyTurnQuota(budgetClassSimple), noProgressLimit: noProgressQuota(budgetClassSimple), scopeID: newGoalScopeID()}
	advance := func() goalAdvanceResult {
		epoch := g.continuationEpoch
		rec := g.newTurnRecorder(g.scopeID, epoch)
		if _, err := rec.RecordGoalReport(tool.GoalReport{Status: "continue", Reason: "same", NextAction: "repeat"}); err != nil {
			t.Fatal(err)
		}
		return g.advance(goalAdvanceInput{report: rec.validReport(epoch), progressEvidence: []string{"same-read"}, expectedEpoch: &epoch})
	}
	if res := advance(); !res.cont || g.noProgressTurns != 0 {
		t.Fatalf("first read = %+v runtime=%+v", res, g.runtimeView())
	}
	for range g.noProgressLimit {
		advance()
	}
	if g.status != GoalStatusRunning || g.stopCause != "" || g.noProgressTurns != g.noProgressLimit {
		t.Fatalf("wire continue changed runtime: %+v", g.runtimeView())
	}
}

func TestGoalLegacyNoProgressSidecarAutoResumesWithoutExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	state := goalState{Goal: "finish", Status: GoalStatusBlocked, StopCause: stopCauseNoProgress,
		Block: "no progress", BudgetClass: budgetClassWrite, TurnsUsed: 6, TurnsLimit: 20,
		NoProgressTurns: 6, NoProgressLimit: 6, RequestsUsed: 12}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SessionGoalState(path), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	g := &goalMachine{}
	_, data, migrated, _ := g.restoreFromState(path)
	if !migrated || g.status != GoalStatusRunning || g.stopCause != "" || g.block != "" {
		t.Fatalf("legacy pause did not migrate: %+v", g)
	}
	if g.turnsLimit != 20 || g.budgetExtensions != 0 || g.requestsUsed != 12 || g.noProgressTurns != 6 {
		t.Fatalf("migration changed counters: %+v", g.runtimeView())
	}
	var normalized goalState
	if err := json.Unmarshal(data, &normalized); err != nil || normalized.Status != GoalStatusRunning || normalized.RequestsUsed != 12 {
		t.Fatalf("normalized sidecar = %+v err=%v", normalized, err)
	}
}

func TestGoalSidecarPreservesUnknownFieldsAndOldReaderIgnoresRequests(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	raw := []byte(`{"goal":"ship","status":"running","turnsLimit":10,"requestsUsed":7,"futurePolicy":{"mode":"adaptive"}}`)
	if err := os.WriteFile(store.SessionGoalState(path), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	g := &goalMachine{}
	g.restoreFromState(path)
	g.mu.Lock()
	_, data, ok := g.buildStateLocked(nil)
	g.mu.Unlock()
	if !ok {
		t.Fatal("expected persisted state")
	}
	var roundTrip map[string]json.RawMessage
	if err := json.Unmarshal(data, &roundTrip); err != nil || string(roundTrip["futurePolicy"]) != `{"mode":"adaptive"}` {
		t.Fatalf("unknown field was lost: %s err=%v", data, err)
	}
	var oldReader struct {
		Goal       string `json:"goal"`
		TurnsLimit int    `json:"turnsLimit"`
	}
	if err := json.Unmarshal(data, &oldReader); err != nil || oldReader.Goal != "ship" || oldReader.TurnsLimit != 10 {
		t.Fatalf("old reader = %+v err=%v", oldReader, err)
	}
}

func TestGoalRunBoundariesPauseAfterTerminalDecisions(t *testing.T) {
	t.Run("continue pauses", func(t *testing.T) {
		g := &goalMachine{goal: "ship", status: GoalStatusRunning, turnsLimit: 10}
		res := g.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusRunning}, pauseCause: stopCauseGoalRunBudget, pauseReason: "16 rounds"})
		if res.cont || g.status != GoalStatusBlocked || g.stopCause != stopCauseGoalRunBudget {
			t.Fatalf("result=%+v runtime=%+v", res, g.runtimeView())
		}
		before := g.turnsLimit
		_, _, _, resumed, extended := g.resume(nil)
		if !resumed || extended || g.turnsLimit != before {
			t.Fatalf("resume extended outer budget: %+v", g)
		}
	})
	t.Run("completion wins", func(t *testing.T) {
		g := &goalMachine{goal: "ship", status: GoalStatusRunning, turnsLimit: 10}
		res := g.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusComplete}, readiness: agent.ReadinessResult{Ready: true}, pauseCause: stopCauseGoalStuck})
		if res.notice != goalCompleteNotice || g.status != GoalStatusComplete || g.stopCause != "" {
			t.Fatalf("result=%+v runtime=%+v", res, g.runtimeView())
		}
	})
}

func TestGoalProgressEvidenceRestoresAsBoundedNoveltyState(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	state := goalState{Goal: "research", Status: GoalStatusRunning, BudgetClass: budgetClassResearch,
		TurnsLimit: legacyTurnQuota(budgetClassResearch), NoProgressTurns: 2, NoProgressLimit: defaultNoProgressLimit,
		ProgressEvidence: []string{"read-a", "read-a", "read-b", strings.Repeat("x", 129)}}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeGoalStateData(goalStatePath(sessionPath), raw); err != nil {
		t.Fatal(err)
	}
	g := &goalMachine{}
	_, data, migrated, _ := g.restoreFromState(sessionPath)
	if !migrated || g.noProgressLimit != 10 || len(g.progressEvidence) != 2 {
		t.Fatalf("restore failed: migrated=%v runtime=%+v", migrated, g)
	}
	var normalized goalState
	if err := json.Unmarshal(data, &normalized); err != nil || len(normalized.ProgressEvidence) != 2 {
		t.Fatalf("normalized sidecar = %+v err=%v", normalized, err)
	}
	g.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusRunning}, progressEvidence: []string{"read-a"}})
	if g.noProgressTurns != 3 {
		t.Fatalf("restored repeat reset streak to %d", g.noProgressTurns)
	}
	g.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusRunning}, progressEvidence: []string{"read-c"}})
	if g.noProgressTurns != 0 {
		t.Fatalf("new evidence left streak at %d", g.noProgressTurns)
	}
}
