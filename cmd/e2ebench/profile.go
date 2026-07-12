package main

import (
	"fmt"
	"strings"
)

const (
	benchmarkProfileBaseline = "baseline"
	benchmarkProfileDelivery = "delivery"
)

func normalizeBenchmarkProfile(profile string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", benchmarkProfileBaseline:
		return benchmarkProfileBaseline, nil
	case benchmarkProfileDelivery:
		return benchmarkProfileDelivery, nil
	default:
		return "", fmt.Errorf("unknown benchmark profile %q (want baseline or delivery)", profile)
	}
}

func appendBenchmarkProfileArgs(args []string, profile string) []string {
	if profile == benchmarkProfileDelivery {
		return append(args, "--profile", "delivery")
	}
	return args
}
