package cli

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/spf13/pflag"

	"reasonix/internal/base/testenv"
)

// A session flag keeps its meaning on either side of the run verb: flags
// written before it travel into run when run reads every one of them as the
// terminal UI would, and otherwise the command line stays the terminal UI's.
func TestNormalizeCommandCarriesLeadingFlagsIntoRun(t *testing.T) {
	cases := []struct {
		args     []string
		wantCmd  string
		wantArgs []string
	}{
		{[]string{"--permission-mode", "auto", "run", "hi"}, "run", []string{"run", "--permission-mode", "auto", "hi"}},
		{[]string{"-y", "run", "hi"}, "run", []string{"run", "-y", "hi"}},
		{[]string{"--yolo", "--model", "m", "run", "--max-steps", "3", "hi"}, "run", []string{"run", "--yolo", "--model", "m", "--max-steps", "3", "hi"}},
		{[]string{"--yolo", "run", "the", "tests"}, "run", []string{"run", "--yolo", "the", "tests"}},
		{[]string{"--resume", "abc", "run", "hi"}, "run", []string{"run", "--resume=abc", "hi"}},
		{[]string{"--model", "m", "run the tests"}, "", []string{"--model", "m", "run the tests"}},
		{[]string{"--model", "m"}, "", []string{"--model", "m"}},
		{[]string{"-y", "hi"}, "-y", []string{"-y", "hi"}},
		{[]string{"--model", "m", "--", "run", "hi"}, "", []string{"--model", "m", "--", "run", "hi"}},
		// The terminal UI's optional-value --resume and its own flags stay its.
		{[]string{"--resume", "run", "hi"}, "", []string{"--resume", "run", "hi"}},
		{[]string{"-r", "run", "hi"}, "", []string{"-r", "run", "hi"}},
		{[]string{"--resume", "--yolo", "run", "hi"}, "", []string{"--resume", "--yolo", "run", "hi"}},
		{[]string{"--model", "m", "--inline", "run", "hi"}, "", []string{"--model", "m", "--inline", "run", "hi"}},
		// Both sets must read the same values, not merely the same names.
		{[]string{"--resume", "--permission-mode", "--permission-mode=bypassPermissions", "run", "hi"}, "", []string{"--resume", "--permission-mode", "--permission-mode=bypassPermissions", "run", "hi"}},
		{[]string{"--resume", "--resume=abc", "run", "hi"}, "", []string{"--resume", "--resume=abc", "run", "hi"}},
		// Print mode: a -p after the verb is run's own, and -y works like --yolo.
		{[]string{"--yolo", "run", "-p", "hi"}, "run", []string{"run", "--yolo", "-p", "hi"}},
		{[]string{"-y", "run", "-p", "hi"}, "run", []string{"run", "-y", "-p", "hi"}},
		{[]string{"-p", "--yolo", "run", "hi"}, "run", []string{"run", "-p", "--yolo", "hi"}},
		{[]string{"--yolo", "-p", "hi"}, "run", []string{"run", "--print", "--yolo", "hi"}},
		{[]string{"-y", "-p", "hi"}, "run", []string{"run", "--print", "-y", "hi"}},
		{[]string{"--model", "m", "--inline", "run", "-p", "hi"}, "", []string{"--model", "m", "--inline", "run", "-p", "hi"}},
		// A run-only leading flag keeps the line the terminal UI's, which reports
		// it; the verb still bounds where a leading -p may sit.
		{[]string{"--model", "m", "--metrics", "x", "run", "-p", "hi"}, "", []string{"--model", "m", "--metrics", "x", "run", "-p", "hi"}},
		{[]string{"--model", "m", "--metrics", "x", "run", "hi"}, "", []string{"--model", "m", "--metrics", "x", "run", "hi"}},
		{[]string{"--model", "m", "--output-format", "json", "run", "-p", "hi"}, "", []string{"--model", "m", "--output-format", "json", "run", "-p", "hi"}},
		{[]string{"--model", "m", "--metrics", "x", "-p", "hi"}, "run", []string{"run", "--print", "--model", "m", "--metrics", "x", "hi"}},
	}
	for _, tc := range cases {
		cmd, args := normalizeCommand(slices.Clone(tc.args))
		if cmd != tc.wantCmd || !slices.Equal(args, tc.wantArgs) {
			t.Errorf("normalizeCommand(%q) = (%q, %q), want (%q, %q)", tc.args, cmd, args, tc.wantCmd, tc.wantArgs)
		}
	}
}

