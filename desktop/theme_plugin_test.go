package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/pluginpkg"
)

// Plugin theme contract tests (Stage 4b): discovery from enabled installed
// plugins, invalid files skipped with warnings, plugin:<plugin>:<theme> id
// activation/persistence, the fallback-preserve contract across plugin
// removal + restore on reinstall, read-only guards, and legacy state files.

type pluginThemeFixture struct {
	fileName  string             // ZIP file name under themes/
	manifest  *ThemePackManifest // nil → garbage bytes (invalid pack)
	withImage bool               // embed a small PNG scene image
}

func testPluginThemeManifest(id, name string) *ThemePackManifest {
	return &ThemePackManifest{
		SchemaVersion: themePackSchemaVersion,
		ID:            id,
		Name:          name,
		Author:        "Fixture",
		BaseStyle:     "graphite",
		Tokens: ThemePackTokens{
			Dark: map[string]string{"accent": "#ff6a3d"},
		},
		Recipes: defaultThemePackRecipes(),
	}
}

func testPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			img.Set(x, y, color.RGBA{R: uint8(x * 30), G: uint8(y * 30), B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// installPluginThemeFixture writes a Manifest v1 plugin root with
// contributes.themes globbing themes/*.reasonix-theme, plus the
// plugin-packages.json entry pointing at it.
func installPluginThemeFixture(t *testing.T, home, pluginName string, enabled bool, themes []pluginThemeFixture) string {
	t.Helper()
	root := filepath.Join(home, "plugin-src", pluginName)
	themesDir := filepath.Join(root, "themes")
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, fx := range themes {
		dest := filepath.Join(themesDir, fx.fileName)
		if fx.manifest == nil {
			if err := os.WriteFile(dest, []byte("this is not a zip"), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		m := *fx.manifest
		var img []byte
		if fx.withImage {
			img = testPNGBytes(t)
			if m.Background == nil {
				bg := defaultThemePackBackground()
				m.Background = &bg
			}
			m.Background.Image = "background.png"
		}
		if err := writeThemeZip(dest, &m, img); err != nil {
			t.Fatalf("write fixture theme %s: %v", fx.fileName, err)
		}
	}
	manifestJSON := fmt.Sprintf(
		`{"apiVersion":%q,"name":%q,"version":"1.0.0","contributes":{"themes":["themes/*.reasonix-theme"]}}`,
		pluginpkg.ManifestAPIVersionV1, pluginName,
	)
	if err := os.WriteFile(filepath.Join(root, pluginpkg.NativeManifest), []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := pluginpkg.Upsert(home, pluginpkg.InstalledPlugin{
		Name:         pluginName,
		Root:         root,
		Version:      "1.0.0",
		ManifestKind: "reasonix",
		Enabled:      enabled,
	}); err != nil {
		t.Fatal(err)
	}
	return root
}

func findThemePackView(list []ThemePackView, id string) *ThemePackView {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

func readThemeStateRaw(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(themeStatePath())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestParsePluginThemeID(t *testing.T) {
	cases := []struct {
		id         string
		ok         bool
		pluginName string
		themeID    string
	}{
		{"plugin:themery:neon-dusk", true, "themery", "neon-dusk"},
		{"plugin:My_Plugin.2:x", true, "My_Plugin.2", "x"},
		{"plugin:themery:neon-dusk:extra", false, "", ""}, // theme ids carry no colons
		{"plugin:themery", false, "", ""},
		{"plugin:themery:", false, "", ""},
		{"plugin::neon-dusk", false, "", ""},
		{"plugin:themery:Neon", false, "", ""}, // themePackIDRe still governs the inner id
		{"neon-dusk", false, "", ""},
		{"official-rose-dawn", false, "", ""},
		{"", false, "", ""},
	}
	for _, tc := range cases {
		pluginName, themeID, ok := parsePluginThemeID(tc.id)
		if ok != tc.ok || pluginName != tc.pluginName || themeID != tc.themeID {
			t.Fatalf("parsePluginThemeID(%q) = %q, %q, %v; want %q, %q, %v",
				tc.id, pluginName, themeID, ok, tc.pluginName, tc.themeID, tc.ok)
		}
	}
	// isPluginThemeID is prefix-only on purpose (fallback contract seam).
	if !isPluginThemeID("plugin:garbage::") {
		t.Fatal("isPluginThemeID must be prefix-only")
	}
	if isPluginThemeID("neon-dusk") || isPluginThemeID("") {
		t.Fatal("isPluginThemeID false positive")
	}
}

func TestPluginThemeDiscoveryAndList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	pluginRoot := installPluginThemeFixture(t, home, "themery", true, []pluginThemeFixture{
		{fileName: "neon.reasonix-theme", manifest: testPluginThemeManifest("neon-dusk", "Neon Dusk"), withImage: true},
	})
	app := NewApp()

	list, err := app.ListThemePacks()
	if err != nil {
		t.Fatal(err)
	}
	view := findThemePackView(list, "plugin:themery:neon-dusk")
	if view == nil {
		t.Fatalf("plugin theme not listed: %+v", list)
	}
	if view.Kind != themeKindPlugin {
		t.Fatalf("kind = %q, want plugin", view.Kind)
	}
	if view.PluginName != "themery" {
		t.Fatalf("pluginName = %q, want themery", view.PluginName)
	}
	if view.Builtin {
		t.Fatal("plugin theme must not be builtin")
	}
	if view.Name != "Neon Dusk" || view.BaseStyle != "graphite" {
		t.Fatalf("view = %+v", view)
	}
	if !view.HasBackground || view.BackgroundURL == "" {
		t.Fatalf("plugin theme must expose its background: %+v", view)
	}
	if !strings.HasPrefix(view.BackgroundURL, themeAssetURLPrefix+"plugin:themery:neon-dusk/") {
		t.Fatalf("background URL not content-addressed under the plugin id: %q", view.BackgroundURL)
	}
	// Plugin themes come after base/official/user.
	if list[len(list)-1].ID != "plugin:themery:neon-dusk" {
		t.Fatalf("plugin themes must sort last: %q", list[len(list)-1].ID)
	}
	// Nothing is copied into the user theme library (the library dir is shared
	// across this test package, so assert on the plugin theme's own id).
	if userThemeExists("neon-dusk") {
		t.Fatal("plugin themes must never be copied into the user library")
	}
	// The pack stays inside the plugin root.
	if _, err := os.Stat(filepath.Join(pluginRoot, "themes", "neon.reasonix-theme")); err != nil {
		t.Fatal(err)
	}
}

func TestPluginThemeInvalidSkippedWithWarning(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	installPluginThemeFixture(t, home, "themery", true, []pluginThemeFixture{
		{fileName: "neon.reasonix-theme", manifest: testPluginThemeManifest("neon-dusk", "Neon Dusk")},
		{fileName: "broken.reasonix-theme", manifest: nil}, // invalid ZIP
	})
	app := NewApp()

	list, err := app.ListThemePacks()
	if err != nil {
		t.Fatal(err)
	}
	if findThemePackView(list, "plugin:themery:broken") != nil {
		t.Fatal("invalid plugin theme must be skipped")
	}
	view := findThemePackView(list, "plugin:themery:neon-dusk")
	if view == nil {
		t.Fatal("valid plugin theme missing")
	}
	if len(view.Warnings) == 0 || !strings.Contains(view.Warnings[0], "broken.reasonix-theme") {
		t.Fatalf("skipped file must surface a warning on the plugin's views: %+v", view.Warnings)
	}

	exp, err := app.GetThemeExperience()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range exp.Warnings {
		if strings.Contains(w, "broken.reasonix-theme") {
			found = true
		}
	}
	if !found {
		t.Fatalf("experience warnings must aggregate the skipped file: %v", exp.Warnings)
	}
}

func TestPluginThemeDisabledNotListed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	installPluginThemeFixture(t, home, "themery", false, []pluginThemeFixture{
		{fileName: "neon.reasonix-theme", manifest: testPluginThemeManifest("neon-dusk", "Neon Dusk")},
	})
	app := NewApp()

	list, err := app.ListThemePacks()
	if err != nil {
		t.Fatal(err)
	}
	if findThemePackView(list, "plugin:themery:neon-dusk") != nil {
		t.Fatal("disabled plugin themes must not be listed")
	}
	if err := app.ActivateThemePack("plugin:themery:neon-dusk"); err == nil {
		t.Fatal("activating a disabled plugin's theme must fail")
	}
}

func TestPluginThemeActivatePersistFallbackRestore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	themes := []pluginThemeFixture{
		{fileName: "neon.reasonix-theme", manifest: testPluginThemeManifest("neon-dusk", "Neon Dusk")},
	}
	installPluginThemeFixture(t, home, "themery", true, themes)
	app := NewApp()

	const activeID = "plugin:themery:neon-dusk"
	if err := app.ActivateThemePack(activeID); err != nil {
		t.Fatal(err)
	}
	active, err := app.GetActiveThemePack()
	if err != nil {
		t.Fatal(err)
	}
	if active.ActiveThemeID != activeID || active.Pack == nil || active.Pack.Kind != themeKindPlugin {
		t.Fatalf("active = %+v", active)
	}
	if !strings.Contains(readThemeStateRaw(t), activeID) {
		t.Fatal("full plugin theme id must persist in desktop-theme-state.json")
	}

	// Plugin disappears (uninstalled): rendering falls back to the base style,
	// but the pointer is PRESERVED — the state file is not rewritten.
	if _, ok, err := pluginpkg.Remove(home, "themery"); err != nil || !ok {
		t.Fatalf("remove plugin: ok=%v err=%v", ok, err)
	}
	before := readThemeStateRaw(t)
	app2 := NewApp()
	exp, err := app2.GetThemeExperience()
	if err != nil {
		t.Fatal(err)
	}
	if exp.ActiveThemeID != "" || exp.ActivePack != nil {
		t.Fatalf("missing plugin must fall back to base style: %+v", exp)
	}
	if exp.EffectiveStyle != exp.BaseStyle {
		t.Fatalf("effective style must be the configured base style: %+v", exp)
	}
	if after := readThemeStateRaw(t); after != before {
		t.Fatalf("state file must not be rewritten while the plugin is missing:\n%s\n!=\n%s", before, after)
	}
	active2, err := app2.GetActiveThemePack()
	if err != nil {
		t.Fatal(err)
	}
	if active2.ActiveThemeID != "" || active2.Pack != nil {
		t.Fatalf("missing plugin must render no pack: %+v", active2)
	}
	if after := readThemeStateRaw(t); after != before {
		t.Fatal("GetActiveThemePack must not clear a plugin pointer")
	}

	// Reinstalling the same plugin id restores the theme.
	installPluginThemeFixture(t, home, "themery", true, themes)
	app3 := NewApp()
	active3, err := app3.GetActiveThemePack()
	if err != nil {
		t.Fatal(err)
	}
	if active3.ActiveThemeID != activeID || active3.Pack == nil || active3.Pack.Kind != themeKindPlugin {
		t.Fatalf("reinstall must restore the plugin theme: %+v", active3)
	}

	// Disabled (not uninstalled) behaves the same: fallback + preserve.
	if err := pluginpkg.SetEnabled(home, "themery", false); err != nil {
		t.Fatal(err)
	}
	before = readThemeStateRaw(t)
	exp, err = NewApp().GetThemeExperience()
	if err != nil {
		t.Fatal(err)
	}
	if exp.ActiveThemeID != "" || exp.ActivePack != nil {
		t.Fatalf("disabled plugin must fall back to base style: %+v", exp)
	}
	if after := readThemeStateRaw(t); after != before {
		t.Fatal("disabling the plugin must not rewrite the theme state")
	}
}

func TestPluginThemeReadOnlyGuards(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	pluginRoot := installPluginThemeFixture(t, home, "themery", true, []pluginThemeFixture{
		{fileName: "neon.reasonix-theme", manifest: testPluginThemeManifest("neon-dusk", "Neon Dusk")},
	})
	app := NewApp()

	const id = "plugin:themery:neon-dusk"
	if _, err := app.SaveThemePack(ThemeSaveInput{ID: id, Name: "Hijack", BaseStyle: "graphite"}); err == nil {
		t.Fatal("SaveThemePack must reject plugin theme ids")
	}
	if err := app.DeleteThemePack(id); err == nil {
		t.Fatal("DeleteThemePack must reject plugin theme ids")
	}
	if _, err := app.ExportThemePack(id, filepath.Join(home, "out")); err == nil {
		t.Fatal("ExportThemePack must reject plugin theme ids")
	}
	if _, err := app.CopyThemePack(id, "neon-copy", "Neon Copy"); err == nil {
		t.Fatal("CopyThemePack must reject plugin theme sources")
	}
	// The plugin ZIP is untouched and no user library entry appeared.
	if _, err := os.Stat(filepath.Join(pluginRoot, "themes", "neon.reasonix-theme")); err != nil {
		t.Fatal(err)
	}
	if userThemeExists("neon-dusk") || userThemeExists("neon-copy") {
		t.Fatal("read-only guards must not write into the user theme library")
	}
}

func TestLegacyStateWithPluginIDPreserved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	// State written by a build that already knew plugin themes; no plugin is
	// installed here. New code must read it, fall back, and preserve it.
	if err := os.MkdirAll(filepath.Dir(themeStatePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"schemaVersion":2,"activeThemeId":"plugin:ghost:pastel"}`)
	if err := os.WriteFile(themeStatePath(), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	st := loadThemeDesktopState()
	if st.ActiveThemeID != "plugin:ghost:pastel" {
		t.Fatalf("legacy plugin pointer must decode: %+v", st)
	}
	app := NewApp()
	exp, err := app.GetThemeExperience()
	if err != nil {
		t.Fatal(err)
	}
	if exp.ActiveThemeID != "" || exp.ActivePack != nil {
		t.Fatalf("unknown plugin pointer must fall back: %+v", exp)
	}
	after, err := os.ReadFile(themeStatePath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, raw) {
		t.Fatalf("legacy plugin pointer must be preserved byte-for-byte:\n%s\n!=\n%s", raw, after)
	}
}

func TestUnknownNonPluginActiveIDStillCleared(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	// The pre-plugin erase behavior is unchanged for official/user ids that no
	// longer resolve: clear the pointer and save.
	if err := os.MkdirAll(filepath.Dir(themeStatePath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(themeStatePath(), []byte(`{"schemaVersion":2,"activeThemeId":"ghost-theme"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	app := NewApp()
	exp, err := app.GetThemeExperience()
	if err != nil {
		t.Fatal(err)
	}
	if exp.ActiveThemeID != "" {
		t.Fatalf("unknown non-plugin id must clear: %+v", exp)
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
		t.Fatalf("unknown non-plugin id must be erased from disk: %s", raw)
	}
}

func TestPluginThemeAssetRoute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	installPluginThemeFixture(t, home, "themery", true, []pluginThemeFixture{
		{fileName: "neon.reasonix-theme", manifest: testPluginThemeManifest("neon-dusk", "Neon Dusk"), withImage: true},
	})
	app := NewApp()

	list, err := app.ListThemePacks()
	if err != nil {
		t.Fatal(err)
	}
	view := findThemePackView(list, "plugin:themery:neon-dusk")
	if view == nil || view.BackgroundURL == "" {
		t.Fatalf("plugin theme with background missing: %+v", view)
	}

	mw := app.themeAssetMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	// GET background served straight out of the plugin ZIP.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, view.BackgroundURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET bg status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("bg content-type %q", ct)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("bg body empty")
	}

	// Wrong digest → 404.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, themeAssetURLPrefix+"plugin:themery:neon-dusk/deadbeefdeadbeef/background.png", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("wrong digest status %d", rec.Code)
	}

	// Undeclared filename → 404.
	pt := findPluginTheme("themery", "neon-dusk")
	if pt == nil {
		t.Fatal("plugin theme lookup failed")
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, themeAssetURLPrefix+"plugin:themery:neon-dusk/"+pt.digests["background.png"]+"/theme.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("undeclared file status %d", rec.Code)
	}

	// Plugin disabled → the asset stops resolving (read-only live view).
	if err := pluginpkg.SetEnabled(home, "themery", false); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, view.BackgroundURL, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("disabled plugin asset status %d", rec.Code)
	}
}

func TestActivateThemePackRejectsMalformedPluginID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()
	if err := app.ActivateThemePack("plugin:themery"); err == nil {
		t.Fatal("malformed plugin theme id must be rejected")
	}
	if err := app.ActivateThemePack("plugin:themery:Neon"); err == nil {
		t.Fatal("plugin theme id must keep themePackIDRe on the inner id")
	}
}
