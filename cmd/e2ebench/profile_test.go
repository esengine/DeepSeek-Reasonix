package main

import (
	"reflect"
	"testing"
)

func TestAppendBenchmarkProfileArgsBaselineIsByteIdentical(t *testing.T) {
	args := []string{"run", "fix the bug"}
	if got := appendBenchmarkProfileArgs(args, benchmarkProfileBaseline); !reflect.DeepEqual(got, args) {
		t.Fatalf("baseline args changed: %v", got)
	}
}

func TestAppendBenchmarkProfileArgsDeliveryUsesRealRuntimeProfile(t *testing.T) {
	args := []string{"run"}
	got := appendBenchmarkProfileArgs(args, benchmarkProfileDelivery)
	want := []string{"run", "--profile", "delivery"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivery args = %v, want %v", got, want)
	}
}

func TestNormalizeBenchmarkProfile(t *testing.T) {
	for _, input := range []string{"", "baseline", " BASELINE "} {
		if got, err := normalizeBenchmarkProfile(input); err != nil || got != benchmarkProfileBaseline {
			t.Fatalf("normalize(%q) = %q, %v", input, got, err)
		}
	}
	if got, err := normalizeBenchmarkProfile("delivery"); err != nil || got != benchmarkProfileDelivery {
		t.Fatalf("normalize(delivery) = %q, %v", got, err)
	}
	if _, err := normalizeBenchmarkProfile("fast"); err == nil {
		t.Fatal("unknown profile should fail")
	}
}
