package remote

import (
	"os"
	"path/filepath"
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
	if aliases[0].HostName != "192.0.2.10" {
		t.Fatalf("included host was not resolved: %+v", aliases[0])
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
