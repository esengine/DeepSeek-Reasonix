package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

func readJSON[T any](t *testing.T, base, path string) T {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", path, resp.StatusCode)
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// Widening what the agent may do to this machine is a write to this machine, so
// both routes ride the provider-edit grant. Without it a networked client could
// hand itself the boundary it is supposed to be held by.
func TestBoundaryWritesRefusedWithoutGrant(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	for _, path := range []string{"/permissions", "/sandbox", "/browser-tools"} {
		resp := postProvider(t, srv.URL, path, `{}`)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("POST %s without the grant = %d, want 403", path, resp.StatusCode)
		}
	}
}

func TestSavePermissionsPersistsAllThreeLists(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	body := `{"mode":"deny","deny":["bash(git push:*)"],"ask":["bash(rm:*)"],"allow":["bash(go test:*)"]}`
	resp := postProvider(t, srv.URL, "/permissions", body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		got, _ := readAllString(resp)
		t.Fatalf("POST /permissions = %d: %s", resp.StatusCode, got)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Permissions.Mode != "deny" {
		t.Fatalf("persisted mode = %q, want deny", cfg.Permissions.Mode)
	}
	if len(cfg.Permissions.Deny) != 1 || cfg.Permissions.Deny[0] != "bash(git push:*)" {
		t.Fatalf("persisted deny = %v", cfg.Permissions.Deny)
	}
	if len(cfg.Permissions.Ask) != 1 || len(cfg.Permissions.Allow) != 1 {
		t.Fatalf("ask = %v, allow = %v", cfg.Permissions.Ask, cfg.Permissions.Allow)
	}

	got := readJSON[control.PermissionRules](t, srv.URL, "/permissions")
	if got.Mode != "deny" || len(got.Deny) != 1 {
		t.Fatalf("read back = %+v", got)
	}
	if got.Path == "" {
		t.Fatal("read back names no config file, so the pane cannot say where an edit lands")
	}
}

// A rule the gate's own parser rejects never reaches the config: it would sit in
// the list looking enforced and match nothing at all.
func TestSavePermissionsRejectsUnparsableRule(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/permissions", `{"mode":"ask","deny":["(no tool name)"]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /permissions with a bad rule = %d, want 400", resp.StatusCode)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Permissions.Deny) != 0 {
		t.Fatalf("rejected rule was persisted: %v", cfg.Permissions.Deny)
	}
}

// Replacing the lists must not leave yesterday's rules behind: the editor sends
// what the screen shows, and anything kept silently is a boundary nobody chose.
func TestSavePermissionsReplacesRatherThanAppends(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	first := postProvider(t, srv.URL, "/permissions", `{"mode":"ask","deny":["bash(rm:*)","bash(git push:*)"]}`)
	first.Body.Close()
	second := postProvider(t, srv.URL, "/permissions", `{"mode":"ask","deny":["bash(rm:*)"]}`)
	second.Body.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Permissions.Deny) != 1 || cfg.Permissions.Deny[0] != "bash(rm:*)" {
		t.Fatalf("deny after removal = %v, want just bash(rm:*)", cfg.Permissions.Deny)
	}
}

func TestSandboxSettingsReportWhatTheConfinerWillUse(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/sandbox", `{"bash":"off","network":false,"allowWrite":["/tmp/scratch"," "]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		got, _ := readAllString(resp)
		t.Fatalf("POST /sandbox = %d: %s", resp.StatusCode, got)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sandbox.Bash != "off" || cfg.Sandbox.Network {
		t.Fatalf("persisted jail = %+v", cfg.Sandbox)
	}
	if len(cfg.Sandbox.AllowWrite) != 1 || cfg.Sandbox.AllowWrite[0] != "/tmp/scratch" {
		t.Fatalf("allowWrite = %v, want the blank entry dropped", cfg.Sandbox.AllowWrite)
	}

	// An empty workspace root is not "anywhere": the expansion still names a
	// directory, and the pane shows that rather than a blank.
	got := readJSON[control.SandboxSettings](t, srv.URL, "/sandbox")
	if len(got.EffectiveWriteRoots) == 0 {
		t.Fatal("no effective write roots reported")
	}
	for _, root := range got.EffectiveWriteRoots {
		if root == "" {
			t.Fatalf("effective roots contain a blank: %v", got.EffectiveWriteRoots)
		}
	}
}

// The editor draws the posture from this, so it has to be the mode that will
// run rather than the word in the file: an unset value enforces everywhere but
// Windows, which forces off however the file reads. A pane fed the file's word
// would report the wrong half of a security boundary on both.
func TestSandboxSettingsReportTheModeThatWillRun(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	want := "enforce"
	if runtime.GOOS == "windows" {
		want = "off"
	}
	if got := readJSON[control.SandboxSettings](t, srv.URL, "/sandbox"); got.EffectiveBash != want {
		t.Fatalf("effectiveBash with nothing configured = %q, want %q", got.EffectiveBash, want)
	}

	resp := postProvider(t, srv.URL, "/sandbox", `{"bash":"off","network":true}`)
	resp.Body.Close()
	if got := readJSON[control.SandboxSettings](t, srv.URL, "/sandbox"); got.EffectiveBash != "off" {
		t.Fatalf("effectiveBash after saving off = %q", got.EffectiveBash)
	}
}

// A host with no OS backend refusing "enforce" is not a malformed request and
// not an unwritable file, and a client that cannot tell the three apart can
// only guess. The code is what carries that; the sentence is for logs.
func TestSaveSandboxRefusesEnforceWithoutABackendByCode(t *testing.T) {
	if sandbox.Available() {
		t.Skip("this host has an OS sandbox, so enforce is not refused here")
	}
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/sandbox", `{"bash":"enforce"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /sandbox enforce = %d, want 409", resp.StatusCode)
	}
	var out struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Code != "sandbox.unavailable" {
		t.Fatalf("refusal code = %q, want sandbox.unavailable", out.Code)
	}
	if cfg, err := config.Load(); err != nil {
		t.Fatal(err)
	} else if cfg.Sandbox.Bash == "enforce" {
		t.Fatal("a refused mode was written to the config anyway")
	}
}

func TestSaveSandboxRejectsUnknownBashMode(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp := postProvider(t, srv.URL, "/sandbox", `{"bash":"maybe"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /sandbox with an unknown mode = %d, want 400", resp.StatusCode)
	}
}

// The switch lands in the user file, not in the runtime alone, and reads back
// from there; a body that does not say on or off changes nothing.
func TestSaveBrowserToolsPersists(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	if got := readJSON[control.BrowserToolsSettings](t, srv.URL, "/browser-tools"); !got.Enabled {
		t.Fatalf("unset switch reads %+v, want on", got)
	}
	empty := postProvider(t, srv.URL, "/browser-tools", `{}`)
	empty.Body.Close()
	if empty.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST /browser-tools without enabled = %d, want 400", empty.StatusCode)
	}
	resp := postProvider(t, srv.URL, "/browser-tools", `{"enabled":false}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		got, _ := readAllString(resp)
		t.Fatalf("POST /browser-tools = %d: %s", resp.StatusCode, got)
	}
	if config.LoadForEdit(config.UserConfigPath()).Tools.BrowserToolsEnabled() {
		t.Fatal("turning it off did not reach the config file")
	}
	if got := readJSON[control.BrowserToolsSettings](t, srv.URL, "/browser-tools"); got.Enabled {
		t.Fatalf("read back = %+v, want off", got)
	}
}
