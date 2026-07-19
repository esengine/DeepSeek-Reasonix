package repair

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStartupTrackerRecommendsSafeModeAfterCrashLoop(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	tracker := NewStartupTracker(filepath.Join(t.TempDir(), "startup.json"))
	tracker.now = func() time.Time { return now }
	tracker.processAlive = func(int) bool { return false }
	if _, err := tracker.Begin("v1", false); err != nil {
		t.Fatal(err)
	}
	if tracker.SafeModeRecommended() {
		t.Fatal("safe mode recommended after only one failed start")
	}
	now = now.Add(time.Minute)
	if _, err := tracker.Begin("v1", false); err != nil {
		t.Fatal(err)
	}
	if !tracker.SafeModeRecommended() {
		t.Fatal("safe mode not recommended for the third start in the crash window")
	}
}

func TestStartupTrackerHealthyResetsCrashLoop(t *testing.T) {
	tracker := NewStartupTracker(filepath.Join(t.TempDir(), "startup.json"))
	tracker.processAlive = func(int) bool { return false }
	if _, err := tracker.Begin("v1", false); err != nil {
		t.Fatal(err)
	}
	if err := tracker.MarkReady(); err != nil {
		t.Fatal(err)
	}
	state, err := tracker.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != "ready" || state.ConsecutiveFailures != 1 {
		t.Fatalf("state = %+v", state)
	}
	if err := tracker.MarkHealthy(); err != nil {
		t.Fatal(err)
	}
	state, err = tracker.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != "healthy" || state.ConsecutiveFailures != 0 {
		t.Fatalf("healthy state = %+v", state)
	}
	if err := tracker.MarkFailed(errors.New("boom")); err != nil {
		t.Fatal(err)
	}
}

func TestStartupTrackerCountsExplicitFailures(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)
	tracker := NewStartupTracker(filepath.Join(t.TempDir(), "startup.json"))
	tracker.now = func() time.Time { return now }
	tracker.processAlive = func(int) bool { return false }
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := tracker.Begin("v1", false); err != nil {
			t.Fatal(err)
		}
		if err := tracker.MarkFailed(errors.New("wails failed")); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
	}
	if !tracker.SafeModeRecommended() {
		t.Fatal("explicit failed phases did not trigger safe mode recommendation")
	}
}

func TestStartupTrackerDoesNotCountLiveDuplicateLaunch(t *testing.T) {
	tracker := NewStartupTracker(filepath.Join(t.TempDir(), "startup.json"))
	tracker.processAlive = func(pid int) bool { return pid == 42 }
	state := StartupState{SchemaVersion: 1, Phase: "starting", PID: 42, ConsecutiveFailures: 1, WindowStartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := tracker.write(state); err != nil {
		t.Fatal(err)
	}
	got, err := tracker.Begin("v2", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 42 || got.ConsecutiveFailures != 1 {
		t.Fatalf("live startup was overwritten: %+v", got)
	}
}

func TestStartupTrackerDoesNotOverwriteLiveReadyInstance(t *testing.T) {
	tracker := NewStartupTracker(filepath.Join(t.TempDir(), "startup.json"))
	tracker.processAlive = func(pid int) bool { return pid == 42 }
	state := StartupState{SchemaVersion: 1, Phase: "ready", PID: 42, Version: "v1"}
	if err := tracker.write(state); err != nil {
		t.Fatal(err)
	}
	got, err := tracker.Begin("v2", false)
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 42 || got.Version != "v1" || got.Phase != "ready" {
		t.Fatalf("live ready instance was overwritten: %+v", got)
	}
}

func TestStartupTrackerIgnoresDuplicateLaunchesWhileHealthy(t *testing.T) {
	// A duplicate launch hands off to the running instance and exits through
	// os.Exit without OnShutdown (Wails single-instance lock), so repeated
	// shortcut clicks during a healthy run must not count as crashes.
	tracker := NewStartupTracker(filepath.Join(t.TempDir(), "startup.json"))
	tracker.processAlive = func(pid int) bool { return pid == 42 }
	state := StartupState{SchemaVersion: 1, Phase: "healthy", PID: 42, Version: "v1", WindowStartedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := tracker.write(state); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		got, err := tracker.Begin("v1", false)
		if err != nil {
			t.Fatal(err)
		}
		if got.PID != 42 || got.Phase != "healthy" || got.ConsecutiveFailures != 0 {
			t.Fatalf("duplicate launch %d overwrote healthy state: %+v", i+1, got)
		}
	}
	if tracker.SafeModeRecommended() {
		t.Fatal("duplicate launches during a healthy run triggered safe mode")
	}
}

// TestStartupTrackerBeginSerializesConcurrentColdStarts pins the file-lock
// contract: racing cold starts must serialize their read-modify-write cycles,
// so every Begin lands one increment instead of losing updates.
func TestStartupTrackerBeginSerializesConcurrentColdStarts(t *testing.T) {
	now := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	tracker := NewStartupTracker(filepath.Join(t.TempDir(), "startup.json"))
	tracker.now = func() time.Time { return now }
	tracker.processAlive = func(int) bool { return false }
	const launches = 8
	var wg sync.WaitGroup
	for i := 0; i < launches; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := tracker.Begin("v1", false); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	state, err := tracker.Read()
	if err != nil {
		t.Fatal(err)
	}
	if state.ConsecutiveFailures != launches {
		t.Fatalf("consecutive failures = %d, want %d (lost update under concurrency)", state.ConsecutiveFailures, launches)
	}
}

// TestStartupTrackerTransitionRefusesLiveForeignOwner pins the ownership rule:
// a process must not rewrite a startup record owned by another live process,
// while records left by dead owners stay transitionable (Guard's post-rollback
// MarkClean).
func TestStartupTrackerTransitionRefusesLiveForeignOwner(t *testing.T) {
	tracker := NewStartupTracker(filepath.Join(t.TempDir(), "startup.json"))
	foreign := os.Getpid() + 1
	tracker.processAlive = func(pid int) bool { return pid == foreign }
	state := StartupState{SchemaVersion: 1, Phase: "starting", PID: foreign, ConsecutiveFailures: 1}
	if err := tracker.write(state); err != nil {
		t.Fatal(err)
	}
	if err := tracker.MarkClean(); err != nil {
		t.Fatal(err)
	}
	got, err := tracker.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != "starting" || got.PID != foreign {
		t.Fatalf("live foreign owner's record was rewritten: %+v", got)
	}
	// Once the owner is dead the same transition must go through.
	tracker.processAlive = func(int) bool { return false }
	if err := tracker.MarkClean(); err != nil {
		t.Fatal(err)
	}
	got, err = tracker.Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != "clean-exit" || got.ConsecutiveFailures != 0 {
		t.Fatalf("dead owner's record was not transitioned: %+v", got)
	}
}

func TestStartupTrackerWithoutStateDirIsDisabled(t *testing.T) {
	tracker := &StartupTracker{now: time.Now, processAlive: func(int) bool { return false }}
	if _, err := tracker.Begin("v1", false); err != nil {
		t.Fatal(err)
	}
	if tracker.Path() != "" {
		t.Fatalf("path = %q", tracker.Path())
	}
}
