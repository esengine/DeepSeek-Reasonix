//go:build !linux

package main

import (
	"os"
	"testing"
)

func TestConfigureWebKitRendererRecoveryIsNoop(t *testing.T) {
	const key = "WEBKIT_DISABLE_DMABUF_RENDERER"
	previous, set := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if set {
			_ = os.Setenv(key, previous)
		} else {
			_ = os.Unsetenv(key)
		}
	})

	configureWebKitRendererRecovery(true)
	if value, exists := os.LookupEnv(key); exists {
		t.Fatalf("non-Linux recovery unexpectedly set %s=%q", key, value)
	}
}
