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
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	catalog := readJSON[agentpreset.Catalog](t, srv.URL, "/agent-presets")
	if len(catalog.Presets) != 2 || catalog.Presets[0].Definition.Name != "日常搭档" || catalog.Presets[1].Definition.Name != "轻装搭档" {
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

func TestAgentPresetImportOnlyReportsMissingRequiredTools(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	for _, optional := range []bool{false, true} {
		d := agentcomposition.Builtins()[0]
		d.ID = "user:imported"
		d.Tools = agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{{ID: "unavailable-image-reader", Optional: optional}}}
		body, err := json.Marshal(map[string]any{"content": d})
		if err != nil {
			t.Fatal(err)
		}
		resp := postProvider(t, srv.URL, "/agent-presets/import", string(body))
		var preview struct {
			Diagnostics []agentcomposition.Diagnostic `json:"diagnostics"`
		}
		err = json.NewDecoder(resp.Body).Decode(&preview)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("optional=%v: import status %d", optional, resp.StatusCode)
		}
		if optional {
			if len(preview.Diagnostics) != 0 {
				t.Fatalf("optional tool refused: %+v", preview.Diagnostics)
			}
		} else if len(preview.Diagnostics) != 1 || preview.Diagnostics[0].Code != "agent_composition.unavailable" || preview.Diagnostics[0].Module != "tools:unavailable-image-reader" {
			t.Fatalf("required tool diagnostic = %+v", preview.Diagnostics)
		}
	}
}
