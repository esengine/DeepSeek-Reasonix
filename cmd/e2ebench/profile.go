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
	profile, cache, anchor string
	arm                    ablation.Set
}

func resolveExperimentAxes(profile, ablate, cache, anchor string) (experimentAxes, error) {
	p, perr := normalizeBenchmarkProfile(profile)
	a, aerr := ablation.Parse(ablate)
	c, cerr := normalizeCacheArm(cache)
	an, anerr := normalizeAnchorArm(anchor)
	return experimentAxes{profile: p, cache: c, anchor: an, arm: a}, errors.Join(perr, aerr, cerr, anerr)
}

const (
	benchmarkProfileBaseline = "baseline"
	benchmarkProfileEconomy  = "economy"
	benchmarkProfileBalanced = "balanced"
	benchmarkProfileDelivery = "delivery"
)

// normalizeBenchmarkProfile validates the tool-surface arm. baseline stays
// distinct from balanced: it passes no --profile flag at all, preserving the
// byte-identical control command line older comparisons were recorded with.
func normalizeBenchmarkProfile(profile string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", benchmarkProfileBaseline:
		return benchmarkProfileBaseline, nil
	case benchmarkProfileEconomy:
		return benchmarkProfileEconomy, nil
	case benchmarkProfileBalanced:
		return benchmarkProfileBalanced, nil
	case benchmarkProfileDelivery:
		return benchmarkProfileDelivery, nil
	default:
		return "", fmt.Errorf("unknown benchmark profile %q (want baseline, economy, balanced, or delivery)", profile)
	}
}

func appendBenchmarkProfileArgs(args []string, profile string) []string {
	if profile == benchmarkProfileBaseline {
		return args
	}
	return append(args, "--profile", profile)
}

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
