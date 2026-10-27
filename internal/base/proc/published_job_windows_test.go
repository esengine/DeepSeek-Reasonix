//go:build windows

package proc

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const publishedJobMarker = "REASONIX_PUBLISHED_JOB_TASKKILL_MARKER"

// A renamed copy records a legacy PID fallback without terminating any PID.
func TestMain(m *testing.M) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if strings.EqualFold(filepath.Base(exe), "taskkill.exe") && os.Getenv(publishedJobMarker) != "" {
		if err := os.WriteFile(os.Getenv(publishedJobMarker), []byte(strings.Join(os.Args[1:], " ")), 0o600); err != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	// A recorder invocation must never fall through into the complete suite.
	if os.Getenv(publishedJobMarker) != "" && os.Getenv("REASONIX_PUBLISHED_JOB_CHILD") == "" {
		os.Exit(2)
	}
	os.Exit(m.Run())
}

func TestPublishedJobRecorderAlias(t *testing.T) {
	marker := publishedJobStub(t)
	cmd := exec.CommandContext(t.Context(), "taskkill", "-test.run=^TestPublishedJobChild$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "REASONIX_PUBLISHED_JOB_CHILD=return")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(data), "-test.run=^TestPublishedJobChild$") {
		t.Fatalf("bare taskkill alias did not enter recorder: %q %v", data, err)
	}
}

func publishedJobStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "taskkill.exe"), data, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "fallback.txt")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(publishedJobMarker, marker)
	resolved, err := exec.LookPath("taskkill")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Stat(filepath.Join(dir, "taskkill.exe"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Stat(resolved)
	if err != nil || !os.SameFile(want, got) {
		t.Fatalf("taskkill resolved outside the fixture: %q %v", resolved, err)
	}
	return marker
}

func publishedJobCommand(t *testing.T, ctx context.Context, hold bool) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestPublishedJobChild$", "-test.timeout=30s")
	mode := "return"
	if hold {
		mode = "hold"
	}
	cmd.Env = append(os.Environ(), "REASONIX_PUBLISHED_JOB_CHILD="+mode)
	HideWindow(cmd)
	return cmd
}

func TestPublishedJobChild(t *testing.T) {
	mode := os.Getenv("REASONIX_PUBLISHED_JOB_CHILD")
	if mode == "" {
		return
	}
	fmt.Println("PUBLISHED-JOB-READY")
	if mode == "hold" {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
}

func TestPublishedJobNoPIDFallback(t *testing.T) {
	for _, hold := range []bool{false, true} {
		name := "after_wait"
		if hold {
			name = "cancelled_wait"
		}
		t.Run(name, func(t *testing.T) {
			marker := publishedJobStub(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			cmd := publishedJobCommand(t, ctx, true)
			input, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			pipe, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
			})
			type result struct {
				tracked *TrackedCommand
				err     error
			}
			done := make(chan result, 1)
			go func() { p, e := RunCommand(ctx, cmd, RunOptions{Track: true}); done <- result{p, e} }()
			line, err := bufio.NewReader(pipe).ReadString('\n')
			if err != nil || strings.TrimSpace(line) != "PUBLISHED-JOB-READY" {
				t.Fatalf("child readiness=%q %v", line, err)
			}
			if hold {
				cancel()
			} else if _, err := fmt.Fprintln(input, "release"); err != nil {
				t.Fatal(err)
			}
			var got result
			select {
			case got = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("owned child did not finish")
			}
			if hold && got.err == nil {
				t.Fatalf("cancel=%v", got.err)
			}
			if !hold && got.err != nil {
				t.Fatal(got.err)
			}
			if got.tracked == nil || !got.tracked.Diagnostics().JobObjectCreated {
				t.Fatal("required native fixture has no Job Object")
			}
			// The assertion starts with the returned owner, after launch publication.
			if err := os.Remove(marker); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			got.tracked.Kill()
			got.tracked.Kill()
			if data, err := os.ReadFile(marker); !os.IsNotExist(err) {
				t.Fatalf("published Job Object fell through to PID taskkill: %q %v", data, err)
			}
		})
	}
}

func TestPublishedJobUsesKernelTree(t *testing.T) {
	cmd := publishedJobCommand(t, t.Context(), false)
	tracked, err := RunCommand(t.Context(), cmd, RunOptions{Track: true})
	if err != nil {
		t.Fatal(err)
	}
	if tracked == nil || !tracked.Diagnostics().JobObjectCreated {
		t.Fatal("required native fixture has no Job Object")
	}
	tracked.mu.Lock()
	owner := tracked.job
	tracked.mu.Unlock()
	if owner != nil {
		t.Cleanup(func() { owner.Kill(cmd) })
	}
	if tracked.Diagnostics().TreeTrackerStarted {
		t.Fatal("owned Job Object also started a PID tree walker")
	}
}

func TestPublishedJobFallbackControl(t *testing.T) {
	marker := publishedJobStub(t)
	cmd := publishedJobCommand(t, t.Context(), false)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	KillTree(cmd)
	data, err := os.ReadFile(marker)
	if err != nil || !strings.Contains(string(data), "/PID") {
		t.Fatalf("legacy control did not reach the recording stub: %q %v", data, err)
	}
}
