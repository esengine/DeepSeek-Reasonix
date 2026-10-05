package agent

import (
	"os"
	"slices"

	"reasonix/internal/safety/evidence"
)

// witnessFileLimit bounds how much of a file the host will read to build a
// witness for a change it never previewed. A file past it stays unwitnessed,
// so review of it has to come from a read receipt.
const witnessFileLimit = 1 << 20

// reviewCoverageOf settles both halves of one call: what it showed of earlier
// changes, and what a later call would have to show of its own.
func (a *Agent) reviewCoverageOf(rec *evidence.Receipt, plan *toolCallPlan, output string) {
	a.decorateShownPaths(rec, output)
	a.noteReviewWitness(rec, plan)
}

// noteReviewWitness records what a later output would have to show to count as
// review of this change. The preview a Previewable writer already produced
// carries both revisions, so the changed lines are known exactly; a shell write
// names no earlier revision, and showing such a file means showing its content.
func (a *Agent) noteReviewWitness(rec *evidence.Receipt, plan *toolCallPlan) {
	if !rec.Success || !(rec.Mutation || rec.Write) || len(rec.Paths) == 0 {
		return
	}
	if a.task.witness == nil {
		a.task.witness = map[string][]string{}
	}
	for _, path := range rec.Paths {
		// The newest change is the one a review still owes, so a second write to
		// the same path replaces what the first one asked an output to show.
		if path == plan.mutationPath && len(plan.mutationWitness) > 0 {
			a.task.witness[path] = plan.mutationWitness
			continue
		}
		if lines := witnessFromDisk(path); len(lines) > 0 {
			a.task.witness[path] = lines
		}
	}
}

// decorateShownPaths records which changed paths this call's output carried.
// A call that changed a file is never the review of it, so writers are skipped
// rather than allowed to answer for themselves.
func (a *Agent) decorateShownPaths(rec *evidence.Receipt, output string) {
	if !rec.Success || rec.Mutation || rec.Write || output == "" || len(a.task.witness) == 0 {
		return
	}
	for path, witness := range a.task.witness {
		if evidence.OutputShowsLines(output, witness) {
			rec.Showed = append(rec.Showed, path)
		}
	}
	// Map order is not an order; the receipt is persisted and read back.
	slices.Sort(rec.Showed)
}

func witnessFromDisk(path string) []string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > witnessFileLimit {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return evidence.ContentLines(string(content))
}
