package main

import (
	"bytes"
	"encoding/json"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func testExternalOpener(id, name, kind string) externalOpenerSpec {
	return externalOpenerSpec{View: ExternalOpenerView{ID: id, Name: name, Kind: kind}, Target: id}
}

func TestResolveExternalOpenerPrefersInstalledSelection(t *testing.T) {
	specs := []externalOpenerSpec{
		testExternalOpener("files", "Files", externalOpenerFileManager),
		testExternalOpener("code", "Code", externalOpenerEditor),
	}
	got, ok := resolveExternalOpener(specs, " CODE ")
	if !ok || got.View.ID != "code" {
		t.Fatalf("resolveExternalOpener = (%+v, %v), want installed code", got, ok)
	}
}

func TestResolveExternalOpenerFallsBackAcrossOperatingSystems(t *testing.T) {
	specs := []externalOpenerSpec{
		testExternalOpener("files", "Files", externalOpenerFileManager),
		testExternalOpener("code", "Code", externalOpenerEditor),
	}
	got, ok := resolveExternalOpener(specs, "finder")
	if !ok || got.View.ID != "files" {
		t.Fatalf("resolveExternalOpener unavailable preference = (%+v, %v), want file manager fallback", got, ok)
	}
}

func TestExternalOpenerViewsAreStableAndDeduplicated(t *testing.T) {
	specs := []externalOpenerSpec{
		testExternalOpener("code", "VS Code", externalOpenerEditor),
		testExternalOpener("CODE", "Duplicate", externalOpenerEditor),
		testExternalOpener("", "Invalid", externalOpenerEditor),
	}
	want := []ExternalOpenerView{{ID: "code", Name: "VS Code", Kind: externalOpenerEditor}}
	if got := externalOpenerViews(specs); !reflect.DeepEqual(got, want) {
		t.Fatalf("externalOpenerViews = %+v, want %+v", got, want)
	}
}

func TestExternalOpenerCatalogCachesUntilTTLExpires(t *testing.T) {
	now := time.Unix(100, 0)
	discoveryCalls := 0
	cache := newExternalOpenerCatalogCache(15*time.Second, func() []externalOpenerSpec {
		discoveryCalls++
		return []externalOpenerSpec{testExternalOpener("code", "Code", externalOpenerEditor)}
	})
	cache.now = func() time.Time { return now }

	first := cache.get()
	first[0].View.Name = "mutated"
	if got := cache.get(); discoveryCalls != 1 || got[0].View.Name != "Code" {
		t.Fatalf("fresh cache = (%d calls, %+v), want one isolated discovery result", discoveryCalls, got)
	}

	now = now.Add(15 * time.Second)
	if got := cache.get(); discoveryCalls != 2 || got[0].View.Name != "Code" {
		t.Fatalf("expired cache = (%d calls, %+v), want a refreshed result", discoveryCalls, got)
	}
}

func TestExternalOpenerCatalogCoalescesConcurrentRefreshes(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var callsMu sync.Mutex
	discoveryCalls := 0
	cache := newExternalOpenerCatalogCache(time.Minute, func() []externalOpenerSpec {
		callsMu.Lock()
		discoveryCalls++
		callsMu.Unlock()
		close(started)
		<-release
		return []externalOpenerSpec{testExternalOpener("code", "Code", externalOpenerEditor)}
	})

	const callers = 8
	results := make(chan []externalOpenerSpec, callers)
	for range callers {
		go func() { results <- cache.get() }()
	}
	<-started
	close(release)
	for range callers {
		if got := <-results; len(got) != 1 || got[0].View.ID != "code" {
			t.Fatalf("coalesced cache result = %+v, want code", got)
		}
	}
	callsMu.Lock()
	defer callsMu.Unlock()
	if discoveryCalls != 1 {
		t.Fatalf("concurrent discovery calls = %d, want 1", discoveryCalls)
	}
}

func BenchmarkExternalOpenerCatalogCacheHit(b *testing.B) {
	cache := newExternalOpenerCatalogCache(time.Minute, func() []externalOpenerSpec {
		return []externalOpenerSpec{testExternalOpener("code", "Code", externalOpenerEditor)}
	})
	cache.get()
	b.ResetTimer()
	for b.Loop() {
		cache.get()
	}
}

func TestPlatformExternalOpenersHaveUniqueSafeIds(t *testing.T) {
	specs := platformExternalOpenerSpecs()
	if len(specs) == 0 {
		t.Fatal("platformExternalOpenerSpecs returned no fallback opener")
	}
	views := externalOpenerViews(specs)
	if len(views) != len(specs) {
		t.Fatalf("platform opener ids are invalid or duplicated: specs=%+v views=%+v", specs, views)
	}
	if _, ok := resolveExternalOpener(specs, "definitely-not-installed"); !ok {
		t.Fatal("platform opener list has no usable fallback")
	}
}

func TestSetPreferredExternalOpenerRejectsRendererCommands(t *testing.T) {
	app := NewApp()
	for _, id := range []string{"", "../../bin/sh", "vscode; rm -rf /"} {
		if err := app.SetPreferredExternalOpener(id); err == nil {
			t.Fatalf("SetPreferredExternalOpener(%q) unexpectedly succeeded", id)
		}
	}
}

func TestExternalOpenerWorkspaceCapabilityUsesTheTabDirectoryNotScope(t *testing.T) {
	projectRoot := t.TempDir()
	globalRoot := t.TempDir()
	missingRoot := filepath.Join(t.TempDir(), "missing")
	fileRoot := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(fileRoot, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}

	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"project": {ID: "project", Scope: "project", WorkspaceRoot: projectRoot},
		"global":  {ID: "global", Scope: "global", WorkspaceRoot: globalRoot},
		"missing": {ID: "missing", Scope: "project", WorkspaceRoot: missingRoot},
		"file":    {ID: "file", Scope: "global", WorkspaceRoot: fileRoot},
		"empty":   {ID: "empty", Scope: "global"},
	}

	for _, tabID := range []string{"project", "global"} {
		if _, err := app.externalOpenerWorkspacePathForTab(tabID); err != nil {
			t.Errorf("externalOpenerWorkspacePathForTab(%q) = %v, want available", tabID, err)
		}
	}
	for _, tabID := range []string{"missing", "file", "empty", "unknown"} {
		if _, err := app.externalOpenerWorkspacePathForTab(tabID); err == nil {
			t.Errorf("externalOpenerWorkspacePathForTab(%q) succeeded, want unavailable", tabID)
		}
	}
}

