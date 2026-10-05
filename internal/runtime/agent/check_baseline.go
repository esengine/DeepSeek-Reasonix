package agent

import (
	"path/filepath"
	"slices"

	"reasonix/internal/safety/evidence"
)

// checkRuns answers where "passed after the latest change" is measured from
// for a check. all names every check whose run is not itself the work; guarded
// holds the files a check may not write and still call it residue: the ones
// the plan names, the ones any other write this turn touched, and every one
// whose first touch this turn was not its creation.
type checkRuns struct {
	ledger  *evidence.Ledger
	all     []string
	guarded map[string]bool
	key     func(string) string
}

func (a *Agent) checkRunsOf(all []string) checkRuns {
	c := checkRuns{ledger: a.task.ledger, all: all, guarded: map[string]bool{}, key: a.workspacePathKey}
	if plan := a.PlanContract(); plan != nil {
		for _, step := range plan.Steps {
			for _, p := range append(slices.Clone(step.VerifiedFiles), step.CandidateFiles...) {
				c.guarded[c.key(p)] = true
			}
		}
	}
	touched := map[string]bool{}
	for _, r := range c.ledger.Receipts() {
		if !r.Mutation && !r.Write {
			continue
		}
		for _, p := range r.Paths {
			k := c.key(p)
			if !touched[k] && !slices.Contains(r.Created, p) {
				c.guarded[k] = true
			}
			touched[k] = true
		}
		if r.Success && r.Mutation && !runsOneOf(r, all) {
			for _, p := range r.Paths {
				c.guarded[c.key(p)] = true
			}
		}
	}
	return c
}

// baseline is the latest proven change, not counting a run of exactly one of
// own that wrote only its residue — an import writes bytecode, and would
// otherwise owe itself another run. A run of any other check is a change.
func (c checkRuns) baseline(own []string) (int, bool) {
	return c.ledger.LatestProvenMutationIndexFunc(func(r evidence.Receipt) bool {
		return !runsOneOf(r, own) || !c.onlyResidue(r)
	})
}

// onlyResidue holds when a complete walk named every file the run wrote and
// none is guarded. A file that was there before the turn is someone's source
// however nobody named it, which guarded already holds.
func (c checkRuns) onlyResidue(r evidence.Receipt) bool {
	if !r.PathsComplete || len(r.Paths) == 0 {
		return false
	}
	return !slices.ContainsFunc(r.Paths, func(p string) bool { return c.guarded[c.key(p)] })
}

func runsOneOf(r evidence.Receipt, checks []string) bool {
	return r.ToolName == "bash" && slices.ContainsFunc(checks, func(c string) bool {
		return evidence.CommandMatches(c, r.Command) && evidence.CommandMatches(r.Command, c)
	})
}

// workspacePathKey is a path's ledger identity with a relative path read
// against the workspace, so a plan's "src/a.go" and a scan's absolute path
// for the same file are one key.
func (a *Agent) workspacePathKey(p string) string {
	if !filepath.IsAbs(p) && a.observeRoot != "" {
		p = filepath.Join(a.observeRoot, p)
	}
	return evidence.NormalizePath(p)
}
