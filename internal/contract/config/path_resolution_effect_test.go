package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestEffectConfigLoadsResolvePathsOnce(t *testing.T) {
	for _, mode := range []string{"", "file"} {
		t.Run("credentials_store="+mode, func(t *testing.T) {
			home, root := testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			t.Setenv("REASONIX_CREDENTIALS_STORE", mode)
			roots := RootsForHome(home)
			userPath := roots.UserConfigPath()
			projectPath := filepath.Join(root, "reasonix.toml")
			user := "credentials_store = 'file'\ndefault_model = 'local/test'\n[[providers]]\nname = 'local'\nkind = 'openai'\nbase_url = 'http://127.0.0.1:1/v1'\napi_key = 'dummy'\nmodels = ['test']\n"
			if err := os.WriteFile(userPath, []byte(user), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(projectPath, []byte("[memory]\nrecall_limit = 7\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			counts := map[string]int{}
			previous := evalSymlinks
			defer func() { evalSymlinks = previous }()
			evalSymlinks = func(path string) (string, error) {
				mu.Lock()
				counts[path]++
				mu.Unlock()
				return previous(path)
			}
			for _, loader := range []struct {
				name        string
				load        func(string) (*Config, error)
				wantUser    int
				wantProject int
			}{
				{"read-only", roots.LoadForRootReadOnly, 1, 1},
				{"runtime", roots.LoadForRoot, 2, 2},
				{"user-scope", roots.LoadUserScopeForRoot, 1, 0},
			} {
				mu.Lock()
				clear(counts)
				mu.Unlock()
				cfg, err := loader.load(root)
				if err != nil {
					t.Fatalf("%s: %v", loader.name, err)
				}
				if cfg.DefaultModel != "local/test" || cfg.CredentialsStore != "file" {
					t.Fatalf("%s lost user settings: model=%q credential store=%q", loader.name, cfg.DefaultModel, cfg.CredentialsStore)
				}
				if loader.wantProject > 0 && cfg.Memory.RecallLimit != 7 {
					t.Fatalf("%s lost project settings: recall_limit=%d", loader.name, cfg.Memory.RecallLimit)
				}
				mu.Lock()
				userCalls, projectCalls := counts[userPath], counts[projectPath]
				mu.Unlock()
				t.Logf("%s canonicalizations: user=%d project=%d", loader.name, userCalls, projectCalls)
				// Runtime loading additionally owns an independent MCP-tier migration snapshot per file.
				if userCalls != loader.wantUser || projectCalls != loader.wantProject {
					t.Errorf("%s canonicalizations: user=%d project=%d, want %d/%d", loader.name, userCalls, projectCalls, loader.wantUser, loader.wantProject)
				}
			}
		})
	}
}
