package main

import (
	"errors"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestEditUserConfigIfChangedPropagatesLockFailureBeforeNoOp(t *testing.T) {
	want := errors.New("timed out acquiring config lock")
	mutated := false
	err := editUserConfigIfChangedAtPath(
		config.UserConfigPath(),
		func(string) (func(), error) { return nil, want },
		func(*config.Config) (bool, error) {
			mutated = true
			return false, nil
		},
	)
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "lock user config edits") {
		t.Fatalf("lock failure = %v, want wrapped %v", err, want)
	}
	if mutated {
		t.Fatal("no-op mutation ran without the cross-process lock")
	}
}
