package builtin

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"reasonix/internal/safety/sandbox"
)

func testProbe(t *testing.T, command string) pipeStatusProbe {
	t.Helper()
	return newPipeStatusProbe(sandbox.Shell{Kind: sandbox.ShellBash}, command, false)
}

func TestPipeStatusProbeAppliesOnlyToMaskedVerification(t *testing.T) {
	if !testProbe(t, "go test ./... 2>&1 | tail -5").active() {
		t.Fatal("a check whose status a later stage swallows must be probed")
	}
	for _, command := range []string{
		"go test ./...",              // status already answers for the check
		"cat notes.md | tail -5",     // nothing to decide
		"tail -5 log | go test ./..", // the check already decides the status
	} {
		if testProbe(t, command).active() {
			t.Errorf("%q was probed, want left alone", command)
		}
	}
	if newPipeStatusProbe(sandbox.Shell{Kind: sandbox.ShellPowerShell}, "go test ./... | tail -5", false).active() {
		t.Error("PowerShell has no PIPESTATUS to ask for")
	}
	if newPipeStatusProbe(sandbox.Shell{Kind: sandbox.ShellBash}, "go test ./... | tail -5", true).active() {
		t.Error("a background job reports its status elsewhere")
	}
}

// The wrapper must keep the command's own exit status and report the stage
// statuses bash saw, or it would trade one wrong answer for another.
func TestPipeStatusProbePreservesExitStatusAndReports(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash unavailable")
	}
	probe := testProbe(t, "go test ./... | tail -5")
	if !probe.active() {
		t.Fatal("probe inactive")
	}

	for _, tc := range []struct {
		name     string
		command  string
		wantCode int
		want     []int
	}{
		{"masked failure", "sh -c 'echo boom; exit 3' | tail -1", 0, []int{3, 0}},
		{"clean pass", "echo ok | tail -1", 0, []int{0, 0}},
		// The right side exits without reading, so a writing left side races it
		// for the pipe and reports SIGPIPE's 141 when it loses — only on a loaded
		// machine, which is what made this a CI-only failure. `true` never writes.
		{"failing tail", "true | sh -c 'exit 4'", 4, []int{0, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := exec.Command("bash", "-c", probe.wrap(tc.command)).CombinedOutput()
			code := 0
			var exitErr *exec.ExitError
			switch {
			case errors.As(err, &exitErr):
				code = exitErr.ExitCode()
			case err != nil:
				t.Fatalf("run: %v", err)
			}
			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d", code, tc.wantCode)
			}
			clean, status := probe.read(string(out))
			if len(status) != len(tc.want) {
				t.Fatalf("status = %v, want %v", status, tc.want)
			}
			for i := range status {
				if status[i] != tc.want[i] {
					t.Fatalf("status = %v, want %v", status, tc.want)
				}
			}
			if strings.Contains(clean, probe.marker()) {
				t.Fatalf("report leaked into model-visible output: %q", clean)
			}
		})
	}
}

func TestPipeStatusProbeReadWithoutReport(t *testing.T) {
	probe := testProbe(t, "go test ./... | tail -5")
	out, status := probe.read("ok  \tlogstat\t0.4s\n")
	if status != nil {
		t.Fatalf("status = %v, want none", status)
	}
	if out != "ok  \tlogstat\t0.4s\n" {
		t.Fatalf("output altered: %q", out)
	}
}

// The shape the traces are full of: a cheap check, then the suite behind a
// pipe. Both sides can end the command, but their widths differ, so the
// captured statuses identify which one ran.
func TestPipeStatusProbeCoversCheckThenPipedSuite(t *testing.T) {
	if !testProbe(t, "go vet ./... && go test ./... 2>&1 | tail -5").active() {
		t.Fatal("a suite piped behind a passing check must be probed")
	}
	if !testProbe(t, `echo "=== tests ===" && go test ./... | grep -E '^(ok|FAIL)'`).active() {
		t.Fatal("an echoed banner before the suite must not hide it")
	}
	// Same width on both sides: the statuses cannot say which pipeline ran.
	if testProbe(t, "cat log | tail && go test ./... | tail").active() {
		t.Error("equal-width candidates must not be probed")
	}
}
