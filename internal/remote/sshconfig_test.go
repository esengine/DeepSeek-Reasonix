package remote

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"reasonix/internal/config"
)

const sampleSSHConfig = `
Host gpu
    HostName 203.0.113.9
    User dev
    Port 2222
    IdentityFile ~/.ssh/gpu_ed25519

Host bastion-*
    User jump

Host viajump
    HostName 10.1.1.1
    ProxyJump bastion-1

Match host somehost
    User shouldbeignored
`

func writeSampleConfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte(sampleSSHConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEffectiveSSHConfigUsesOpenSSHOutputAndKeepsAllIdentities(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	src.resolveOpenSSH = func(_ context.Context, path, alias string) ([]byte, error) {
		if path != src.Path() || alias != "gpu" {
			t.Fatalf("ssh -G request = path %q alias %q", path, alias)
		}
		return []byte("hostname resolved.example\nuser effective-user\nport 2207\nidentityfile ~/.ssh/first\nidentityfile ~/.ssh/second\nproxyjump jump-a,jump-b\nidentitiesonly yes\n"), nil
	}

	got := src.Effective("gpu")
	if got.HostName != "resolved.example" || got.User != "effective-user" || got.Port != 2207 || got.ProxyJump != "jump-a,jump-b" || !got.IdentitiesOnly {
		t.Fatalf("effective config = %+v", got)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	wantIdentities := []string{filepath.Join(home, ".ssh", "first"), filepath.Join(home, ".ssh", "second")}
	if len(got.IdentityFiles) != 2 || got.IdentityFiles[0] != wantIdentities[0] || got.IdentityFiles[1] != wantIdentities[1] {
		t.Fatalf("identity files = %v", got.IdentityFiles)
	}
}

func TestSSHConfigMatchExecUsesOpenSSHEvenWhenFallbackRejectsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	contents := "Host matched-box\n  HostName 192.0.2.10\nMatch exec \"true\"\n  User matched-user\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := LoadSSHConfig(path)
	if err != nil {
		t.Fatalf("valid OpenSSH Match exec config was rejected: %v", err)
	}
	src.resolveOpenSSH = func(_ context.Context, gotPath, alias string) ([]byte, error) {
		if gotPath != path || alias != "matched-box" {
			t.Fatalf("ssh -G request = path %q alias %q", gotPath, alias)
		}
		return []byte("hostname 192.0.2.10\nuser matched-user\nport 22\nidentitiesonly no\n"), nil
	}
	aliases := src.Aliases()
	if len(aliases) != 1 || aliases[0].Alias != "matched-box" {
		t.Fatalf("Match exec aliases = %+v", aliases)
	}
	got, err := src.EffectiveWithError("matched-box")
	if err != nil {
		t.Fatal(err)
	}
	if got.User != "matched-user" {
		t.Fatalf("Match exec effective config = %+v", got)
	}
}

func TestSSHConfigMatchExecRealOpenSSH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Match exec command is shell-dependent on Windows")
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("OpenSSH client is not installed")
	}
	path := filepath.Join(t.TempDir(), "config")
	contents := "Host real-match-box\n  HostName 192.0.2.11\nMatch exec \"true\"\n  User real-match-user\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := LoadSSHConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	got := src.Effective("real-match-box")
	if got.HostName != "192.0.2.11" || got.User != "real-match-user" {
		t.Fatalf("real ssh -G Match exec result = %+v", got)
	}
}

func TestSSHConfigLookups(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := src.HostName("gpu"); got != "203.0.113.9" {
		t.Errorf("HostName(gpu) = %q", got)
	}
	if got := src.User("gpu"); got != "dev" {
		t.Errorf("User(gpu) = %q", got)
	}
	if got := src.Port("gpu"); got != 2222 {
		t.Errorf("Port(gpu) = %d", got)
	}
	if got := src.ProxyJump("viajump"); got != "bastion-1" {
		t.Errorf("ProxyJump(viajump) = %q", got)
	}
}

