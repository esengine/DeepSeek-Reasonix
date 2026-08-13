package control

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/evidence"
	"reasonix/internal/store"
	"reasonix/internal/tool"
)

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

func TestGoalTurnsAndNoProgressAreObservationalOnly(t *testing.T) {
	for _, class := range []string{budgetClassSimple, budgetClassWrite, budgetClassResearch} {
		t.Run(class, func(t *testing.T) {
			g := &goalMachine{goal: "keep working", status: GoalStatusRunning, budgetClass: class, turnsLimit: unlimitedGoalTurns}
			for i := range 101 {
				res := g.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusRunning, reason: "continue"}})
				if !res.cont || g.status != GoalStatusRunning || g.stopCause != "" {
					t.Fatalf("Goal paused at turn %d: result=%+v runtime=%+v", i+1, res, g.runtimeView())
				}
			}
			if g.turnsUsed != 101 || g.noProgressTurns != 101 || g.runtimeView().TurnsLimit != 0 || g.runtimeView().NoProgressLimit != 0 {
				t.Fatalf("observational counters = %+v", g.runtimeView())
			}
		})
	}
}

func TestGoalResumeNeverExtendsNumericQuota(t *testing.T) {
	g := &goalMachine{goal: "ship", status: GoalStatusBlocked, stopCause: stopCauseManual, turnsLimit: 20, budgetExtensions: 3}
	_, _, _, resumed := g.resume(nil)
	if !resumed || g.status != GoalStatusRunning || g.turnsLimit != unlimitedGoalTurns {
		t.Fatalf("resume = resumed:%v machine:%+v", resumed, g)
	}
	view := g.runtimeView()
	if view.TurnsLimit != 0 || view.BudgetExtensions != 0 {
		t.Fatalf("deprecated public fields = %+v", view)
	}
}

func TestWireContinueWithRepeatedEvidenceKeepsRunning(t *testing.T) {
	g := &goalMachine{goal: "repeat", status: GoalStatusRunning, turnsLimit: unlimitedGoalTurns, scopeID: newGoalScopeID()}
	for i := range 100 {
		epoch := g.continuationEpoch
		rec := g.newTurnRecorder(g.scopeID, epoch)
		if _, err := rec.RecordGoalReport(tool.GoalReport{Status: "continue", Reason: "same", NextAction: "repeat"}); err != nil {
			t.Fatal(err)
		}
		res := g.advance(goalAdvanceInput{report: rec.validReport(epoch), progressEvidence: []string{"same-read"}, expectedEpoch: &epoch})
		if !res.cont {
			t.Fatalf("wire continue paused at %d: %+v", i+1, res)
		}
	}
	if g.status != GoalStatusRunning || g.stopCause != "" || g.noProgressTurns != 99 {
		t.Fatalf("wire continue runtime: %+v", g.runtimeView())
	}
}

func TestRemovedNumericPauseSidecarsMigrateToRunning(t *testing.T) {
	causes := []string{stopCauseBudgetTurns, stopCauseBudgetTokens, stopCauseGoalRunBudget, stopCauseGoalStuck, stopCauseNoProgress}
	for _, cause := range causes {
		t.Run(cause, func(t *testing.T) {
			sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
			state := goalState{
				Goal: "finish", Status: GoalStatusBlocked, StopCause: cause, Block: "numeric pause",
				BudgetClass: budgetClassWrite, TurnsUsed: 57, TurnsLimit: 20, TokensUsed: 2800,
				RequestsUsed: 143, WorkDurationMs: 42_000, NoProgressTurns: 6, NoProgressLimit: 6,
				BudgetExtensions: 2, Todos: []evidence.TodoItem{{Content: "keep", Status: "in_progress"}},
			}
			raw, err := marshalGoalState(state, map[string]json.RawMessage{"futurePolicy": json.RawMessage(`{"mode":"adaptive"}`)})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.SessionGoalState(sessionPath), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			g := &goalMachine{}
			_, data, migrated, _ := g.restoreFromState(sessionPath)
			if !migrated || g.status != GoalStatusRunning || g.stopCause != "" || g.block != "" {
				t.Fatalf("legacy pause did not migrate: %+v", g)
			}
			if g.turnsLimit != unlimitedGoalTurns || g.requestsUsed != 143 || g.workDurationMs != 42_000 || g.budgetExtensions != 0 {
				t.Fatalf("migration changed history: %+v", g.runtimeView())
			}
			var normalized map[string]json.RawMessage
			if err := json.Unmarshal(data, &normalized); err != nil {
				t.Fatal(err)
			}
			if string(normalized["turnsLimit"]) != "-1" || string(normalized["futurePolicy"]) != `{"mode":"adaptive"}` {
				t.Fatalf("normalized sidecar lost downgrade fence/unknown fields: %s", data)
			}
		})
	}
}

