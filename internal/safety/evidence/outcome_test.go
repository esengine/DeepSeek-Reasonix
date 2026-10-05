package evidence

import (
	"encoding/json"
	"reasonix/internal/contract/hostaudit"
	"testing"
)

func TestOutcomeTrackerSeparatesExplorationFromObjective(t *testing.T) {
	tr := NewOutcomeTracker()

	read := readReceipt("a.go")
	read.OutputBytes = 10
	s := tr.ScoreRound([]Receipt{read})
	if s.Exploration != 1 || s.Objective != 0 {
		t.Fatalf("new read = %+v, want exploration 1 objective 0", s)
	}

	// First verification failure: an attempt plus localization, no objective.
	s = tr.ScoreRound([]Receipt{bashReceipt("go test ./x", false)})
	if s.Verification != 1 || s.Exploration != 1 || s.Objective != 0 {
		t.Fatalf("first failing verify = %+v, want verification 1 exploration 1", s)
	}

	// A mutation is churn, not objective progress — the legacy scorer disagrees.
	write := ReceiptFromToolCall("write_file", json.RawMessage(`{"path":"b.go","content":"x"}`), true, ToolFacts{WritesNamedPaths: true})
	s = tr.ScoreRound([]Receipt{write})
	if s.Churn != 1 || s.Objective != 0 || s.Exploration != 0 {
		t.Fatalf("mutation = %+v, want churn 1 only", s)
	}
	if s.LegacyGain != gainMutation {
		t.Fatalf("mutation legacy gain = %d, want %d", s.LegacyGain, gainMutation)
	}

	// The failing verification turning green is the objective transition.
	s = tr.ScoreRound([]Receipt{bashReceipt("go test ./x", true)})
	if s.Objective != 1 || s.Verification != 1 || s.Regression != 0 {
		t.Fatalf("fail→pass verify = %+v, want objective 1", s)
	}

	// The same verification breaking again is a regression.
	s = tr.ScoreRound([]Receipt{bashReceipt("go test ./x", false)})
	if s.Regression != 1 || s.Objective != 0 {
		t.Fatalf("pass→fail verify = %+v, want regression 1", s)
	}
}

func TestOutcomeTrackerDelegationAndRepeatsAreExplorationAtBest(t *testing.T) {
	tr := NewOutcomeTracker()

	task := ReceiptFromToolCall("task", json.RawMessage(`{"prompt":"dig"}`), true, ToolFacts{})
	s := tr.ScoreRound([]Receipt{task})
	if s.Exploration != 1 || s.Objective != 0 {
		t.Fatalf("delegation = %+v, want exploration 1 objective 0", s)
	}

	// A first passing verification run establishes a baseline, not progress.
	s = tr.ScoreRound([]Receipt{bashReceipt("go vet ./...", true)})
	if s.Verification != 1 || s.Objective != 0 || s.Exploration != 0 {
		t.Fatalf("baseline verify = %+v, want verification 1 only", s)
	}
	s = tr.ScoreRound([]Receipt{bashReceipt("go vet ./...", true)})
	if s.Verification != 1 || s.Objective != 0 {
		t.Fatalf("repeated passing verify = %+v, want no objective", s)
	}

	// A repeat delegation still returned content the host cannot judge — it
	// stays exploration and can never move the objective dimension.
	repeat := ReceiptFromToolCall("task", json.RawMessage(`{"prompt":"dig"}`), true, ToolFacts{})
	s = tr.ScoreRound([]Receipt{repeat})
	if s.Exploration != 1 || s.Objective != 0 {
		t.Fatalf("repeated delegation = %+v, want exploration 1 objective 0", s)
	}

	var nilTracker *OutcomeTracker
	if got := nilTracker.ScoreRound([]Receipt{task}); got != (hostaudit.OutcomeSample{}) {
		t.Fatalf("nil tracker sample = %+v, want zero", got)
	}
}

func TestOutcomeTrackerVerificationDebtLifecycle(t *testing.T) {
	tr := NewOutcomeTracker()

	// A mutation opens debt; silent rounds age it.
	write := ReceiptFromToolCall("write_file", json.RawMessage(`{"path":"pkg/repro.py","content":"x"}`), true, ToolFacts{WritesNamedPaths: true})
	if s := tr.ScoreRound([]Receipt{write}); s.DebtAge != 1 || s.Discriminating != 0 {
		t.Fatalf("mutation round = %+v, want debt age 1", s)
	}
	read := readReceipt("other.go")
	read.OutputBytes = 5
	if s := tr.ScoreRound([]Receipt{read}); s.DebtAge != 2 {
		t.Fatalf("silent round = %+v, want debt age 2", s)
	}
	// An unrelated command does not discriminate.
	if s := tr.ScoreRound([]Receipt{bashReceipt("ls -la", true)}); s.DebtAge != 3 || s.Discriminating != 0 {
		t.Fatalf("unrelated command = %+v, want debt age 3", s)
	}
	// Reading the mutated file is inspection, not discrimination: debt ages on.
	if s := tr.ScoreRound([]Receipt{bashReceipt("cat pkg/repro.py", true)}); s.Discriminating != 0 || s.DebtAge != 4 {
		t.Fatalf("read-only inspection = %+v, want no discrimination, debt age 4", s)
	}
	// A second mutation raises the blind count; the counter tracks mutations,
	// not rounds.
	if s := tr.ScoreRound([]Receipt{ReceiptFromToolCall("write_file", json.RawMessage(`{"path":"pkg/b.py","content":"y"}`), true, ToolFacts{WritesNamedPaths: true})}); s.BlindMutations != 2 {
		t.Fatalf("second mutation = %+v, want blind 2", s)
	}
	// Running the mutated file is a discriminating observation even though it
	// is not delivery verification: debt and the blind count settle.
	if s := tr.ScoreRound([]Receipt{bashReceipt("python3 pkg/repro.py", false)}); s.Discriminating != 1 || s.DebtAge != 0 || s.BlindMutations != 0 {
		t.Fatalf("repro run = %+v, want discriminating 1, debt and blind settled", s)
	}
	// Debt stays settled until the next mutation; delivery verification also
	// counts as discriminating without any mutated-path match.
	if s := tr.ScoreRound([]Receipt{bashReceipt("go test ./pkg", true)}); s.Discriminating != 1 || s.DebtAge != 0 {
		t.Fatalf("verification round = %+v, want discriminating 1, no debt", s)
	}
}

