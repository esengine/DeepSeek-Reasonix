package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Legacy desktop theme-state compatibility gate for the extension-kernel
// work. Plugin themes introduce plugin:<plugin>:<theme> IDs, but the
// persisted desktop-theme-state.json must keep round-tripping for older
// states: v1 files migrate to v2, user theme IDs survive a save, base-style
// IDs are never persisted, and unknown/garbage content falls back to the
// default state without crashing.

func TestLegacyThemeStateV1MigratesToV2(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	path := themeStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// v1 state: schemaVersion 0/1 with an active user theme id.
	if err := os.WriteFile(path, []byte(`{"activeThemeId":"my-user-theme"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	st := loadThemeDesktopState()
	if st.SchemaVersion != themeStateSchemaVerV1 {
		t.Fatalf("missing schemaVersion should decode as v1, got %d", st.SchemaVersion)
	}
	if st.ActiveThemeID != "my-user-theme" {
		t.Fatalf("active theme id lost: %+v", st)
	}

	if err := saveThemeDesktopState(st); err != nil {
		t.Fatalf("saveThemeDesktopState: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved ThemeDesktopState
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("saved state unreadable: %v", err)
	}
	if saved.SchemaVersion != themeStateSchemaVer {
		t.Fatalf("save should stamp schema v%d, got %d", themeStateSchemaVer, saved.SchemaVersion)
	}
	if saved.ActiveThemeID != "my-user-theme" {
		t.Fatalf("user theme id did not round-trip: %s", raw)
	}
}

func TestLegacyThemeStateGarbageFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	path := themeStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	st := loadThemeDesktopState()
	if st.SchemaVersion != themeStateSchemaVer || st.ActiveThemeID != "" {
		t.Fatalf("garbage state should fall back to default, got %+v", st)
	}
}

func TestLegacyThemeStateBaseStyleNeverPersisted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	if err := saveThemeDesktopState(ThemeDesktopState{ActiveThemeID: "graphite"}); err != nil {
		t.Fatalf("saveThemeDesktopState: %v", err)
	}
	raw, err := os.ReadFile(themeStatePath())
	if err != nil {
		t.Fatal(err)
	}
	var saved ThemeDesktopState
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.ActiveThemeID != "" {
		t.Fatalf("base style id must never persist as activeThemeId: %s", raw)
	}
}
