package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/evidence"
)

// A real module, a real toolchain run: the criterion the task began under is
// evaluated against the code that replaced its test.
func baselineWorkspace(t *testing.T, impl, currentTest string) *Agent {
	t.Helper()
	root := testenv.TempDir(t)
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module spike\n\ngo 1.26\n")
	write("cache.go", impl)
	write("cache_test.go", currentTest)

	a := &Agent{}
	a.writeWorkspaceRoot = root
	a.archiveDir = testenv.TempDir(t)

	// The host holds what the test said before the turn rewrote it.
	captured := "package spike\n\nimport \"testing\"\n\nfunc TestEvict(t *testing.T) {\n\tif got := Evict(3); got != 2 {\n\t\tt.Fatalf(\"Evict(3) = %d, want 2\", got)\n\t}\n}\n"
	criterion, err := a.baselineCriteriaStore().Capture(filepath.Join(root, "cache_test.go"), []byte(captured))
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	a.task.baselineCriteria = map[string]evidence.TestCriterion{filepath.Join(root, "cache_test.go"): criterion}
	return a
}

const assertsNothing = "package spike\n\nimport \"testing\"\n\nfunc TestEvict(t *testing.T) { _ = true }\n"

// The attack, end to end: the workspace test was rewritten to assert nothing
// and passes, while the captured criterion fails against the same code. The
// green tick answers for the criterion that replaced it, not the one owed.
func TestARewrittenGreenTestLeavesTheCapturedCriterionOwed(t *testing.T) {
	a := baselineWorkspace(t, "package spike\n\nfunc Evict(n int) int { return n }\n", assertsNothing)
	a.evaluateBaselineCriteriaOnce(context.Background())

	facts := a.baselineFacts()
	if len(facts) != 1 {
		t.Fatalf("facts = %+v, want one", facts)
	}
	if facts[0].Provenance != evidence.CriterionRewritten {
		t.Fatalf("provenance = %q, want rewritten", facts[0].Provenance)
	}
	if facts[0].Evaluation == nil || facts[0].Evaluation.Result != evidence.CriterionFail {
		t.Fatalf("evaluation = %+v, want the captured criterion to fail", facts[0].Evaluation)
	}
	if owed := evidence.BaselineTestObligations(facts, a.mutationEpoch()); len(owed) != 1 {
		t.Fatalf("obligations = %+v, want the criterion still owed", owed)
	}
	// The workspace file is untouched: the criterion went in through the build,
	// not through the tree.
	current, err := os.ReadFile(filepath.Join(a.writeWorkspaceRoot, "cache_test.go"))
	if err != nil || string(current) != assertsNothing {
		t.Fatalf("workspace test = %q, %v; want it exactly as the turn left it", current, err)
	}
}

// The captured criterion passing against the final code settles it.
func TestTheCapturedCriterionPassingSettlesIt(t *testing.T) {
	a := baselineWorkspace(t, "package spike\n\nfunc Evict(n int) int { return n - 1 }\n", assertsNothing)
	a.evaluateBaselineCriteriaOnce(context.Background())

	facts := a.baselineFacts()
	if facts[0].Evaluation == nil || facts[0].Evaluation.Result != evidence.CriterionPass {
		t.Fatalf("evaluation = %+v, want a pass", facts[0].Evaluation)
	}
	if owed := evidence.BaselineTestObligations(facts, a.mutationEpoch()); len(owed) != 0 {
		t.Fatalf("obligations = %+v, want none", owed)
	}
}

// A rename the task legitimately made can leave the captured criterion unable to
// build. The host says it reached no verdict, and the criterion stays owed —
// nothing here decides that dropping it was allowed.
func TestACriterionThatCannotBuildIsUnavailableAndStillOwed(t *testing.T) {
	renamed := "package spike\n\nimport \"testing\"\n\nfunc TestEvictOne(t *testing.T) { _ = EvictOne(1) }\n"
	a := baselineWorkspace(t, "package spike\n\nfunc EvictOne(n int) int { return n - 1 }\n", renamed)
	a.evaluateBaselineCriteriaOnce(context.Background())

	facts := a.baselineFacts()
	if facts[0].Evaluation == nil || facts[0].Evaluation.Result != evidence.CriterionUnavailable {
		t.Fatalf("evaluation = %+v, want unavailable rather than a failure it never proved", facts[0].Evaluation)
	}
	owed := evidence.BaselineTestObligations(facts, a.mutationEpoch())
	if len(owed) != 1 {
		t.Fatalf("obligations = %+v, want it still owed", owed)
	}
}

// A criterion the workspace still holds byte for byte was run by the project's
// own checks. Reaching for the overlay there costs a build to prove what the
// ordinary suite just proved — and a broad mutation captures every test file in
// the tree, so it costs one per file for a turn that rewrote none of them.
func TestAnUnchangedCriterionCostsNoBuild(t *testing.T) {
	captured := "package spike\n\nimport \"testing\"\n\nfunc TestEvict(t *testing.T) {\n\tif got := Evict(3); got != 2 {\n\t\tt.Fatalf(\"Evict(3) = %d, want 2\", got)\n\t}\n}\n"
	a := baselineWorkspace(t, "package spike\n\nfunc Evict(n int) int { return n - 1 }\n", captured)

	start := time.Now()
	a.evaluateBaselineCriteriaOnce(context.Background())
	elapsed := time.Since(start)

	if len(a.turn.baselineEval) != 0 {
		t.Fatalf("evaluations = %v, want none for a criterion the workspace still holds", a.turn.baselineEval)
	}
	if len(a.baselineFacts()) != 0 {
		t.Fatalf("facts = %+v, want none: the ordinary checks already ran these bytes", a.baselineFacts())
	}
	// A toolchain build cannot finish this fast, so this fails if one ran.
	if elapsed > 300*time.Millisecond {
		t.Fatalf("took %s — an unchanged criterion reached for the toolchain", elapsed)
	}
}
