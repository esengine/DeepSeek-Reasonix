package serve

import (
	"sync"
	"testing"

	"reasonix/internal/control"
)

// TestControllerAccessorIsRaceSafe guards the switchModel concurrency contract:
// handlers read the controller through ctl() while a swap runs under the write
// lock. With the lock removed this fails under `go test -race` (the CI race job).
func TestControllerAccessorIsRaceSafe(t *testing.T) {
	a, b := &control.Controller{}, &control.Controller{}
	s := &Server{ctrl: a}

	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			if got := s.ctl(); got != a && got != b {
				t.Errorf("ctl() returned a pointer that was never set")
			}
		})
	}
	for range 16 {
		wg.Go(func() {
			s.mu.Lock()
			if s.ctrl == a {
				s.ctrl = b
			} else {
				s.ctrl = a
			}
			s.mu.Unlock()
		})
	}
	wg.Wait()
}
