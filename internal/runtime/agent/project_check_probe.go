// project_check_probe.go — what readiness would owe if it read obligations.
package agent

import (
	"maps"
	"slices"
	"strings"

	"reasonix/internal/contract/event"
	"reasonix/internal/safety/evidence"
)

// probeProjectChecks compares the gate's project-check derivation against the
// obligations the ledger derives from the same declarations, and records what
// explains a disagreement: "the candidate blocks more often" cannot say by
// itself whether that is the baseline protection working or a regression. It
// runs where the gate runs, so a path the gate skips is out of the comparison.
func (a *Agent) probeProjectChecks(legacyGaps, writer int) {
	ledger := a.task.ledger
	if ledger == nil {
		return
	}
	declared, normalized, legacy := a.legacyProjectCheckView(writer)
	if len(declared) == 0 && len(a.task.checkpoint.BaselineChecks) == 0 {
		return
	}
	candidate := map[string]bool{}
	for _, owed := range a.obligations() {
		switch owed.Kind {
		case evidence.ObligationMissingProjectCheck, evidence.ObligationBaselineCheck:
			candidate[owed.Subject()] = true
		}
	}
	candidateAfter, _ := ledger.LatestSuccessfulMutationIndex()
	probe := event.ProjectCheckProbe{
		Declared:         len(declared),
		Baseline:         len(a.task.checkpoint.BaselineChecks),
		LegacyBlocked:    legacyGaps > 0,
		CandidateBlocked: len(candidate) > 0,
		LegacyAfter:      writer,
		CandidateAfter:   candidateAfter,
	}
	disputed := maps.Clone(legacy)
	maps.Copy(disputed, candidate)
	for _, id := range slices.Sorted(maps.Keys(disputed)) {
		if legacy[id] && candidate[id] {
			probe.AgreedMissing++
			continue
		}
		probe.Diffs = append(probe.Diffs, event.ProjectCheckDiff{
			Identity: id,
			Class: projectCheckDiffClass(ledger, id, projectCheckSides{
				candidateOnly: candidate[id],
				declaredNow:   declared[id],
				normalized:    normalized[id],
				legacyAfter:   writer,
				after:         candidateAfter,
			}),
		})
	}
	event.RecordProjectCheckProbe(a.svc.sink, probe)
}

// legacyProjectCheckView reads the declaration the way the gate does — raw
// command text, current declaration only — and reports it under the identities
// the obligations are keyed on, so the two sides are comparable at all.
func (a *Agent) legacyProjectCheckView(writer int) (declared, normalized, unmet map[string]bool) {
	declared, normalized, unmet = map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, check := range a.projectChecks {
		command := strings.TrimSpace(check.Command)
		id := evidence.VerificationIdentity(command)
		if command == "" || id == "" {
			continue
		}
		declared[id] = true
		if id != command {
			normalized[id] = true
		}
		if !a.task.ledger.HasSuccessfulCommandAfter(command, writer) {
			unmet[id] = true
		}
	}
	return declared, normalized, unmet
}

// projectCheckSides is what one disputed identity looked like to each side.
type projectCheckSides struct {
	candidateOnly bool
	declaredNow   bool
	normalized    bool
	legacyAfter   int
	after         int
}

// projectCheckDiffClass names the one explanation that accounts for a
// disagreement, strongest first: a criterion the current declaration dropped,
// then the two baselines, then canonicalisation. What survives all three is
// unexplained, and only those two classes are candidate defects.
func projectCheckDiffClass(ledger *evidence.Ledger, id string, sides projectCheckSides) string {
	switch {
	case sides.candidateOnly && !sides.declaredNow:
		return event.ProjectCheckBaselinePreservation
	case ledger.HasSuccessfulCommandAfter(id, sides.legacyAfter) != ledger.HasSuccessfulCommandAfter(id, sides.after):
		return event.ProjectCheckMutationIndex
	case sides.normalized:
		return event.ProjectCheckIdentityNormalization
	case sides.candidateOnly:
		return event.ProjectCheckCandidateOnly
	default:
		return event.ProjectCheckLegacyOnly
	}
}
