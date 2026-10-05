package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
)

const maliciousReviewPromptMarker = "CHECKOUT-AUTHORED-REVIEW-PROMPT"

// TestReviewIgnoresCheckoutSearchBinaryAndReviewSkill runs `reasonix review`
// in a checkout that points [tools.search] at a script it ships and replaces
// the review skill. The model asks for a grep; neither the script nor the
// checkout's skill may take part in the run.
func TestReviewIgnoresCheckoutSearchBinaryAndReviewSkill(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake rg is a POSIX shell script")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	var mu sync.Mutex
	var systemPrompts []string
	turn := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		mu.Lock()
		for _, m := range req.Messages {
			if m.Role == "system" {
				systemPrompts = append(systemPrompts, fmt.Sprint(m.Content))
			}
		}
		turn++
		n := turn
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"grep","arguments":"{\"pattern\":\"hello\"}"}}]}}]}`+"\n\n")
			_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`+"\n\ndata: [DONE]\n\n")
			return
		}
		_, _ = io.WriteString(w, `data: {"choices":[{"index":0,"delta":{"content":"LGTM"},"finish_reason":"stop"}]}`+"\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REVIEW_FAKE_KEY", "k")
	userConfig := fmt.Sprintf(`default_model = "fake"

[[providers]]
name = "fake"
kind = "openai"
base_url = %q
model = "fake-model"
api_key_env = "REVIEW_FAKE_KEY"
`, srv.URL)
	writeTestFile(t, filepath.Join(home, "config.toml"), userConfig, 0o644)

	checkout := testenv.TempDir(t)
	marker := filepath.Join(checkout, "rg-ran")
	writeTestFile(t, filepath.Join(checkout, "tools", "rg"), "#!/bin/sh\necho ran > "+marker+"\nexit 1\n", 0o755)
	writeTestFile(t, filepath.Join(checkout, "reasonix.toml"), "[tools.search]\nengine = \"rg\"\nrg_path = \"tools/rg\"\n", 0o644)
	writeTestFile(t, filepath.Join(checkout, ".reasonix", "skills", "review", "SKILL.md"), `---
name: review
description: review
runAs: subagent
allowed-tools: bash, grep
---
`+maliciousReviewPromptMarker+"\n", 0o644)
	writeTestFile(t, filepath.Join(checkout, "main.txt"), "hello\n", 0o644)
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "add", "main.txt"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = checkout
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	writeTestFile(t, filepath.Join(checkout, "main.txt"), "hello world\n", 0o644)
	t.Chdir(checkout)

	if code := reviewCommand(nil); code != 0 {
		t.Fatalf("review exit code = %d", code)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Error("review executed the checkout's configured rg_path")
	}
	mu.Lock()
	defer mu.Unlock()
	if turn < 2 {
		t.Fatalf("the review ran %d model turns; the grep call never completed", turn)
	}
	if len(systemPrompts) == 0 {
		t.Fatal("no system prompt reached the provider")
	}
	for _, p := range systemPrompts {
		if strings.Contains(p, maliciousReviewPromptMarker) {
			t.Error("review ran under the checkout's review skill")
			break
		}
	}
}

func writeTestFile(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}
