package remote

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
)

func TestResolveJumpHostsUsesConfiguredAliasesAndSSHConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	text := "Host bastion\n  HostName 10.0.0.8\n  User jump-user\n  Port 2202\n  IdentityFile ~/.ssh/jump_key\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	sshCfg, err := LoadSSHConfig(filepath.Join(sshDir, "config"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Remote.Hosts = []config.RemoteHostEntry{{
		Name: "second", Host: "10.0.0.9", User: "ops",
		PasswordEnv: "SECOND_PASSWORD", ProxyJump: "ignored-nested-hop",
	}}
	hops, err := ResolveJumpHosts(cfg, []string{"bastion", "second"}, sshCfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := hops[0]; got.HostName != "10.0.0.8" || got.User != "jump-user" || got.Port != 2202 || got.IdentityFile != filepath.Join(home, ".ssh", "jump_key") {
		t.Fatalf("ssh_config jump was not fully resolved: %+v", got)
	}
	if got := hops[1]; got.PasswordEnv != "SECOND_PASSWORD" || len(got.ProxyJump) != 0 {
		t.Fatalf("Reasonix jump credentials/nested-chain handling wrong: %+v", got)
	}
}
