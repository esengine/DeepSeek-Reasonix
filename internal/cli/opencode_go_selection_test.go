package cli

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

func TestOpenCodeGoCLIResumeAndExplicitSelection(t *testing.T) {
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
	path := filepath.Join(dir, "session.jsonl")
	s := agent.NewSession("fixture")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := agent.SetBranchModelPreserveUpdated(path, ref); err != nil {
		t.Fatal(err)
	}
	if _, err := modelForResumePath("", path, cfg); err == nil {
		t.Fatal("implicit CLI resume adopted changed connection")
	}
	if got, err := modelForResumePath(ref, path, cfg); err != nil || got != ref {
		t.Fatalf("explicit CLI selection blocked: %q, %v", got, err)
	}
	ctrl := control.New(control.Options{ModelRef: ref, ModelIdentity: cfg.ModelSelectionIdentity(ref)})
	defer ctrl.Close()
	ctrl.Resume(s, path)
	if err := persistCLIModelSelection(ctrl); err != nil {
		t.Fatal(err)
	}
	if got, err := modelForResumePath("", path, cfg); err != nil || got != ref {
		t.Fatalf("acknowledged CLI resume failed: %q, %v", got, err)
	}
	copyPath, err := copySessionForWriting(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := modelForResumePath("", copyPath, cfg); err != nil || got != ref {
		t.Fatalf("copied session lost acknowledgement: %q, %v", got, err)
	}
	cfg.Providers[0].APIKeyEnv = "CHANGED_AGAIN"
	if _, err := modelForResumePath("", copyPath, cfg); err == nil {
		t.Fatal("copy silently adopted a later account change")
	}
}
