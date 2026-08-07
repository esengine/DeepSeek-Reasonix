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

func TestRemovedRemoteWorkbenchCommandsFailWithMigrationHint(t *testing.T) {
	for _, command := range []string{"attach-workspace", "runtime-workbench", "workbench-build-id"} {
		t.Run(command, func(t *testing.T) {
			stdout, stderr := captureCLIOutput(t, func() {
				if got := remoteCommand([]string{command}, "v1.2.3"); got != 1 {
					t.Fatalf("exit = %d, want 1", got)
				}
			})
			if stdout != "" {
				t.Fatalf("migration error wrote stdout: %q", stdout)
			}
			if !strings.Contains(stderr, "Remote Workbench") ||
				!strings.Contains(stderr, "remote connect <host> --open") {
				t.Fatalf("migration error = %q, want removal and replacement hints", stderr)
			}
		})
	}
}

func TestRemoteAddListRemoveRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("HOME", home)

	if got := remoteAddCLI([]string{"box", "dev@10.0.0.9:2222", "--workspace", "~/app"}); got != 0 {
		t.Fatalf("add exit = %d", got)
	}
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

func TestParseRemoteConnectSyntaxFlagOrder(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		openAlias bool
		wantName  string
		wantOpen  bool
		wantWS    string
		wantPort  int
		wantErr   bool
	}{
		{name: "name then flags (documented / GUIDE order)", args: []string{"gpu-box", "--open", "--workspace", "/tmp/ws", "--local-port", "8080"}, wantName: "gpu-box", wantOpen: true, wantWS: "/tmp/ws", wantPort: 8080},
		{name: "flags then name", args: []string{"--open", "--workspace", "/tmp/ws", "gpu-box"}, wantName: "gpu-box", wantOpen: true, wantWS: "/tmp/ws"},
		{name: "single-dash open before name", args: []string{"-open", "gpu-box"}, wantName: "gpu-box", wantOpen: true},
		{name: "name only", args: []string{"gpu-box"}, wantName: "gpu-box"},
		{name: "open alias sets open without flag", args: []string{"gpu-box"}, openAlias: true, wantName: "gpu-box", wantOpen: true},
		{name: "missing name", args: []string{"--open"}, wantErr: true},
		{name: "extra positional after name-first flags", args: []string{"gpu-box", "extra"}, wantErr: true},
		{name: "two names after flags", args: []string{"--open", "a", "b"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRemoteConnectSyntax(tt.args, tt.openAlias)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.name != tt.wantName || got.open != tt.wantOpen || got.workspace != tt.wantWS || got.localPort != tt.wantPort {
				t.Fatalf("got %+v, want name=%q open=%v workspace=%q localPort=%d", got, tt.wantName, tt.wantOpen, tt.wantWS, tt.wantPort)
			}
		})
	}
}

func TestRemoteConnectSyntaxUsesSharedFlagContract(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantCode int
		want     string
		noUsage  bool
	}{
		{name: "help before name", args: []string{"connect", "--help"}, wantCode: 0, want: "Usage of remote connect:"},
		{name: "help after name", args: []string{"connect", "gpu-box", "--help"}, wantCode: 0, want: "Usage of remote connect:"},
		{name: "unknown flag before name", args: []string{"connect", "--unknown", "gpu-box"}, wantCode: 2, want: "flag provided but not defined: -unknown", noUsage: true},
		{name: "unknown flag after name", args: []string{"connect", "gpu-box", "--unknown"}, wantCode: 2, want: "flag provided but not defined: -unknown", noUsage: true},
		{name: "missing name", args: []string{"connect", "--open"}, wantCode: 2, want: remoteConnectUsage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr := captureCLIOutput(t, func() {
				if code := remoteConnectCLI(tt.args, "test-version"); code != tt.wantCode {
					t.Fatalf("remoteConnectCLI(%q) = %d, want %d", tt.args, code, tt.wantCode)
				}
			})
			output := stderr
			if tt.wantCode == 0 {
				output = stdout
				if stderr != "" {
					t.Fatalf("help wrote stderr: %q", stderr)
				}
			} else if stdout != "" {
				t.Fatalf("error wrote stdout: %q", stdout)
			}
			if !strings.Contains(output, tt.want) {
				t.Fatalf("output = %q, want %q", output, tt.want)
			}
			if tt.noUsage && strings.Contains(output, "Usage of") {
				t.Fatalf("parse error should be concise, got usage:\n%s", output)
			}
		})
	}
}
