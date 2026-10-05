package agent

import (
	"slices"

	"reasonix/internal/safety/evidence"
)

// unmetReviewKinds lists the structured reviews that have not covered paths
// since freshness, in the order the gate asks for them.
func (a *Agent) unmetReviewKinds(freshness int, paths []string) []evidence.ReviewKind {
	var owed []evidence.ReviewKind
	for _, kind := range []evidence.ReviewKind{evidence.ReviewKindReview, evidence.ReviewKindSecurity} {
		if ok, blocking, _ := a.task.ledger.HasStructuredReviewAfter(kind, freshness, paths); !ok || blocking {
			owed = append(owed, kind)
		}
	}
	return owed
}

// liveOwedReview is what this process's ledger says a high-risk change owes.
func (a *Agent) liveOwedReview() *evidence.OwedReview {
	baseline, ok := a.task.ledger.UnreviewedMutationBaseline()
	if !ok || a.task.ledger.MutationRiskAfter(baseline, a.projectSensitivePaths) != evidence.RiskHigh {
		return nil
	}
	freshness, ok := a.task.ledger.LatestProvenMutationIndex()
	if !ok || freshness < baseline {
		freshness = baseline
	}
	paths := productionPaths(a.task.ledger.PathsSince(baseline))
	if kinds := a.unmetReviewKinds(freshness, paths); len(kinds) > 0 {
		return &evidence.OwedReview{Kinds: kinds, Paths: paths}
	}
	return nil
}

// carriedOwedReview is what an earlier process recorded as owed and no review
// in this ledger has settled. Every receipt here postdates the change that
// incurred it, so any covering review counts.
func (a *Agent) carriedOwedReview(prior *evidence.OwedReview) *evidence.OwedReview {
	if prior == nil {
		return nil
	}
	var kinds []evidence.ReviewKind
	for _, kind := range prior.Kinds {
		if slices.Contains(a.unmetReviewKinds(-1, prior.Paths), kind) {
			kinds = append(kinds, kind)
		}
	}
	if len(kinds) == 0 {
		return nil
	}
	return &evidence.OwedReview{Kinds: kinds, Paths: prior.Paths}
}

// owedReview is the checkpoint's next answer: what the live ledger owes, joined
// with what was carried in and is still unsettled. Neither half may erase the
// other — a restarted ledger derives nothing, and that is not a discharge.
func (a *Agent) owedReview(prior *evidence.OwedReview) *evidence.OwedReview {
	live, carried := a.liveOwedReview(), a.carriedOwedReview(prior)
	switch {
	case live == nil:
		return carried
	case carried == nil:
		return live
	}
	out := &evidence.OwedReview{Kinds: slices.Clone(live.Kinds), Paths: slices.Clone(live.Paths)}
	for _, kind := range carried.Kinds {
		if !slices.Contains(out.Kinds, kind) {
			out.Kinds = append(out.Kinds, kind)
		}
	}
	for _, p := range carried.Paths {
		if !slices.Contains(out.Paths, p) {
			out.Paths = append(out.Paths, p)
		}
	}
	return out
}

// carriedReviewFailure is the gate's answer for a review owed before this
// process began: the ledger that incurred it is gone, so the checkpoint is the
// only thing that still knows.
func (a *Agent) carriedReviewFailure() string {
	if !a.deliveryProfile || !a.deliveryCheckpointApplies() {
		return ""
	}
	carried := a.carriedOwedReview(a.task.checkpoint.OwedReview)
	if carried == nil {
		return ""
	}
	return a.reviewDemand(carried.Kinds[0], carried.Paths)
}

// reviewDemand is the instruction that settles one owed review kind.
func (a *Agent) reviewDemand(kind evidence.ReviewKind, paths []string) string {
	name := "review"
	if kind == evidence.ReviewKindSecurity {
		name = "security_review"
	}
	return "high-risk changes require " + name + " with review_report after the latest mutation" +
		a.reviewProofHint(kind) + reviewCoverageHint(paths)
}
