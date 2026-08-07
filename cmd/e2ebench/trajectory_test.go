package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeTrajectoryAttributesTimeAndSkipsTruncatedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.trajectory.jsonl")
	lines := []string{
		`{"schema_version":1,"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"schema_version":1,"seq":2,"ts":1500,"event":{"kind":"tool_dispatch","tool":{"name":"bash"}}}`,
		`{"schema_version":1,"seq":3,"ts":2000,"event":{"kind":"tool_result","tool":{"name":"bash","durationMs":400}}}`,
		`{"schema_version":1,"seq":4,"ts":2500,"event":{"kind":"tool_result","tool":{"name":"grep","durationMs":300,"parentId":"task-1"}}}`,
		`{"schema_version":1,"seq":5,"ts":4000,"event":{"kind":"turn_done"}}`,
		`{"schema_version":1,"seq":6,"ts":4100,"event":{"kind":"tool_res`, // killed mid-write
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.Records != 5 {
		t.Errorf("records = %d, want 5 (truncated tail skipped)", s.Records)
	}
	if s.SpanMs != 3000 {
		t.Errorf("span = %d, want 3000", s.SpanMs)
	}
	if s.ToolMs != 400 {
		t.Errorf("tool ms = %d, want 400 (subagent call must not double-book)", s.ToolMs)
	}
	if s.ModelMs != 2600 {
		t.Errorf("model ms = %d, want 2600", s.ModelMs)
	}
}

func TestSummarizeTrajectoryDecomposesModelRounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rounds.trajectory.jsonl")
	lines := []string{
		`{"seq":1,"ts":1000,"event":{"kind":"turn_started"}}`,
		`{"seq":2,"ts":3000,"event":{"kind":"tool_dispatch","tool":{"name":"read_file","partial":true}}}`, // round 1 gap: 2000
		`{"seq":3,"ts":3400,"event":{"kind":"tool_dispatch","tool":{"name":"read_file"}}}`,                // same batch: no new round
		`{"seq":4,"ts":5000,"event":{"kind":"tool_result","tool":{"name":"read_file","durationMs":400}}}`,
		`{"seq":5,"ts":5100,"event":{"kind":"tool_dispatch","tool":{"name":"grep","parentId":"task-1"}}}`, // subagent: ignored
		`{"seq":6,"ts":5200,"event":{"kind":"tool_result","tool":{"name":"grep","durationMs":50,"parentId":"task-1"}}}`,
		`{"seq":7,"ts":6000,"event":{"kind":"retrying"}}`,
		`{"seq":8,"ts":9000,"event":{"kind":"tool_dispatch","tool":{"name":"bash"}}}`, // round 2 gap: 9000-5000=4000
		`{"seq":9,"ts":9500,"event":{"kind":"tool_result","tool":{"name":"bash","durationMs":450}}}`,
		`{"seq":10,"ts":12000,"event":{"kind":"turn_done"}}`, // final answer round gap: 2500
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	s, err := summarizeTrajectory(path)
	if err != nil {
		t.Fatalf("summarizeTrajectory: %v", err)
	}
	if s.ModelRounds != 3 {
		t.Errorf("model rounds = %d, want 3 (two tool rounds + final answer)", s.ModelRounds)
	}
	if s.ModelGapTotalMs != 8500 {
		t.Errorf("model gap total = %d, want 8500", s.ModelGapTotalMs)
	}
	if s.ModelGapP95Ms != 4000 {
		t.Errorf("model gap p95 = %d, want 4000", s.ModelGapP95Ms)
	}
	if s.Retries != 1 {
		t.Errorf("retries = %d, want 1", s.Retries)
	}
	if s.ToolMs != 850 {
		t.Errorf("tool ms = %d, want 850 (subagent excluded)", s.ToolMs)
	}
}

func TestRenderBodyReportsPerSolvedEfficiency(t *testing.T) {
	solved := result{task: task{ID: "a"}, Passed: true, WallMs: 60_000}
	solved.Steps = 6
	solved.ToolCalls = 9
	solved.Trajectory = &trajectorySummary{ModelRounds: 5}
	failed := result{task: task{ID: "b"}, WallMs: 40_000}
	failed.Steps = 8
	failed.ToolCalls = 3
	failed.Trajectory = &trajectorySummary{ModelRounds: 7}

	got := renderBody([]result{solved, failed})
	for _, want := range []string{
		"**Per solved task:**",
		"**model requests** 14.0", // failures' spend charged to the solve
		"tool calls 12.0",
		"wall 1m40s",
		"model rounds 12.0",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("per-solved line missing %q:\n%s", want, got)
		}
	}

	if got := renderBody([]result{failed}); strings.Contains(got, "Per solved task") {
		t.Fatalf("no solves must mean no per-solved line:\n%s", got)
	}
}

func TestRenderBodyIncludesTimeAttributionOnlyForRecordedRuns(t *testing.T) {
	plain := result{task: task{ID: "a"}, Passed: true}
	if got := renderBody([]result{plain}); strings.Contains(got, "Time attribution") {
		t.Fatalf("unrecorded run must not report time attribution:\n%s", got)
	}

	recorded := plain
	recorded.Trajectory = &trajectorySummary{Records: 5, SpanMs: 3000, ToolMs: 1000, ModelMs: 2000}
	got := renderBody([]result{recorded})
	if !strings.Contains(got, "Time attribution") || !strings.Contains(got, "(1 recorded runs)") {
		t.Fatalf("recorded run missing time attribution:\n%s", got)
	}
}
