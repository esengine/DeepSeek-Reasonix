package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/permission"
	"reasonix/internal/safety/sandbox"
)

func runPlainAnswerFixture(t *testing.T) {
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
	t.Setenv("RUN_FLAGS_FAKE_KEY", "k")
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf("default_model = \"fake\"\n\n[[providers]]\nname = \"fake\"\nkind = \"openai\"\nbase_url = %q\nmodel = \"fake-model\"\napi_key_env = \"RUN_FLAGS_FAKE_KEY\"\n", srv.URL), 0o644)
	t.Chdir(testenv.TempDir(t))
}

func runCLIJSONResult(t *testing.T, argv ...string) (int, map[string]any, string) {
	t.Helper()
	var code int
	stdout, stderr := captureCLIOutput(t, func() { code = Run(argv, "test") })
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var result map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &result); err != nil {
		t.Fatalf("Run(%q) last stdout line is not JSON: %v\nstdout:\n%s\nstderr:\n%s", argv, err, stdout, stderr)
	}
	return code, result, stderr
}

// The model finished after writing without running a check. Decision 3 of the
// 1.x migration: that is a verdict in the result, exit 0; only
// --fail-on-unverified turns it into the dedicated status 3.
func TestRunUnverifiedFinishExitsZeroUnlessAsked(t *testing.T) {
	runWriteFileFixture(t)
	code, result, stderr := runCLIJSONResult(t, "run", "-y", "--output-format", "json", "write it")
	if code != 0 || result["is_error"] != false || result["subtype"] != "success" {
		t.Fatalf("unverified finish exited %d with %v, want 0 and a successful result\nstderr:\n%s", code, result, stderr)
	}
	readiness, ok := result["readiness"].(map[string]any)
	if !ok || readiness["attempts"] == nil {
		t.Fatalf("result lacks the unmet readiness verdict: %v", result)
	}
	if result["num_turns"] != float64(1) {
		t.Fatalf("num_turns = %v, want 1", result["num_turns"])
	}

	runWriteFileFixture(t)
	code, result, _ = runCLIJSONResult(t, "run", "-y", "--fail-on-unverified", "--output-format", "json", "write it")
	if code != runExitUnverified || result["readiness"] == nil {
		t.Fatalf("--fail-on-unverified exited %d with %v, want %d and the same verdict", code, result, runExitUnverified)
	}
}

func TestRunProseOnlyFinishHasNoReadinessDebt(t *testing.T) {
	dir := runToolCallFixture(t, "write_file", `{"path":"notes.md","content":"A neutral note."}`)
	if err := os.Remove(filepath.Join(dir, "note.txt")); err != nil {
		t.Fatal(err)
	}
	code, result, stderr := runCLIJSONResult(t, "run", "-y", "--fail-on-unverified", "--output-format", "json", "write a note")
	if code != 0 || result["is_error"] != false || result["readiness"] != nil {
		t.Fatalf("prose finish exited %d with %v; stderr: %s", code, result, stderr)
	}
}

// A write the headless policy refuses is not a failed run, but it is not
// silent either: the result lists it the way permission_denials readers expect.
func TestRunReportsPermissionDenials(t *testing.T) {
	runWriteFileFixture(t)
	code, result, stderr := runCLIJSONResult(t, "run", "--output-format", "json", "write it")
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr:\n%s", code, stderr)
	}
	denials, _ := result["permission_denials"].([]any)
	if len(denials) != 1 {
		t.Fatalf("permission_denials = %v, want the refused write_file", result["permission_denials"])
	}
	denial := denials[0].(map[string]any)
	wantCode := permission.RefusalUnattended
	if sandbox.Available() {
		wantCode = permission.RefusalUntrustedFolder
	}
	if denial["tool_name"] != "write_file" || denial["tool_use_id"] != "call_1" || denial["code"] != wantCode {
		t.Fatalf("denial = %v", denial)
	}

	runWriteFileFixture(t)
	var printCode int
	_, printErr := captureCLIOutput(t, func() { printCode = Run([]string{"-p", "write it"}, "test") })
	if printCode != 0 || !strings.Contains(printErr, "permission policy refused 1 tool call(s): write_file") {
		t.Fatalf("-p exited %d; stderr must name the refused call:\n%s", printCode, printErr)
	}

	runWriteFileFixture(t)
	_, done, _ := runCLIJSONResult(t, "run", "--events-jsonl", "write it")
	if done["kind"] != "run_done" || done["permission_denials"] != float64(1) {
		t.Fatalf("run_done = %v, want a content-free count of 1", done)
	}
}

func TestSuccessfulRunListsNoDenials(t *testing.T) {
	runPlainAnswerFixture(t)
	_, result, _ := runCLIJSONResult(t, "run", "--output-format", "json", "hi")
	if denials, ok := result["permission_denials"].([]any); !ok || len(denials) != 0 || result["readiness"] != nil {
		t.Fatalf("clean run result = %v, want permission_denials [] and no readiness", result)
	}
}