func TestGoalSidecarUnlimitedSentinelIsSafeForOldReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	raw := []byte(`{"goal":"ship","status":"running","turnsUsed":57,"turnsLimit":10,"requestsUsed":7,"futurePolicy":{"mode":"adaptive"}}`)
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
		TurnsUsed  int `json:"turnsUsed"`
		TurnsLimit int `json:"turnsLimit"`
	}
	if err := json.Unmarshal(data, &oldReader); err != nil || oldReader.TurnsLimit != unlimitedGoalTurns {
		t.Fatalf("old reader = %+v err=%v", oldReader, err)
	}
	if oldReader.TurnsLimit > 0 && oldReader.TurnsUsed >= oldReader.TurnsLimit {
		t.Fatal("old reader would incorrectly exhaust the unlimited sentinel")
	}
}

func TestGoalNumericPauseMigrationWriteFailureRollsBackMemory(t *testing.T) {
	dir := t.TempDir()
	sessionPath := filepath.Join(dir, "session.jsonl")
	raw := []byte(`{"goal":"ship","status":"blocked","stopCause":"goal_stuck","block":"old numeric pause","turnsUsed":12,"turnsLimit":20}`)
	if err := os.WriteFile(goalStatePath(sessionPath), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	blockingParent := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockingParent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	g := &goalMachine{}
	g.setStatePath(filepath.Join(blockingParent, "goal.json"))
	if _, _, migrated, _ := g.restoreFromState(sessionPath); migrated {
		t.Fatal("failed normalization must not report a committed migration")
	}
	if g.status != GoalStatusBlocked || g.stopCause != stopCauseGoalStuck || g.block != "old numeric pause" || g.turnsLimit != 20 {
		t.Fatalf("in-memory state was left half-migrated: %+v", g)
	}
	onDisk, err := os.ReadFile(goalStatePath(sessionPath))
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(raw) {
		t.Fatalf("failed atomic migration changed original sidecar: %s", onDisk)
	}
}

func TestGoalCompletionAndRealBlockedStillTerminate(t *testing.T) {
	complete := &goalMachine{goal: "ship", status: GoalStatusRunning, turnsLimit: unlimitedGoalTurns}
	res := complete.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusComplete}, readiness: agent.ReadinessResult{Ready: true}})
	if res.notice != goalCompleteNotice || complete.status != GoalStatusComplete {
		t.Fatalf("complete result=%+v runtime=%+v", res, complete.runtimeView())
	}

	blocked := &goalMachine{goal: "ship", status: GoalStatusRunning, turnsLimit: unlimitedGoalTurns}
	res = blocked.advance(goalAdvanceInput{report: &goalTurnReport{status: GoalStatusBlocked, reason: "need user credentials"}})
	if res.cont || blocked.status != GoalStatusBlocked || blocked.stopCause != "" {
		t.Fatalf("blocked result=%+v runtime=%+v", res, blocked.runtimeView())
	}
}

func TestGoalProgressEvidenceRestoresAsBoundedNoveltyState(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	state := goalState{Goal: "research", Status: GoalStatusRunning, BudgetClass: budgetClassResearch,
		TurnsLimit: 40, NoProgressTurns: 2, NoProgressLimit: 10,
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
	if !migrated || g.turnsLimit != unlimitedGoalTurns || g.noProgressLimit != 0 || len(g.progressEvidence) != 2 {
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
