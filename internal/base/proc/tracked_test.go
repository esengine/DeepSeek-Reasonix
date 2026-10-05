package proc

import (
	"sync"
	"testing"
)

// TestTrackedJobReleasesOnce is the invariant the handle exists to hold: a
// sidecar reaches kill from four places, two of them their own goroutine, and
// its wait path releases as well. Whichever arrived second used to close the
// handle again, and on Windows the second close lands on whatever the OS has
// since given that value to.
func TestTrackedJobReleasesOnce(t *testing.T) {
	var released int
	job := &TrackedJob{}
	job.release = func() { released++ }

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(2)
		go func() { defer wg.Done(); job.Kill(nil) }()
		go func() { defer wg.Done(); job.Finish() }()
	}
	wg.Wait()

	if released != 1 {
		t.Fatalf("released %d times across concurrent kill and finish; want exactly 1", released)
	}
}

// A job nobody started releases nothing and still answers: teardown runs on
// paths where the process never began.
func TestNilTrackedJobIsInert(t *testing.T) {
	var job *TrackedJob
	job.Finish()
	job.Kill(nil)
	if job.Tracked() {
		t.Fatal("a nil job reported an OS handle")
	}
}
