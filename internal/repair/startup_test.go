package repair

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func writeLegacyStartupState(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStartupTrackerObservesDeadLegacyOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.json")
	started := time.Now().UTC().Add(-time.Minute)
	updated := started.Add(45 * time.Second)
	writeLegacyStartupState(t, path, `{
  "schemaVersion": 1,
  "phase": "healthy",
  "version": "v1.19.1",
  "installProfile": "installer",
  "updateFromVersion": "v1.19.0",
  "updateToVersion": "v1.19.1",
  "pid": 42,
  "safeMode": true,
  "startedAt": "`+started.Format(time.RFC3339Nano)+`",
  "updatedAt": "`+updated.Format(time.RFC3339Nano)+`"
}`)

	tracker := NewStartupTracker(path)
	tracker.processAlive = func(int) bool { return false }
	got := tracker.ObservePreviousRun()
	if !got.Abnormal || got.Phase != "healthy" || got.Version != "v1.19.1" || got.InstallProfile != "installer" {
		t.Fatalf("observation = %+v", got)
	}
	if got.UpdateFrom != "v1.19.0" || got.UpdateTo != "v1.19.1" || got.UptimeBucket != "m_0_2" {
		t.Fatalf("observation metadata = %+v", got)
	}
	if got := tracker.ObservePreviousRun(); got.Abnormal {
		t.Fatalf("claimed legacy record replayed: %+v", got)
	}
}

func TestStartupTrackerIgnoresLiveAndCleanLegacyRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.json")
	tracker := NewStartupTracker(path)
	tracker.processAlive = func(pid int) bool { return pid == 42 }

	writeLegacyStartupState(t, path, `{"phase":"ready","pid":42}`)
	if got := tracker.ObservePreviousRun(); got.Abnormal {
		t.Fatalf("live owner reported abnormal: %+v", got)
	}
	writeLegacyStartupState(t, path, `{"phase":"clean-exit","pid":42}`)
	if got := tracker.ObservePreviousRun(); got.Abnormal {
		t.Fatalf("clean exit reported abnormal: %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("clean legacy record was not consumed: %v", err)
	}
}

func TestStartupTrackerConcurrentClaimReportsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.json")
	writeLegacyStartupState(t, path, `{"phase":"healthy","version":"v1.19.1","pid":42}`)

	const observers = 16
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	var reports atomic.Int32
	for range observers {
		ready.Add(1)
		done.Go(func() {
			tracker := NewStartupTracker(path)
			tracker.processAlive = func(int) bool { return false }
			ready.Done()
			<-start
			if tracker.ObservePreviousRun().Abnormal {
				reports.Add(1)
			}
		})
	}
	ready.Wait()
	close(start)
	done.Wait()
	if got := reports.Load(); got != 1 {
		t.Fatalf("concurrent reports = %d, want 1", got)
	}
}

func TestStartupTrackerInvalidOrMissingStateIsIgnored(t *testing.T) {
	path := filepath.Join(t.TempDir(), "startup.json")
	tracker := NewStartupTracker(path)
	if got := tracker.ObservePreviousRun(); got.Abnormal {
		t.Fatalf("missing state reported abnormal: %+v", got)
	}
	writeLegacyStartupState(t, path, `{broken`)
	if got := tracker.ObservePreviousRun(); got.Abnormal {
		t.Fatalf("invalid state reported abnormal: %+v", got)
	}
}
