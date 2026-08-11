//go:build windows

package mcplaunch

import (
	"fmt"
	"testing"

	"golang.org/x/sys/windows"
)

func TestLaunchLockContentionRecognizesWindowsDeleteRaces(t *testing.T) {
	for _, err := range []error{
		windows.ERROR_ACCESS_DENIED,
		windows.ERROR_SHARING_VIOLATION,
		fmt.Errorf("open lock: %w", windows.ERROR_ACCESS_DENIED),
	} {
		if !launchLockContention(err) {
			t.Fatalf("launchLockContention(%v) = false, want true", err)
		}
	}
	if launchLockContention(windows.ERROR_PATH_NOT_FOUND) {
		t.Fatal("path-not-found must not be retried as lock contention")
	}
}
