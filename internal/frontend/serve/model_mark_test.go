package serve

import (
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
)

func TestModelsCarryTheVendorOfTheirPresetOnly(t *testing.T) {
	srv := newRichProviderServer(t)
	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Models []struct {
			Ref         string `json:"ref"`
			Mark        string `json:"mark"`
			ServiceMark string `json:"serviceMark"`
		} `json:"models"`
	}
	getJSON(t, srv.URL+"/models", &got)
	if len(got.Models) == 0 {
		t.Fatal("no models listed")
	}
	getJSON(t, srv.URL+"/models", &got)
	for _, m := range got.Models {
		if m.Mark != "" {
			t.Fatalf("model %s without a preset carries mark %q", m.Ref, m.Mark)
		}
	}
	withPreset := strings.Replace(string(body), `name = "rich"`, "name = \"rich\"\npreset_id = \"deepseek-anthropic\"", 1)
	if err := os.WriteFile(config.UserConfigPath(), []byte(withPreset), 0o600); err != nil {
		t.Fatal(err)
	}
	getJSON(t, srv.URL+"/models", &got)
	preset, _ := config.CuratedProviderPreset("deepseek-anthropic")
	for _, m := range got.Models {
		if m.Mark != preset.Vendor || m.Mark == "" || m.ServiceMark != m.Mark {
			t.Fatalf("model %s mark = %q, want %q", m.Ref, m.Mark, preset.Vendor)
		}
	}
}

func TestARelayKeepsItsMarkOffTheModelRows(t *testing.T) {
	srv := newRichProviderServer(t)
	body, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	relay := strings.Replace(string(body), `name = "rich"`, "name = \"rich\"\npreset_id = \"openrouter\"", 1)
	if err := os.WriteFile(config.UserConfigPath(), []byte(relay), 0o600); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Models []struct {
			Ref         string `json:"ref"`
			Mark        string `json:"mark"`
			ServiceMark string `json:"serviceMark"`
		} `json:"models"`
	}
	getJSON(t, srv.URL+"/models", &got)
	if len(got.Models) == 0 {
		t.Fatal("no models listed")
	}
	for _, m := range got.Models {
		if m.ServiceMark != "openrouter" || m.Mark != "" {
			t.Fatalf("model %s: serviceMark %q mark %q, want the service's mark on the heading only", m.Ref, m.ServiceMark, m.Mark)
		}
	}
}
