package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestProviderPresetsAreTheKernelsCuratedList(t *testing.T) {
	s := newProviderEditServer(t)
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	got := readJSON[[]providerPresetView](t, srv.URL, "/providers/presets")
	want := config.CuratedProviderPresets()
	if len(got) != len(want) || len(got) == 0 {
		t.Fatalf("served %d presets, kernel curates %d", len(got), len(want))
	}
	for i, p := range got {
		entry := want[i].Entries[0]
		if p.ID != want[i].ID || p.Kind != entry.Kind || p.BaseURL != entry.BaseURL || p.Label == "" || p.Vendor != want[i].Vendor || p.Vendor == "" {
			t.Errorf("preset %d = %+v, want id %q kind %q url %q", i, p, want[i].ID, entry.Kind, entry.BaseURL)
		}
	}
}

func TestInstallingAPresetCreatesTheWholeDeclaredEntry(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	installed := 0
	for _, preset := range config.CuratedProviderPresets() {
		if len(preset.Entries) != 1 {
			continue
		}
		resp := postProvider(t, srv.URL, "/providers/presets/install", `{"presetId":"`+preset.ID+`","apiKey":"sk-fixture"}`)
		body, _ := readAllString(resp)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("install %s = %d: %s", preset.ID, resp.StatusCode, body)
		}
		got, ok := config.LoadForEdit(config.UserConfigPath()).Provider(preset.Entries[0].Name)
		if !ok {
			t.Fatalf("%s: not saved", preset.ID)
		}
		want := preset.Entries[0]
		if got.PresetID != preset.ID || got.PresetVersion != config.ProviderPresetVersion {
			t.Errorf("%s: PresetID %q version %d", preset.ID, got.PresetID, got.PresetVersion)
		}
		if !reflect.DeepEqual(got.Models, want.Models) || !reflect.DeepEqual(got.VisionModels, want.VisionModels) ||
			got.ContextWindow != want.ContextWindow || got.Default != want.Default || got.Kind != want.Kind || got.BaseURL != want.BaseURL ||
			got.Thinking != want.Thinking || !reflect.DeepEqual(got.SupportedEfforts, want.SupportedEfforts) || got.AuthHeader != want.AuthHeader {
			t.Errorf("%s: saved entry differs from the declared shape:\n got %+v\nwant %+v", preset.ID, got, want)
		}
		if strings.TrimSpace(got.APIKeyEnv) == "" {
			t.Errorf("%s: no key slot", preset.ID)
		}
		installed++
	}
	if installed != len(config.CuratedProviderPresets()) {
		t.Fatalf("exercised %d of %d presets", installed, len(config.CuratedProviderPresets()))
	}
}

func TestInstallingAPresetRefusesWithTypedCodes(t *testing.T) {
	s := newProviderEditServer(t)
	s.AllowProviderEdit()
	srv := httptest.NewServer(operatorHandler(s))
	defer srv.Close()
	for body, want := range map[string]string{
		`{"presetId":"nope"}`:                      "provider.preset_unknown",
		`{"presetId":"kimi-cn","name":"a b"}`:      "provider.name_invalid",
		`{"presetId":"kimi-cn","name":"existing"}`: "provider.name_taken",
	} {
		resp := postProvider(t, srv.URL, "/providers/presets/install", body)
		var reason Reason
		_ = json.NewDecoder(resp.Body).Decode(&reason)
		resp.Body.Close()
		if reason.Code != want {
			t.Errorf("%s -> %d %q, want %q", body, resp.StatusCode, reason.Code, want)
		}
	}
	resp := postProvider(t, srv.URL, "/providers/presets/install", `{"presetId":"kimi-cn"}`)
	resp.Body.Close()
	resp = postProvider(t, srv.URL, "/providers/presets/install", `{"presetId":"kimi-cn"}`)
	resp.Body.Close()
	names := map[string]bool{}
	for _, p := range config.LoadForEdit(config.UserConfigPath()).Providers {
		names[p.Name] = true
	}
	if !names["kimi-cn"] || !names["kimi-cn-2"] {
		t.Fatalf("installing twice without a name must keep both under unique names: %v", names)
	}
}
