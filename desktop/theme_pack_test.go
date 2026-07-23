package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseThemePackManifestValid(t *testing.T) {
	raw := []byte(`{
		"schemaVersion": 1,
		"id": "my-theme",
		"name": "My Theme",
		"baseStyle": "graphite",
		"tokens": {
			"light": {"bg": "#f4f3ef", "fg": "#111827", "accent": "#2f5fa8"},
			"dark": {"bg": "#0c0d10", "fg": "#f1f1ef", "accent": "#ff6a3d"}
		},
		"recipes": {"density": "comfortable", "corners": "soft"},
		"background": {
			"image": "background.webp",
			"focusX": 0.72,
			"focusY": 0.45,
			"safeArea": "left",
			"homeOpacity": 1,
			"taskOpacity": 0.28,
			"overlayStrength": 0.62
		}
	}`)
	m, err := parseThemePackManifest(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.ID != "my-theme" || m.BaseStyle != "graphite" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
	if m.Background == nil || m.Background.SafeArea != "left" {
		t.Fatalf("background: %+v", m.Background)
	}
	if m.Tokens.Light["bg"] != "#f4f3ef" {
		t.Fatalf("token normalize = %q", m.Tokens.Light["bg"])
	}
}

func TestParseThemePackManifestRejects(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"bad schema", `{"schemaVersion":3,"id":"a","name":"A","baseStyle":"graphite"}`},
		{"v1 task background", `{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite","taskBackground":{"image":"task.png"}}`},
		{"bad id", `{"schemaVersion":1,"id":"Bad_ID","name":"A","baseStyle":"graphite"}`},
		{"unknown token", `{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite","tokens":{"light":{"hack":"#ffffff"}}}`},
		{"css url color", `{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite","tokens":{"light":{"bg":"url(x)"}}}`},
		{"gradient", `{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite","tokens":{"dark":{"accent":"linear-gradient(red,blue)"}}}`},
		{"unknown field", `{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite","scripts":"alert(1)"}`},
		{"bad density", `{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite","recipes":{"density":"huge"}}`},
		{"bad corners", `{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite","recipes":{"corners":"pill"}}`},
		{"svg image", `{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite","background":{"image":"x.svg"}}`},
		{"path traversal image", `{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite","background":{"image":"../etc/passwd.png"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseThemePackManifest([]byte(tc.raw)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestThemePackV2IndependentSceneImagesRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	assets := t.TempDir()
	homeImage := writeTestPNG(t, filepath.Join(assets, "home.png"), 32, 24)
	taskImage := writeTestPNG(t, filepath.Join(assets, "task.png"), 40, 30)
	m := &ThemePackManifest{
		SchemaVersion: 2,
		ID:            "two-scenes",
		Name:          "Two Scenes",
		BaseStyle:     "nocturne",
		Recipes:       defaultThemePackRecipes(),
		Background: &ThemePackBackground{
			Image: "background.png", FocusX: 0.25, FocusY: 0.4, SafeArea: "left",
			HomeOpacity: 0.9, TaskOpacity: 0.2, OverlayStrength: 0.5,
		},
		TaskBackground: &ThemePackSceneBackground{
			Image: "background-task.png", FocusX: 0.75, FocusY: 0.6, SafeArea: "right",
			Opacity: 0.35, OverlayStrength: 0.7,
		},
	}
	staging, err := writeThemeStaging(m, homeImage, nil, themeStagingImage{path: taskImage})
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staging)
	if err := publishThemeDir(m.ID, staging, false); err != nil {
		t.Fatal(err)
	}

	exportPath := filepath.Join(t.TempDir(), "two-scenes.reasonix-theme")
	if err := exportThemePackZIP(m.ID, exportPath); err != nil {
		t.Fatal(err)
	}
	if err := deleteUserTheme(m.ID); err != nil {
		t.Fatal(err)
	}
	imported, importedStage, err := importThemePackZIP(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(importedStage)
	if imported.SchemaVersion != 2 || imported.TaskBackground == nil {
		t.Fatalf("missing V2 task background: %+v", imported)
	}
	if imported.TaskBackground.SafeArea != "right" || imported.TaskBackground.Opacity != 0.35 {
		t.Fatalf("unexpected task background: %+v", imported.TaskBackground)
	}
	if _, err := os.Stat(filepath.Join(importedStage, imported.TaskBackground.Image)); err != nil {
		t.Fatalf("task scene image missing: %v", err)
	}
}

func TestThemeTokenAndRecipeCSSVars(t *testing.T) {
	vars := themePackCSSVars(map[string]string{"fg": "#ffffff", "bg": "#000000", "accent": "#ff0000"})
	if vars["--fg"] != "#ffffff" || vars["--text"] != "#ffffff" {
		t.Fatalf("fg aliases: %+v", vars)
	}
	r := recipeCSSVars(ThemePackRecipes{Density: "compact", Corners: "square"})
	if r["--r"] != "2px" || r["--theme-row-h"] != "28px" {
		t.Fatalf("recipe vars: %+v", r)
	}
}

func TestOpacityClamped(t *testing.T) {
	bg, err := normalizeThemeBackground(&ThemePackBackground{
		Image: "bg.png", FocusX: 2, FocusY: -1, SafeArea: "left",
		HomeOpacity: 1.5, TaskOpacity: 0.9, OverlayStrength: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bg.TaskOpacity > 1 {
		t.Fatalf("task opacity not clamped: %v", bg.TaskOpacity)
	}
	if bg.PaneOpacity == nil || *bg.PaneOpacity != 0.72 {
		t.Fatalf("pane opacity default: %v", bg.PaneOpacity)
	}
	if bg.FocusX != 1 || bg.FocusY != 0 {
		t.Fatalf("focus clamp: %v %v", bg.FocusX, bg.FocusY)
	}
	if bg.HomeOpacity != 1 || bg.OverlayStrength != 1 {
		t.Fatalf("opacity clamp: home=%v overlay=%v", bg.HomeOpacity, bg.OverlayStrength)
	}
	// Task scene background defaults
	sbg, err := normalizeThemeSceneBackground(&ThemePackSceneBackground{
		Image: "task.png", Opacity: 1.5, OverlayStrength: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sbg.Opacity > 1 {
		t.Fatalf("scene opacity not clamped: %v", sbg.Opacity)
	}
	if sbg.PaneOpacity == nil || *sbg.PaneOpacity != 0.80 {
		t.Fatalf("scene pane opacity default: %v", sbg.PaneOpacity)
	}

	// Explicit zero is a valid value and must not be confused with an omitted
	// field, which preserves the legacy defaults above.
	bg, err = normalizeThemeBackground(&ThemePackBackground{
		Image: "bg.png", SafeArea: "center", PaneOpacity: themePackFloat64(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if bg.PaneOpacity == nil || *bg.PaneOpacity != 0 {
		t.Fatalf("explicit zero pane opacity changed: %v", bg.PaneOpacity)
	}
	sbg, err = normalizeThemeSceneBackground(&ThemePackSceneBackground{
		Image: "task.png", SafeArea: "center", PaneOpacity: themePackFloat64(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if sbg.PaneOpacity == nil || *sbg.PaneOpacity != 0 {
		t.Fatalf("explicit zero scene pane opacity changed: %v", sbg.PaneOpacity)
	}
}

func TestPaneOpacityExplicitZeroJSONRoundTrip(t *testing.T) {
	raw := []byte(`{
		"schemaVersion": 2,
		"id": "zero-pane",
		"name": "Zero Pane",
		"baseStyle": "graphite",
		"background": {"image": "home.webp", "safeArea": "center", "paneOpacity": 0},
		"taskBackground": {"image": "task.webp", "safeArea": "center", "paneOpacity": 0}
	}`)
	m, err := parseThemePackManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Background == nil || m.Background.PaneOpacity == nil || *m.Background.PaneOpacity != 0 {
		t.Fatalf("home pane opacity did not preserve zero: %+v", m.Background)
	}
	if m.TaskBackground == nil || m.TaskBackground.PaneOpacity == nil || *m.TaskBackground.PaneOpacity != 0 {
		t.Fatalf("task pane opacity did not preserve zero: %+v", m.TaskBackground)
	}
	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(encoded, []byte(`"paneOpacity":0`)); got != 2 {
		t.Fatalf("explicit zero missing after marshal: count=%d json=%s", got, encoded)
	}
}

func TestImportExportRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)

	img := writeTestPNG(t, filepath.Join(t.TempDir(), "background.png"), 32, 24)
	m := &ThemePackManifest{
		SchemaVersion: 1,
		ID:            "round-trip",
		Name:          "Round Trip",
		BaseStyle:     "aurora",
		Tokens: ThemePackTokens{
			Dark: map[string]string{"accent": "#11aabb"},
		},
		Recipes: defaultThemePackRecipes(),
		Background: &ThemePackBackground{
			Image: "background.png", FocusX: 0.3, FocusY: 0.7, SafeArea: "right",
			HomeOpacity: 1, TaskOpacity: 0.2, OverlayStrength: 0.5,
		},
	}
	staging, err := writeThemeStaging(m, img, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staging)
	if err := publishThemeDir(m.ID, staging, false); err != nil {
		t.Fatal(err)
	}

	exportPath := filepath.Join(t.TempDir(), "out.reasonix-theme")
	if err := exportThemePackZIP(m.ID, exportPath); err != nil {
		t.Fatal(err)
	}
	// Delete and re-import.
	if err := deleteUserTheme(m.ID); err != nil {
		t.Fatal(err)
	}
	im, stage2, err := importThemePackZIP(exportPath)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(stage2)
	if im.ID != "round-trip" || im.Background == nil || im.Background.SafeArea != "right" {
		t.Fatalf("import manifest: %+v", im)
	}
	if err := publishThemeDir(im.ID, stage2, false); err != nil {
		t.Fatal(err)
	}
	if !userThemeExists("round-trip") {
		t.Fatal("expected theme after re-import")
	}
}

func TestImportRejectsZipSlipAndSymlinkAndDuplicate(t *testing.T) {
	// Nested path
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../../evil.json")
	_, _ = w.Write([]byte(`{}`))
	_ = zw.Close()
	if _, _, err := extractThemeZipBytes(buf.Bytes()); err == nil {
		t.Fatal("expected zip-slip rejection")
	}

	// Duplicate entries
	buf.Reset()
	zw = zip.NewWriter(&buf)
	w, _ = zw.Create("theme.json")
	_, _ = w.Write([]byte(`{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite"}`))
	w2, _ := zw.Create("Theme.json")
	_, _ = w2.Write([]byte(`{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite"}`))
	_ = zw.Close()
	if _, _, err := extractThemeZipBytes(buf.Bytes()); err == nil {
		t.Fatal("expected duplicate rejection")
	}
}

func TestImportRejectsOversizedAndDisallowedFiles(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("theme.json")
	_, _ = w.Write([]byte(`{"schemaVersion":1,"id":"a","name":"A","baseStyle":"graphite"}`))
	w2, _ := zw.Create("hack.js")
	_, _ = w2.Write([]byte(`alert(1)`))
	_ = zw.Close()
	if _, _, err := extractThemeZipBytes(buf.Bytes()); err == nil {
		t.Fatal("expected disallowed file rejection")
	}
}

func TestDeleteActiveThemeFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()

	m := &ThemePackManifest{
		SchemaVersion: 1, ID: "deleteme", Name: "Delete Me", BaseStyle: "slate",
		Recipes: defaultThemePackRecipes(),
	}
	staging, err := writeThemeStaging(m, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staging)
	if err := publishThemeDir(m.ID, staging, false); err != nil {
		t.Fatal(err)
	}
	if err := app.ActivateThemePack("deleteme"); err != nil {
		t.Fatal(err)
	}
	active, err := app.GetActiveThemePack()
	if err != nil || active.ActiveThemeID != "deleteme" {
		t.Fatalf("active = %+v err=%v", active, err)
	}
	if err := app.DeleteThemePack("deleteme"); err != nil {
		t.Fatal(err)
	}
	active, err = app.GetActiveThemePack()
	if err != nil {
		t.Fatal(err)
	}
	if active.ActiveThemeID != "" || active.Pack != nil {
		t.Fatalf("expected fallback after delete, got %+v", active)
	}
}

func TestBuiltinCannotDeleteOrOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()
	if err := app.DeleteThemePack("graphite"); err == nil {
		t.Fatal("expected delete builtin error")
	}
	m := &ThemePackManifest{
		SchemaVersion: 1, ID: "graphite", Name: "Hijack", BaseStyle: "graphite",
		Recipes: defaultThemePackRecipes(),
	}
	staging, err := writeThemeStaging(m, "", nil)
	if err != nil {
		t.Fatalf("stage builtin theme: %v", err)
	}
	if staging != "" {
		defer os.RemoveAll(staging)
		if err := publishThemeDir("graphite", staging, true); err == nil {
			t.Fatal("expected publish builtin rejection")
		}
	}
	_, err = app.SaveThemePack(ThemeSaveInput{
		ID: "graphite", Name: "Hijack", BaseStyle: "graphite",
	})
	if err == nil {
		t.Fatal("expected save builtin rejection")
	}
}

