package serve

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// The posture chosen on the composer has to survive a relaunch, and saving it
// must not cost the user the rest of their config: the edit path rewrites the
// whole file, so a dropped provider block would take its key reference with it.
func TestPersistDesktopApprovalModeKeepsRestOfConfig(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	path := filepath.Join(home, "config.toml")
	seed := `config_version = 6
default_model = "deepseek-flash/deepseek-v4-flash"

[[providers]]
name        = "deepseek-flash"
kind        = "anthropic"
base_url    = "https://api.deepseek.com/anthropic"
models      = ["deepseek-v4-flash"]
default     = "deepseek-v4-flash"
api_key_env = "DEEPSEEK_API_KEY"
`
	if err := os.WriteFile(path, []byte(seed), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	persistDesktopApprovalMode(control.ToolApprovalYolo)

	saved := config.LoadForEdit(path)
	if mode := saved.DesktopDefaultToolApprovalMode(); mode != control.ToolApprovalYolo {
		t.Fatalf("approval mode = %q, want %q", mode, control.ToolApprovalYolo)
	}
	entry, ok := saved.Provider("deepseek-flash")
	if !ok {
		written, _ := os.ReadFile(path)
		t.Fatalf("provider block lost by the approval write; file now:\n%s", written)
	}
	if entry.APIKeyEnv != "DEEPSEEK_API_KEY" || entry.Default != "deepseek-flash" {
		t.Fatalf("provider entry damaged: %+v", entry)
	}
	if saved.DefaultModel != "deepseek-flash/deepseek-flash" {
		t.Fatalf("default_model = %q, want it preserved", saved.DefaultModel)
	}
}

// Each posture round-trips, including back to ask — the mode the shell now reads
// at launch, so a write that normalised to the default would silently undo it.
func TestPersistDesktopApprovalModeRoundTripsEveryPosture(t *testing.T) {
	for _, mode := range []string{control.ToolApprovalAuto, control.ToolApprovalYolo, control.ToolApprovalAsk} {
		t.Run(mode, func(t *testing.T) {
			home := testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			persistDesktopApprovalMode(mode)
			saved := config.LoadForEdit(filepath.Join(home, "config.toml"))
			if got := saved.DesktopDefaultToolApprovalMode(); got != mode {
				t.Fatalf("approval mode = %q, want %q", got, mode)
			}
		})
	}
}
