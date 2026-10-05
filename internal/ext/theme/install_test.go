package theme

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Loose files are how a folder arrives from a page that never learns its
// path: the manifest and the images are kept, the rest is named back.
func TestInstallKeepsThePackFilesAndNamesTheRest(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	got, err := Install(map[string][]byte{
		"theme.json":     []byte(minimal),
		"Background.PNG": []byte("png"),
		"notes.txt":      []byte("hi"),
	}, "ignored-hint")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pack.ID != "x" || got.Pack.Name != "Dusk" {
		t.Fatalf("pack = %+v, want the manifest's id", got.Pack)
	}
	if got.Pack.Background == nil || !got.Pack.Background.Image {
		t.Fatalf("background = %+v, want the uploaded image found", got.Pack.Background)
	}
	if !slices.Equal(got.Ignored, []string{"notes.txt"}) {
		t.Fatalf("ignored = %v", got.Ignored)
	}
	if _, err := os.Stat(filepath.Join(Dir(), "x", "notes.txt")); !os.IsNotExist(err) {
		t.Fatalf("an unlisted file reached the pack directory: %v", err)
	}
}

// A zip usually wraps its pack in one folder; that folder's name is the id
// when the manifest declares none.
func TestInstallArchiveReadsTheManifestFolder(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	noID := `{"schemaVersion":1,"name":"Dusk","tokens":{"light":{"bg":"#FFFFFF"},"dark":{"bg":"#000000"}}}`
	raw := zipOf(t, map[string]string{
		"dusk/theme.json":          noID,
		"dusk/preview.webp":        "webp",
		"__MACOSX/dusk/theme.json": "junk",
		"elsewhere/background.png": "not this folder",
	})
	got, err := InstallArchive(raw, "download.zip")
	if err != nil {
		t.Fatal(err)
	}
	if got.Pack.ID != "dusk" || !got.Pack.HasPreview {
		t.Fatalf("pack = %+v, want id dusk with its preview", got.Pack)
	}
	if got.Pack.Background != nil {
		t.Fatalf("a file outside the manifest's folder was installed: %+v", got.Pack.Background)
	}
}

// Importing a pack again is how an author ships a new version of it.
func TestInstallReplacesAnInstalledPack(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	writePack(t, Dir(), "x", minimal)
	if err := os.WriteFile(filepath.Join(Dir(), "x", "background.webp"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(map[string][]byte{"theme.json": []byte(minimal)}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Dir(), "x", "background.webp")); !os.IsNotExist(err) {
		t.Fatalf("the replaced pack kept a file the new one does not ship: %v", err)
	}
	entries, _ := os.ReadDir(Dir())
	if len(entries) != 1 {
		t.Fatalf("staging left entries behind: %v", entries)
	}
}

// Every refusal is one the user can act on, and none of them writes.
func TestInstallRefusesWhatIsNotAPack(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	cases := map[string]map[string][]byte{
		"no manifest":  {"background.png": []byte("png")},
		"bad schema":   {"theme.json": []byte(`{"schemaVersion":9,"id":"x","tokens":{}}`)},
		"dot id":       {"theme.json": []byte(`{"schemaVersion":1,"id":"..","tokens":{"light":{"bg":"#FFF"},"dark":{"bg":"#000"}}}`)},
		"plugin id":    {"theme.json": []byte(`{"schemaVersion":1,"id":"plugin:a:b","tokens":{"light":{"bg":"#FFF"},"dark":{"bg":"#000"}}}`)},
		"only dark":    {"theme.json": []byte(`{"schemaVersion":1,"id":"y","tokens":{"dark":{"bg":"#000"}}}`)},
		"huge picture": {"theme.json": []byte(minimal), "background.png": make([]byte, maxAssetBytes+1)},
	}
	for name, files := range cases {
		if _, err := Install(files, ""); !errors.Is(err, ErrNotAPack) {
			t.Errorf("%s: err = %v, want ErrNotAPack", name, err)
		}
	}
	if entries, _ := os.ReadDir(Dir()); len(entries) != 0 {
		t.Fatalf("a refused import wrote %v", entries)
	}
}

func TestInstallArchiveWantsExactlyOneManifest(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	two := zipOf(t, map[string]string{"a/theme.json": minimal, "b/theme.json": minimal})
	if _, err := InstallArchive(two, ""); !errors.Is(err, ErrNotAPack) {
		t.Fatalf("err = %v, want ErrNotAPack", err)
	}
	if _, err := InstallArchive([]byte("not a zip"), ""); !errors.Is(err, ErrNotAPack) {
		t.Fatalf("err = %v, want ErrNotAPack", err)
	}
}
