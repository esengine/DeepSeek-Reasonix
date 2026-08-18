//go:build darwin && !cgo

package main

import (
	"errors"
	"testing"
)

func TestDarwinWorkspaceWatcherWithoutCGOIsUnavailable(t *testing.T) {
	w, err := newWorkspaceWatcher()
	if w != nil || !errors.Is(err, errDarwinWorkspaceWatchingRequiresCGO) {
		t.Fatalf("newWorkspaceWatcher() = (%T, %v), want nil and CGO requirement", w, err)
	}
}
