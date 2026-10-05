package serve

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/BurntSushi/toml"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// The settings endpoint persists one choice into a user config the 1.x line
// wrote; everything else in that file, including keys this build does not
// read, stays as it was.
func TestPersistDesktopApprovalModeKeepsA1xUserConfig(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	original, err := os.ReadFile(filepath.Join("..", "..", "contract", "config", "testdata", "user_config_1x_v1.39.5.toml"))
	if err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	persistDesktopApprovalMode(control.ToolApprovalAuto)
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
	want["desktop"].(map[string]any)["default_tool_approval_mode"] = control.ToolApprovalAuto
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("approval write changed more than default_tool_approval_mode:\n%s", saved)
	}
}
