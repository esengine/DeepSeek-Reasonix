package serve

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func getJSON(t *testing.T, url string, into any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}

// A rename is a label and nothing else: the file keeps the name every ref,
// the default model and the running conversation point at, and every surface
// that lists the source shows the new label.
func TestRenameProviderChangesOnlyTheLabel(t *testing.T) {
	srv := newRichProviderServer(t)

	resp := postProvider(t, srv.URL, "/providers/display-name", `{"names":["rich"],"displayName":"  工作网关  "}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /providers/display-name = %d", resp.StatusCode)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := cfg.Provider("rich")
	if !ok || p.DisplayName != "工作网关" {
		t.Fatalf("stored entry = %+v, want display name 工作网关 under the name rich", p)
	}
	if cfg.DefaultModel != "rich/alpha" || p.ContextWindow != 131072 || p.PriceForModel("alpha") == nil {
		t.Fatalf("rename disturbed the entry: default=%q window=%d", cfg.DefaultModel, p.ContextWindow)
	}

	var providers []providerView
	getJSON(t, srv.URL+"/providers", &providers)
	if len(providers) != 1 || providers[0].Name != "rich" || providers[0].DisplayName != "工作网关" {
		t.Fatalf("GET /providers = %+v", providers)
	}
	var models struct {
		Current string       `json:"current"`
		Models  []modelEntry `json:"models"`
	}
	getJSON(t, srv.URL+"/models", &models)
	if models.Current != "rich/alpha" {
		t.Fatalf("current model = %q, want rich/alpha", models.Current)
	}
	for _, m := range models.Models {
		if m.Provider == "rich" && (m.DisplayName != "工作网关" || !strings.HasPrefix(m.Ref, "rich/")) {
			t.Fatalf("model entry = %+v, want ref under rich labelled 工作网关", m)
		}
	}
	var status map[string]any
	getJSON(t, srv.URL+"/status", &status)
	if status["modelRef"] != "rich/alpha" || status["providerDisplayName"] != "工作网关" {
		t.Fatalf("status modelRef=%v providerDisplayName=%v", status["modelRef"], status["providerDisplayName"])
	}

	resp = postProvider(t, srv.URL, "/providers/display-name", `{"names":["rich"],"displayName":""}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("clearing = %d", resp.StatusCode)
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "display_name") {
		t.Fatalf("a cleared label was still written:\n%s", raw)
	}
}

func TestRenameProviderRefusals(t *testing.T) {
	srv := newRichProviderServer(t)
	before, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		body   string
		status int
		code   string
	}{
		{`{"names":[],"displayName":"x"}`, http.StatusBadRequest, codeMissingField},
		{`{"names":["rich","ghost"],"displayName":"x"}`, http.StatusNotFound, codeNotFound},
		{`{"names":["rich"],"displayName":"` + strings.Repeat("x", config.MaxProviderDisplayNameRunes+1) + `"}`, http.StatusBadRequest, "provider.display_name_too_long"},
		{`{"names":["rich"],"displayName":"a\tb"}`, http.StatusBadRequest, "provider.display_name_invalid"},
	}
	for _, c := range cases {
		resp := postProvider(t, srv.URL, "/providers/display-name", c.body)
		code := reasonCode(t, resp)
		resp.Body.Close()
		if resp.StatusCode != c.status || code != c.code {
			t.Fatalf("%s = %d %s, want %d %s", c.body, resp.StatusCode, code, c.status, c.code)
		}
	}
	after, _ := os.ReadFile(config.UserConfigPath())
	if string(after) != string(before) {
		t.Fatalf("a refused rename rewrote the config:\n%s", after)
	}
}
