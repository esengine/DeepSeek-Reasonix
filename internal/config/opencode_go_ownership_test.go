package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A bare model name resolves to its first owner. Splitting an earlier account
// by route must not let a later account overtake models it never owned, on
// disk and for project references that are only resolved in memory.
func TestOpenCodeGoV10SplitKeepsBareModelOwnership(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	t.Setenv("ACCOUNT_A_KEY", "test-account-a")
	t.Setenv("ACCOUNT_B_KEY", "test-account-b")
	path := filepath.Join(home, "config.toml")
	if err := os.WriteFile(path, []byte(openCodeGoUpgradeFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyUserConfigUpgradesOnStartup(path); err != nil {
		t.Fatal(err)
	}
	var cfg Config
	if _, err := decodeTOMLFile(path, &cfg); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range cfg.Providers {
		names = append(names, p.Name)
	}
	if len(names) != 5 || names[0] != "go" || names[1] != "go-chat-2" || names[4] != "go-chat" {
		t.Fatalf("split groups did not follow their source: %v", names)
	}
	cfg.loadOpenCodeGoJournal(path)
	for _, ref := range []string{"deepseek-v4-pro", "deepseek-v4-flash"} {
		e, ok := cfg.ResolveModel(ref)
		if !ok || e.APIKeyEnv != "ACCOUNT_A_KEY" || e.Name != "go-chat-2" {
			t.Fatalf("bare %q changed account: %+v", ref, e)
		}
	}

	root := t.TempDir()
	project := `default_model = "deepseek-v4-pro"
[agent]
planner_model = "go/deepseek-v4-flash"
`
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(project), 0o600); err != nil {
		t.Fatal(err)
	}
	merged, err := LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{merged.DefaultModel, merged.Agent.PlannerModel} {
		e, ok := merged.ResolveModel(ref)
		if !ok || e.APIKeyEnv != "ACCOUNT_A_KEY" {
			t.Fatalf("project ref %q resolved to another account: %+v", ref, e)
		}
	}
}
