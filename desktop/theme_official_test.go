package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The release gate: all eight embedded official themes must parse through the
// Theme Pack V1 validator with unique ids/names, valid images and budgets.
func TestOfficialThemesAllValid(t *testing.T) {
	resetOfficialRegistryForTest()
	if err := validateOfficialThemes(); err != nil {
		t.Fatalf("official registry invalid: %v", err)
	}
	themes := officialThemes()
	if len(themes) != officialExpectedCount {
		t.Fatalf("expected %d official themes, got %d", officialExpectedCount, len(themes))
	}
	ids := map[string]bool{}
	names := map[string]bool{}
	var total int64
	for _, ot := range themes {
		m := ot.manifest
		if ids[m.ID] {
			t.Fatalf("duplicate official id %q", m.ID)
		}
		ids[m.ID] = true
		if names[m.Name] {
			t.Fatalf("duplicate official name %q", m.Name)
		}
		names[m.Name] = true
		if !strings.HasPrefix(m.ID, "official-") {
			t.Fatalf("official id %q must carry the official- prefix", m.ID)
		}
		if isBuiltinThemeID(m.ID) {
			t.Fatalf("official id %q collides with a base style", m.ID)
		}
		if m.Background == nil || m.Background.Image != "background.webp" {
			t.Fatalf("%s: background must be background.webp", m.ID)
		}
		if m.Background.PaneOpacity == nil || *m.Background.PaneOpacity != 0.5 {
			t.Fatalf("%s: paneOpacity must be 0.5, got %v", m.ID, m.Background.PaneOpacity)
		}
		if m.Background.SafeArea != "left" {
			t.Fatalf("%s: safeArea must be left, got %q", m.ID, m.Background.SafeArea)
		}
		if m.Recipes.Density != "comfortable" {
			t.Fatalf("%s: density must be comfortable", m.ID)
		}
		if m.Author != "Reasonix Contributors" || m.License != "MIT" {
			t.Fatalf("%s: author/license = %q/%q", m.ID, m.Author, m.License)
		}
		if ot.bgSize > officialMaxBackground {
			t.Fatalf("%s: background %d bytes exceeds budget", m.ID, ot.bgSize)
		}
		if len(ot.bgDigest) != 16 || len(ot.previewDigest) != 16 {
			t.Fatalf("%s: digests not computed", m.ID)
		}
		total += ot.bgSize
		// Every official manifest must ship full light+dark surface coverage and
		// zero contrast warnings.
		for _, mode := range []string{"light", "dark"} {
			tk := m.Tokens.Light
			if mode == "dark" {
				tk = m.Tokens.Dark
			}
			for _, key := range []string{"bg", "bgSoft", "bgElev", "panel", "sidebar", "chat", "workspace", "workspaceFiles", "border", "borderSoft", "fg", "fgDim", "fgFaint", "accent", "accentFg"} {
				if strings.TrimSpace(tk[key]) == "" {
					t.Fatalf("%s %s: missing token %q", m.ID, mode, key)
				}
			}
			// ok/warn/err stay inherited from the base style.
			for _, key := range []string{"ok", "warn", "err"} {
				if _, set := tk[key]; set {
					t.Fatalf("%s %s: %s must inherit the base style", m.ID, mode, key)
				}
			}
		}
		if warns := computeContrastWarnings(&m); len(warns) != 0 {
			t.Fatalf("%s: contrast warnings: %v", m.ID, warns)
		}
	}
	if total > officialMaxTotalBytes {
		t.Fatalf("official backgrounds total %d bytes exceeds %d", total, officialMaxTotalBytes)
	}
}

func TestOfficialImagesDimensionsAndBudgets(t *testing.T) {
	for _, ot := range officialThemes() {
		bg, err := officialThemesFS.ReadFile(officialThemeDirName + "/" + ot.manifest.ID + "/background.webp")
		if err != nil {
			t.Fatal(err)
		}
		if err := validateOfficialImage(bg, "background.webp", officialBackgroundWidth, officialBackgroundHeight, officialMaxBackground); err != nil {
			t.Fatalf("%s background: %v", ot.manifest.ID, err)
		}
		pv, err := officialThemesFS.ReadFile(officialThemeDirName + "/" + ot.manifest.ID + "/preview.webp")
		if err != nil {
			t.Fatal(err)
		}
		if err := validateOfficialImage(pv, "preview.webp", officialPreviewWidth, officialPreviewHeight, officialMaxPreview); err != nil {
			t.Fatalf("%s preview: %v", ot.manifest.ID, err)
		}
	}
}

