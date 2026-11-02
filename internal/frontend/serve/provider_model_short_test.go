package serve

import (
	"net/http"
	"testing"

	"reasonix/internal/contract/config"
)

func TestHidingTheSharedModelPrefixIsDisplayOnlyAndReachesEveryListing(t *testing.T) {
	srv := newRichProviderServer(t)
	resp := postProvider(t, srv.URL, "/providers/edit", `{"name":"rich","models":["grp/alpha","grp/beta"],"default":"grp/alpha","vision":[],"hideModelPrefix":true}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("edit status %d", resp.StatusCode)
	}
	var models struct {
		Models []struct {
			Ref   string `json:"ref"`
			Model string `json:"model"`
			Short string `json:"modelShort"`
		} `json:"models"`
	}
	getJSON(t, srv.URL+"/models", &models)
	if len(models.Models) != 2 {
		t.Fatalf("models = %+v", models.Models)
	}
	for _, m := range models.Models {
		want := map[string]string{"grp/alpha": "alpha", "grp/beta": "beta"}[m.Model]
		if m.Short != want || m.Ref != "rich/"+m.Model {
			t.Fatalf("model %+v: short want %q and the ref untouched", m, want)
		}
	}
	var providers []struct {
		Name   string            `json:"name"`
		Hide   bool              `json:"hideModelPrefix"`
		Shorts map[string]string `json:"modelShorts"`
	}
	getJSON(t, srv.URL+"/providers", &providers)
	if len(providers) != 1 || !providers[0].Hide || providers[0].Shorts["grp/alpha"] != "alpha" {
		t.Fatalf("providers = %+v", providers)
	}
	entry, _ := config.Load()
	if p, ok := entry.Provider("rich"); !ok || !p.HideModelPrefix || p.Models[0] != "grp/alpha" {
		t.Fatalf("stored entry = %+v", p)
	}
	off := postProvider(t, srv.URL, "/providers/edit", `{"name":"rich","models":["grp/alpha","grp/beta"],"default":"grp/alpha","vision":[],"hideModelPrefix":false}`)
	off.Body.Close()
	var after []struct {
		Hide   bool              `json:"hideModelPrefix"`
		Shorts map[string]string `json:"modelShorts"`
	}
	getJSON(t, srv.URL+"/providers", &after)
	if len(after) != 1 || after[0].Hide || len(after[0].Shorts) != 0 {
		t.Fatalf("switched off, yet %+v", after)
	}
}
