package serve

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/theme"
)

func installPack(t *testing.T, id, name string) {
	t.Helper()
	dir := filepath.Join(theme.Dir(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"schemaVersion":1,"name":"` + name + `","tokens":{
	  "light":{"bg":"#FFFFFF","fg":"#111111","accent":"#7A5A16"},
	  "dark":{"bg":"#0B0B0B","fg":"#EEEEEE","accent":"#DDA144"}}}`
	if err := os.WriteFile(filepath.Join(dir, "theme.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func themeServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(operatorHandler(New(&extensionCtl{}, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

type themeRow struct {
	ID         string                       `json:"id"`
	Name       string                       `json:"name"`
	Active     bool                         `json:"active"`
	Tokens     map[string]map[string]string `json:"tokens"`
	Background *theme.Background            `json:"background"`
	HasPreview bool                         `json:"hasPreview"`
}

func listThemes(t *testing.T, base string) []themeRow {
	t.Helper()
	resp, err := http.Get(base + "/themes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /themes = %d", resp.StatusCode)
	}
	var out []themeRow
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// The listing carries each pack's whole token set so a picker can preview on
// hover without a second request.
func TestThemesListCarriesTokensForPreview(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	installPack(t, "dusk", "Dusk")

	got := find(t, listThemes(t, themeServer(t).URL), "dusk")
	if got.Name != "Dusk" {
		t.Fatalf("theme = %+v", got)
	}
	if got.Tokens["dark"]["accent"] != "#DDA144" {
		t.Fatalf("tokens missing from the listing: %+v", got.Tokens)
	}
	if got.Active {
		t.Fatal("a pack is active before anything activated it")
	}
}

func TestActivateThemeMarksItActiveAndPersists(t *testing.T) {
	home := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", home)
	installPack(t, "dusk", "Dusk")
	srv := themeServer(t)

	if resp := postJSON(t, srv.URL+"/themes", map[string]string{"id": "dusk"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("activate = %d, want 204", resp.StatusCode)
	}
	if !find(t, listThemes(t, srv.URL), "dusk").Active {
		t.Fatal("the activated pack is not marked active")
	}
	// The choice has to survive a restart, so it lands in the config file
	// rather than in the server's memory.
	if cfg, err := config.Load(); err != nil || cfg.Desktop.ThemePack != "dusk" {
		t.Fatalf("config theme_pack = %q (err %v)", cfg.Desktop.ThemePack, err)
	}
}

// Turning a pack off is a real choice, not a missing one.
func TestActivateEmptyIDRestoresTheDefaultAppearance(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	installPack(t, "dusk", "Dusk")
	srv := themeServer(t)

	postJSON(t, srv.URL+"/themes", map[string]string{"id": "dusk"})
	if resp := postJSON(t, srv.URL+"/themes", map[string]string{"id": ""}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("deactivate = %d, want 204", resp.StatusCode)
	}
	if find(t, listThemes(t, srv.URL), "dusk").Active {
		t.Fatal("the pack is still active after being turned off")
	}
}

func TestActivateUnknownThemeIsRejectedAndChangesNothing(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	srv := themeServer(t)

	if resp := postJSON(t, srv.URL+"/themes", map[string]string{"id": "nope"}); resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("unknown theme = %d, want 422", resp.StatusCode)
	}
	if cfg, err := config.Load(); err == nil && cfg.Desktop.ThemePack != "" {
		t.Fatalf("a rejected activation still wrote %q", cfg.Desktop.ThemePack)
	}
}

// find picks one pack out of the listing, which also carries the shipped set.
func find(t *testing.T, packs []themeRow, id string) themeRow {
	t.Helper()
	for _, p := range packs {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("pack %q not in the listing (%d packs)", id, len(packs))
	panic("unreachable")
}

// The window draws the background straight from this URL, so it has to answer
// with the image bytes and its own type — not JSON, and not the SPA shell.
func TestThemeAssetServesTheShippedImage(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	srv := themeServer(t)

	resp, err := http.Get(srv.URL + "/themes/official-noir-gold/background")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET background = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/webp" {
		t.Fatalf("content type = %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) < 1000 {
		t.Fatalf("background is %d bytes", len(body))
	}
}

// A pack id is a path segment here, so the traversal has to die at the router
// or in the reader — never as a file read outside the pack directory.
func TestThemeAssetRefusesTraversalAndUnknownKinds(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	srv := themeServer(t)

	for _, path := range []string{
		"/themes/..%2F..%2Fetc/background",
		"/themes/official-noir-gold/manifest",
		"/themes/official-noir-gold/theme.json",
		"/themes/nope/background",
	} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			t.Fatalf("%s answered 200", path)
		}
	}
}

// The listing says whether a pack has a picture and how it wants it placed, so
// a picker can show the two opacities before anything is activated.
func TestThemesListCarriesBackgroundPlacement(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	got := find(t, listThemes(t, themeServer(t).URL), "official-noir-gold")
	if got.Background == nil || !got.Background.Image {
		t.Fatalf("background = %+v, want the shipped image", got.Background)
	}
	if got.Background.TaskOpacity >= got.Background.HomeOpacity {
		t.Fatalf("opacities = %+v, want the image to recede while working", got.Background)
	}
	if !got.HasPreview {
		t.Fatal("shipped pack reports no preview")
	}
}
