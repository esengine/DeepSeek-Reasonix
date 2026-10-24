package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

func installationRoots(t *testing.T) Roots {
	t.Helper()
	t.Setenv("REASONIX_STATE_HOME", "")
	return RootsForHome(filepath.Join(t.TempDir(), "new-profile"))
}

func TestNewInstallationCollaborationDefaultIsDurableWithoutReadOnlyWrites(t *testing.T) {
	roots := installationRoots(t)
	project := t.TempDir()
	preview, err := roots.LoadForRootReadOnly(project)
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := preview.CollaborationMode(); err != nil || mode != "auto" {
		t.Fatalf("fresh default: %s %v", mode, err)
	}
	if _, err := os.Stat(roots.Home()); !os.IsNotExist(err) {
		t.Fatalf("read-only preview created a profile: %v", err)
	}
	cfg, err := roots.LoadForRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := cfg.CollaborationMode(); err != nil || mode != "auto" {
		t.Fatalf("runtime default: %s %v", mode, err)
	}
	marker := filepath.Join(roots.Home(), "collaboration-default-v1.json")
	before, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	noIntegration := false
	cfg.Agent.AutoIntegrate = &noIntegration
	if err := cfg.SaveTo(roots.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	again, err := roots.LoadUserConfigReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := again.CollaborationMode(); err != nil || mode != "auto" || again.AutoIntegrationEnabled() {
		t.Fatalf("first save changed default or user choice: %s %v", mode, err)
	}
	if err := again.SetCollaborationMode(""); err != nil {
		t.Fatal(err)
	}
	if err := again.SaveTo(roots.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	legacy, err := roots.LoadUserConfigReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := legacy.CollaborationMode(); err != nil || mode != "legacy" {
		t.Fatalf("explicit compatibility choice lost: %s %v", mode, err)
	}
	after, err := os.ReadFile(marker)
	if err != nil || string(before) != string(after) {
		t.Fatal("user override rewrote installation provenance", err)
	}
}

func TestExistingInstallationWithoutCollaborationKeepsLegacyAndOriginalBytes(t *testing.T) {
	for _, kind := range []string{"config", "state", "relocated-state"} {
		t.Run(kind, func(t *testing.T) {
			roots := installationRoots(t)
			if err := os.MkdirAll(roots.Home(), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("# keep these legacy choices\n[agent]\nwrite_lease='optimistic'\nsubagent_claims='optimistic'\n[future]\nopaque='retained'\n")
			switch kind {
			case "config":
				if err := os.WriteFile(roots.UserConfigPath(), original, 0600); err != nil {
					t.Fatal(err)
				}
			case "state":
				if err := os.Mkdir(filepath.Join(roots.Home(), "sessions"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(roots.Home(), "sessions", "previous.jsonl"), []byte("retained history"), 0600); err != nil {
					t.Fatal(err)
				}
			case "relocated-state":
				state := t.TempDir()
				t.Setenv("REASONIX_STATE_HOME", state)
				if err := os.WriteFile(filepath.Join(state, "history.json"), []byte("old state"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := roots.LoadForRoot(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if mode, err := cfg.CollaborationMode(); err != nil || mode != "legacy" {
				t.Fatalf("old profile migrated: %s %v", mode, err)
			}
			if kind == "config" && (!cfg.WriteLeaseOptimistic() || !cfg.SubagentClaimsOptimistic()) {
				t.Fatal("legacy coordination choices changed")
			}
			if kind == "config" {
				after, err := os.ReadFile(roots.UserConfigPath())
				if err != nil || string(after) != string(original) {
					t.Fatal("legacy file was rewritten", err)
				}
			}
			if _, err := os.Stat(filepath.Join(roots.Home(), "collaboration-default-v1.json")); !os.IsNotExist(err) {
				t.Fatal("legacy profile received a new-install marker", err)
			}
		})
	}
}

func TestNewInstallationConcurrentReadersAgreeAfterStateAppears(t *testing.T) {
	roots := installationRoots(t)
	project := t.TempDir()
	var joined sync.WaitGroup
	errors := make(chan error, 8)
	for id := range 8 {
		joined.Go(func() {
			cfg, err := roots.LoadForRoot(project)
			if err != nil {
				errors <- err
				return
			}
			mode, err := cfg.CollaborationMode()
			if err != nil {
				errors <- err
				return
			}
			if mode != "auto" {
				errors <- &unexpectedInstallationMode{mode}
				return
			}
			if err := os.MkdirAll(filepath.Join(roots.Home(), "sessions"), 0700); err != nil {
				errors <- err
				return
			}
			if err := os.WriteFile(filepath.Join(roots.Home(), "sessions", fmt.Sprintf("chat-%d.jsonl", id)), []byte("admitted history"), 0600); err != nil {
				errors <- err
			}
		})
	}
	joined.Wait()
	close(errors)
	for err := range errors {
		t.Error(err)
	}
}

func TestOldConfigurationImportedIntoFreshInstallationKeepsCompatibility(t *testing.T) {
	roots := installationRoots(t)
	if _, err := roots.LoadForRoot(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	before := []byte("# old configuration without the new strategy\n[agent]\nwrite_lease='optimistic'\nsubagent_claims='optimistic'\n")
	if err := os.WriteFile(roots.UserConfigPath(), before, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roots.LoadForRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := cfg.CollaborationMode(); err != nil || mode != "legacy" {
		t.Fatalf("imported old settings migrated: %s %v", mode, err)
	}
	after, err := os.ReadFile(roots.UserConfigPath())
	if err != nil || string(before) != string(after) {
		t.Fatal("imported old file changed", err)
	}
}

func TestUserSnapshotAppearingAfterDefaultSelectionKeepsLegacy(t *testing.T) {
	roots := installationRoots(t)
	cfg := roots.defaultConfig()
	if mode, err := cfg.CollaborationMode(); err != nil || mode != "auto" {
		t.Fatal("fixture did not select a fresh default", mode, err)
	}
	if err := os.MkdirAll(roots.Home(), 0700); err != nil {
		t.Fatal(err)
	}
	path := roots.UserConfigPath()
	if err := os.WriteFile(path, []byte("[agent]\nwrite_lease='optimistic'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeFileSnapshot(cfg, path); err != nil {
		t.Fatal(err)
	}
	if mode, err := cfg.CollaborationMode(); err != nil || mode != "legacy" || !cfg.WriteLeaseOptimistic() {
		t.Fatalf("configuration imported during load inherited a new-install default: %s %v", mode, err)
	}
}

type unexpectedInstallationMode struct{ mode string }

func (e *unexpectedInstallationMode) Error() string {
	return "concurrent initialization chose " + e.mode
}

func TestUnsupportedInstallationDefaultIsPreservedAndCannotBecomeWritableDefault(t *testing.T) {
	for _, body := range []string{`{"version":999,"mode":"auto"}`, `{"version":1,"mode":"auto","future":true}`, `{"version":1,"mode":"auto"} {}`} {
		t.Run(body, func(t *testing.T) {
			roots := installationRoots(t)
			if err := os.MkdirAll(roots.Home(), 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(roots.Home(), "collaboration-default-v1.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := roots.LoadForRootReadOnly(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if mode, err := cfg.CollaborationMode(); err != nil || mode != "legacy" || cfg.UsesMutationTransactions() {
				t.Fatalf("unsupported default must run the compatible protocol, got %q %v", mode, err)
			}
			if !slices.ContainsFunc(cfg.LoadWarnings(), func(w string) bool { return strings.Contains(w, "installation collaboration default unavailable") }) {
				t.Fatalf("unsupported default was not reported: %v", cfg.LoadWarnings())
			}
			edit := roots.defaultConfig()
			if err := edit.SaveTo(roots.UserConfigPath()); err == nil {
				t.Fatal("unsupported metadata became a new user configuration")
			}
			if _, err := os.Stat(roots.UserConfigPath()); !os.IsNotExist(err) {
				t.Fatal("failed edit manufactured user configuration", err)
			}
			_, _ = roots.LoadForRoot(t.TempDir())
			after, readErr := os.ReadFile(path)
			if readErr != nil || string(after) != body {
				t.Fatal("unsupported policy changed", readErr)
			}
		})
	}
}

func TestProjectCannotReplaceNewInstallationDefault(t *testing.T) {
	roots := installationRoots(t)
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "reasonix.toml"), []byte("[agent]\ncollaboration='shared'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roots.LoadForRoot(project)
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := cfg.CollaborationMode(); err != nil || mode != "auto" {
		t.Fatal("project replaced installation default", mode, err)
	}
	if !strings.Contains(RenderTOMLForScope(cfg, RenderScopeUser), "collaboration = \"auto\"") {
		t.Fatal("new user config does not save its selected default")
	}
}

func TestFirstUserConfigurationSaveIncludesAutoAfterBootstrapMetadata(t *testing.T) {
	roots := installationRoots(t)
	t.Setenv("REASONIX_HOME", roots.Home())
	for _, dir := range []string{"logs", "sessions"} {
		if err := os.MkdirAll(filepath.Join(roots.Home(), dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(roots.Home(), "logs", "bootstrap.log"), []byte("startup"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	if err := cfg.SaveTo(roots.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(roots.UserConfigPath())
	if err != nil || !strings.Contains(string(stored), "collaboration = \"auto\"") {
		t.Fatalf("first save did not freeze its default: %v %s", err, stored)
	}
	loaded, err := roots.LoadUserConfigReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := loaded.CollaborationMode(); err != nil || mode != "auto" {
		t.Fatal(mode, err)
	}
}

func TestBoundUserConfigurationSavePreservesOpaqueDataAndPrivateMode(t *testing.T) {
	roots := installationRoots(t)
	if err := os.MkdirAll(roots.Home(), 0700); err != nil {
		t.Fatal(err)
	}
	before := []byte("# user's opaque setting\n[future]\nretained='unrecognized'\n[agent]\nworkspace_write_lock=false\n")
	if err := os.WriteFile(roots.UserConfigPath(), before, 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := roots.LoadUserConfigReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetCollaborationMode("shared"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(roots.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(roots.UserConfigPath())
	if err != nil || !strings.Contains(string(after), "retained='unrecognized'") || !strings.Contains(string(after), "# user's opaque setting") {
		t.Fatalf("bound save lost user data: %v %s", err, after)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(roots.UserConfigPath())
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("user config not private: %v %v", info, err)
		}
	}
	loaded, err := roots.LoadUserConfigReadOnly()
	if err != nil {
		t.Fatal(err)
	}
	if mode, err := loaded.CollaborationMode(); err != nil || mode != "shared" {
		t.Fatal(mode, err)
	}
}
