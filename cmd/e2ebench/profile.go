package main

import (
	"fmt"
	"strings"
)

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