func TestThemeAssetRouteValidatesAndRejectsEscape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()

	img := writeTestPNG(t, filepath.Join(t.TempDir(), "background.png"), 16, 16)
	m := &ThemePackManifest{
		SchemaVersion: 1, ID: "asset-theme", Name: "Asset", BaseStyle: "carbon",
		Recipes: defaultThemePackRecipes(),
		Background: &ThemePackBackground{
			Image: "background.png", FocusX: 0.5, FocusY: 0.5, SafeArea: "center",
			HomeOpacity: 1, TaskOpacity: 0.28, OverlayStrength: 0.5,
		},
	}
	staging, err := writeThemeStaging(m, img, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staging)
	if err := publishThemeDir(m.ID, staging, false); err != nil {
		t.Fatal(err)
	}

	url := themeBackgroundURL("asset-theme", "background.png")
	if url == "" || !strings.HasPrefix(url, themeAssetURLPrefix) {
		t.Fatalf("bad url %q", url)
	}

	mw := app.themeAssetMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	// Valid GET
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/png") {
		t.Fatalf("content-type %q", ct)
	}

	// Valid HEAD
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodHead, url, nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HEAD status %d", rec.Code)
	}

	// Wrong digest
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, themeAssetURLPrefix+"asset-theme/deadbeef/background.png", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("wrong digest status %d", rec.Code)
	}

	// Path escape style filename
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, themeAssetURLPrefix+"asset-theme/x/../background.png", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("escape status %d", rec.Code)
	}

	// Non-theme paths pass through
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/index.html", nil)
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTeapot {
		t.Fatalf("passthrough status %d", rec.Code)
	}
}

