package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/contract/config"
)

// A setting saved from the command line changes that key and nothing else in
// a user config the 1.x line wrote.
func TestConfigCompactRatioKeepsA1xUserConfig(t *testing.T) {
	original, err := os.ReadFile(filepath.Join("..", "..", "contract", "config", "testdata", "user_config_1x_v1.39.5.toml"))
	if err != nil {
		t.Fatal(err)
	}
	isolateCLIConfigHome(t)
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	captureStdout(t, func() {
		if rc := Run([]string{"config", "compact-ratio", "75"}, "test-version"); rc != 0 {
			t.Fatalf("config compact-ratio rc = %d, want 0", rc)
		}
	})
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want, got map[string]any
	if _, err := toml.Decode(string(original), &want); err != nil {
		t.Fatal(err)
	}
	if _, err := toml.Decode(string(saved), &got); err != nil {
		t.Fatalf("saved config does not parse: %v\n%s", err, saved)
	}
	want["agent"].(map[string]any)["compact_ratio"] = 0.75
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config compact-ratio changed more than agent.compact_ratio:\n%s", saved)
	}
}
