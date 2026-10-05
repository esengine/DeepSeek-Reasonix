package serve

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

// fakeBackupService stands in for the accounts service's /me/backups routes and
// records every envelope it is handed, so a test can check what left the kernel.
type fakeBackupService struct {
	mu        sync.Mutex
	envelopes map[string][]byte
	uploads   [][]byte
}

func (f *fakeBackupService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-token" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"Sign in to continue."}}`)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/me/backups":
		raw, _ := io.ReadAll(r.Body)
		f.uploads = append(f.uploads, raw)
		var body struct {
			Envelope string `json:"envelope"`
		}
		_ = json.Unmarshal(raw, &body)
		env, _ := base64.StdEncoding.DecodeString(body.Envelope)
		f.envelopes["b1"] = env
		_, _ = io.WriteString(w, `{"backup":{"id":"b1","label":"x"}}`)
	case r.Method == http.MethodGet && r.URL.Path == "/me/backups":
		_, _ = io.WriteString(w, `{"backups":[{"id":"b1"}],"limits":{"maxCount":10,"maxBytes":4194304}}`)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/me/backups/"):
		env, ok := f.envelopes[strings.TrimPrefix(r.URL.Path, "/me/backups/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"backup_not_found","message":"gone"}}`)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"backup": map[string]any{"id": "b1"}, "envelope": env})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func backupServer(t *testing.T, granted bool) (*httptest.Server, *fakeBackupService) {
	t.Helper()
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", filepath.Join(home, "home"))
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(home, "state"))
	fake := &fakeBackupService{envelopes: map[string][]byte{}}
	upstream := httptest.NewServer(fake)
	t.Cleanup(upstream.Close)
	t.Setenv("REASONIX_ACCOUNTS_URL", upstream.URL)
	t.Setenv("REASONIX_ACCOUNT_TOKEN", "test-token")
	return accountServer(t, granted), fake
}

func postBackup(t *testing.T, srv *httptest.Server, path string, body any) (*http.Response, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := srv.Client().Post(srv.URL+path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestBackupRoutesNeedTheAccountGrantAndASession(t *testing.T) {
	srv, _ := backupServer(t, false)
	resp, _ := postBackup(t, srv, "/backups", map[string]any{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("without the host grant = %d", resp.StatusCode)
	}
	srv, _ = backupServer(t, true)
	t.Setenv("REASONIX_ACCOUNT_TOKEN", "")
	resp, out := postBackup(t, srv, "/backups", map[string]any{})
	if resp.StatusCode != http.StatusUnauthorized || out["code"] != "backup.signed_out" {
		t.Fatalf("signed out = %d %v", resp.StatusCode, out)
	}
}

func TestBackupUploadIsSealedAndRestoreAsksForConsent(t *testing.T) {
	srv, fake := backupServer(t, true)
	cfg := "[statusline]\ncommand = \"echo from-backup\"\n\n[[providers]]\nname = \"p\"\nkind = \"openai\"\nbase_url = \"https://api.example.com/v1\"\nheaders = { X-Secret = \"literal-header-value\" }\n"
	if err := os.MkdirAll(filepath.Dir(config.UserConfigPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.UserConfigPath(), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	resp, out := postBackup(t, srv, "/backups", map[string]any{
		"label": "laptop", "categories": []string{"settings", "automation"}, "passphrase": "short",
	})
	if resp.StatusCode != http.StatusBadRequest || out["code"] != "backup.weak_passphrase" {
		t.Fatalf("weak passphrase = %d %v", resp.StatusCode, out)
	}
	resp, out = postBackup(t, srv, "/backups", map[string]any{
		"label": "laptop", "categories": []string{"settings", "automation"}, "passphrase": "a long enough passphrase",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create = %d %v", resp.StatusCode, out)
	}
	for _, leak := range []string{"api.example.com", "echo from-backup", "literal-header-value"} {
		if bytes.Contains(fake.uploads[0], []byte(leak)) {
			t.Fatalf("upload carries %q in the clear", leak)
		}
	}

	if err := os.WriteFile(config.UserConfigPath(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	resp, out = postBackup(t, srv, "/backups/b1/preview", map[string]any{"passphrase": "not the passphrase"})
	if resp.StatusCode != http.StatusUnprocessableEntity || out["code"] != "backup.cannot_decrypt" {
		t.Fatalf("wrong passphrase = %d %v", resp.StatusCode, out)
	}
	resp, out = postBackup(t, srv, "/backups/b1/preview", map[string]any{"passphrase": "a long enough passphrase"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview = %d %v", resp.StatusCode, out)
	}
	planID, _ := out["planId"].(string)
	resp, out = postBackup(t, srv, "/backups/apply", map[string]any{"planId": planID, "items": []string{"statusline:statusline"}})
	if resp.StatusCode != http.StatusConflict || out["code"] != "backup.consent_required" {
		t.Fatalf("apply without consent = %d %v", resp.StatusCode, out)
	}
	if got, _ := os.ReadFile(config.UserConfigPath()); bytes.Contains(got, []byte("from-backup")) {
		t.Fatal("a refused apply wrote the status line")
	}
	resp, out = postBackup(t, srv, "/backups/apply", map[string]any{
		"planId": planID, "items": []string{"statusline:statusline"}, "consented": []string{"statusline:statusline"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("apply = %d %v", resp.StatusCode, out)
	}
	if got, _ := os.ReadFile(config.UserConfigPath()); !bytes.Contains(got, []byte("echo from-backup")) {
		t.Fatalf("status line not restored: %s", got)
	}
	resp, out = postBackup(t, srv, "/backups/b9/preview", map[string]any{"passphrase": "a long enough passphrase"})
	if resp.StatusCode != http.StatusNotFound || out["code"] != "backup.not_found" {
		t.Fatalf("missing backup = %d %v", resp.StatusCode, out)
	}
}