func TestExternalOpenersForGlobalTabReportsWorkspaceCapability(t *testing.T) {
	app := NewApp()
	app.tabs = map[string]*WorkspaceTab{
		"global": {ID: "global", Scope: "global", WorkspaceRoot: t.TempDir()},
	}
	view := app.ExternalOpenersForTab("global")
	if !view.WorkspaceOpenable {
		t.Fatal("ExternalOpenersForTab(global) workspaceOpenable = false, want true")
	}
	if view.Openers == nil {
		t.Fatal("ExternalOpenersForTab(global) openers = nil, want a Wails-safe array")
	}
	unavailable := app.ExternalOpenersForTab("unknown")
	if unavailable.WorkspaceOpenable || unavailable.Openers == nil {
		t.Fatalf("ExternalOpenersForTab(unknown) = %+v, want unavailable with an empty Wails-safe array", unavailable)
	}
}

func TestLocalSaveDestinationIsSourceDetectsFilesystemAliases(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "Readme.md")
	if err := os.WriteFile(source, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(source)
	if err != nil {
		t.Fatal(err)
	}

	hardLink := filepath.Join(dir, "readme-hardlink.md")
	if err := os.Link(source, hardLink); err != nil {
		t.Fatalf("create hard link: %v", err)
	}
	if same, err := localSaveDestinationIsSource(info, hardLink); err != nil || !same {
		t.Fatalf("hard-link alias = (%v, %v), want (true, nil)", same, err)
	}

	symlink := filepath.Join(dir, "readme-symlink.md")
	if err := os.Symlink(source, symlink); err == nil {
		if same, err := localSaveDestinationIsSource(info, symlink); err != nil || !same {
			t.Fatalf("symlink alias = (%v, %v), want (true, nil)", same, err)
		}
	}

	missing := filepath.Join(dir, "new-copy.md")
	if same, err := localSaveDestinationIsSource(info, missing); err != nil || same {
		t.Fatalf("missing destination = (%v, %v), want (false, nil)", same, err)
	}
}

func TestCopyLocalPathAsRejectsAliasWithoutChangingSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.md")
	alias := filepath.Join(dir, "alias.md")
	content := []byte("source content")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, alias); err != nil {
		t.Fatalf("create hard link: %v", err)
	}
	if err := copyLocalPathAs(source, alias); err == nil {
		t.Fatal("copyLocalPathAs(alias) succeeded, want same-source error")
	}
	if got, err := os.ReadFile(source); err != nil || !reflect.DeepEqual(got, content) {
		t.Fatalf("source after rejected alias copy = (%q, %v), want original content", got, err)
	}
}

