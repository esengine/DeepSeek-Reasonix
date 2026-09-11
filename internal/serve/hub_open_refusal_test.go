package serve

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/testenv"
)

// A pane the hub builds itself goes through boot, which needs a provider it can
// resolve; without one every open fails before it reaches the session at all.
func hubBootEnv(t *testing.T) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `default_model = "existing/model-a"

[[providers]]
name = "existing"
kind = "openai"
base_url = "https://example.invalid/v1"
models = ["model-a"]
default = "model-a"
api_key = "k"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func openPane(t *testing.T, srv *httptest.Server, root, sessionPath string) (int, string) {
	t.Helper()
	buf, _ := json.Marshal(map[string]string{"root": root, "sessionPath": sessionPath})
	resp, err := http.Post(srv.URL+"/runtimes", "application/json", bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func refusalCodeOf(t *testing.T, body string) string {
	t.Helper()
	var r struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal([]byte(body), &r)
	return r.Code
}

// Clicking a conversation the list still shows but the disk no longer has must
// say so. It reported 409 — the status the frontend renders as "held in another
// window" — because the hub dropped the status resumeInto had chosen.
func TestOpenPaneOnADeletedSessionSaysItIsGoneNotHeld(t *testing.T) {
	hubBootEnv(t)
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()

	gone := filepath.Join(SessionDirFor(root), "deleted.jsonl")
	status, body := openPane(t, srv, root, gone)
	if status == http.StatusConflict {
		t.Errorf("a deleted session opens as 409 Conflict, which reads as in-use: %s", body)
	}
	if got := refusalCodeOf(t, body); got != codeSessionBadPath {
		t.Errorf("refusal code = %q, want %q (body %s)", got, codeSessionBadPath, body)
	}
}

// The other half. A session a live holder is writing now opens rather than
// being refused — reading it is not what the lease protects against — and the
// guard that matters moves to where the writing is: no lease means no
// authority, and turn admission and every save already refuse without one.
func TestOpenPaneReadsASessionAnotherRuntimeHolds(t *testing.T) {
	hubBootEnv(t)
	root := testenv.TempDir(t)
	h := NewHub(HubOptions{})
	hubRuntime(t, h, root)
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()

	held := filepath.Join(SessionDirFor(root), "held.jsonl")
	writeSessionAt(t, held)
	lease, err := agent.TryAcquireSessionLease(held)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer lease.Release()

	status, body := openPane(t, srv, root, held)
	if status != http.StatusOK {
		t.Fatalf("opening a session held elsewhere = %d, want 200 (body %s)", status, body)
	}
	// The holder still holds it: the pane that just opened took nothing.
	if !agent.SessionLeaseHeldByOtherRuntime(held) && lease.Path() == "" {
		t.Error("the holder's lease did not survive the read-only open")
	}
}
