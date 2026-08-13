//go:build windows

package main

import "testing"

func TestCurrentProcessTopLevelWindowReusesEnumCallback(t *testing.T) {
	if enumWindowsCallback == 0 {
		t.Fatal("EnumWindows callback was not registered")
	}

	// Go's Windows runtime currently supports a finite callback table. The old
	// implementation allocated one callback per poll and exhausted that table
	// during a normal long-running session.
	for range 2100 {
		_ = currentProcessTopLevelWindow()
	}
}
