package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// One host reached under both protocols, holding a mixed vision/text model set,
// a per-model price and a context window — every capability the picker reads.
const mixedVendorConfig = `default_model = "mixed/text-only"

[[providers]]
name = "mixed"
kind = "openai"
base_url = "https://gateway.invalid/v1"
models = ["text-only", "vision-pro"]
default = "text-only"
vision_models = ["vision-pro"]
context_window = 131072
api_key_env = "MIXED_API_KEY"

[providers.prices.vision-pro]
input = 2.0
output = 8.0
currency = "CNY"

[[providers]]
name = "mixed-anthropic"
kind = "anthropic"
base_url = "https://gateway.invalid/anthropic"
models = ["text-only"]
default = "text-only"
api_key_env = "MIXED_API_KEY"
`

func modelsByRef(t *testing.T, cfgBody string) map[string]modelEntry {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	if _, err := config.SetCredential("MIXED_API_KEY", "sk-test"); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{
		Sink: bc, Label: "text-only", ModelRef: "mixed/text-only", SessionDir: testenv.TempDir(t),
	})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/models")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /models = %d", resp.StatusCode)
	}
	var body struct {
		Models []modelEntry `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	out := make(map[string]modelEntry, len(body.Models))
	for _, m := range body.Models {
		out[m.Ref] = m
	}
	return out
}

func TestModelsReportVisionPerModelNotPerProvider(t *testing.T) {
	got := modelsByRef(t, mixedVendorConfig)

	if e, ok := got["mixed/text-only"]; !ok || e.Vision {
		t.Fatalf("mixed/text-only vision = %v (present %v), want text-only", e.Vision, ok)
	}
	if e, ok := got["mixed/vision-pro"]; !ok || !e.Vision {
		t.Fatalf("mixed/vision-pro vision = %v (present %v), want image input", e.Vision, ok)
	}
}

// The old picker offered six effort levels to every model. A model whose
// endpoint exposes no effort vocabulary has to say so by staying empty, or the
// list is back to advertising levels the provider will reject.
func TestModelsOfferOnlyTheEffortLevelsTheEndpointHas(t *testing.T) {
	got := modelsByRef(t, mixedVendorConfig)

	plain, ok := got["mixed/text-only"]
	if !ok {
		t.Fatal("mixed/text-only missing from the catalog")
	}
	if len(plain.Efforts) != 0 {
		t.Fatalf("a plain OpenAI-compatible endpoint offered efforts %v", plain.Efforts)
	}

	anth, ok := got["mixed-anthropic/text-only"]
	if !ok {
		t.Fatal("mixed-anthropic/text-only missing from the catalog")
	}
	if len(anth.Efforts) == 0 || anth.Efforts[0] != "auto" {
		t.Fatalf("anthropic efforts = %v, want a list starting at auto", anth.Efforts)
	}
}

// Two config entries on one host are one service with two routes. Without a
// shared vendor the picker lists the same model twice with nothing to choose
// between the rows.
func TestModelsShareAVendorAcrossProtocolRoutes(t *testing.T) {
	got := modelsByRef(t, mixedVendorConfig)

	openai := got["mixed/text-only"]
	anth := got["mixed-anthropic/text-only"]
	if openai.Vendor != "gateway.invalid" || anth.Vendor != openai.Vendor {
		t.Fatalf("vendors = %q and %q, want both gateway.invalid", openai.Vendor, anth.Vendor)
	}
	if openai.Kind == anth.Kind {
		t.Fatalf("both routes reported kind %q; the fold would have nothing to offer", openai.Kind)
	}
}

func TestModelsCarryPriceAndWindowWhereDeclared(t *testing.T) {
	got := modelsByRef(t, mixedVendorConfig)

	priced, ok := got["mixed/vision-pro"]
	if !ok {
		t.Fatal("mixed/vision-pro missing from the catalog")
	}
	if priced.Price == nil {
		t.Fatal("the per-model price did not reach the catalog")
	}
	if priced.Price.Input != 2 || priced.Price.Output != 8 || priced.Price.Currency != "CNY" {
		t.Fatalf("price = %+v", priced.Price)
	}
	if priced.ContextWindow != 131072 {
		t.Fatalf("context window = %d, want the provider-wide 131072", priced.ContextWindow)
	}

	// Nothing declares a price for this one, and a picker must not invent one.
	if e := got["mixed/text-only"]; e.Price != nil {
		t.Fatalf("an undeclared price surfaced as %+v", e.Price)
	}
}

// The switch rebuilds the running controller; nothing about that reaches disk.
// Without persistence the next launch boots from default_model and lands back
// on the previous choice, which reads as the picker not having worked.
func TestSwitchingModelSurvivesARestart(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	if _, err := config.SetCredential("MIXED_API_KEY", "sk-test"); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(mixedVendorConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	persistDefaultModel("mixed-anthropic/text-only", nil)

	// A fresh load is what a restart sees.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "mixed-anthropic/text-only" {
		t.Fatalf("default_model = %q after switching, want the model that was chosen", cfg.DefaultModel)
	}
}

// A ref nothing resolves must not overwrite a working default.
func TestPersistingAnUnknownModelLeavesTheDefaultAlone(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("REASONIX_CREDENTIALS_STORE", "file")
	if _, err := config.SetCredential("MIXED_API_KEY", "sk-test"); err != nil {
		t.Fatal(err)
	}
	path := config.UserConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(mixedVendorConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	persistDefaultModel("nobody/nothing", nil)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultModel != "mixed/text-only" {
		t.Fatalf("default_model = %q, want the original left intact", cfg.DefaultModel)
	}
}

func TestModelsReportGlmForcedThinking(t *testing.T) {
	got := modelsByRef(t, `default_model = "glm/glm-5.3"

[[providers]]
name = "glm"
kind = "openai"
base_url = "https://api.z.ai/api/coding/paas/v4"
models = ["glm-5.3", "glm-5.3-flash", "glm-5.2"]
api_key_env = "MIXED_API_KEY"
`)
	for _, model := range []string{"glm-5.3", "glm-5.3-flash", "glm-5.2"} {
		row, ok := got["glm/"+model]
		if !ok {
			t.Fatalf("%s missing from /models", model)
		}
		forced := model != "glm-5.2"
		if row.ForcesThinking != forced {
			t.Errorf("%s forcesThinking = %v, want %v", model, row.ForcesThinking, forced)
		}
		if forced && !slices.Equal(row.Efforts, []string{"auto", "low", "high", "max"}) {
			t.Errorf("%s efforts = %v", model, row.Efforts)
		}
	}
}