// Fail-closed: a broken entry never makes it into the registry.
func TestOfficialFailClosedSkipsInvalid(t *testing.T) {
	if _, err := loadOfficialTheme("does-not-exist"); err == nil {
		t.Fatal("expected error for missing official theme")
	}
	if findOfficialTheme("does-not-exist") != nil {
		t.Fatal("missing theme must not enter the registry")
	}
	if isOfficialThemeID("graphite") {
		t.Fatal("base styles are not official themes")
	}
}

func TestOfficialReservedIDsRefused(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()

	officialID := officialThemes()[0].manifest.ID
	for _, id := range []string{"graphite", "aurora", officialID} {
		if !isReservedThemeID(id) {
			t.Fatalf("%s must be reserved", id)
		}
		if err := app.DeleteThemePack(id); err == nil {
			t.Fatalf("delete %s must fail", id)
		}
		if _, err := app.SaveThemePack(ThemeSaveInput{ID: id, Name: "Hijack", BaseStyle: "graphite"}); err == nil {
			t.Fatalf("save %s must fail", id)
		}
		if _, err := app.CopyThemePack("graphite", id, "Hijack"); err == nil {
			t.Fatalf("copy onto reserved id %s must fail", id)
		}
		if _, err := app.ExportThemePack(id, filepath.Join(t.TempDir(), "out.reasonix-theme")); err == nil {
			t.Fatalf("export %s must fail", id)
		}
		if _, _, err := importThemePackZIPBytesForID(id); err == nil {
			t.Fatalf("import over reserved id %s must fail", id)
		}
	}
	// Overwrite path must refuse reserved ids too.
	if err := publishThemeDir(officialID, t.TempDir(), true); err == nil {
		t.Fatal("publish over official id must fail")
	}
}

// importThemePackZIPBytesForID builds an in-memory package for a reserved id.
func importThemePackZIPBytesForID(id string) (*ThemePackManifest, string, error) {
	raw := fmt.Sprintf(`{"schemaVersion":1,"id":%q,"name":"Hijack","baseStyle":"graphite"}`, id)
	tmp := filepath.Join(os.TempDir(), "hijack-"+id+".reasonix-theme")
	if err := writeThemeZip(tmp, &ThemePackManifest{SchemaVersion: 1, ID: id, Name: "Hijack", BaseStyle: "graphite"}, nil); err != nil {
		return nil, "", err
	}
	defer os.Remove(tmp)
	_ = raw
	return importThemePackZIP(tmp)
}

func TestOfficialListOrderAndKinds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()

	// Two user themes.
	for _, id := range []string{"user-a", "user-b"} {
		m := &ThemePackManifest{SchemaVersion: 1, ID: id, Name: id, BaseStyle: "slate", Recipes: defaultThemePackRecipes()}
		staging, err := writeThemeStaging(m, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(staging)
		if err := publishThemeDir(id, staging, false); err != nil {
			t.Fatal(err)
		}
	}

	list, err := app.ListThemePacks()
	if err != nil {
		t.Fatal(err)
	}
	base, official, user := 0, 0, 0
	seenOfficial := 0
	for i, p := range list {
		switch p.Kind {
		case themeKindBase:
			base++
			if !p.Builtin {
				t.Fatal("base must keep builtin=true")
			}
		case themeKindOfficial:
			official++
			seenOfficial = i
			if !p.Builtin || !p.HasBackground || p.BackgroundURL == "" || p.PreviewURL == "" {
				t.Fatalf("official view incomplete: %+v", p)
			}
			if p.NameKey == "" || p.DescriptionKey == "" {
				t.Fatalf("official i18n keys missing: %+v", p)
			}
		case themeKindUser:
			user++
			if p.Builtin {
				t.Fatal("user theme must not be builtin")
			}
		default:
			t.Fatalf("unknown kind %q", p.Kind)
		}
	}
	if base != 6 || official != officialExpectedCount || user < 2 {
		t.Fatalf("list = %d base + %d official + %d user", base, official, user)
	}
	haveA, haveB := false, false
	for _, p := range list {
		if p.ID == "user-a" {
			haveA = true
		}
		if p.ID == "user-b" {
			haveB = true
		}
	}
	if !haveA || !haveB {
		t.Fatal("expected user-a and user-b in list")
	}
	// Order: base first, then official, then user.
	if list[0].Kind != themeKindBase || list[6].Kind != themeKindOfficial || list[len(list)-1].Kind != themeKindUser {
		t.Fatal("list order must be base, official, user")
	}
	_ = seenOfficial
}