// TestOutcomeObjectiveSurvivesOutputTrimming pins the transition an agent
// actually produces: it never re-runs a verification byte-for-byte, so keying
// the fail→pass edge on the raw command string never fires.
func TestOutcomeObjectiveSurvivesOutputTrimming(t *testing.T) {
	tr := NewOutcomeTracker()

	// The pipeline's exit status is head's, so the receipt reports success even
	// though the suite failed; the host's classification is the honest one.
	failing := bashReceipt(`go test ./... 2>&1 | head -60`, true)
	failing.Verification = VerificationFailed
	s := tr.ScoreRound([]Receipt{failing})
	if s.Objective != 0 || s.Verification != 1 {
		t.Fatalf("first failing verify = %+v, want verification 1 objective 0", s)
	}

	// The re-run is piped too, so its success is head's as much as the first
	// one's was. What says the suite passed is the same host classification.
	passing := bashReceipt(`go test ./... 2>&1 | grep -E 'FAIL|ok ' | head -12`, true)
	passing.Verification = VerificationPassed
	s = tr.ScoreRound([]Receipt{passing})
	if s.Objective != 1 {
		t.Fatalf("same check re-run through a different filter = %+v, want objective 1", s)
	}

	// A narrower run is a different check, so it starts its own history.
	s = tr.ScoreRound([]Receipt{bashReceipt(`go test -run 'TestOne' -v . 2>&1 | tail -5`, true)})
	if s.Objective != 0 {
		t.Fatalf("first run of a narrower check = %+v, want objective 0", s)
	}
}

// The shape a turn spends itself on: edit, check, same failure, edit again.
// The live scorer pays gainMutation for the edit and gainRepeatFailure for the
// check, so every round of this nets positive and the no-progress ladder it
// feeds never arrives. Outcome scoring is where the standing still has to be
// counted, because nothing else counts it.
func TestOutcomeTrackerCountsTheCheckATurnIsStuckOn(t *testing.T) {
	tr := NewOutcomeTracker()
	edit := func() Receipt {
		return ReceiptFromToolCall("edit_file", json.RawMessage(`{"path":"game.js"}`), true, ToolFacts{WritesNamedPaths: true})
	}
	check := func(passed bool) Receipt { return bashReceipt("node --check game.js", passed) }

	s := tr.ScoreRound([]Receipt{check(false)})
	if s.Stall != 0 || s.StallAge != 0 {
		t.Fatalf("a check's first failure = %+v, want it localizing, not stalling", s)
	}
	for round := 1; round <= 3; round++ {
		if s = tr.ScoreRound([]Receipt{edit()}); s.Churn != 1 {
			t.Fatalf("edit round = %+v, want churn 1", s)
		}
		s = tr.ScoreRound([]Receipt{check(false)})
		if s.Stall != 1 || s.StallAge != round || s.StallMutations != round {
			t.Fatalf("repeat %d = %+v, want stall 1, age %d, %d change(s) against it", round, s, round, round)
		}
		// Every round of it reads as progress to the scorer that drives the ladder.
		if s.LegacyGain+gainMutation <= 0 {
			t.Fatalf("repeat %d legacy gain = %d + %d, want the pair to stay positive", round, gainMutation, s.LegacyGain)
		}
	}

	s = tr.ScoreRound([]Receipt{check(true)})
	if s.Objective != 1 || s.Stall != 0 || s.StallAge != 0 || s.StallMutations != 0 {
		t.Fatalf("the check going green = %+v, want the stall cleared with it", s)
	}
	if s = tr.ScoreRound([]Receipt{edit()}); s.StallAge != 0 || s.StallMutations != 0 {
		t.Fatalf("change after the check moved = %+v, want no stall carried over", s)
	}
}

// A check that fails once, gets fixed, and is broken again by a later change is
// the regression path — a turn moving, badly. It must not read as standing still.
func TestOutcomeTrackerKeepsRegressionOutOfTheStall(t *testing.T) {
	tr := NewOutcomeTracker()
	tr.ScoreRound([]Receipt{bashReceipt("go test ./x", false)})
	tr.ScoreRound([]Receipt{bashReceipt("go test ./x", true)})
	s := tr.ScoreRound([]Receipt{bashReceipt("go test ./x", false)})
	if s.Regression != 1 || s.Stall != 0 || s.StallAge != 0 {
		t.Fatalf("pass→fail = %+v, want a regression and no stall", s)
	}
	// It is now failing again, so the next repeat is where a stall can start.
	s = tr.ScoreRound([]Receipt{bashReceipt("go test ./x", false)})
	if s.Stall != 1 || s.StallAge != 1 {
		t.Fatalf("fail→fail after a regression = %+v, want the stall to start here", s)
	}
}
