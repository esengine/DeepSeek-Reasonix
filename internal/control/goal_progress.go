package control

import "strings"

// mergeGoalProgressEvidence updates the bounded Goal-scoped novelty window.
func mergeGoalProgressEvidence(existing, observed []string) ([]string, bool) {
	if len(existing) > maxGoalProgressEvidence {
		existing = existing[len(existing)-maxGoalProgressEvidence:]
	}
	if len(observed) > maxGoalProgressEvidence {
		observed = observed[len(observed)-maxGoalProgressEvidence:]
	}
	out := make([]string, 0, min(len(existing)+len(observed), maxGoalProgressEvidence))
	seen := make(map[string]struct{}, min(len(existing)+len(observed), maxGoalProgressEvidence))
	appendValid := func(sig string) bool {
		sig = strings.TrimSpace(sig)
		if sig == "" || len(sig) > 128 {
			return false
		}
		if _, ok := seen[sig]; ok {
			return false
		}
		seen[sig] = struct{}{}
		out = append(out, sig)
		return true
	}
	for _, sig := range existing {
		appendValid(sig)
	}
	progressed := false
	for _, sig := range observed {
		if appendValid(sig) {
			progressed = true
		}
	}
	if len(out) > maxGoalProgressEvidence {
		out = append([]string(nil), out[len(out)-maxGoalProgressEvidence:]...)
	}
	return out, progressed
}

func (g *goalMachine) observeGoalProgress(in goalAdvanceInput) {
	progressed := false
	g.progressEvidence, progressed = mergeGoalProgressEvidence(g.progressEvidence, in.progressEvidence)
	if in.report != nil && in.report.status != GoalStatusRunning {
		progressed = true
	}
	if progressed {
		g.noProgressTurns = 0
		return
	}
	g.noProgressTurns++
}
