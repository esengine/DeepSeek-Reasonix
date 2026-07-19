//go:build windows

package mcptrust

import (
	"fmt"
	"testing"

	"golang.org/x/sys/windows"
)

func TestTrustLockContentionRecognizesWindowsDeleteRaces(t *testing.T) {
	for _, err := range []error{
		windows.ERROR_ACCESS_DENIED,
		windows.ERROR_SHARING_VIOLATION,
		fmt.Errorf("open lock: %w", windows.ERROR_ACCESS_DENIED),
	} {
		if !trustLockContention(err) {
			t.Fatalf("trustLockContention(%v) = false, want true", err)
		}
	}
	if trustLockContention(windows.ERROR_PATH_NOT_FOUND) {
		t.Fatal("path-not-found must not be retried as lock contention")
	}
}
