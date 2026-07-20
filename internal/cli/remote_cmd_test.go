package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestRemoteCommandUsageExit(t *testing.T) {
	if got := remoteCommand(nil, "test"); got != 2 {
		t.Errorf("no-arg remote exit = %d, want 2", got)
	}
	if got := remoteCommand([]string{"bogus"}, "test"); got != 2 {
		t.Errorf("unknown subcommand exit = %d, want 2", got)
	}
	if got := remoteCommand([]string{"help"}, "test"); got != 0 {
		t.Errorf("help exit = %d, want 0", got)
	}
}

func TestRemoteAddListRemoveRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)

	if got := remoteAddCLI([]string{"box", "dev@10.0.0.9:2222", "--workspace", "~/app"}); got != 0 {
		t.Fatalf("add exit = %d", got)
	}
	// Verify it landed in the user config, global-only.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	h, ok := cfg.RemoteHost("box")
	if !ok {
		t.Fatal("host not persisted")
	}
	if h.User != "dev" || h.Host != "10.0.0.9" || h.Port != 2222 || h.Workspace != "~/app" {
		t.Fatalf("host fields wrong: %+v", h)
	}
	// Confirm it went to config.toml.
	raw, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	if !strings.Contains(string(raw), "[[remote.hosts]]") || !strings.Contains(string(raw), `name = "box"`) {
		t.Fatalf("config.toml missing remote host:\n%s", raw)
	}

	if got := remoteRemoveCLI([]string{"box"}); got != 0 {
		t.Fatalf("remove exit = %d", got)
	}
	if got := remoteRemoveCLI([]string{"box"}); got != 1 {
		t.Errorf("second remove exit = %d, want 1", got)
	}
}

func TestRemoteRemoveCleansGeneratedCredentialsButKeepsUserManagedOnes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	passwordKey := config.RemotePasswordCredentialEnvName("secure-box")
	passphraseKey := config.RemotePassphraseCredentialEnvName("secure-box")
	const sharedKey = "TEAM_SHARED_SSH_PASSWORD"
	for key, value := range map[string]string{
		passwordKey: "generated-password", passphraseKey: "generated-passphrase", sharedKey: "shared-password",
	} {
		if _, err := config.SetCredential(key, value); err != nil {
			t.Fatal(err)
		}
		key := key
		t.Cleanup(func() { _ = config.RemoveCredential(key) })
	}
	if err := editUserConfig(func(c *config.Config) error {
		if err := c.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "secure-box", Host: "192.0.2.20", PasswordEnv: passwordKey, PassphraseEnv: passphraseKey,
		}); err != nil {
			return err
		}
		return c.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "shared-box", Host: "192.0.2.21", PasswordEnv: sharedKey,
		})
	}); err != nil {
		t.Fatal(err)
	}

	if got := remoteRemoveCLI([]string{"secure-box"}); got != 0 {
		t.Fatalf("remove generated host exit = %d", got)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(home, passwordKey); got.Set {
		t.Fatal("generated password remained after CLI host removal")
	}
	if got := config.ResolveCredentialForRootGlobalFirst(home, passphraseKey); got.Set {
		t.Fatal("generated passphrase remained after CLI host removal")
	}
	if got := remoteRemoveCLI([]string{"shared-box"}); got != 0 {
		t.Fatalf("remove shared host exit = %d", got)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(home, sharedKey); !got.Set || got.Value != "shared-password" {
		t.Fatalf("user-managed credential was removed: %+v", got)
	}
}

func TestRemoteAddReplacementCleansDroppedGeneratedCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	passwordKey := config.RemotePasswordCredentialEnvName("box")
	if _, err := config.SetCredential(passwordKey, "generated-password"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = config.RemoveCredential(passwordKey) })
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{Name: "box", Host: "192.0.2.30", PasswordEnv: passwordKey})
	}); err != nil {
		t.Fatal(err)
	}
	if got := remoteAddCLI([]string{"box", "dev@192.0.2.31"}); got != 0 {
		t.Fatalf("replace exit = %d", got)
	}
	if got := config.ResolveCredentialForRootGlobalFirst(home, passwordKey); got.Set {
		t.Fatal("generated credential remained after CLI replacement dropped its reference")
	}
}

func TestRemoteImportPreservesReasonixSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host box\n  HostName 192.0.2.44\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := editUserConfig(func(c *config.Config) error {
		return c.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "box", Host: "old.example", Workspace: "/srv/app", ServeInstall: "never",
			PasswordEnv: "REMOTE_BOX_PASSWORD",
			Forwards:    []config.RemoteForwardEntry{{Type: "local", Bind: "127.0.0.1:8080", Target: "127.0.0.1:80"}},
		})
	}); err != nil {
		t.Fatal(err)
	}
	if got := remoteImportCLI([]string{"box"}); got != 0 {
		t.Fatalf("import exit = %d", got)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	host, ok := cfg.RemoteHost("box")
	if !ok || host.Host != "box" || !host.UseSSHConfig || host.Workspace != "/srv/app" || host.ServeInstall != "never" {
		t.Fatalf("imported host = %+v, exists=%v", host, ok)
	}
	if host.PasswordEnv != "REMOTE_BOX_PASSWORD" || len(host.Forwards) != 1 {
		t.Fatalf("import wiped hidden settings: %+v", host)
	}
}

func TestRemoteForwardAddPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)
	if got := remoteAddCLI([]string{"box", "dev@10.0.0.9"}); got != 0 {
		t.Fatalf("add exit = %d", got)
	}
	if got := remoteForwardAdd([]string{"box", "-L", "8080:127.0.0.1:80"}); got != 0 {
		t.Fatalf("forward add exit = %d", got)
	}
	cfg, _ := config.Load()
	h, _ := cfg.RemoteHost("box")
	if len(h.Forwards) != 1 || h.Forwards[0].Type != "local" || h.Forwards[0].Bind != "127.0.0.1:8080" {
		t.Fatalf("forward not persisted: %+v", h.Forwards)
	}
}

func TestSplitHostPath(t *testing.T) {
	cases := []struct {
		in         string
		host, path string
		ok         bool
	}{
		{"box:/home/dev/file", "box", "/home/dev/file", true},
		{"box:file", "box", "file", true},
		{"nocolon", "", "", false},
		{":path", "", "", false},
		{"box:", "", "", false},
	}
	for _, c := range cases {
		h, p, ok := splitHostPath(c.in)
		if ok != c.ok || h != c.host || p != c.path {
			t.Errorf("splitHostPath(%q) = (%q,%q,%v), want (%q,%q,%v)", c.in, h, p, ok, c.host, c.path, c.ok)
		}
	}
}
