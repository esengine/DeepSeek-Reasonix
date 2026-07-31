package regexp2

import (
	"fmt"
	"testing"
	"time"
)

func init() {
	//speed up testing by making the timeout clock 1ms instead of 100ms...
	//bad for benchmark tests though
	SetTimeoutCheckPeriod(time.Millisecond)
}
func TestDeadline(t *testing.T) {
	for _, delay := range []time.Duration{
		clockPeriod / 10,
		clockPeriod,
		clockPeriod * 5,
		clockPeriod * 10,
	} {
		delay := delay // Make copy for parallel sub-test.
		t.Run(fmt.Sprint(delay), func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			d := makeDeadline(delay)
			if d.reached() {
				t.Fatalf("deadline (%v) unexpectedly expired immediately", delay)
			}
			time.Sleep(delay / 2)
			if d.reached() {
				t.Fatalf("deadline (%v) expired too soon (after %v)", delay, time.Since(start))
			}
			time.Sleep(delay/2 + 2*clockPeriod) // Give clock time to tick
			if !d.reached() {
				t.Fatalf("deadline (%v) did not expire within %v", delay, time.Since(start))
			}
		})
	}
}

func TestStopTimeoutClock(t *testing.T) {
	// run a quick regex with a long timeout
	// make sure the stop clock returns quickly
	r := MustCompile(".", 0)
	r.MatchTimeout = time.Second * 10

	r.MatchString("a")
	start := time.Now()
	StopTimeoutClock()
	stop := time.Now()

	if want, got := clockPeriod*2, stop.Sub(start); want < got {
		t.Errorf("Expected duration less than %v, got %v", want, got)
	}
	if want, got := false, fast.running; want != got {
		t.Errorf("Expected isRunning to be %v, got %v", want, got)
	}
}
