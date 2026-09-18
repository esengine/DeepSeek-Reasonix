package cli

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
)

func TestOpenCodeGoCLIResumeAndExplicitSelection(t *testing.T) {
	isolateCLIConfigHome(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`config_version = 9
[[providers]]
name = "go"
kind = "anthropic"
base_url = "https://opencode.ai/zen/go"
api_key_env = "CLI_SELECTION_KEY"
model = "deepseek-v4-flash"
`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.ApplyUserConfigUpgradesOnStartup(configPath); err != nil {
		t.Fatal(err)
	}
	cfg := config.LoadForEdit(configPath)
	cfg.Providers[0].NoProxy = true
	const ref = "go/deepseek-v4-flash"

	sessionDir := resolveCLISessionDir()
	id := nextNativeTestID()
	seedNativeSessionWithModel(t, sessionDir, id, "fixture", ref, cfg.ModelSelectionIdentity(ref))
	locator := v4ResumeLocator(id)

	// The saved connection changed, so an implicit resume must fail closed
	// rather than silently adopt a different account.
	cfg.Providers[0].APIKeyEnv = "CHANGED_AGAIN"
	if _, err := modelForResumePath("", locator, cfg); err == nil {
		t.Fatal("implicit CLI resume adopted changed connection")
	}
	if got, err := modelForResumePath(ref, locator, cfg); err != nil || got != ref {
		t.Fatalf("explicit CLI selection blocked: %q, %v", got, err)
	}
}