func TestRunTakesTheYoloSpellings(t *testing.T) {
	for _, flag := range []string{"--yolo", "--dangerously-skip-permissions"} {
		fs := pflag.NewFlagSet("run", pflag.ContinueOnError)
		auto, yolo := registerRunApprovalFlags(fs)
		if err := fs.Parse([]string{flag, "hi"}); err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		got, err := resolveRunPermissionMode("ask", *auto, *yolo, false)
		if err != nil {
			t.Fatalf("%s: %v", flag, err)
		}
		if mode, err := parsePermissionMode(got); err != nil || mode.approval != "yolo" {
			t.Fatalf("%s resolved to %q (%+v, %v), want yolo", flag, got, mode, err)
		}
	}
	for _, tc := range []struct {
		auto, yolo, explicit bool
		want                 string
	}{
		{false, true, true, "--yolo cannot be combined with --permission-mode"},
		{true, false, true, "--auto/-y cannot be combined with --permission-mode"},
		{true, true, false, "--auto/-y cannot be combined with --yolo"},
	} {
		if _, err := resolveRunPermissionMode("ask", tc.auto, tc.yolo, tc.explicit); err == nil || err.Error() != tc.want {
			t.Errorf("auto=%v yolo=%v explicit=%v: err %v, want %q", tc.auto, tc.yolo, tc.explicit, err, tc.want)
		}
	}
}

// The posture a spelling selects is the one the headless run enforces: the
// model asks to write a file, and the file exists only where the resolved
// mode lets an unattended write through.
func TestRunApprovalFlagsReachTheHeadlessGate(t *testing.T) {
	for _, tc := range []struct {
		argv  []string
		wrote bool
	}{
		{[]string{"run", "hi"}, false},
		{[]string{"run", "-y", "hi"}, true},
		{[]string{"run", "--yolo", "hi"}, true},
		{[]string{"run", "--dangerously-skip-permissions", "hi"}, true},
		{[]string{"--yolo", "run", "hi"}, true},
		{[]string{"-y", "run", "hi"}, true},
		{[]string{"--permission-mode", "auto", "run", "hi"}, true},
		{[]string{"--permission-mode", "dontAsk", "run", "hi"}, false},
	} {
		dir := runWriteFileFixture(t)
		var rc int
		captureStderr(t, func() {
			captureStdout(t, func() { rc = Run(tc.argv, "test-version") })
		})
		_, err := os.Stat(filepath.Join(dir, "marker.go"))
		if wrote := err == nil; wrote != tc.wrote {
			t.Errorf("Run(%q) rc=%d wrote marker=%v, want %v", tc.argv, rc, wrote, tc.wrote)
		}
	}
}

// runWriteFileFixture points an isolated home at a fake provider whose first
// reply asks for write_file marker.go, and returns the workspace it runs in.
func runWriteFileFixture(t *testing.T) string {
	t.Helper()
	var mu sync.Mutex
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		turn++
		n := turn
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"marker.go\",\"content\":\"x\"}"}}]}}]}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("RUN_FLAGS_FAKE_KEY", "k")
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf(`default_model = "fake"

[[providers]]
name = "fake"
kind = "openai"
base_url = %q
model = "fake-model"
api_key_env = "RUN_FLAGS_FAKE_KEY"
`, srv.URL), 0o644)
	dir := testenv.TempDir(t)
	t.Chdir(dir)
	return dir
}
