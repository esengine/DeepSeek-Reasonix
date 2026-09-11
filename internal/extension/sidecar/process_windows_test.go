//go:build windows

package sidecar

import (
	"errors"
	"sync"
	"testing"

	"golang.org/x/sys/windows"

	"reasonix/internal/proc"
)

func TestReleasedJobHandleIsNeverClosedAgain(t *testing.T) {
	for _, first := range []string{"kill", "finish"} {
		t.Run(first, func(t *testing.T) {
			cmd := proc.Command("cmd", "/c", "exit", "0")
			if err := cmd.Run(); err != nil {
				t.Fatal(err)
			}
			job, err := windows.CreateJobObject(nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			p := &process{cmd: cmd, job: uintptr(job)}
			if first == "kill" {
				p.kill()
			} else {
				p.finishJob()
			}
			if err := windows.SetHandleInformation(job, 0, 0); !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
				t.Fatalf("completed cleanup did not invalidate the Job Object: %v", err)
			}
			event, err := windows.CreateEvent(nil, 0, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer windows.CloseHandle(event)
			// Model Windows reusing the stale handle for an unrelated object;
			// choosing its value explicitly avoids depending on allocator timing.
			p.job = uintptr(event)
			var calls sync.WaitGroup
			for range 8 {
				calls.Go(p.kill)
				calls.Go(p.finishJob)
			}
			calls.Wait()
			if err := windows.SetEvent(event); err != nil {
				t.Fatalf("cleanup closed an unrelated reused handle: %v", err)
			}
		})
	}
}
