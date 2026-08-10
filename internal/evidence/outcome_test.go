package evidence

import (
	"encoding/json"
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
	write := ReceiptFromToolCall("write_file", json.RawMessage(`{"path":"b.go","content":"x"}`), true, false)
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

	task := ReceiptFromToolCall("task", json.RawMessage(`{"prompt":"dig"}`), true, false)
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
	repeat := ReceiptFromToolCall("task", json.RawMessage(`{"prompt":"dig"}`), true, false)
	s = tr.ScoreRound([]Receipt{repeat})
	if s.Exploration != 1 || s.Objective != 0 {
		t.Fatalf("repeated delegation = %+v, want exploration 1 objective 0", s)
	}

	var nilTracker *OutcomeTracker
	if got := nilTracker.ScoreRound([]Receipt{task}); got != (OutcomeSample{}) {
		t.Fatalf("nil tracker sample = %+v, want zero", got)
	}
}

func TestOutcomeTrackerVerificationDebtLifecycle(t *testing.T) {
	tr := NewOutcomeTracker()

	// A mutation opens debt; silent rounds age it.
	write := ReceiptFromToolCall("write_file", json.RawMessage(`{"path":"pkg/repro.py","content":"x"}`), true, false)
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
	if s := tr.ScoreRound([]Receipt{ReceiptFromToolCall("write_file", json.RawMessage(`{"path":"pkg/b.py","content":"y"}`), true, false)}); s.BlindMutations != 2 {
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
