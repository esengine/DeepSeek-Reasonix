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
