package main

import (
	"testing"
	"testing/synctest"
)

func TestPromptHistoryTapeIdentityDoesNotDependOnClockTicks(t *testing.T) {
	dirA, dirB := t.TempDir(), t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		first, err := newPromptHistoryTape(dirA, "")
		if err != nil {
			t.Fatal(err)
		}
		second, err := newPromptHistoryTape(dirB, "")
		if err != nil {
			t.Fatal(err)
		}
		if first.nonce == second.nonce {
			t.Fatal("different prompt-history tapes shared an identity within one clock tick")
		}
	})
}
