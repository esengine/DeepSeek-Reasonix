package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/state/agentpreset"
)

func TestAgentPresetRoutesKeepRevisionsAndRejectStaleWrites(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	catalog := readJSON[agentpreset.Catalog](t, srv.URL, "/agent-presets")
	if len(catalog.Presets) != 2 || catalog.Presets[0].Definition.Name != "标准模式" || catalog.Presets[1].Definition.Name != "精简模式" {
		t.Fatalf("templates: %+v", catalog.Presets)
	}
	d := agentcomposition.Builtins()[0]
	d.ID, d.Name = "user:custom", "Custom"
	body, _ := json.Marshal(map[string]any{"definition": d, "expectedRevision": ""})
	resp := postProvider(t, srv.URL, "/agent-presets", string(body))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save %d", resp.StatusCode)
	}
	var v agentcomposition.Version
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	stale := postProvider(t, srv.URL, "/agent-presets", string(body))
	defer stale.Body.Close()
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("stale save: %d", stale.StatusCode)
	}
	if v.Revision == "" {
		t.Fatal("version missing")
	}
}
