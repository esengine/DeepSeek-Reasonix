package mcplaunch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIdentityFingerprintIgnoresHostPolicyAndCredentialValues(t *testing.T) {
	base := Identity{
		Server: "reader", Transport: "stdio", CommandPath: "/bin/tool",
		CommandSHA256: "abc", Args: []string{"--serve"}, EnvKeys: []string{"TOKEN"},
		ConfigSource: "project_config", ReadRoots: []string{"/workspace"},
		IsolationPolicy: "enforced",
	}
	changedPolicy := base
	changedPolicy.ReadRoots = []string{"/different"}
	changedPolicy.IsolationPolicy = "unavailable_unconfined"
	a, err := IdentityFingerprint(base)
	if err != nil {
		t.Fatal(err)
	}
	b, err := IdentityFingerprint(changedPolicy)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("host policy changed the exact server identity")
	}
	changedCommand := base
	changedCommand.Args = []string{"--serve", "--network"}
	c, err := IdentityFingerprint(changedCommand)
	if err != nil {
		t.Fatal(err)
	}
	if a == c {
		t.Fatal("command arguments did not change the server identity")
	}
}

func TestLaunchGrantIsExactDurableAndWorkspaceScoped(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFilename)
	a := NewManager(path, "/workspace/a")
	if err := a.Authorize("project", "project_config", "identity-a"); err != nil {
		t.Fatal(err)
	}
	if authorized, changed, err := a.LaunchAuthorized("project", "project_config", "identity-a"); err != nil || !authorized || changed {
		t.Fatalf("exact grant = (%v,%v,%v)", authorized, changed, err)
	}
	if authorized, changed, err := a.LaunchAuthorized("project", "project_config", "identity-b"); err != nil || authorized || !changed {
		t.Fatalf("changed identity = (%v,%v,%v)", authorized, changed, err)
	}
	b := NewManager(path, "/workspace/b")
	if authorized, changed, err := b.LaunchAuthorized("project", "project_config", "identity-a"); err != nil || authorized || changed {
		t.Fatalf("other workspace grant = (%v,%v,%v)", authorized, changed, err)
	}
	reloaded := NewManager(path, "/workspace/a")
	if authorized, changed, err := reloaded.LaunchAuthorized("project", "project_config", "identity-a"); err != nil || !authorized || changed {
		t.Fatalf("reloaded grant = (%v,%v,%v)", authorized, changed, err)
	}
}

func TestRevokeOnlyRemovesCurrentWorkspaceServerGrant(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFilename)
	a := NewManager(path, "/workspace/a")
	b := NewManager(path, "/workspace/b")
	for _, item := range []struct {
		manager *Manager
		server  string
	}{
		{a, "one"}, {a, "two"}, {b, "one"},
	} {
		if err := item.manager.Authorize(item.server, "project_config", "identity"); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.Revoke("one"); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := a.LaunchAuthorized("one", "project_config", "identity"); ok {
		t.Fatal("revoked current-workspace grant remains authorized")
	}
	if ok, _, _ := a.LaunchAuthorized("two", "project_config", "identity"); !ok {
		t.Fatal("unrelated server grant was revoked")
	}
	if ok, _, _ := b.LaunchAuthorized("one", "project_config", "identity"); !ok {
		t.Fatal("other workspace grant was revoked")
	}
}

func TestLauncherLockRoundTrip(t *testing.T) {
	manager := NewManager(filepath.Join(t.TempDir(), StateFilename), "/workspace")
	lock := LauncherLock{Server: "project", Locator: "pkg@latest", ResolvedVersion: "1.2.3", ContentSHA256: "abc"}
	if err := manager.PutLauncherLock(lock); err != nil {
		t.Fatal(err)
	}
	got, found, err := manager.GetLauncherLock("project", "pkg@latest")
	if err != nil || !found {
		t.Fatalf("launcher lock = (%+v,%v,%v)", got, found, err)
	}
	if got.ResolvedVersion != lock.ResolvedVersion || got.ContentSHA256 != lock.ContentSHA256 {
		t.Fatalf("launcher lock = %+v", got)
	}
	if LauncherLockFingerprint(got) == "" {
		t.Fatal("empty launcher lock fingerprint")
	}
}

func TestRetiredStateIsPreservedButIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StateFilename)
	workspaceFP := WorkspaceFingerprint("/workspace")
	body := `{
  "version": 1,
  "receipts": [{"scope":"workspace","workspace_fingerprint":"` + workspaceFP + `","server":"legacy","config_source":"project_config","identity_fingerprint":"other","tools":[{"raw_name":"read","trusted_reader":true,"future":"keep"}]}],
  "official_denials": [{"server":"official","future":"keep"}],
  "legacy_imports": {"future":"keep"}
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(path, "/workspace")
	if authorized, changed, err := manager.LaunchAuthorized("ordinary", "project_config", "identity"); err != nil || authorized || changed {
		t.Fatalf("retired receipt granted unrelated authority = (%v,%v,%v)", authorized, changed, err)
	}
	if err := manager.Authorize("new", "project_config", "identity"); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{`"tools"`, `"trusted_reader"`, `"future": "keep"`, `"official_denials"`, `"legacy_imports"`} {
		if !strings.Contains(string(written), marker) {
			t.Fatalf("retired state marker %s was lost:\n%s", marker, written)
		}
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(written, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded["launch_grants"]) == 0 {
		t.Fatal("new launch grant missing")
	}
}

func TestExactLegacyReceiptMigratesOnlyToLaunchGrant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StateFilename)
	workspaceFP := WorkspaceFingerprint("/workspace")
	body := `{"version":1,"receipts":[{"scope":"workspace","workspace_fingerprint":"` + workspaceFP + `","server":"project","config_source":"workspace_config","identity_fingerprint":"identity","tools":[{"raw_name":"read","trusted_reader":true}]}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(path, "/workspace")
	if authorized, changed, err := manager.LaunchAuthorized("project", "project_config", "identity"); err != nil || !authorized || changed {
		t.Fatalf("legacy launch migration = (%v,%v,%v)", authorized, changed, err)
	}
	state, err := manager.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.LaunchGrants) != 1 || len(state.LegacyReceipts) != 1 {
		t.Fatalf("migration state = %+v", state)
	}
}

func TestStateFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFilename)
	manager := NewManager(path, "/workspace")
	if err := manager.Authorize("project", "project_config", "identity"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("state permissions = %o, want owner-only", info.Mode().Perm())
	}
}
