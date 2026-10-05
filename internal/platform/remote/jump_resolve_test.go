package remote

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

// This binds two things: that ResolveJumpHosts reads configured aliases and
// ssh_config, and that this machine's real ssh runs. The second failed once on
// Windows CI at 15.50s — the product's own subprocess timeout, so ssh was
// starved, not refused; isolated on that same runner it answers in 43ms. If it
// recurs, split the two rather than shorten a timeout CI only made look tight.
func TestResolveJumpHostsUsesConfiguredAliasesAndSSHConfig(t *testing.T) {
	home := testenv.TempDir(t)
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
