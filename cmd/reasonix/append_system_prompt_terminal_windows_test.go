package main

import "testing"

func runAppendPromptTUI(t *testing.T, _ *contractCLI) {
	t.Helper()
	t.Skip("interactive CLI contract requires a Unix PTY; run/resume remain covered on Windows")
}
