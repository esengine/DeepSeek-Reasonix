package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// runTextFixture points an isolated home at a fake provider that answers every
// request with plain text, and returns the workspace the run starts in.
func runTextFixture(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("RUN_TEXT_FAKE_KEY", "k")
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf(`default_model = "fake"

[[providers]]
name = "fake"
kind = "openai"
base_url = %q
model = "fake-model"
api_key_env = "RUN_TEXT_FAKE_KEY"
`, srv.URL), 0o644)
	dir := testenv.TempDir(t)
	t.Chdir(dir)
	return dir
}

// 1.x routes `chat` and `code` to the interactive session; they are the
// terminal UI's names here too, never an unknown command.
func TestChatAndCodeAreTheTerminalUI(t *testing.T) {
	for _, verb := range []string{"chat", "code"} {
		var code int
		stdout, stderr := captureCLIOutput(t, func() { code = Run([]string{verb, "--help"}, "test") })
		if code != 0 {
			t.Fatalf("reasonix %s --help exited %d, want 0 as in 1.x\nstdout:\n%s\nstderr:\n%s", verb, code, stdout, stderr)
		}
		if strings.Contains(stdout+stderr, "unknown command") {
			t.Fatalf("reasonix %s is still an unknown command:\n%s%s", verb, stdout, stderr)
		}
		if !strings.Contains(stdout+stderr, "--permission-mode") {
			t.Fatalf("reasonix %s --help did not print the session flags:\n%s%s", verb, stdout, stderr)
		}
	}
}

// 1.x accepts --preset ahead of everything, so `reasonix --preset X -p task`
// is a print-mode run with that preset.
func TestLeadingPresetRoutesPrintModeLike1x(t *testing.T) {
	cmd, args := normalizeCommand([]string{"--preset", "delivery", "-p", "hello"})
	if want := []string{"run", "--print", "--preset", "delivery", "hello"}; cmd != "run" || !slices.Equal(args, want) {
		t.Fatalf("normalizeCommand = (%q, %q), want (run, %q)", cmd, args, want)
	}
	runTextFixture(t)
	var code int
	stdout, stderr := captureCLIOutput(t, func() {
		code = Run([]string{"--preset", "balanced", "-p", "hello"}, "test")
	})
	if code != 0 || !strings.Contains(stdout, "done") {
		t.Fatalf("reasonix --preset balanced -p hello exited %d, want 0 with the answer\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

// 1.x's run takes --takeover; a command line carrying it must still run.
func TestRunAcceptsTakeover(t *testing.T) {
	runTextFixture(t)
	var code int
	stdout, stderr := captureCLIOutput(t, func() {
		code = Run([]string{"run", "--takeover", "hello"}, "test")
	})
	if code != 0 || !strings.Contains(stdout, "done") {
		t.Fatalf("reasonix run --takeover hello exited %d, want 0 with the answer\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
}

func TestTakeoverRefusalNamesWhyAndTheWayOut(t *testing.T) {
	msg := sessionLeaseResumeRefusal(sessionstore.ErrSessionLeaseHeld, true)
	for _, want := range []string{"--takeover", "--copy"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("takeover refusal %q does not mention %s", msg, want)
		}
	}
}

// 1.x's `reasonix sessions reindex|diagnose|cleanup` exit 0 and keep their
// JSON keys; cleanup is a dry run until --apply, which moves only a recovery
// branch another session fully covers.
func TestSessionsMaintenanceVerbsMatch1x(t *testing.T) {
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Chdir(testenv.TempDir(t))
	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	covered := forkCLIRecoveryBranch(t, dir, "covered", true)
	diverged := forkCLIRecoveryBranch(t, dir, "diverged", false)

	run := func(args ...string) (int, string) {
		t.Helper()
		var code int
		stdout, stderr := captureCLIOutput(t, func() { code = Run(append([]string{"sessions"}, args...), "test") })
		if code != 0 {
			t.Fatalf("reasonix sessions %s exited %d, want 0\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), code, stdout, stderr)
		}
		return code, stdout
	}

	_, out := run("reindex")
	if !strings.Contains(out, "4 sessions") {
		t.Fatalf("reindex should count the four sessions on disk:\n%s", out)
	}
	_, out = run("reindex", "--json")
	var status map[string]any
	if err := json.Unmarshal([]byte(out), &status); err != nil || status["indexed"] != float64(4) || status["state"] != "ready" {
		t.Fatalf("reindex --json = %s (%v), want state ready and indexed 4", out, err)
	}

	_, out = run("diagnose", "--json")
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("diagnose --json: %v\n%s", err, out)
	}
	for key, want := range map[string]float64{"sourceSessions": 4, "branches": 2, "cleanupEligible": 1, "movedToTrash": 0} {
		if report[key] != want {
			t.Fatalf("diagnose %s = %v, want %v\n%s", key, report[key], want, out)
		}
	}
	if report["dryRun"] != true {
		t.Fatalf("diagnose must be a dry run:\n%s", out)
	}

	_, out = run("cleanup")
	if !strings.Contains(out, "dry run; pass --apply") {
		t.Fatalf("cleanup without --apply must say it is a dry run:\n%s", out)
	}
	if _, err := os.Stat(covered); err != nil {
		t.Fatalf("dry-run cleanup touched the covered branch: %v", err)
	}

	_, out = run("cleanup", "--apply", "--json")
	if err := json.Unmarshal([]byte(out), &report); err != nil || report["movedToTrash"] != float64(1) {
		t.Fatalf("cleanup --apply = %s (%v), want movedToTrash 1", out, err)
	}
	if _, err := os.Stat(covered); !os.IsNotExist(err) {
		t.Fatalf("covered branch still present after cleanup --apply: %v", err)
	}
	if _, err := os.Stat(diverged); err != nil {
		t.Fatalf("cleanup moved a branch holding unique turns: %v", err)
	}
}

func forkCLIRecoveryBranch(t *testing.T, dir, name string, cover bool) string {
	t.Helper()
	parentPath := filepath.Join(dir, name+".jsonl")
	disk := sessionstore.NewSession("sys")
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	disk.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	disk.Add(provider.Message{Role: provider.RoleUser, Content: "disk " + name})
	if err := disk.Save(parentPath); err != nil {
		t.Fatal(err)
	}
	stale := sessionstore.NewSession("sys")
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	stale.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	stale.Add(provider.Message{Role: provider.RoleUser, Content: "local " + name})
	info, err := stale.SaveRecoveryBranch(sessionstore.RecoveryBranchOptions{OriginalPath: parentPath})
	if err != nil {
		t.Fatal(err)
	}
	if cover {
		merged, err := sessionstore.LoadSession(parentPath)
		if err != nil {
			t.Fatal(err)
		}
		merged.Replace(stale.Snapshot())
		merged.Add(provider.Message{Role: provider.RoleAssistant, Content: "answered after recovery"})
		if err := merged.SaveRewrite(parentPath); err != nil {
			t.Fatal(err)
		}
	}
	return info.Path
}
