package serve

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/theme"
)

const importedPack = `{"schemaVersion":1,"id":"dusk","name":"Dusk","tokens":{
  "light":{"bg":"#FFFFFF","fg":"#111111"},"dark":{"bg":"#0B0B0B","fg":"#EEEEEE"}}}`

// An imported pack is listed by the same read the picker makes, so the next
// GET /themes is the proof that it landed.
func TestImportThemeListsThePack(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	srv := themeServer(t)

	resp := postJSON(t, srv.URL+"/themes/import", map[string]any{
		"name":  "dusk",
		"files": map[string]string{"theme.json": base64.StdEncoding.EncodeToString([]byte(importedPack))},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /themes/import = %d", resp.StatusCode)
	}
	var got theme.Installed
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Pack.ID != "dusk" {
		t.Fatalf("installed = %+v", got)
	}
	found := false
	for _, row := range listThemes(t, srv.URL) {
		found = found || row.ID == "dusk"
	}
	if !found {
		t.Fatal("the imported pack is not listed")
	}
}

// What the user can fix is told apart from what the disk refused.
func TestImportThemeRefusesANonPackWithItsCode(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	srv := themeServer(t)

	resp := postJSON(t, srv.URL+"/themes/import", map[string]any{
		"files": map[string]string{"notes.txt": base64.StdEncoding.EncodeToString([]byte("hi"))},
	})
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", resp.StatusCode)
	}
	var reason Reason
	if err := json.NewDecoder(resp.Body).Decode(&reason); err != nil {
		t.Fatal(err)
	}
	if reason.Code != "theme.not_a_pack" {
		t.Fatalf("code = %q", reason.Code)
	}
}

func TestOpenThemeFolderRevealsTheInstallDirectory(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	var shown string
	old := revealFolder
	revealFolder = func(dir string) error { shown = dir; return nil }
	t.Cleanup(func() { revealFolder = old })
	srv := themeServer(t)

	resp := postJSON(t, srv.URL+"/themes/folder", struct{}{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /themes/folder = %d", resp.StatusCode)
	}
	if shown != theme.Dir() {
		t.Fatalf("revealed %q, want %q", shown, theme.Dir())
	}
}
