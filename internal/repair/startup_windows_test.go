//go:build windows

package repair

import (
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestStartupTrackerWindowsHandleContentionReportsOnce(t *testing.T) {
	for iteration := range 100 {
		path := filepath.Join(t.TempDir(), "startup.json")
		writeLegacyStartupState(t, path, `{"phase":"healthy","pid":42}`)
		start := make(chan struct{})
		var reports atomic.Int32
		var group sync.WaitGroup
		for range 8 {
			group.Go(func() {
				tracker := NewStartupTracker(path)
				tracker.processAlive = func(int) bool { return false }
				<-start
				if tracker.ObservePreviousRun().Abnormal {
					reports.Add(1)
				}
			})
		}
		close(start)
		group.Wait()
		if got := reports.Load(); got != 1 {
			t.Fatalf("iteration %d reports = %d, want 1", iteration, got)
		}
	}
}
