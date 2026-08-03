package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestValidateAndWriteRefusesConcurrentModification(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	body, err := renderTOMLForScopeErr(Default(), RenderScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	stateID, err := configFileStateID(path)
	if err != nil {
		t.Fatal(err)
	}
	// Another process modifies the file between edit-read and write.
	if err := os.WriteFile(path, []byte("default_model = \"other\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := writeConfigOptions{scope: RenderScopeUser}
	_, err = validateAndWriteConfigResolved(path, body, 0o600, opts, stateID)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected concurrent-change error, got %v", err)
	}
	// The concurrent content survives.
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "other") {
		t.Errorf("concurrent write was overwritten: %q", b)
	}
}

func TestValidateAndWriteRequiresParseableOutput(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	// A body with an invalid TOML escape must never be written.
	_, err := validateAndWriteConfigResolved(path, "command = \"D:\\开发\\x.exe\"\n", 0o600, writeConfigOptions{scope: RenderScopeUser}, "")
	if err == nil {
		t.Fatal("write of unparseable config succeeded")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("file was created despite validation failure: %v", statErr)
	}
}

func TestConfigFileStateIDChangesWithContent(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("a = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := configFileStateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("a = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := configFileStateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("state ID did not change with content")
	}
	absent, err := configFileStateID(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if absent != "absent" {
		t.Fatalf("missing file state = %q, want absent", absent)
	}
}

func TestValidateAndWriteDetectsSilentlyDroppedField(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	// A body whose decoded config does NOT match the intended config (the
	// renderer "forgot" default_model) must be rejected by the persisted
	// semantics comparison.
	want := Default()
	want.DefaultModel = "deepseek-pro"
	body, err := renderTOMLForScopeErr(want, RenderScopeUser)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a renderer dropping the field: strip default_model from the body.
	lines := strings.Split(body, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if strings.HasPrefix(line, "default_model") {
			continue
		}
		kept = append(kept, line)
	}
	opts := writeConfigOptions{scope: RenderScopeUser, want: want}
	_, err = validateAndWriteConfigResolved(path, strings.Join(kept, "\n"), 0o600, opts, "")
	if err == nil || !strings.Contains(err.Error(), "default_model") {
		t.Fatalf("dropped-field body accepted: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("dropped-field body was written")
	}
}

func TestLoadForEditSaveToRefusesStaleSnapshot(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	original := Default()
	original.DefaultModel = "deepseek-flash"
	if err := original.WriteFile(path); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.SetDefaultModel("deepseek-pro"); err != nil {
		t.Fatal(err)
	}

	// Another process replaces the file after load.
	if err := os.WriteFile(path, []byte("default_model = \"hijacked\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = cfg.SaveTo(path)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected concurrent-change error on public Load→Save path, got %v", err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "hijacked") {
		t.Fatalf("stale SaveTo overwrote concurrent content: %s", body)
	}
	if strings.Contains(string(body), "deepseek-pro") {
		t.Fatalf("stale edit was persisted: %s", body)
	}
}

func TestLoadForEditSaveToRefusesConcurrentCreateOfAbsentFile(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "missing.toml")

	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatalf("load absent: %v", err)
	}
	if !cfg.editOriginBound || cfg.editOriginState != "absent" {
		t.Fatalf("edit origin = bound:%v state:%q, want absent", cfg.editOriginBound, cfg.editOriginState)
	}
	if err := cfg.SetDefaultModel("deepseek-pro"); err != nil {
		t.Fatal(err)
	}

	// Another process creates the file before our create-only publish.
	if err := os.WriteFile(path, []byte("default_model = \"already-there\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = cfg.SaveTo(path)
	if err == nil {
		t.Fatal("expected concurrent-create error, save succeeded")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "already-there") {
		t.Fatalf("create-only path overwrote concurrent create: %s", body)
	}
	if strings.Contains(string(body), "deepseek-pro") {
		t.Fatalf("stale absent-origin save was persisted: %s", body)
	}
}

func TestProjectDeltaValidationDetectsDroppedCustomProviderField(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := os.WriteFile(path, []byte("default_model = \"deepseek-flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Delta claims a custom provider with base_url, but the merged body drops it.
	// Built-in providers would occupy providers[0] under index comparison.
	delta := `
[[providers]]
name     = "custom-relay"
kind     = "openai"
base_url = "https://relay.example/v1"
model    = "relay-model"
`
	// Body retains only the default_model line — custom provider missing entirely.
	body := "default_model = \"deepseek-flash\"\n"
	opts := writeConfigOptions{
		scope: RenderScopeProject,
		delta: delta,
	}
	_, err := validateAndWriteConfigResolved(path, body, 0o644, opts, "")
	if err == nil {
		t.Fatal("dropped custom provider field was accepted")
	}
	if !strings.Contains(err.Error(), "custom-relay") && !strings.Contains(err.Error(), "base_url") && !strings.Contains(err.Error(), "providers") {
		t.Fatalf("error should mention the custom provider field, got %v", err)
	}
	// Original file preserved.
	got, _ := os.ReadFile(path)
	if string(got) != "default_model = \"deepseek-flash\"\n" {
		t.Fatalf("original project file changed: %q", got)
	}
}

func TestExtraChecksRunWithoutDelta(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := os.WriteFile(path, []byte("[desktop]\nlegacy = \"keep\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Body claims provider_access but value differs from extraChecks.
	body := "[desktop]\nlegacy = \"keep\"\nprovider_access = [\"other\"]\n"
	opts := writeConfigOptions{
		scope:       RenderScopeProject,
		extraChecks: map[string]any{"desktop.provider_access": []string{"expected"}},
	}
	_, err := validateAndWriteConfigResolved(path, body, 0o644, opts, "")
	if err == nil {
		t.Fatal("extraChecks with empty delta were skipped")
	}
	if !strings.Contains(err.Error(), "provider_access") {
		t.Fatalf("expected provider_access drift, got %v", err)
	}
}

func TestBindAbsentEditTargetAllowsLegacySeedToAbsentUserConfig(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "reasonix.toml")
	user := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(project, []byte("default_model = \"legacy-model\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForEditReadOnlyStrict(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.BindAbsentEditTarget(user); err != nil {
		t.Fatalf("BindAbsentEditTarget: %v", err)
	}
	if cfg.editOriginState != "absent" {
		t.Fatalf("target state = %q, want absent", cfg.editOriginState)
	}
	if err := cfg.SaveTo(user); err != nil {
		t.Fatalf("SaveTo user after seed: %v", err)
	}
	body, _ := os.ReadFile(user)
	if !strings.Contains(string(body), "legacy-model") {
		t.Fatalf("seeded user config missing model: %s", body)
	}
	// Ordinary cross-path without BindAbsentEditTarget still fails.
	cfg2, err := LoadForEditReadOnlyStrict(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg2.SaveTo(user); err == nil {
		t.Fatal("SaveTo cross-path without BindAbsentEditTarget should fail")
	}
}

func TestRebindUsesPublishedBytesNotReread(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	cfg.DefaultModel = "first"
	// Bind as if loaded from absent target, then save.
	if err := cfg.BindAbsentEditTarget(path); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("first SaveTo: %v", err)
	}
	published := cfg.editOriginState
	// Concurrent writer changes the file between publish and a would-be reread.
	if err := os.WriteFile(path, []byte("default_model = \"attacker\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Origin must still be the published body, not the attacker content.
	if cfg.editOriginState != published {
		t.Fatalf("origin mutated without rebind helper: %q vs %q", cfg.editOriginState, published)
	}
	// Second SaveTo must refuse because on-disk state no longer matches published origin.
	cfg.DefaultModel = "second"
	err := cfg.SaveTo(path)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("expected concurrent-change error, got %v", err)
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "attacker") {
		t.Fatalf("attacker content was overwritten: %s", body)
	}
}

func TestDuplicateProviderNamesKeepOccurrenceIdentity(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := os.WriteFile(path, []byte("default_model = \"deepseek-flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	delta := `
[[providers]]
name     = "dup"
kind     = "openai"
base_url = "https://first.example/v1"
model    = "m1"

[[providers]]
name     = "dup"
kind     = "openai"
base_url = "https://second.example/v1"
model    = "m2"
`
	// Body keeps only the second same-name provider — first was dropped.
	body := `
default_model = "deepseek-flash"

[[providers]]
name     = "dup"
kind     = "openai"
base_url = "https://second.example/v1"
model    = "m2"
`
	opts := writeConfigOptions{scope: RenderScopeProject, delta: delta}
	_, err := validateAndWriteConfigResolved(path, body, 0o644, opts, "")
	if err == nil {
		t.Fatal("dropping first same-name provider should fail validation")
	}
	if !strings.Contains(err.Error(), "dup") && !strings.Contains(err.Error(), "providers") {
		t.Fatalf("error should mention providers/dup, got %v", err)
	}
}

func TestExtraBodyMapKeyDoesNotCollideWithNestedTable(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	cfg := Default()
	// One provider with both a dotted map key and a nested table under extra_body.
	cfg.Providers = append(cfg.Providers, ProviderEntry{
		Name:    "custom",
		Kind:    "openai",
		BaseURL: "https://example.com/v1",
		Model:   "m",
		ExtraBody: map[string]any{
			"a.b": "flat",
			"a": map[string]any{
				"b": "nested",
			},
		},
	})
	if err := cfg.WriteFile(path); err != nil {
		t.Fatalf("WriteFile with nested extra_body: %v", err)
	}
	// Round-trip load should keep both values.
	loaded, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := loaded.Provider("custom")
	if !ok {
		t.Fatal("custom provider missing")
	}
	if p.ExtraBody["a.b"] != "flat" {
		t.Fatalf("flat key a.b = %#v", p.ExtraBody["a.b"])
	}
	nested, _ := p.ExtraBody["a"].(map[string]any)
	if nested == nil || nested["b"] != "nested" {
		t.Fatalf("nested a.b = %#v", p.ExtraBody["a"])
	}
}

func TestBindAbsentEditTargetRefusesExistingTarget(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "reasonix.toml")
	user := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(project, []byte("default_model = \"legacy-model\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(user, []byte("default_model = \"user-owned\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForEditReadOnlyStrict(project)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.BindAbsentEditTarget(user); err == nil {
		t.Fatal("BindAbsentEditTarget authorized an existing target")
	}
	// Existing user content must remain untouched.
	body, _ := os.ReadFile(user)
	if !strings.Contains(string(body), "user-owned") {
		t.Fatalf("existing target was modified: %s", body)
	}
}

func TestBoundConfigConsecutiveSaveSucceeds(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "config.toml")
	first := Default()
	first.DefaultModel = "deepseek-flash"
	if err := first.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.SetDefaultModel("deepseek-pro"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("first SaveTo: %v", err)
	}
	// Second save on the same bound Config must not mis-detect Windows mode drift.
	if err := cfg.SetDefaultModel("deepseek-flash"); err != nil {
		t.Fatal(err)
	}
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("second SaveTo on bound config: %v", err)
	}
}

func TestProjectDeltaValidationDetectsDroppedModelOverrides(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := os.WriteFile(path, []byte("default_model = \"deepseek-flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	delta := `
[[providers]]
name = "custom"
kind = "openai"
base_url = "https://example.com/v1"
model = "m"
model_overrides = { "m" = { context_window = 12345 } }
`
	// Body keeps the provider but drops model_overrides entirely.
	body := `
default_model = "deepseek-flash"

[[providers]]
name = "custom"
kind = "openai"
base_url = "https://example.com/v1"
model = "m"
`
	opts := writeConfigOptions{scope: RenderScopeProject, delta: delta}
	_, err := validateAndWriteConfigResolved(path, body, 0o644, opts, "")
	if err == nil {
		t.Fatal("dropped model_overrides was accepted")
	}
	if !strings.Contains(err.Error(), "model_overrides") && !strings.Contains(err.Error(), "custom") {
		t.Fatalf("error should mention model_overrides, got %v", err)
	}
}

func TestProjectDeltaValidationDetectsDroppedFieldOnPaddedProviderName(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := os.WriteFile(path, []byte("default_model = \"deepseek-flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Leading/trailing spaces in name must not desync snapshot vs mask identity.
	delta := `
[[providers]]
name     = " custom "
kind     = "openai"
base_url = "https://intended.example/v1"
model    = "m"
`
	body := `
default_model = "deepseek-flash"

[[providers]]
name     = " custom "
kind     = "openai"
base_url = "https://dropped.example/v1"
model    = "m"
`
	opts := writeConfigOptions{scope: RenderScopeProject, delta: delta}
	_, err := validateAndWriteConfigResolved(path, body, 0o644, opts, "")
	if err == nil {
		t.Fatal("padded provider name should still validate base_url drift")
	}
	if !strings.Contains(err.Error(), "base_url") && !strings.Contains(err.Error(), "custom") && !strings.Contains(err.Error(), "providers") {
		t.Fatalf("error should mention provider field drift, got %v", err)
	}
}

func TestProjectDeltaValidationDetectsDroppedFieldOnPaddedPluginName(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	path := filepath.Join(t.TempDir(), "reasonix.toml")
	if err := os.WriteFile(path, []byte("default_model = \"deepseek-flash\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	delta := `
[[plugins]]
name    = " padded "
command = "intended-bin"
`
	body := `
default_model = "deepseek-flash"

[[plugins]]
name    = " padded "
command = "other-bin"
`
	opts := writeConfigOptions{scope: RenderScopeProject, delta: delta}
	_, err := validateAndWriteConfigResolved(path, body, 0o644, opts, "")
	if err == nil {
		t.Fatal("padded plugin name should still validate command drift")
	}
	if !strings.Contains(err.Error(), "command") && !strings.Contains(err.Error(), "padded") && !strings.Contains(err.Error(), "plugins") {
		t.Fatalf("error should mention plugin field drift, got %v", err)
	}
}

func TestBindAbsentEditTargetErrorIsErrEditTargetExists(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	target := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(target, []byte("default_model = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	err := cfg.BindAbsentEditTarget(target)
	if err == nil {
		t.Fatal("expected exists error")
	}
	if !errors.Is(err, ErrEditTargetExists) {
		t.Fatalf("errors.Is(ErrEditTargetExists) = false for %v", err)
	}
}

func TestEffectivePersistedFileModePassThroughAndWindowsMapping(t *testing.T) {
	// Non-Windows path: bits pass through unchanged.
	if runtime.GOOS != "windows" {
		if got := effectivePersistedFileMode(0o600); got != 0o600 {
			t.Fatalf("unix mode 0600 = %o, want 0600", got)
		}
		if got := effectivePersistedFileMode(0o444); got != 0o444 {
			t.Fatalf("unix mode 0444 = %o, want 0444", got)
		}
	}
	// Windows mapping rules (callable on every OS).
	if got := windowsEffectivePersistedFileMode(0o600); got != 0o666 {
		t.Fatalf("windows writable 0600 maps to %o, want 0666", got)
	}
	if got := windowsEffectivePersistedFileMode(0o666); got != 0o666 {
		t.Fatalf("windows writable 0666 maps to %o, want 0666", got)
	}
	if got := windowsEffectivePersistedFileMode(0o444); got != 0o444 {
		t.Fatalf("windows read-only 0444 maps to %o, want 0444", got)
	}
	if got := windowsEffectivePersistedFileMode(0o400); got != 0o444 {
		t.Fatalf("windows read-only 0400 maps to %o, want 0444", got)
	}
	if runtime.GOOS != "windows" {
		return
	}
	// Real Windows Stat/chmod identity change.
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("a = 1\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	writableID, err := configFileStateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	readonlyID, err := configFileStateID(path)
	if err != nil {
		t.Fatal(err)
	}
	if writableID == readonlyID {
		t.Fatal("windows read-only mode should change StateID")
	}
	_ = os.Chmod(path, 0o666)
}
