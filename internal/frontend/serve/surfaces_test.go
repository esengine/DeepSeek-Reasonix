package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
)

func surfaceServer(t *testing.T) string {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	srv := httptest.NewServer(operatorHandler(New(&pluginCtl{root: testenv.TempDir(t)}, NewBroadcaster(), config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv.URL
}

func readSlots(t *testing.T, base string) map[string]string {
	t.Helper()
	resp, err := http.Get(base + "/surfaces")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Slots map[string]string `json:"slots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.Slots
}

// The user's placement outlives the window it was made in, which is the whole
// reason it is config rather than something the page remembers.
func TestSurfacePlacementPersists(t *testing.T) {
	base := surfaceServer(t)
	if got := readSlots(t, base); len(got) != 0 {
		t.Fatalf("slots = %v, want none before anything is placed", got)
	}
	if resp := postJSON(t, base+"/surfaces", map[string]any{
		"surface": "opengo:quota", "slot": "composer-trailing",
	}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /surfaces = %d", resp.StatusCode)
	}
	if got := readSlots(t, base)["opengo:quota"]; got != "composer-trailing" {
		t.Fatalf("slot = %q, want the placement to have been recorded", got)
	}
	cfg, err := config.Load()
	if err != nil || cfg.Desktop.SurfaceSlots["opengo:quota"] != "composer-trailing" {
		t.Fatalf("config = %v (err %v)", cfg.Desktop.SurfaceSlots, err)
	}
}

// Clearing is a real choice: it hands the decision back to the extension's own
// suggestion rather than pinning the surface out of sight.
func TestEmptySlotClearsThePlacement(t *testing.T) {
	base := surfaceServer(t)
	postJSON(t, base+"/surfaces", map[string]any{"surface": "opengo:quota", "slot": "composer-trailing"})
	postJSON(t, base+"/surfaces", map[string]any{"surface": "opengo:quota", "slot": ""})
	if got := readSlots(t, base); len(got) != 0 {
		t.Fatalf("slots = %v, want the entry gone", got)
	}
}

// A slot name means something only to the frontend that offers it, so this end
// stores what it is told. What it does refuse is a surface with no name, and an
// unbounded number of placements.
func TestSurfacePlacementValidatesOnlyWhatItCanKnow(t *testing.T) {
	base := surfaceServer(t)
	if resp := postJSON(t, base+"/surfaces", map[string]any{
		"surface": "plugin:s", "slot": "a-place-only-one-frontend-has",
	}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("an unfamiliar slot name was rejected: %d", resp.StatusCode)
	}
	if resp := postJSON(t, base+"/surfaces", map[string]any{"slot": "composer-trailing"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a placement with no surface = %d, want 400", resp.StatusCode)
	}
	for i := range maxSurfaceSlots {
		postJSON(t, base+"/surfaces", map[string]any{"surface": string(rune('a'+i)) + ":s", "slot": "x"})
	}
	if resp := postJSON(t, base+"/surfaces", map[string]any{"surface": "one-too-many:s", "slot": "x"}); resp.StatusCode == http.StatusNoContent {
		t.Error("placements grew without a bound")
	}
}
