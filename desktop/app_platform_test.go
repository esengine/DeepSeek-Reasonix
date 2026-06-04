package main

import (
	"runtime"
	"testing"
)

func TestAppPlatformReturnsRuntimeGOOS(t *testing.T) {
	app := NewApp()

	if got := app.Platform(); got != runtime.GOOS {
		t.Fatalf("Platform() = %q, want %q", got, runtime.GOOS)
	}
}
