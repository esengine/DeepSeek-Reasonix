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
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/safety/sandbox"
)

// untrustedFolderRun drives the real `run` path against a provider that asks
// for one file write and then answers with whatever the tool result let it say.
type untrustedFolderRun struct {
	dir string

	mu       sync.Mutex
	toolText string
}

func newUntrustedFolderRun(t *testing.T) *untrustedFolderRun {
	t.Helper()
	if !sandbox.Available() {
		t.Skip("no OS sandbox backend: the default posture asks here for a different reason")
	}
	r := &untrustedFolderRun{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		var sent struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &sent)
		for _, m := range sent.Messages {
			if m.Role == "tool" {
				r.mu.Lock()
				r.toolText = m.Content
				r.mu.Unlock()
				_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
				return
			}
		}
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"w1","type":"function","function":{"name":"write_file","arguments":"{\"path\":\"notes.md\",\"content\":\"x\\n\"}"}}]},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	isolateCLIConfigHome(t)
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("RUN_FLAGS_FAKE_KEY", "k")
	writeTestFile(t, filepath.Join(home, "config.toml"), fmt.Sprintf("default_model = \"fake\"\n\n[sandbox]\nbash = \"enforce\"\n\n[[providers]]\nname = \"fake\"\nkind = \"openai\"\nbase_url = %q\nmodel = \"fake-model\"\napi_key_env = \"RUN_FLAGS_FAKE_KEY\"\n", srv.URL), 0o644)
	r.dir = testenv.TempDir(t)
	t.Chdir(r.dir)
	return r
}

func (r *untrustedFolderRun) wrote() bool {
	_, err := os.Stat(filepath.Join(r.dir, "notes.md"))
	return err == nil
}

func (r *untrustedFolderRun) modelSaw() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.toolText
}

// In a folder nobody trusted, a headless run that could not write says so on
// stderr with the refusal's identity and the remedy, tells the model the same
// cause, and exits 4 (3 under --fail-on-unverified); trusting the folder lets the
// identical command write.
func TestRunInUntrustedFolderSaysWhyNothingWasDone(t *testing.T) {
	r := newUntrustedFolderRun(t)

	var code int
	_, stderr := captureCLIOutput(t, func() { code = Run([]string{"run", "write notes.md"}, "test") })
	if code != runExitUntrustedFolder || r.wrote() {
		t.Fatalf("exit %d, wrote %v: a plain run exits %d and the policy keeps the write out\n%s", code, r.wrote(), runExitUntrustedFolder, stderr)
	}
	for _, want := range []string{"refused 1 tool call(s): write_file", "permission.untrusted_folder", "reasonix trust --dir '"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, stderr)
		}
	}
	saw := r.modelSaw()
	for _, want := range []string{"no trust decision", "reasonix trust --dir '"} {
		if !strings.Contains(saw, want) {
			t.Fatalf("the model was told %q, want %q", saw, want)
		}
	}

	_, _ = captureCLIOutput(t, func() { code = Run([]string{"run", "--fail-on-unverified", "write notes.md"}, "test") })
	if code != runExitUnverified {
		t.Fatalf("--fail-on-unverified exited %d, want %d: every write was refused by project trust", code, runExitUnverified)
	}

	stdout, _ := captureCLIOutput(t, func() { code = Run([]string{"-p", "--output-format", "json", "write notes.md"}, "test") })
	var result struct {
		Denials      []runPermissionDenial `json:"permission_denials"`
		UnverifiedBy string                `json:"unverified_by"`
		Subtype      string                `json:"subtype"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil || len(result.Denials) != 1 {
		t.Fatalf("result %q: %v", stdout, err)
	}
	if result.UnverifiedBy != "permission.untrusted_folder" || result.Subtype != "success" {
		t.Fatalf("the result must say why it is unverified: %+v", result)
	}
	d := result.Denials[0]
	if d.ToolName != "write_file" || d.Code != "permission.untrusted_folder" || !strings.Contains(d.Remedy, "reasonix trust --dir '") {
		t.Fatalf("denial = %+v", d)
	}

	_, _ = captureCLIOutput(t, func() { code = Run([]string{"trust", "--yes", "--dir", r.dir}, "test") })
	_, stderr = captureCLIOutput(t, func() { code = Run([]string{"run", "write notes.md"}, "test") })
	if code != 0 || !r.wrote() || strings.Contains(stderr, "refused") {
		t.Fatalf("after trust: exit %d, wrote %v\n%s", code, r.wrote(), stderr)
	}
}
