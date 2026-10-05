package taskcontract

import (
	"testing"

	"reasonix/internal/safety/evidence"
)

// A suite whose exit status the shell hid neither satisfies a declared check
// nor fails it: the check simply keeps waiting for a readable run.
func TestInconclusiveVerificationLeavesTheCheckWaiting(t *testing.T) {
	c := New("fix the parser")
	c.AddCheck("go test ./...")
	c.Observe(evidence.Receipt{
		ToolName:     "bash",
		Command:      "go test ./... | head -100",
		Success:      true,
		Verification: evidence.VerificationInconclusive,
	})
	if got := c.Checks[0].Status; got == Satisfied {
		t.Fatal("an unreadable run satisfied the check; a red suite exits 0 through a pipe")
	}
	if got := c.Checks[0].Status; got == Failed {
		t.Fatalf("status = %v, want the check still waiting rather than failed", got)
	}
	if c.Complete() {
		t.Fatal("contract reported complete on an unreadable verification")
	}
}

func TestReadableVerificationStillSettlesTheCheck(t *testing.T) {
	passed := New("fix the parser")
	passed.AddCheck("go test ./...")
	passed.Observe(evidence.Receipt{
		ToolName:     "bash",
		Command:      "go test ./...",
		Success:      true,
		Verification: evidence.VerificationPassed,
	})
	if passed.Checks[0].Status != Satisfied {
		t.Fatalf("status = %v, want satisfied", passed.Checks[0].Status)
	}

	failed := New("fix the parser")
	failed.AddCheck("go test ./...")
	failed.Observe(evidence.Receipt{
		ToolName:     "bash",
		Command:      "go test ./...",
		Success:      false,
		Verification: evidence.VerificationFailed,
	})
	if failed.Checks[0].Status != Failed {
		t.Fatalf("status = %v, want failed", failed.Checks[0].Status)
	}
}

// A check is settled by the verdict, not by the company the command keeps: a
// readable run still counts beside an unclassified command, and an unreadable
// one is not laundered into proof by a succeeding neighbour.
func TestCompanyDoesNotDecideACheck(t *testing.T) {
	// Control first, so the negative case below cannot pass vacuously on a
	// command that simply failed to match the declared check.
	readable := New("fix the parser")
	readable.AddCheck("go test ./...")
	readable.Observe(evidence.Receipt{
		ToolName:     "bash",
		Command:      "go test ./... && gofmt -l .",
		Success:      true,
		Verification: evidence.VerificationPassed,
	})
	if readable.Checks[0].Status != Satisfied {
		t.Fatalf("status = %v, want satisfied: a readable verdict counts in mixed company", readable.Checks[0].Status)
	}

	laundered := New("fix the parser")
	laundered.AddCheck("go test ./...")
	laundered.Observe(evidence.Receipt{
		ToolName:     "bash",
		Command:      "go test ./... | head -50 && gofmt -l .",
		Success:      true,
		Verification: evidence.VerificationInconclusive,
	})
	if laundered.Checks[0].Status == Satisfied {
		t.Fatal("a piped suite satisfied a declared check through a succeeding neighbour")
	}
}

// Receipts carrying no host classification keep their pre-existing meaning.
func TestUnclassifiedVerificationKeepsItsOwnOutcome(t *testing.T) {
	c := New("fix the parser")
	c.AddCheck("go test ./...")
	c.Observe(evidence.Receipt{ToolName: "bash", Command: "go test ./...", Success: true})
	if c.Checks[0].Status != Satisfied {
		t.Fatalf("status = %v, want satisfied for an unclassified successful run", c.Checks[0].Status)
	}
}
