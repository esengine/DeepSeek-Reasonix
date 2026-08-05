//go:build linux

package main

import "testing"

func TestParseHelperPhaseLine(t *testing.T) {
	phase, ok := parseHelperPhaseLine(helperPhasePrefix + "installing")
	if !ok || phase != "installing" {
		t.Fatalf("got %q %v", phase, ok)
	}
	if _, ok := parseHelperPhaseLine("E: Could not get lock"); ok {
		t.Fatal("apt noise must not parse as phase")
	}
	if _, ok := parseHelperPhaseLine(helperPhasePrefix); ok {
		t.Fatal("empty phase must not parse")
	}
}
