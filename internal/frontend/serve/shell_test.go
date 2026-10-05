package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

func readShell(t *testing.T, base string) control.ShellSettings {
	t.Helper()
	resp, err := http.Get(base + "/shell")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /shell = %d", resp.StatusCode)
	}
	var out control.ShellSettings
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The pane's first line is a fact about this machine, so the read side answers
// without any grant: naming the interpreter that is already running gives a
// client nothing it could not learn from the next command's own card.
func TestShellSettingsReportsTheRunningInterpreter(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	got := readShell(t, srv.URL)
	if got.Prefer != "auto" {
		t.Fatalf("prefer = %q, want auto", got.Prefer)
	}
	if got.Effective.Name == "" || got.Platform == "" {
		t.Fatalf("effective shell not identified: %+v", got)
	}
}

// Choosing the program the agent's commands are handed to is a write to the host
// machine, so it rides the same grant as the other config-editing routes.
func TestSaveShellRefusedWithoutGrant(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/shell", `{"prefer":"bash"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST /shell without the grant = %d, want 403", resp.StatusCode)
	}
}

func TestSaveShellPersistsTheChoice(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/shell", `{"prefer":"bash"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := readAllString(resp)
		t.Fatalf("POST /shell = %d: %s", resp.StatusCode, b)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Shell.Prefer != "bash" {
		t.Fatalf("persisted prefer = %q, want bash", cfg.Tools.Shell.Prefer)
	}
	if got := readShell(t, srv.URL); got.Prefer != "bash" {
		t.Fatalf("prefer read back = %q, want bash", got.Prefer)
	}
}

// A path that cannot run comes back as a message the pane can show, not as a
// stored setting that breaks the next command.
func TestSaveShellRejectsUnusablePath(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/shell", `{"prefer":"bash","path":"/definitely/not/a/shell"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /shell with a bad path = %d, want 400", resp.StatusCode)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Tools.Shell.Path != "" {
		t.Fatalf("rejected path was persisted: %q", cfg.Tools.Shell.Path)
	}
}