func TestOfficialActivateRestoreReset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()

	id := officialThemes()[1].manifest.ID
	if err := app.ActivateThemePack(id); err != nil {
		t.Fatal(err)
	}
	active, err := app.GetActiveThemePack()
	if err != nil {
		t.Fatal(err)
	}
	if active.ActiveThemeID != id || active.Pack == nil || active.Pack.Kind != themeKindOfficial {
		t.Fatalf("active = %+v", active)
	}
	if active.Pack.BackgroundURL == "" || active.Pack.PreviewURL == "" {
		t.Fatalf("official pack must carry background + preview URLs: %+v", active.Pack)
	}
	// Restart recovery: a fresh App reads the same state file.
	app2 := NewApp()
	active2, err := app2.GetActiveThemePack()
	if err != nil {
		t.Fatal(err)
	}
	if active2.ActiveThemeID != id {
		t.Fatalf("restart did not restore official theme: %+v", active2)
	}
	// Reset returns to the Graphite path.
	if err := app2.ResetThemePack(); err != nil {
		t.Fatal(err)
	}
	active3, _ := app2.GetActiveThemePack()
	if active3.ActiveThemeID != "" || active3.Pack != nil {
		t.Fatalf("reset failed: %+v", active3)
	}
}

func TestOfficialCopyBecomesEditableUserTheme(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()

	src := officialThemes()[2].manifest.ID
	created, err := app.CopyThemePack(src, "my-fortune", "My Fortune")
	if err != nil {
		t.Fatal(err)
	}
	if created.Kind != themeKindUser || created.ID != "my-fortune" {
		t.Fatalf("copy view = %+v", created)
	}
	// Background bytes were embedded -> private copy on disk.
	m, err := loadUserThemeManifest("my-fortune")
	if err != nil {
		t.Fatal(err)
	}
	if m.Background == nil || m.Background.Image == "" {
		t.Fatal("copy must keep the background")
	}
	img, err := resolveThemeImageAbs("my-fortune", m.Background.Image)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateThemeImageFile(img); err != nil {
		t.Fatalf("copied background invalid: %v", err)
	}
	// The copy is an ordinary user theme: editable and exportable.
	if _, err := app.SaveThemePack(ThemeSaveInput{ID: "my-fortune", Name: "Renamed", BaseStyle: m.BaseStyle, Replace: true}); err != nil {
		t.Fatalf("edit copy: %v", err)
	}
	if _, err := app.ExportThemePack("my-fortune", filepath.Join(t.TempDir(), "copy")); err != nil {
		t.Fatalf("export copy: %v", err)
	}
}

func TestOfficialAssetRoute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()

	ot := officialThemes()[0]
	id := ot.manifest.ID
	bgURL := officialAssetURL(id, "background.webp")
	pvURL := officialAssetURL(id, officialPreviewName)
	if bgURL == "" || pvURL == "" {
		t.Fatal("official URLs empty")
	}
	if officialAssetURL(id, "theme.json") != "" || officialAssetURL(id, "../theme.json") != "" {
		t.Fatal("undeclared assets must not resolve")
	}

	mw := app.themeAssetMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	// GET background
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, bgURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET bg status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/webp" {
		t.Fatalf("bg content-type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("official cache-control %q", cc)
	}
	if rec.Body.Len() != int(ot.bgSize) {
		t.Fatalf("bg bytes %d != %d", rec.Body.Len(), ot.bgSize)
	}

	// HEAD preview
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, pvURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD preview status %d", rec.Code)
	}

	// Wrong digest
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, themeAssetURLPrefix+id+"/deadbeefdeadbeef/background.webp", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("wrong digest status %d", rec.Code)
	}

	// Undeclared filename
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, themeAssetURLPrefix+id+"/"+ot.bgDigest+"/theme.json", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("undeclared file status %d", rec.Code)
	}

	// Path traversal stays rejected
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, themeAssetURLPrefix+id+"/x/../background.webp", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("traversal status %d", rec.Code)
	}

	// Wrong method
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, bgURL, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status %d", rec.Code)
	}
}

// Safe mode never lists or serves official assets; Graphite path only.
func TestOfficialSafeMode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	t.Setenv("REASONIX_SAFE_MODE", "1")
	app := NewApp()

	list, err := app.ListThemePacks()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 6 {
		t.Fatalf("safe mode must list only base styles, got %d", len(list))
	}
	if err := app.ActivateThemePack(officialThemes()[0].manifest.ID); err == nil {
		t.Fatal("safe mode must refuse official activation")
	}
	mw := app.themeAssetMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) }))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, officialAssetURL(officialThemes()[0].manifest.ID, "background.webp"), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("safe mode asset status %d", rec.Code)
	}
}
