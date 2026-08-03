package main

import (
	"fmt"
	"strings"
)

const (
	benchmarkProfileBaseline           = "baseline"
	benchmarkProfileDelivery           = "delivery"
	benchmarkProfileProjection         = "projection"
	benchmarkProfileDeliveryProjection = "delivery-projection"
)

func normalizeBenchmarkProfile(profile string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "", benchmarkProfileBaseline:
		return benchmarkProfileBaseline, nil
	case benchmarkProfileDelivery:
		return benchmarkProfileDelivery, nil
	case benchmarkProfileProjection:
		return benchmarkProfileProjection, nil
	case benchmarkProfileDeliveryProjection:
		return benchmarkProfileDeliveryProjection, nil
	default:
		return "", fmt.Errorf("unknown benchmark profile %q (want baseline, delivery, projection, or delivery-projection)", profile)
	}
}

func appendBenchmarkProfileArgs(args []string, profile string) []string {
	if profile == benchmarkProfileDelivery || profile == benchmarkProfileDeliveryProjection {
		args = append(args, "--profile", "delivery")
	}
	if profile == benchmarkProfileProjection || profile == benchmarkProfileDeliveryProjection {
		args = append(args, "--tool-result-projection")
	}
	return args
}
