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
}