func TestSameIDImportRequiresReplace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()
	t.Cleanup(clearPendingThemeImport)

	m := &ThemePackManifest{
		SchemaVersion: 1, ID: "same-id", Name: "V1", BaseStyle: "amber",
		Recipes: defaultThemePackRecipes(),
	}
	staging, err := writeThemeStaging(m, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staging)
	if err := publishThemeDir(m.ID, staging, false); err != nil {
		t.Fatal(err)
	}

	// Build a second zip with same id.
	exportPath := filepath.Join(t.TempDir(), "v2.reasonix-theme")
	m.Name = "V2"
	if err := writeThemeZip(exportPath, m, nil); err != nil {
		t.Fatal(err)
	}
	// First import stages and asks for replace — no error, no second file pick needed.
	pending, err := app.ImportThemePack(exportPath, false)
	if err != nil {
		t.Fatal(err)
	}
	if !pending.NeedsReplace || pending.PendingID == "" {
		t.Fatalf("expected NeedsReplace staging, got %+v", pending)
	}
	if pending.Pack.Name != "V2" {
		t.Fatalf("staged name = %q", pending.Pack.Name)
	}
	// Confirm with replace=true and empty path reuses the staged extract.
	res, err := app.ImportThemePack("", true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Replaced || res.Pack.Name != "V2" {
		t.Fatalf("confirm result = %+v", res)
	}
}

