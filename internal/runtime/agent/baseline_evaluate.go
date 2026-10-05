// Running a captured criterion against the code that replaced it. The bytes go
// in through the toolchain's own build overlay, so the workspace is read and
// never written: what the verifier compiles is the host's copy, and the file on
// disk stays whatever the turn made it.
package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	"reasonix/internal/contract/event"
	"reasonix/internal/safety/evidence"
)

// baselineBackend names the capability that produced a result, so a claim never
// outlives the thing that could make it. It is narrow on purpose: the overlay is
// a build-level view, not a filesystem one, and it answers only for Go sources
// the toolchain compiles.
const baselineBackend = "go_overlay"

// criterionProvenance reads what the workspace did to the criterion. It is a
// fact about the tree, never a verdict about the code.
func (a *Agent) criterionProvenance(criterion evidence.TestCriterion, path string) evidence.CriterionProvenance {
	current, err := os.ReadFile(path)
	return evidence.CompareCriterion(criterion, current, err == nil)
}

// runBaselineCriterion compiles the package with the captured bytes in place of
// what the workspace holds and reads the verdict for that criterion alone. ok is
// false when the host could not run it at all, which is not a result.
func (a *Agent) runBaselineCriterion(ctx context.Context, store *evidence.BaselineStore, criterion evidence.TestCriterion, path string) (evidence.CriterionResult, bool) {
	captured, err := store.Open(criterion)
	if err != nil {
		return "", false
	}
	names := evidence.EvaluableCriterionNames(captured)
	if len(names) == 0 {
		// Captured, but of a kind a plain run does not execute. The host says so
		// rather than signing evidence it cannot produce.
		return evidence.CriterionUnavailable, true
	}
	overlay, cleanup, err := a.writeBaselineOverlay(store, criterion, path)
	if err != nil {
		return "", false
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, "go", "test", "-overlay="+overlay, "-json", "-count=1", packagePathOf(a.writeWorkspaceRoot, path))
	cmd.Dir = a.writeWorkspaceRoot
	out, _ := cmd.Output()
	return evidence.BaselineOutcomeFromTestJSON(out, names), true
}

// packagePathOf names the criterion's package as the toolchain expects it,
// relative to the module root the command runs in.
func packagePathOf(root, path string) string {
	rel, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil || rel == "." || rel == "" {
		return "."
	}
	return "./" + filepath.ToSlash(rel)
}

// writeBaselineOverlay points the criterion's path at the host's copy. The map
// lives beside the store, never in the workspace: an overlay written into the
// tree would be a mutation made to measure it.
func (a *Agent) writeBaselineOverlay(store *evidence.BaselineStore, criterion evidence.TestCriterion, path string) (string, func(), error) {
	dir, err := os.MkdirTemp(baselineCriteriaRoot(a.archiveDir), "overlay-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	captured, err := store.Open(criterion)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	backing := filepath.Join(dir, filepath.Base(path))
	if err := os.WriteFile(backing, captured, 0o400); err != nil {
		cleanup()
		return "", nil, err
	}
	body, err := json.Marshal(map[string]map[string]string{"Replace": {path: backing}})
	if err != nil {
		cleanup()
		return "", nil, err
	}
	overlay := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlay, body, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}
	return overlay, cleanup, nil
}

// baselineFacts pairs every captured criterion with what the workspace did to
// it and whatever verdict this turn reached. A criterion with no verdict is
// owed — capture alone is the debt, and only a run against the captured bytes
// settles it.
func (a *Agent) baselineFacts() []evidence.BaselineTestFact {
	if a == nil || len(a.task.baselineCriteria) == 0 {
		return nil
	}
	facts := make([]evidence.BaselineTestFact, 0, len(a.task.baselineCriteria))
	for path, criterion := range a.task.baselineCriteria {
		provenance := a.criterionProvenance(criterion, path)
		if provenance == evidence.CriterionUnchanged {
			// The workspace still holds these exact bytes, so the project's own
			// checks ran this criterion. Nothing here is owed that the ordinary
			// verification obligations do not already say.
			continue
		}
		fact := evidence.BaselineTestFact{Criterion: criterion, Provenance: provenance}
		if got, ok := a.turn.baselineEval[criterion.Identity()]; ok {
			fact.Evaluation = &got
		}
		facts = append(facts, fact)
	}
	return facts
}

// evaluateBaselineCriteriaOnce runs the captured criteria for this turn. It is
// called where the turn asks whether it may stop, because a run costs a build
// and its answer cannot change between two tool calls that changed nothing.
func (a *Agent) evaluateBaselineCriteriaOnce(ctx context.Context) {
	if a == nil || len(a.task.baselineCriteria) == 0 || a.turn.baselineEval != nil {
		return
	}
	a.turn.baselineEval = map[string]evidence.BaselineEvidence{}
	epoch := a.mutationEpoch()
	store := a.baselineCriteriaStore()
	// A build takes seconds the reader would otherwise watch as an unexplained
	// spinner: the model has answered, and the host is still checking.
	announced := false
	for path, criterion := range a.task.baselineCriteria {
		// Only a criterion the workspace no longer holds needs the overlay. Running
		// one costs a build, and doing it for every file captured against a broad
		// mutation is a build per test file for a turn that rewrote none of them.
		if a.criterionProvenance(criterion, path) == evidence.CriterionUnchanged {
			continue
		}
		if !announced {
			a.emitTurnPhase(event.TurnPhaseChecking)
			announced = true
		}
		result, ok := a.runBaselineCriterion(ctx, store, criterion, path)
		if !ok {
			continue
		}
		a.turn.baselineEval[criterion.Identity()] = evidence.BaselineEvidence{
			Criterion: criterion, StateEpoch: epoch, Result: result, Backend: baselineBackend,
		}
	}
}
