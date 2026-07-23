package main

import (
	"context"
	"testing"
	"time"
)

func TestWindowsWebView2StartupFallbackScope(t *testing.T) {
	tests := []struct {
		name string
		goos string
		want bool
	}{
		{name: "Windows window", goos: "windows", want: true},
		{name: "non-Windows window", goos: "darwin", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldStartWindowsWebView2StartupFallback(tt.goos); got != tt.want {
				t.Fatalf("shouldStartWindowsWebView2StartupFallback(%q) = %v, want %v", tt.goos, got, tt.want)
			}
		})
	}
}

func TestAwaitStartupFallbackFiresWhenDOMIsNotReady(t *testing.T) {
	timeout := make(chan time.Time, 1)
	timeout <- time.Now()
	if !awaitStartupFallback(context.Background(), timeout, func() bool { return false }) {
		t.Fatal("startup fallback did not fire after timeout")
	}
}

func TestAwaitStartupFallbackSkipsReadyWindow(t *testing.T) {
	timeout := make(chan time.Time, 1)
	timeout <- time.Now()
	if awaitStartupFallback(context.Background(), timeout, func() bool { return true }) {
		t.Fatal("startup fallback fired after domReady")
	}
}

func TestAwaitStartupFallbackStopsWithApplication(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if awaitStartupFallback(ctx, make(chan time.Time), func() bool { return false }) {
		t.Fatal("startup fallback fired after application shutdown")
	}
}
