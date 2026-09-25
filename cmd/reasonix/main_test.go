package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/crashreport"
)

func TestRunWithCrashCaptureRecordsAndReraises(t *testing.T) {
	home := testenv.TempDir(t)
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

const fatalChildEnv = "REASONIX_TEST_FATAL_CHILD"

func TestGoroutinePanicIsReportedOnNextStart(t *testing.T) {
	if os.Getenv(fatalChildEnv) == "1" {
		runCLI = func([]string, string) int {
			go func() { panic("private prompt from background goroutine") }()
			select {}
		}
		runWithCrashCapture([]string{"chat"}, "v1.20.0")
		return
	}

	home := testenv.TempDir(t)
	child := exec.Command(os.Args[0], "-test.run=^TestGoroutinePanicIsReportedOnNextStart$")
	child.Env = append(os.Environ(), fatalChildEnv+"=1", "REASONIX_HOME="+home)
	output, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("child exited cleanly; output:\n%s", output)
	}
	if !strings.Contains(string(output), "panic:") {
		t.Fatalf("child did not die from the goroutine panic:\n%s", output)
	}

	t.Setenv("REASONIX_HOME", home)
	previous := runCLI
	t.Cleanup(func() { runCLI = previous })
	runCLI = func([]string, string) int { return 0 }
	runWithCrashCapture([]string{"version"}, "v1.20.0")

	reports, err := crashreport.List(home)
	if err != nil || len(reports) != 1 {
		t.Fatalf("pending reports after restart = %d (err %v), want the goroutine panic", len(reports), err)
	}
	report := reports[0].Report
	if !strings.Contains(report.Stack, "goroutine ") || !strings.Contains(report.Stack, "TestGoroutinePanicIsReportedOnNextStart") {
		t.Fatalf("stack = %q, want the panicking goroutine", report.Stack)
	}
	preview, err := crashreport.Preview(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(preview), "private prompt") {
		t.Fatalf("panic value leaked into report: %s", preview)
	}
}
