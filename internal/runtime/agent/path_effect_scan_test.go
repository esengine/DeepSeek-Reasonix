package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/safety/evidence"
)

func scanTree(t *testing.T, files int) string {
	t.Helper()
	root := t.TempDir()
	for i := range files {
		if err := os.WriteFile(filepath.Join(root, "f"+string(rune('a'+i%26))+string(rune('a'+i/26))), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The walk runs on the turn's critical path, before the command and again
// after it, and a tree worth walking is big enough that an uninterruptible one
// is what a user experiences as the stop button not working. What it must not
// do is claim completeness for a walk it abandoned.
func TestTheEffectWalkStopsWhenTheTurnDoes(t *testing.T) {
	root := scanTree(t, 600)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	done := make(chan workspaceScan, 1)
	go func() { done <- scanWorkspace(ctx, root) }()
	select {
	case scan := <-done:
		if scan.complete {
			t.Fatal("a walk that was cancelled reported itself complete")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the walk did not stop for a cancelled turn")
	}
}

// A tree bigger than the walk may hold is one the walk can say nothing about,
// and it is that size on the next call too. Told apart from a vanished file or
// a denied directory, which are not.
func TestAWalkPastItsLimitSaysWhyItStoppedShort(t *testing.T) {
	root := scanTree(t, 20)
	scan := scanWorkspaceTo(t.Context(), root, 5)
	if scan.complete {
		t.Fatal("a truncated walk reported itself complete")
	}
	if !scan.overLimit {
		t.Fatal("a walk stopped by its limit did not say so")
	}
	full := scanWorkspaceTo(t.Context(), root, 1000)
	if !full.complete || full.overLimit {
		t.Fatalf("a walk that finished reported complete=%v overLimit=%v", full.complete, full.overLimit)
	}
}

// This is the walk every unclassified call pays twice, and past the limit it
// buys nothing: changed() refuses a truncated scan, so the whole cost lands on
// exactly the trees it cannot answer for. Once is how it is found out; twice
// would be paying to be told again.
func TestAWorkspaceKnownTooBigIsNotWalkedAgain(t *testing.T) {
	a := &Agent{}
	a.writeWorkspaceRoot = scanTree(t, 20)
	plan := &toolCallPlan{evidenceName: "bash", evidenceArgs: []byte(`{"command":"./build.sh"}`)}
	if evidence.ToolCallMutationClass(plan.evidenceName, plan.evidenceArgs, false) != evidence.MutationUnknown {
		t.Fatal("this test needs a call the host cannot classify")
	}

	a.task.overScanLimit = new(atomic.Bool)
	a.task.noteWorkspaceOverScanLimit()
	if scan := a.scanBeforeUnprovenCall(t.Context(), plan); scan.complete || len(scan.state) != 0 {
		t.Fatalf("a workspace known past the limit was walked again: %+v", scan.complete)
	}
	// And the memo is what production sets, not only what a test can: a walk
	// that hits the limit records it.
	b := &Agent{}
	b.writeWorkspaceRoot = a.writeWorkspaceRoot
	if got := scanWorkspaceTo(t.Context(), b.writeWorkspaceRoot, 5); !got.overLimit {
		t.Fatal("the walk that feeds the memo did not report the limit")
	}
}

// Nothing to compare against is not a reason to walk. A call that took no
// before-scan can only reach changed() to be told so, and the walk that gets
// there costs the same as one that answers.
func TestNothingToCompareTakesNoWalk(t *testing.T) {
	a := &Agent{}
	a.writeWorkspaceRoot = scanTree(t, 20)
	rec := evidence.Receipt{Success: true, MutationEvidence: evidence.MutationUnknown}

	// A backgrounded job is the reachable case: its receipt is written when it
	// starts, so scanBeforeUnprovenCall deliberately took nothing.
	a.settleUnchangedWorkspace(t.Context(), &rec, &toolCallPlan{})
	if rec.MutationEvidence != evidence.MutationUnknown {
		t.Fatalf("a call with no before-scan settled to %q", rec.MutationEvidence)
	}
	if rec.Mutation {
		t.Fatal("a call with no before-scan was decided on")
	}
}
