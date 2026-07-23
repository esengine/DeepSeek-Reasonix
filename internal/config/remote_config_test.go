package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProjectConfigCannotOverrideRemote pins [remote] as a user-global
// security control: a cloned repository's reasonix.toml must not be able to
// inject SSH hosts, jump chains, or port forwards.
func TestProjectConfigCannotOverrideRemote(t *testing.T) {
	isolateUserConfigHome(t)
	t.Setenv("REASONIX_HOME", "")
	globalDir := filepath.Dir(UserConfigPath())
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	globalTOML := "[remote]\n[[remote.hosts]]\nname = \"trusted\"\nhost = \"trusted.example\"\n"
	if err := os.WriteFile(filepath.Join(globalDir, "config.toml"), []byte(globalTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	project := t.TempDir()
	projectTOML := "[remote]\n[[remote.hosts]]\nname = \"evil\"\nhost = \"attacker.example\"\nproxy_jump = \"attacker-jump\"\n"
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte(projectTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForRoot(project)
	if err != nil {
		t.Fatalf("LoadForRoot() error = %v", err)
	}
	if len(cfg.Remote.Hosts) != 1 || cfg.Remote.Hosts[0].Name != "trusted" {
		t.Fatalf("remote hosts = %+v, want only the user-global \"trusted\" host", cfg.Remote.Hosts)
	}
	if _, ok := cfg.RemoteHost("evil"); ok {
		t.Error("project reasonix.toml injected a remote host; [remote] must stay user-global")
	}
}

func TestRemoteConfigDecodeAndDefaults(t *testing.T) {
	isolateUserConfigHome(t)
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	toml := `
[remote]
import_ssh_config = true

[[remote.hosts]]
name = "gpu-box"
host = "203.0.113.7"
port = 2222
user = "dev"
identity_file = "~/.ssh/id_ed25519"
passphrase_env = "REASONIX_REMOTE_GPUBOX_PASSPHRASE"
proxy_jump = "bastion.corp"
workspace = "~/projects/app"
serve_install = "npm"
use_ssh_config = true

[[remote.hosts.forwards]]
type = "local"
bind = "127.0.0.1:5432"
target = "127.0.0.1:5432"

[[remote.hosts]]
name = "minimal"
host = "10.0.0.1"
`
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Remote.ImportSSHConfig {
		t.Error("import_ssh_config not decoded")
	}
	h, ok := cfg.RemoteHost("gpu-box")
	if !ok {
		t.Fatal("gpu-box host missing")
	}
	if h.Port != 2222 || h.User != "dev" || h.ProxyJump != "bastion.corp" || !h.UseSSHConfig {
		t.Fatalf("gpu-box decoded wrong: %+v", h)
	}
	if h.ServeInstallMode() != "npm" {
		t.Fatalf("ServeInstallMode = %q", h.ServeInstallMode())
	}
	if len(h.Forwards) != 1 || h.Forwards[0].Type != "local" || h.Forwards[0].Bind != "127.0.0.1:5432" {
		t.Fatalf("forwards decoded wrong: %+v", h.Forwards)
	}
	m, ok := cfg.RemoteHost("minimal")
	if !ok {
		t.Fatal("minimal host missing")
	}
	if m.PortOrDefault() != 22 {
		t.Fatalf("PortOrDefault = %d, want 22", m.PortOrDefault())
	}
	if m.ServeInstallMode() != "auto" {
		t.Fatalf("default ServeInstallMode = %q, want auto", m.ServeInstallMode())
	}
}

// TestUpsertRemoteHostRoundTripsThroughSave pins that hosts written via the
// CRUD helpers survive a full user-scope re-render (SaveTo renders the whole
// file from the struct — a missing [remote] renderer would silently drop
// every saved host on the next unrelated settings save).
func TestUpsertRemoteHostRoundTripsThroughSave(t *testing.T) {
	isolateUserConfigHome(t)
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte("default_model = \"deepseek\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := LoadForEdit(path)
	if cfg == nil {
		t.Fatal("LoadForEdit returned nil")
	}
	host := RemoteHostEntry{
		Name:          "box",
		Host:          "198.51.100.4",
		Port:          22,
		User:          "dev",
		PassphraseEnv: "REASONIX_REMOTE_BOX_PASSPHRASE",
		Forwards:      []RemoteForwardEntry{{Type: "local", Bind: "127.0.0.1:8080", Target: "127.0.0.1:80"}},
	}
	if err := cfg.UpsertRemoteHost(host); err != nil {
		t.Fatalf("UpsertRemoteHost: %v", err)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[[remote.hosts]]", `name = "box"`, `host = "198.51.100.4"`, "[[remote.hosts.forwards]]", `bind = "127.0.0.1:8080"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("saved config missing %q:\n%s", want, raw)
		}
	}

	reloaded := LoadForEdit(path)
	got, ok := reloaded.RemoteHost("box")
	if !ok {
		t.Fatal("host lost after save/reload")
	}
	if got.PassphraseEnv != host.PassphraseEnv || len(got.Forwards) != 1 {
		t.Fatalf("host mutated across round-trip: %+v", got)
	}

	// Replace + remove.
	host.User = "ops"
	if err := reloaded.UpsertRemoteHost(host); err != nil {
		t.Fatal(err)
	}
	if h, _ := reloaded.RemoteHost("box"); h.User != "ops" || len(reloaded.Remote.Hosts) != 1 {
		t.Fatalf("upsert did not replace in place: %+v", reloaded.Remote.Hosts)
	}
	if !reloaded.RemoveRemoteHost("box") {
		t.Fatal("RemoveRemoteHost reported missing")
	}
	if reloaded.RemoveRemoteHost("box") {
		t.Fatal("second remove reported present")
	}
}

func TestUpsertRemoteHostValidates(t *testing.T) {
	cfg := Default()
	bad := []RemoteHostEntry{
		{Name: "", Host: "h"},
		{Name: "a b", Host: "h"},
		{Name: "user@host", Host: "h"},
		{Name: "ok", Host: ""},
		{Name: "ok", Host: "h", Port: 70000},
		{Name: "ok", Host: "h", ServeInstall: "curlpipe"},
		{Name: "ok", Host: "h", Forwards: []RemoteForwardEntry{{Type: "dynamic", Bind: "1", Target: "2"}}},
		{Name: "ok", Host: "h", Forwards: []RemoteForwardEntry{{Type: "local", Bind: "", Target: "2"}}},
		{Name: "ok", Host: "h", Forwards: []RemoteForwardEntry{{Type: "local", Bind: "abc", Target: "svc:80"}}},
		{Name: "ok", Host: "h", Forwards: []RemoteForwardEntry{{Type: "local", Bind: "8080", Target: "svc:0"}}},
		{Name: "ok", Host: "h", Forwards: []RemoteForwardEntry{{Type: "local", Bind: "8080", Target: "svc:80"}, {Type: "local", Bind: "127.0.0.1:8080", Target: "other:80"}}},
	}
	for i, e := range bad {
		if err := cfg.UpsertRemoteHost(e); err == nil {
			t.Errorf("case %d (%+v): invalid host accepted", i, e)
		}
	}
	if len(cfg.Remote.Hosts) != 0 {
		t.Fatalf("invalid hosts persisted: %+v", cfg.Remote.Hosts)
	}
}

// TestRemoteCredentialEnvNamesCollected pins that remote passphrase/password
// env names flow into CredentialEnvNames -> secrets.RegisterCredentialEnvKeys
// so they are filtered from tool subprocess environments.
func TestRemoteCredentialEnvNamesCollected(t *testing.T) {
	cfg := Default()
	cfg.Remote.Hosts = []RemoteHostEntry{
		{Name: "a", Host: "h1", PassphraseEnv: "REMOTE_A_PASSPHRASE"},
		{Name: "b", Host: "h2", PasswordEnv: "REMOTE_B_PASSWORD"},
		{Name: "c", Host: "h3", PassphraseEnv: "REMOTE_A_PASSPHRASE"}, // dup collapses
	}
	names := credentialEnvNamesFromConfig(cfg)
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	if !got["REMOTE_A_PASSPHRASE"] || !got["REMOTE_B_PASSWORD"] {
		t.Fatalf("remote credential envs missing from %v", names)
	}
}

func TestRemotePathHelpers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	if got := RemoteStateDir(); got != filepath.Join(home, "remote") {
		t.Fatalf("RemoteStateDir = %q", got)
	}
	if got := RemoteKnownHostsPath(); got != filepath.Join(home, "remote", "known_hosts") {
		t.Fatalf("RemoteKnownHostsPath = %q", got)
	}
}
