package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestConfigPathResolutionReloadsLinksAndPreservesScope(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("nested=%t", nested), func(t *testing.T) {
			home, root, outside := testenv.TempDir(t), testenv.TempDir(t), testenv.TempDir(t)
			t.Setenv("REASONIX_HOME", home)
			userPath := UserConfigPath()
			projectPath := filepath.Join(root, "reasonix.toml")
			if nested {
				projectPath = filepath.Join(root, ".reasonix", "reasonix.toml")
				if err := os.Mkdir(filepath.Dir(projectPath), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			userTarget := filepath.Join(outside, "first-user.toml")
			projectTarget := filepath.Join(root, "first-project.toml")
			write := func(path, scope, value string) {
				t.Helper()
				body := fmt.Sprintf("[[providers]]\nname = 'shared'\nkind = 'openai'\nbase_url = 'https://%s.example/v1'\nmodels = ['test']\n[[plugins]]\nname = 'shared'\ncommand = '%s'\n[desktop]\nprovider_access = ['%s']\n", value, value, scope)
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			link := func(path, target string) {
				t.Helper()
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			write(userTarget, "user", "user-first")
			write(projectTarget, "project", "project-first")
			link(userPath, userTarget)
			link(projectPath, projectTarget)
			check := func(load func(string) (*Config, error), user, project string) {
				t.Helper()
				cfg, err := load(root)
				if err != nil {
					t.Fatal(err)
				}
				provider, ok := cfg.Provider("shared")
				if !ok || provider.BaseURL != "https://"+user+".example/v1" || cfg.providerSources["shared"] != providerSourceUser {
					t.Fatalf("user provider provenance lost: provider=%+v sources=%v", provider, cfg.providerSources)
				}
				if len(cfg.Plugins) != 1 || cfg.Plugins[0].Command != project || cfg.Plugins[0].Source != MCPSourceProjectConfig {
					t.Fatalf("project plugin precedence/provenance lost: %+v", cfg.Plugins)
				}
				if !slices.Equal(cfg.Desktop.ProviderAccess, []string{"user", "project"}) {
					t.Fatalf("provider access merge = %v", cfg.Desktop.ProviderAccess)
				}
			}
			check(LoadForRoot, "user-first", "project-first")
			check(LoadForRootReadOnly, "user-first", "project-first")
			userTarget = filepath.Join(outside, "second-user.toml")
			projectTarget = filepath.Join(root, "second-project.toml")
			write(userTarget, "user", "user-second")
			write(projectTarget, "project", "project-second")
			for path, target := range map[string]string{userPath: userTarget, projectPath: projectTarget} {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				link(path, target)
			}
			check(LoadForRootReadOnly, "user-second", "project-second")
			check(LoadForRoot, "user-second", "project-second")
			if err := os.Remove(projectPath); err != nil {
				t.Fatal(err)
			}
			link(projectPath, userTarget)
			if _, err := LoadForRootReadOnly(root); err == nil {
				t.Fatal("retargeted project link escaped the workspace")
			}
		})
	}
}

func TestConfigPathResolutionKeepsMissingAndBrokenDistinct(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root := testenv.TempDir(t)
	projectPath := filepath.Join(root, "reasonix.toml")
	if _, err := LoadForRootReadOnly(root); err != nil {
		t.Fatalf("missing config should load defaults: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "absent.toml"), projectPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := LoadForRootReadOnly(root)
	var pathErr *fs.PathError
	if !errors.Is(err, fs.ErrNotExist) || !errors.As(err, &pathErr) {
		t.Fatalf("broken link lost typed filesystem cause: %v", err)
	}
	if err := os.Remove(projectPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, []byte("[memory]\nrecall_limit = 9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForRootReadOnly(root)
	if err != nil || cfg.Memory.RecallLimit != 9 {
		t.Fatalf("new file after missing/broken load was not observed: cfg=%+v err=%v", cfg, err)
	}
}

func TestConfigLoadRejectsResolvedEntryReplacedByExternalLink(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	root, outside := testenv.TempDir(t), testenv.TempDir(t)
	target := filepath.Join(root, "config", "settings.toml")
	if err := os.Mkdir(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("[memory]\nrecall_limit = 7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(outside, "settings.toml")
	if err := os.WriteFile(external, []byte("[memory]\nrecall_limit = 937\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(root, "reasonix.toml")
	if err := os.Symlink(target, projectPath); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	previous := evalSymlinks
	defer func() { evalSymlinks = previous }()
	replaced := false
	projectResolutions := 0
	evalSymlinks = func(path string) (string, error) {
		if path == projectPath {
			projectResolutions++
		}
		resolved, err := previous(path)
		if err == nil && path == projectPath && !replaced {
			if err := os.Remove(resolved); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(external, resolved); err != nil {
				t.Fatal(err)
			}
			replaced = true
		}
		return resolved, err
	}
	cfg, err := RootsForHome(os.Getenv("REASONIX_HOME")).LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if !replaced {
		t.Fatal("load did not reach the canonicalization handoff")
	}
	if cfg.Memory.RecallLimit == 937 {
		t.Fatal("load accepted a resolved entry replaced by a link outside its parent")
	}
	if projectResolutions != 1 {
		t.Fatalf("confinement refusal used uncached fallback: resolutions=%d, want 1", projectResolutions)
	}
}

func TestRuntimeLoadReadsConfigAfterTierMigration(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	root := testenv.TempDir(t)
	path := filepath.Join(root, "reasonix.toml")
	body := "[memory]\nrecall_limit = 937\n[[plugins]]\nname = 'test'\ncommand = 'test'\ntier = 123\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := LoadForRootReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if before.Memory.RecallLimit == 937 {
		t.Fatal("read-only fixture did not reject the invalid retired field")
	}
	after, err := LoadForRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	if after.Memory.RecallLimit != 937 || len(after.Plugins) != 1 || after.Plugins[0].Tier != "" {
		t.Fatalf("runtime load did not consume the migrated source: recall_limit=%d plugins=%+v", after.Memory.RecallLimit, after.Plugins)
	}
}
