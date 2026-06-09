//go:build windows

package main

import (
	"runtime"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDesktopTrayLoopRunsOnLockedOSThread(t *testing.T) {
	done := make(chan struct{})
	runDesktopTrayLoop(func() {
		first := windows.GetCurrentThreadId()
		for i := 0; i < 100; i++ {
			runtime.Gosched()
		}
		if got := windows.GetCurrentThreadId(); got != first {
			t.Fatalf("tray loop moved OS threads: first=%d got=%d", first, got)
		}
		close(done)
	})

	select {
	case <-done:
	default:
		t.Fatal("tray loop did not run")
	}
}
