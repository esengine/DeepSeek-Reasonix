// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build go1.27 && !http2legacy

package http2_test

import (
	"os/exec"
	"testing"
)

func TestLegacy(t *testing.T) {
	goTest(t, "-tags=http2legacy", "golang.org/x/net/http2")
}

// TestSuccess is a no-op test for confirming we can re-run "go test".
func TestSuccess(t *testing.T) {}

func goTest(t *testing.T, args ...string) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("can't find go tool: %v", err)
	}

	// Skip this test if we can't run any tests at all.
	checkCmd := exec.CommandContext(t.Context(), goTool,
		"test", "-run=^TestSuccess$", ".")
	if out, err := checkCmd.CombinedOutput(); err != nil {
		t.Skipf("can't run trivial go test\n%s", out)
	}

	testCmd := exec.CommandContext(t.Context(), goTool, "test")
	testCmd.Args = append(testCmd.Args, args...)
	if out, err := testCmd.CombinedOutput(); err != nil {
		t.Fatalf("%q failed: %v\n%s", testCmd.Args, err, out)
	}
}
