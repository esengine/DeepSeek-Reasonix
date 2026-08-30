package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/remote"
)

func credProxyOpts(baseURL string) *CredentialProxyOptions {
	return &CredentialProxyOptions{
		BaseURL:  baseURL,
		Token:    "virtual-token-123",
		Provider: "reasonix-desktop-proxy",
		Model:    "deepseek-v4-flash",
	}
}

// TestEnsureCredentialProviderAppendsAndIsIdempotent covers install and healing.
func TestEnsureCredentialProviderAppendsAndIsIdempotent(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	conn := newFakeConn(t, root, func(string) (remote.ExecResult, error) { return ok("") })
	fs, err := conn.SFTP()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := ensureCredentialProvider(ctx, fs, root, credProxyOpts("http://127.0.0.1:18999")); err != nil {
		t.Fatalf("first install: %v", err)
	}
	first, rerr := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	for _, want := range []string{
		`[[providers]]`,
		`name = "reasonix-desktop-proxy"`,
		`base_url = "http://127.0.0.1:18999"`,
		`api_key_env = "REASONIX_PROXY_TOKEN"`,
		`model = "deepseek-v4-flash"`,
	} {
		if !strings.Contains(string(first), want) {
			t.Fatalf("config missing %q:\n%s", want, first)
		}
	}

	// Idempotent: same options ⇒ no rewrite.
	if _, err := ensureCredentialProvider(ctx, fs, root, credProxyOpts("http://127.0.0.1:18999")); err != nil {
		t.Fatalf("second install: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if string(first) != string(second) {
		t.Fatalf("idempotent run rewrote the config:\n%s\n---\n%s", first, second)
	}

	// A base_url change (tunnel port moved) rewrites only that assignment.
	if _, err := ensureCredentialProvider(ctx, fs, root, credProxyOpts("http://127.0.0.1:19000")); err != nil {
		t.Fatalf("port change: %v", err)
	}
	third, _ := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if !strings.Contains(string(third), `base_url = "http://127.0.0.1:19000"`) {
		t.Fatalf("port change not applied:\n%s", third)
	}
	if strings.Count(string(third), "[[providers]]") != 1 {
		t.Fatalf("port change duplicated the block:\n%s", third)
	}
	other := credProxyOpts("http://127.0.0.1:19000")
	other.Provider, other.TokenEnv, other.Token = "reasonix-desktop-proxy-other", "REASONIX_PROXY_TOKEN_OTHER", "other-token"
	if _, err := ensureCredentialProvider(ctx, fs, root, other); err != nil {
		t.Fatalf("second workspace: %v", err)
	}
	cfg, _ := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	env, _ := os.ReadFile(filepath.Join(root, ".reasonix", ".env"))
	if !strings.Contains(string(cfg), `name = "reasonix-desktop-proxy-other"`) || !strings.Contains(string(env), "REASONIX_PROXY_TOKEN=virtual-token-123") || !strings.Contains(string(env), "REASONIX_PROXY_TOKEN_OTHER=other-token") {
		t.Fatalf("workspace credentials collided:\nconfig=%s\nenv=%s", cfg, env)
	}
	healed := credProxyOpts("http://127.0.0.1:20000")
	if _, err := ensureCredentialProvider(ctx, fs, root, healed); err != nil {
		t.Fatalf("multi-workspace port heal: %v", err)
	}
	cfg, _ = os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if got := strings.Count(string(cfg), `base_url = "http://127.0.0.1:20000"`); got != 2 {
		t.Fatalf("managed workspace providers did not heal together (got %d):\n%s", got, cfg)
	}
}

// TestEnsureCredentialProviderPreservesUserConfig keeps existing content.
func TestEnsureCredentialProviderPreservesUserConfig(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".reasonix"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "default_model = \"deepseek/deepseek-v4-flash\"\n\n[[providers]]\nname = \"mine\"\nkind = \"openai\"\nbase_url = \"https://api.deepseek.com\"\napi_key_env = \"MY_KEY\"\n"
	if err := os.WriteFile(filepath.Join(root, ".reasonix", "config.toml"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(string) (remote.ExecResult, error) { return ok("") })
	fs, err := conn.SFTP()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ensureCredentialProvider(context.Background(), fs, root, credProxyOpts("http://127.0.0.1:18999")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if !strings.HasPrefix(string(got), existing) {
		t.Fatalf("user config prefix disturbed:\n%s", got)
	}
	if strings.Count(string(got), "[[providers]]") != 2 {
		t.Fatalf("expected two provider blocks:\n%s", got)
	}
	if idx := providerBlockIndex(string(got), "mine"); idx < 0 {
		t.Fatalf("user provider block lost:\n%s", got)
	}
}

// TestLaunchCommandCredentialInjection keeps tokens out of the shell command.
func TestLaunchCommandCredentialInjection(t *testing.T) {
	paths := StatePaths{Dir: "/d", TokenFile: "/d/t", PortFile: "/d/p", PidFile: "/d/i", LogFile: "/d/l"}
	cmd := LaunchCommand("/usr/bin/reasonix", "/ws", paths, &CredentialProxyOptions{
		BaseURL: "http://127.0.0.1:18999", Token: "to'ken $x", Provider: "reasonix-desktop-proxy", Model: "m",
	})
	for _, want := range []string{
		`--model 'reasonix-desktop-proxy'`,
		`nohup '/usr/bin/reasonix' serve`,
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("LaunchCommand missing %q:\n%s", want, cmd)
		}
	}
	if strings.Contains(cmd, "to'ken") || strings.Contains(cmd, TokenEnvName) {
		t.Errorf("virtual token leaked into launch command:\n%s", cmd)
	}
}

// TestEnsureCredentialProviderMaterializesBuiltinDefault keeps default_model valid.
func TestEnsureCredentialProviderMaterializesBuiltinDefault(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".reasonix"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "config_version = 6\n\ndefault_model = \"deepseek-flash\"   # user's choice stays\n\n[ui]\ntheme = \"dark\"\n"
	if err := os.WriteFile(filepath.Join(root, ".reasonix", "config.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(string) (remote.ExecResult, error) { return ok("") })
	fs, err := conn.SFTP()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := ensureCredentialProvider(ctx, fs, root, credProxyOpts("http://127.0.0.1:18999")); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	got := string(first)
	for _, want := range []string{
		`default_model = "deepseek-flash"   # user's choice stays`,
		`name = "deepseek-flash"`,
		`api_key_env = "DEEPSEEK_API_KEY"`,
		`name = "reasonix-desktop-proxy"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("config missing %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "[[providers]]"); n != 2 {
		t.Fatalf("provider blocks = %d, want 2 (materialized + ours):\n%s", n, got)
	}
	// The materialized entry must come BEFORE ours so file order reads
	// user-default then desktop-proxy.
	if strings.Index(got, `name = "deepseek-flash"`) > strings.Index(got, `name = "reasonix-desktop-proxy"`) {
		t.Fatalf("materialized entry should precede ours:\n%s", got)
	}

	// Idempotent.
	if _, err := ensureCredentialProvider(ctx, fs, root, credProxyOpts("http://127.0.0.1:18999")); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if string(first) != string(second) {
		t.Fatalf("second run rewrote the config:\n%s\n---\n%s", first, second)
	}
}

// TestMaterializeDefaultProviderSkipsNonBuiltin leaves user-owned gaps alone.
func TestMaterializeDefaultProviderSkipsNonBuiltin(t *testing.T) {
	before := "default_model = \"custom/pro-model\"\n"
	after := materializeDefaultProvider(before)
	if before != after {
		t.Fatalf("non-builtin default was rewritten:\n%s", after)
	}
	if got := defaultModelProvider("default_model = \"deepseek/deepseek-v4-flash\"\n"); got != "deepseek" {
		t.Fatalf("provider extraction = %q, want deepseek", got)
	}
	if got := defaultModelProvider("[ui]\ndefault_model = \"deepseek-flash\"\n"); got != "" {
		t.Fatalf("table-scoped default_model leaked: %q", got)
	}
}

// TestEnsureCredentialProviderRewritesKindDrift heals provider kind changes.
func TestEnsureCredentialProviderRewritesKindDrift(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	conn := newFakeConn(t, root, func(string) (remote.ExecResult, error) { return ok("") })
	fs, err := conn.SFTP()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, err := ensureCredentialProvider(ctx, fs, root, credProxyOpts("http://127.0.0.1:18999")); err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if !strings.Contains(string(installed), "kind = \"openai\"") {
		t.Fatalf("default install missing the openai kind:\n%s", installed)
	}

	switched := credProxyOpts("http://127.0.0.1:18999")
	switched.Kind = "anthropic"
	if _, err := ensureCredentialProvider(ctx, fs, root, switched); err != nil {
		t.Fatal(err)
	}
	rewritten, _ := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if !strings.Contains(string(rewritten), "kind = \"anthropic\"") || strings.Contains(string(rewritten), "kind = \"openai\"") {
		t.Fatalf("kind drift not rewritten:\n%s", rewritten)
	}
	if !strings.Contains(string(rewritten), "base_url = \"http://127.0.0.1:18999\"") {
		t.Fatalf("base_url lost in the kind rewrite:\n%s", rewritten)
	}

	// Idempotent once the kind matches.
	if _, err := ensureCredentialProvider(ctx, fs, root, switched); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if string(again) != string(rewritten) {
		t.Fatalf("matching re-run rewrote the config:\n%s\n---\n%s", rewritten, again)
	}
}

func TestEnsureCredentialProviderPersistsLateBuiltinMaterialization(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	configDir := filepath.Join(root, ".reasonix")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate an older desktop: its proxy block is current, but it never
	// materialized the builtin selected by default_model.
	legacy := "default_model = \"deepseek-flash\"\n" + credentialProviderBlock(credProxyOpts("http://127.0.0.1:18999"))
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(string) (remote.ExecResult, error) { return ok("") })
	fs, err := conn.SFTP()
	if err != nil {
		t.Fatal(err)
	}
	changed, err := ensureCredentialProvider(context.Background(), fs, root, credProxyOpts("http://127.0.0.1:18999"))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("late builtin materialization was not reported as a config change")
	}
	got, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `name = "deepseek-flash"`) {
		t.Fatalf("materialized builtin was not persisted:\n%s", got)
	}
}

func TestEnsureCredentialProviderRewritesModelDrift(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	conn := newFakeConn(t, root, func(string) (remote.ExecResult, error) { return ok("") })
	fs, err := conn.SFTP()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := ensureCredentialProvider(ctx, fs, root, credProxyOpts("http://127.0.0.1:18999")); err != nil {
		t.Fatal(err)
	}
	switched := credProxyOpts("http://127.0.0.1:18999")
	switched.Model = "deepseek-v4-pro"
	if _, err := ensureCredentialProvider(ctx, fs, root, switched); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, ".reasonix", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `model = "deepseek-v4-pro"`) || strings.Contains(string(got), `model = "deepseek-v4-flash"`) {
		t.Fatalf("model drift not rewritten:\n%s", got)
	}
}
