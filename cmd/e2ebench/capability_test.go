package main

import (
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestRenderCapabilitySetsFlagsMoreThanOneVersion(t *testing.T) {
	run := func(id, hash string) result {
		r := result{task: task{ID: id}}
		r.Trajectory = &trajectorySummary{CapabilitiesHash: hash}
		return r
	}
	one := renderCapabilitySets([]result{run("a", "sha256:aaaaaaaaaaaaaaaa"), run("b", "sha256:aaaaaaaaaaaaaaaa")})
	if !strings.Contains(one, "one set across 2 runs (aaaaaaaaaaaa)") {
		t.Fatalf("one set = %q", one)
	}
	two := renderCapabilitySets([]result{run("a", "sha256:aaaaaaaaaaaaaaaa"), run("b", "sha256:bbbbbbbbbbbbbbbb")})
	if !strings.Contains(two, "2 different sets across 2 runs — not one version") {
		t.Fatalf("two sets = %q", two)
	}
	if renderCapabilitySets([]result{{task: task{ID: "c"}}}) != "" {
		t.Fatal("runs without a header must render nothing")
	}
}

func TestSummarizeTrajectoryReadsTheCapabilitySet(t *testing.T) {
	path := testenv.TempDir(t) + "/caps.trajectory.jsonl"
	if err := writeLines(path, []string{`{"seq":1,"ts":1000,"run_header":{"system":"x","system_hash":"h","tools_hash":"t","prefix_hash":"p","capabilities_hash":"sha256:cafe"}}`}); err != nil {
		t.Fatal(err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil || s.CapabilitiesHash != "sha256:cafe" {
		t.Fatalf("capabilities hash = %q, %v", s.CapabilitiesHash, err)
	}
}
