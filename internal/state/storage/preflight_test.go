package storage

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func TestPlanCreatesNothingForATargetStillBeingTyped(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	base := testenv.TempDir(t)
	target := filepath.Join(base, "deep", "er", "moved")

	plan := PlanMove(t.Context(), config.RootState, target)

	if !plan.OK() {
		t.Fatalf("a writable parent was refused: %+v", plan.Refusals)
	}
	if entries, err := os.ReadDir(base); err != nil || len(entries) != 0 {
		t.Fatalf("planning left %d entries under %s (err %v), want none", len(entries), base, err)
	}
	if plan.Free <= 0 {
		t.Fatalf("free space for a missing target was not read from its nearest existing folder: %d", plan.Free)
	}
}

func TestPlanRefusesATargetWhoseNearestFolderIsAFile(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	base := testenv.TempDir(t)
	file := filepath.Join(base, "plain")
	write(t, file, 16)

	plan := PlanMove(t.Context(), config.RootState, filepath.Join(file, "moved"))

	if plan.OK() {
		t.Fatal("a target under a regular file was accepted")
	}
	if entries, _ := os.ReadDir(base); len(entries) != 1 {
		t.Fatalf("planning changed %s", base)
	}
}

func TestMoveCreatesTheTargetItself(t *testing.T) {
	home := stateHome(t)
	seedState(t, home)
	target := filepath.Join(testenv.TempDir(t), "a", "b", "moved")

	plan := PlanMove(t.Context(), config.RootState, target)
	if err := Move(t.Context(), plan, nil); err != nil {
		t.Fatalf("move into a folder that did not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "sessions", "a.jsonl")); err != nil {
		t.Fatalf("copy missing from the created target: %v", err)
	}
}
