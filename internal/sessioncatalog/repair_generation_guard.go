package sessioncatalog

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"

	"reasonix/internal/agent"
)

type repairBatchGeneration struct {
	contentFingerprint string
	metaFingerprint    string
	locked             bool
}

func lockRepairBatchGenerations(ctx context.Context, outcomes []repairOutcome) ([]repairBatchGeneration, func(), error) {
	generations := make([]repairBatchGeneration, len(outcomes))
	order := make([]int, 0, len(outcomes))
	for index := range outcomes {
		if outcomes[index].result.ContentFingerprint != "" || outcomes[index].result.MetaFingerprint != "" {
			order = append(order, index)
		}
	}
	sort.Slice(order, func(i, j int) bool {
		return agent.CanonicalSessionPath(outcomes[order[i]].item.path) < agent.CanonicalSessionPath(outcomes[order[j]].item.path)
	})

	unlocks := make([]func(), 0, len(order))
	release := func() {
		for _, unlock := range slices.Backward(unlocks) {
			unlock()
		}
	}
	for _, index := range order {
		if err := ctx.Err(); err != nil {
			release()
			return nil, func() {}, err
		}
		generation, unlock, err := agent.TryLockSessionListingGeneration(outcomes[index].item.path)
		if err != nil {
			outcomes[index].err = errors.Join(outcomes[index].err, err)
			continue
		}
		generations[index] = repairBatchGeneration{
			contentFingerprint: generation.ContentFingerprint,
			metaFingerprint:    generation.MetaFingerprint,
			locked:             true,
		}
		unlocks = append(unlocks, unlock)
	}
	return generations, release, nil
}

func repairBatchFingerprints(outcome repairOutcome, generation repairBatchGeneration) (string, string) {
	if generation.locked {
		return generation.contentFingerprint, generation.metaFingerprint
	}
	contentFingerprint, metaFingerprint, _ := strings.Cut(outcome.item.sourceFingerprint, "\x00")
	return contentFingerprint, metaFingerprint
}
