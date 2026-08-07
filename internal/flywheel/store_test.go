package flywheel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreSaveLoadTrajectory(t *testing.T) {
	s, err := NewStore(filepath.Join(t.TempDir(), "flywheel"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tr := &Trajectory{
		ID: "traj_1", Task: "fix build", Session: "s1",
		Steps: []Step{{Kind: "tool_call", Tool: "read_file", DurMs: 3, OK: true}},
	}
	if err := s.SaveTrajectory(tr); err != nil {
		t.Fatalf("SaveTrajectory: %v", err)
	}
	all, err := s.LoadTrajectories()
	if err != nil {
		t.Fatalf("LoadTrajectories: %v", err)
	}
	if len(all) != 1 || all[0].ID != "traj_1" || all[0].Task != "fix build" {
		t.Fatalf("loaded = %+v", all)
	}
}

func TestJudgeTrajectoryLabels(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "fw"))
	j := HeuristicJudge{}

	// Green with multiple successful steps → excellent.
	green := &Trajectory{ID: "g", Verify: &Verify{Kind: "go_test", OK: true, Detail: "ok"},
		Steps: []Step{{Kind: "tool_call", Tool: "a", OK: true}, {Kind: "tool_call", Tool: "b", OK: true}}}
	l, err := s.JudgeTrajectory(green, j)
	if err != nil {
		t.Fatalf("JudgeTrajectory: %v", err)
	}
	if l.Name != "excellent" {
		t.Errorf("green want excellent, got %+v", l)
	}

	// Red verification → failed + failure file persisted.
	red := &Trajectory{ID: "r", Verify: &Verify{Kind: "go_test", OK: false, Detail: "boom"},
		Steps: []Step{{Kind: "tool_call", Tool: "a", OK: true}}}
	l, err = s.JudgeTrajectory(red, j)
	if err != nil {
		t.Fatalf("JudgeTrajectory: %v", err)
	}
	if l.Name != "failed" {
		t.Errorf("red want failed, got %+v", l)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "memory", "failures", "r.md")); err != nil {
		t.Errorf("failure file not written: %v", err)
	}
}

func TestSearchTrajectoriesBM25(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "fw"))
	_ = s.SaveTrajectory(&Trajectory{ID: "a", Task: "fix go build failure in agent package"})
	_ = s.SaveTrajectory(&Trajectory{ID: "b", Task: "add godot smoke test for scene loading"})

	hits, err := s.SearchTrajectories("go build failure", 5)
	if err != nil {
		t.Fatalf("SearchTrajectories: %v", err)
	}
	if len(hits) == 0 || hits[0].Trajectory.ID != "a" {
		t.Fatalf("want build trajectory first, got %+v", hits)
	}
}

func TestNotesAndFailureFiles(t *testing.T) {
	s, _ := NewStore(filepath.Join(t.TempDir(), "fw"))
	if err := s.AppendNote("- 约定：不建副本仓库"); err != nil {
		t.Fatalf("AppendNote: %v", err)
	}
	notes, err := s.ReadNotes()
	if err != nil || !strings.Contains(notes, "不建副本仓库") {
		t.Fatalf("ReadNotes = %q, err=%v", notes, err)
	}
}

func TestVerifyRun(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not on PATH")
	}
	v := VerifyRun(context.Background(), "../..", "go_test",
		[]string{"go", "test", "./internal/flywheel/", "-count=1", "-run", "TestStoreSaveLoadTrajectory"}, 30*time.Second)
	if !v.OK {
		t.Fatalf("verify should pass: %+v", v)
	}
	if v.Kind != "go_test" || v.Detail == "" {
		t.Errorf("verify fields: %+v", v)
	}

	// Failing command → red verify.
	v = VerifyRun(context.Background(), ".", "smoke", []string{"sh", "-c", "exit 3"}, 10*time.Second)
	if v.OK {
		t.Errorf("failing command must be red: %+v", v)
	}

	// Empty command guarded.
	v = VerifyRun(context.Background(), ".", "smoke", nil, 0)
	if v.OK || !strings.Contains(v.Detail, "no command") {
		t.Errorf("empty cmd: %+v", v)
	}
}
