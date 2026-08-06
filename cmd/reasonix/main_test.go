package main

import (
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/crashreport"
)

func TestRunWithCrashCaptureRecordsAndReraises(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	previous := runCLI
	t.Cleanup(func() { runCLI = previous })
	secret := "private prompt from panic"
	runCLI = func([]string, string) int { panic(secret) }

	func() {
		defer func() {
			if recovered := recover(); recovered != secret {
				t.Fatalf("reraised panic = %#v", recovered)
			}
		}()
		runWithCrashCapture([]string{"run"}, "v1.20.0")
	}()

	reports, err := crashreport.List(config.ReasonixHomeDir())
	if err != nil || len(reports) != 1 {
		t.Fatalf("captured reports=%d err=%v", len(reports), err)
	}
	// Token-like function and file basenames may be redacted by the current
	// privacy filter. Keep asserting that the top frame is the test call site
	// without requiring its pre-redaction basename.
	if got := reports[0].Report.TopFrame; !strings.Contains(got, "_test.go:") {
		t.Fatalf("top frame = %q, want sanitized panic call site in a test file", got)
	}
	preview, err := crashreport.Preview(reports[0].Report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(preview), secret) {
		t.Fatalf("panic value leaked into report: %s", preview)
	}
}

func TestRunWithCrashCapturePassesThroughExitCode(t *testing.T) {
	previous := runCLI
	t.Cleanup(func() { runCLI = previous })
	runCLI = func(args []string, version string) int {
		if len(args) != 1 || args[0] != "version" || version != "v1.20.0" {
			t.Fatalf("runCLI args=%v version=%q", args, version)
		}
		return 17
	}
	if got := runWithCrashCapture([]string{"version"}, "v1.20.0"); got != 17 {
		t.Fatalf("exit code=%d", got)
	}
}
