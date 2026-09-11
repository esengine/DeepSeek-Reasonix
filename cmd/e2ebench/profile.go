package main

import (
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/ablation"
)

// experimentAxes is the validated set of fixed axes one suite invocation runs
// on. They are resolved together so a second bad flag is reported alongside
// the first instead of being masked by an early exit.
type experimentAxes struct {
	cache, anchor string
	arm           ablation.Set
}

func resolveExperimentAxes(ablate, cache, anchor string) (experimentAxes, error) {
	a, aerr := ablation.Parse(ablate)
	c, cerr := normalizeCacheArm(cache)
	an, anerr := normalizeAnchorArm(anchor)
	return experimentAxes{cache: c, anchor: an, arm: a}, errors.Join(aerr, cerr, anerr)
}

// benchmarkProfileStandard keeps the historical JSON field readable while the
// harness itself has one execution contract. It is metadata, not an arm.
const benchmarkProfileStandard = "standard"

const (
	benchmarkCacheCold = "cold"
	benchmarkCacheWarm = "warm"
)

func normalizeCacheArm(arm string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(arm)) {
	case "", benchmarkCacheCold:
		return benchmarkCacheCold, nil
	case benchmarkCacheWarm:
		return benchmarkCacheWarm, nil
	default:
		return "", fmt.Errorf("unknown cache arm %q (want cold or warm)", arm)
	}
}