func TestCopyLocalPathAsReplacesDestinationWithoutChangingSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.md")
	target := filepath.Join(dir, "target.md")
	sourceContent := []byte("source content")
	if err := os.WriteFile(source, sourceContent, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old destination"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyLocalPathAs(source, target); err != nil {
		t.Fatalf("copyLocalPathAs = %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || !reflect.DeepEqual(got, sourceContent) {
		t.Fatalf("destination after copy = (%q, %v), want source content", got, err)
	}
	if got, err := os.ReadFile(source); err != nil || !reflect.DeepEqual(got, sourceContent) {
		t.Fatalf("source after copy = (%q, %v), want unchanged source", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".target.md.reasonix-copy-") {
			t.Fatalf("temporary copy was not cleaned up: %s", entry.Name())
		}
	}
}

func TestExternalOpenerLaunchPathUsesParentForTerminalFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.md")
	if err := os.WriteFile(path, []byte("report"), 0o600); err != nil {
		t.Fatal(err)
	}

	terminal := externalOpenerSpec{View: ExternalOpenerView{Kind: externalOpenerTerminal}, LaunchMode: "application"}
	if got := externalOpenerLaunchPath(terminal, path); got != dir {
		t.Fatalf("terminal launch path = %q, want parent directory %q", got, dir)
	}
	editor := externalOpenerSpec{View: ExternalOpenerView{Kind: externalOpenerEditor}, LaunchMode: "application"}
	if got := externalOpenerLaunchPath(editor, path); got != path {
		t.Fatalf("editor launch path = %q, want file %q", got, path)
	}
}

func TestExternalOpenerIconFileDataURLAcceptsBoundedImages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(path, []byte("png-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := externalOpenerIconFileDataURL(path)
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("externalOpenerIconFileDataURL = %q, want PNG data URL", got)
	}
}

func TestExternalOpenerPNGRestoresAlphaFromBlackAndWhiteComposites(t *testing.T) {
	black := []byte{
		0, 0, 0, 255,
		0, 0, 0, 255,
		0, 0, 128, 255,
	}
	white := []byte{
		0, 0, 0, 255,
		255, 255, 255, 255,
		127, 127, 255, 255,
	}
	encoded := externalOpenerPNGFromBGRAComposites(black, white, 3, 1)
	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	want := []color.NRGBA{
		{R: 0, G: 0, B: 0, A: 255},
		{0, 0, 0, 0},
		{R: 255, G: 0, B: 0, A: 128},
	}
	for x, expected := range want {
		got := color.NRGBAModel.Convert(decoded.At(x, 0)).(color.NRGBA)
		if got != expected {
			t.Fatalf("pixel %d = %#v, want %#v", x, got, expected)
		}
	}
}

func TestExternalOpenerViewIconIsBackwardCompatible(t *testing.T) {
	withoutIcon, err := json.Marshal(ExternalOpenerView{ID: "vscode", Name: "VS Code", Kind: externalOpenerEditor})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(withoutIcon), "iconDataUrl") {
		t.Fatalf("empty optional icon should be omitted: %s", withoutIcon)
	}
	withIcon, err := json.Marshal(ExternalOpenerView{ID: "vscode", Name: "VS Code", Kind: externalOpenerEditor, IconDataURL: "data:image/png;base64,AA=="})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(withIcon), `"iconDataUrl":"data:image/png;base64,AA=="`) {
		t.Fatalf("native icon missing from JSON contract: %s", withIcon)
	}

	withoutCapability, err := json.Marshal(ExternalOpenersView{Openers: []ExternalOpenerView{}, Preferred: "finder"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(withoutCapability), "workspaceOpenable") {
		t.Fatalf("false workspace capability should be omitted for old readers: %s", withoutCapability)
	}
	withCapability, err := json.Marshal(ExternalOpenersView{
		Openers:           []ExternalOpenerView{},
		Preferred:         "finder",
		WorkspaceOpenable: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(withCapability), `"workspaceOpenable":true`) {
		t.Fatalf("workspace capability missing from JSON contract: %s", withCapability)
	}
	var oldReader struct {
		Openers   []ExternalOpenerView `json:"openers"`
		Preferred string               `json:"preferred"`
	}
	if err := json.Unmarshal(withCapability, &oldReader); err != nil || oldReader.Preferred != "finder" || oldReader.Openers == nil {
		t.Fatalf("old reader rejected additive workspace capability: reader=%+v err=%v", oldReader, err)
	}
}
