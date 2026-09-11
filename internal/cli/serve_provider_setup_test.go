package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const serveMissingKeyHelperEnv = "REASONIX_TEST_SERVE_MISSING_KEY_HELPER"

// TestServeStartsWithMissingProviderKey exercises the real runServe lifecycle:
// the listener and port file must become available before a Provider key exists,
// and the authenticated browser surface must show the setup page.
func TestServeStartsWithMissingProviderKey(t *testing.T) {
	if os.Getenv(serveMissingKeyHelperEnv) == "1" {
		// The package TestMain clears path overrides before dispatching tests, so
		// restore this helper's isolated home after that process-wide guard runs.
		if err := os.Setenv("REASONIX_HOME", os.Getenv("REASONIX_TEST_SERVE_HOME")); err != nil {
			os.Exit(2)
		}
		code := runServe([]string{
			"--model", "remote-demo/model-a",
			"--addr", "127.0.0.1:0",
			"--port-file", os.Getenv("REASONIX_TEST_SERVE_PORT_FILE"),
			"--auth", "token",
			"--token", "serve-setup-test-token",
		})
		os.Exit(code)
	}

	home := t.TempDir()
	balanceStarted := make(chan struct{})
	releaseBalance := make(chan struct{})
	balanceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-balanceStarted:
		default:
			close(balanceStarted)
		}
		<-releaseBalance
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(balanceServer.Close)
	t.Cleanup(func() { close(releaseBalance) })
	configPath := filepath.Join(home, "config.toml")
	configBody := `default_model = "remote-demo/model-a"

[[providers]]
name = "remote-demo"
kind = "openai"
base_url = "https://example.invalid/v1"
balance_url = "` + balanceServer.URL + `"
models = ["model-a"]
default = "model-a"
api_key_env = "REASONIX_TEST_REMOTE_MISSING_KEY"
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}
	portFile := filepath.Join(home, "serve.addr")
	cmd := exec.Command(os.Args[0], "-test.run=^TestServeStartsWithMissingProviderKey$")
	cmd.Env = replaceServeTestEnv(os.Environ(),
		serveMissingKeyHelperEnv+"=1",
		"REASONIX_HOME="+home,
		"REASONIX_TEST_SERVE_HOME="+home,
		"REASONIX_CREDENTIALS_STORE=file",
		"REASONIX_TEST_REMOTE_MISSING_KEY=",
		"REASONIX_TEST_SERVE_PORT_FILE="+portFile,
	)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})

	var addr string
	var lastReadErr error
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(portFile)
		if err == nil && strings.TrimSpace(string(data)) != "" {
			addr = strings.TrimSpace(string(data))
			break
		}
		// Every read error is retryable inside the deadline: on Windows the
		// writing child briefly holds the file (sharing violation), which is a
		// timing condition, not a failure.
		lastReadErr = err
		time.Sleep(20 * time.Millisecond)
	}
	if addr == "" {
		t.Fatalf("Serve did not publish its port with a missing Provider key (last read err: %v):\n%s", lastReadErr, output.String())
	}
	select {
	case <-balanceStarted:
	case <-time.After(5 * time.Second):
		t.Fatalf("Serve did not start the configured balance diagnostic:\n%s", output.String())
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + "/?token=serve-setup-test-token")
	if err != nil {
		t.Fatalf("open missing-key Serve: %v\n%s", err, output.String())
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("missing-key Serve status = %d, want 200: %s\n%s", resp.StatusCode, body, output.String())
	}
	if !bytes.Contains(body, []byte("Reasonix Provider Setup")) {
		t.Fatalf("missing-key Serve did not show setup page:\n%s", body)
	}
}

func replaceServeTestEnv(base []string, overrides ...string) []string {
	keys := make(map[string]bool, len(overrides))
	for _, item := range overrides {
		key, _, _ := strings.Cut(item, "=")
		keys[key] = true
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, item := range base {
		key, _, _ := strings.Cut(item, "=")
		if !keys[key] {
			out = append(out, item)
		}
	}
	return append(out, overrides...)
}