func TestSSHConfigAliasesSkipWildcards(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	aliases := src.Aliases()
	names := map[string]bool{}
	for _, a := range aliases {
		names[a.Alias] = true
	}
	if !names["gpu"] || !names["viajump"] {
		t.Fatalf("expected concrete aliases gpu/viajump, got %v", names)
	}
	if names["bastion-*"] {
		t.Fatal("wildcard pattern surfaced as an importable alias")
	}
}

func TestSSHConfigAliasesIncludeImportedFiles(t *testing.T) {
	dir := t.TempDir()
	included := filepath.Join(dir, "hosts.conf")
	if err := os.WriteFile(included, []byte("Host included-box\n  HostName 192.0.2.10\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "config")
	if err := os.WriteFile(main, []byte("Include "+included+"\nHost direct-box\n  HostName 192.0.2.9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := LoadSSHConfig(main)
	if err != nil {
		t.Fatal(err)
	}
	aliases := src.Aliases()
	if len(aliases) != 2 || aliases[0].Alias != "included-box" || aliases[1].Alias != "direct-box" {
		t.Fatalf("included aliases = %+v", aliases)
	}
	got, err := src.EffectiveWithError("included-box")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostName != "192.0.2.10" {
		t.Fatalf("included host was not resolved on demand: %+v", got)
	}
}

func TestSSHAliasesDoNotResolveEveryHost(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	src.resolveOpenSSH = func(context.Context, string, string) ([]byte, error) {
		calls++
		return nil, nil
	}
	if got := src.Aliases(); len(got) != 2 {
		t.Fatalf("aliases = %+v", got)
	}
	if calls != 0 {
		t.Fatalf("alias discovery invoked ssh -G %d times", calls)
	}
}

func TestEffectiveSSHConfigPreservesIdentityFileNone(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	src.resolveOpenSSH = func(context.Context, string, string) ([]byte, error) {
		return []byte("hostname host.test\nidentityfile none\nidentityfile ~/.ssh/explicit\n"), nil
	}
	got, err := src.EffectiveWithError("gpu")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IdentityFileNone || len(got.IdentityFiles) != 1 || filepath.Base(got.IdentityFiles[0]) != "explicit" {
		t.Fatalf("identity settings = %+v", got)
	}
}

func TestEmbeddedSSHConfigPreservesIdentityFileNone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("Host none-box\n  IdentityFile none\n  IdentitiesOnly yes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := LoadSSHConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	src.resolveOpenSSH = nil
	got, err := src.EffectiveWithError("none-box")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IdentityFileNone || len(got.IdentityFiles) != 0 || !got.IdentitiesOnly {
		t.Fatalf("embedded identity settings = %+v", got)
	}
}

func TestEffectiveSSHConfigPropagatesInstalledOpenSSHErrors(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	want := context.DeadlineExceeded
	src.resolveOpenSSH = func(context.Context, string, string) ([]byte, error) { return nil, want }
	if _, err := src.EffectiveWithError("gpu"); !errors.Is(err, want) {
		t.Fatalf("EffectiveWithError error = %v, want %v", err, want)
	}
}

func TestResolveHostPropagatesInstalledOpenSSHErrors(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	want := context.DeadlineExceeded
	src.resolveOpenSSH = func(context.Context, string, string) ([]byte, error) { return nil, want }
	cfg := config.Default()
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{Name: "gpu", Host: "gpu", UseSSHConfig: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveHost(cfg, "gpu", src); !errors.Is(err, want) {
		t.Fatalf("ResolveHost error = %v, want %v", err, want)
	}
}

func TestEffectiveSSHConfigFallsBackOnlyWhenOpenSSHUnavailable(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	src.resolveOpenSSH = func(context.Context, string, string) ([]byte, error) { return nil, exec.ErrNotFound }
	got, err := src.EffectiveWithError("gpu")
	if err != nil {
		t.Fatal(err)
	}
	if got.HostName != "203.0.113.9" || got.User != "dev" {
		t.Fatalf("embedded fallback = %+v", got)
	}
}

func TestMissingOpenSSHExecutableIsDetectable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := runOpenSSHEffectiveConfig(context.Background(), "", "missing-ssh-box")
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing ssh error = %v, want exec.ErrNotFound", err)
	}
}

func TestLoadUserSSHConfigUsesNormalOpenSSHConfigStack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if runtime.GOOS == "windows" {
		t.Setenv("USERPROFILE", home)
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte("Host user-box\n  HostName 192.0.2.55\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	src, err := LoadUserSSHConfig()
	if err != nil {
		t.Fatal(err)
	}
	src.resolveOpenSSH = func(_ context.Context, path, alias string) ([]byte, error) {
		if path != "" || alias != "user-box" {
			t.Fatalf("normal ssh -G request = path %q alias %q", path, alias)
		}
		return []byte("hostname 192.0.2.55\n"), nil
	}
	if _, err := src.EffectiveWithError("user-box"); err != nil {
		t.Fatal(err)
	}
}

func TestSSHConfigMissingFileIsEmpty(t *testing.T) {
	src, err := LoadSSHConfig(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(src.Aliases()) != 0 {
		t.Fatal("missing file yielded aliases")
	}
	if src.HostName("anything") != "" {
		t.Fatal("missing file returned a hostname")
	}
}

// TestResolveHostLayersSSHConfig checks the precedence: an explicit TOML field
// wins, but unset fields fall through to ~/.ssh/config when use_ssh_config.
func TestResolveHostLayersSSHConfig(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{
		Name:         "gpu",
		Host:         "gpu", // alias; ssh_config supplies the real HostName
		User:         "override",
		UseSSHConfig: true,
	}); err != nil {
		t.Fatal(err)
	}
	h, err := ResolveHost(cfg, "gpu", src)
	if err != nil {
		t.Fatal(err)
	}
	if h.HostName != "203.0.113.9" {
		t.Errorf("HostName not taken from ssh_config: %q", h.HostName)
	}
	if h.User != "override" {
		t.Errorf("explicit TOML user should win: %q", h.User)
	}
	if h.Port != 2222 {
		t.Errorf("Port not taken from ssh_config: %d", h.Port)
	}
}

func TestResolveHostUsesPersistedHostAsTheSSHConfigLookupKey(t *testing.T) {
	src, err := LoadSSHConfig(writeSampleConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	// Make the test independent of the local OpenSSH executable.
	src.resolveOpenSSH = nil

	t.Run("legacy import remains a snapshot", func(t *testing.T) {
		cfg := config.Default()
		if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "gpu", Host: "203.0.113.9", User: "legacy-user", Port: 2201,
			IdentityFile: "/legacy/id", UseSSHConfig: true,
		}); err != nil {
			t.Fatal(err)
		}
		h, err := ResolveHost(cfg, "gpu", src)
		if err != nil {
			t.Fatal(err)
		}
		if h.HostName != "203.0.113.9" || h.User != "legacy-user" || h.Port != 2201 || h.IdentityFile != "/legacy/id" {
			t.Fatalf("legacy snapshot was redirected through its display label: %+v", h)
		}
	})

	t.Run("display label does not replace saved lookup key", func(t *testing.T) {
		cfg := config.Default()
		if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "my-gpu-label", Host: "gpu", UseSSHConfig: true,
		}); err != nil {
			t.Fatal(err)
		}
		h, err := ResolveHost(cfg, "my-gpu-label", src)
		if err != nil {
			t.Fatal(err)
		}
		if h.HostName != "203.0.113.9" || h.User != "dev" {
			t.Fatalf("saved Host alias was lost: %+v", h)
		}
	})

	t.Run("display label collision cannot redirect saved alias", func(t *testing.T) {
		cfg := config.Default()
		if err := cfg.UpsertRemoteHost(config.RemoteHostEntry{
			Name: "gpu", Host: "viajump", UseSSHConfig: true,
		}); err != nil {
			t.Fatal(err)
		}
		h, err := ResolveHost(cfg, "gpu", src)
		if err != nil {
			t.Fatal(err)
		}
		if h.HostName != "10.1.1.1" || h.ProxyJump[0] != "bastion-1" {
			t.Fatalf("display label collision redirected the saved Host alias: %+v", h)
		}
	})
}
