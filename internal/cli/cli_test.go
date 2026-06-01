package cli

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/config"
)

// TestConfigureKeys verifies that a shared api_key_env (each vendor's SKUs use
// the same env var) is asked only once, and entered keys become env lines.
func TestConfigureKeys(t *testing.T) {
	selected := config.ProviderPresets() // DeepSeek + MiMo variants

	// Two distinct keys to enter: DEEPSEEK_API_KEY, then MIMO_API_KEY.
	input := "ds-key\nmi-key\n"
	env := configureKeys(selected, strings.NewReader(input), io.Discard)

	if len(env) != 2 {
		t.Fatalf("env = %v (want 2: DeepSeek asked once + MiMo asked once)", env)
	}
	if env[0] != "DEEPSEEK_API_KEY=ds-key" {
		t.Errorf("env[0] = %q", env[0])
	}
	if env[1] != "MIMO_API_KEY=mi-key" {
		t.Errorf("env[1] = %q", env[1])
	}
}

// TestAppendEnvUpsertReplacesExistingKey covers the bug where re-running the
// wizard with a corrected key would append a second line for the same env
// var. loadDotEnv is first-wins, so without dedupe the stale key kept
// authenticating, and the user saw a 401 with no obvious cause.
func TestAppendEnvUpsertReplacesExistingKey(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "") // also covers the os.Setenv pin path
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("# initial\nDEEPSEEK_API_KEY=stale\nMIMO_API_KEY=keepme\n"), 0o600)

	if err := appendEnv(p, []string{"DEEPSEEK_API_KEY=fresh"}); err != nil {
		t.Fatalf("appendEnv: %v", err)
	}
	got, _ := os.ReadFile(p)
	want := "# initial\nMIMO_API_KEY=keepme\nDEEPSEEK_API_KEY=fresh\n"
	if string(got) != want {
		t.Errorf("after upsert =\n%s\nwant =\n%s", got, want)
	}
	if got := os.Getenv("DEEPSEEK_API_KEY"); got != "fresh" {
		t.Errorf("process env DEEPSEEK_API_KEY = %q, want %q (upsert should pin in-process)", got, "fresh")
	}
}

// TestAppendEnvUpsertHandlesExportPrefix proves `export FOO=...` style lines
// also get replaced, since users might hand-edit .env in shell-friendly form.
func TestAppendEnvUpsertHandlesExportPrefix(t *testing.T) {
	t.Setenv("FOO", "")
	p := filepath.Join(t.TempDir(), ".env")
	os.WriteFile(p, []byte("export FOO=old\nKEEP=yes\n"), 0o600)
	if err := appendEnv(p, []string{"FOO=new"}); err != nil {
		t.Fatalf("appendEnv: %v", err)
	}
	got, _ := os.ReadFile(p)
	if !strings.Contains(string(got), "FOO=new") || strings.Contains(string(got), "FOO=old") {
		t.Errorf("export-prefixed line not replaced:\n%s", got)
	}
}

// TestGroupByFamily verifies the wizard groups the provider presets into
// "deepseek" and "mimo" families, preserving the order each family first
// appears in.
func TestGroupByFamily(t *testing.T) {
	order, members, info := groupByFamily(config.ProviderPresets())

	if got := order; !reflect.DeepEqual(got, []string{"deepseek", "mimo"}) {
		t.Fatalf("family order = %v, want [deepseek mimo]", got)
	}
	if got := members["deepseek"]; !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("deepseek members = %v, want [0]", got)
	}
	if got := members["mimo"]; !reflect.DeepEqual(got, []int{1, 2}) {
		t.Errorf("mimo members = %v, want [1 2]", got)
	}
	if info["deepseek"].name != "DeepSeek" || info["mimo"].name != "MiMo (Xiaomi)" {
		t.Errorf("display names = %q / %q", info["deepseek"].name, info["mimo"].name)
	}
}