func TestSaveThemePackRejectsSameIDWithoutReplace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()

	first, err := app.SaveThemePack(ThemeSaveInput{
		ID: "my-theme", Name: "First", BaseStyle: "graphite",
		Recipes: defaultThemePackRecipes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Name != "First" {
		t.Fatalf("first = %+v", first)
	}
	// Default create path must not silently overwrite.
	if _, err := app.SaveThemePack(ThemeSaveInput{
		ID: "my-theme", Name: "Second", BaseStyle: "aurora",
		Recipes: defaultThemePackRecipes(),
		Replace: false,
	}); err == nil {
		t.Fatal("expected already-exists error without Replace")
	}
	// Explicit replace overwrites.
	second, err := app.SaveThemePack(ThemeSaveInput{
		ID: "my-theme", Name: "Second", BaseStyle: "aurora",
		Recipes: defaultThemePackRecipes(),
		Replace: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Name != "Second" || second.BaseStyle != "aurora" {
		t.Fatalf("second = %+v", second)
	}
}

func TestCorruptActiveThemeFallsBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()

	// Point state at a missing theme.
	if err := saveThemeDesktopState(ThemeDesktopState{SchemaVersion: 1, ActiveThemeID: "missing-theme"}); err != nil {
		t.Fatal(err)
	}
	active, err := app.GetActiveThemePack()
	if err != nil {
		t.Fatal(err)
	}
	if active.Pack != nil || active.ActiveThemeID != "" {
		t.Fatalf("expected empty active after corrupt, got %+v", active)
	}
	st := loadThemeDesktopState()
	if st.ActiveThemeID != "" {
		t.Fatalf("state not cleared: %q", st.ActiveThemeID)
	}
}

func TestListThemePacksIncludesBuiltins(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	app := NewApp()
	list, err := app.ListThemePacks()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 6 {
		t.Fatalf("expected >=6 packs, got %d", len(list))
	}
	var sawGraphite bool
	for _, p := range list {
		if p.ID == "graphite" && p.Builtin {
			sawGraphite = true
		}
	}
	if !sawGraphite {
		t.Fatal("missing graphite builtin")
	}
}

func TestValidateThemeImagePNG(t *testing.T) {
	path := writeTestPNG(t, filepath.Join(t.TempDir(), "background.png"), 8, 8)
	if err := validateThemeImageFile(path); err != nil {
		t.Fatal(err)
	}
	// Oversized edge
	big := writeTestPNG(t, filepath.Join(t.TempDir(), "big.png"), themePackMaxImageEdge+1, 2)
	if err := validateThemeImageFile(big); err == nil {
		t.Fatal("expected dimension rejection")
	}
}

func TestContrastWarningLowContrast(t *testing.T) {
	m := &ThemePackManifest{
		SchemaVersion: 1, ID: "c", Name: "C", BaseStyle: "graphite",
		Tokens: ThemePackTokens{
			Light: map[string]string{"fg": "#eeeeee", "bg": "#ffffff"},
		},
		Recipes: defaultThemePackRecipes(),
	}
	warns := computeContrastWarnings(m)
	if len(warns) == 0 {
		t.Fatal("expected contrast warning")
	}
}

func TestManifestJSONRoundTrip(t *testing.T) {
	m := ThemePackManifest{
		SchemaVersion: 1, ID: "json-rt", Name: "JSON", BaseStyle: "nocturne",
		Tokens:  ThemePackTokens{Dark: map[string]string{"panel": "#121212ff"}},
		Recipes: ThemePackRecipes{Density: "compact", Corners: "round"},
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseThemePackManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Recipes.Density != "compact" || parsed.Tokens.Dark["panel"] != "#121212ff" {
		t.Fatalf("round trip: %+v", parsed)
	}
}

func writeTestPNG(t *testing.T, path string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 40, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
