package boot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/pricing"
)

// millionTokenCapture records each request and answers with a turn that spent a
// million prompt and a million completion tokens, so the quote is the rate card.
type millionTokenCapture struct {
	mu   sync.Mutex
	seen []map[string]any
}

func (c *millionTokenCapture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req map[string]any
	_ = json.Unmarshal(body, &req)
	c.mu.Lock()
	c.seen = append(c.seen, req)
	c.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprint(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1000000,\"completion_tokens\":1000000,\"total_tokens\":2000000}}\n\n")
	fmt.Fprint(w, "data: [DONE]\n\n")
}

type mimoShape struct {
	models, vision []string
	def            string
	window         int
}

// mimoPrices writes the rate card the way an install persists it, one entry per
// model it lists.
func mimoPrices(models []string) string {
	rates := map[string]string{
		"mimo-v2.6-pro":            "cache_hit = 0.025, input = 3, output = 6",
		"mimo-v2.6-flash":          "cache_hit = 0.02, input = 1, output = 2",
		"mimo-v2.6-pro-ultraspeed": "cache_hit = 0.25, input = 30, output = 60",
		"mimo-v2.5-pro":            "cache_hit = 0.025, input = 3, output = 6",
		"mimo-v2.5":                "cache_hit = 0.02, input = 1, output = 2",
	}
	parts := make([]string, len(models))
	for i, m := range models {
		parts[i] = fmt.Sprintf("%q = { %s, currency = \"¥\" }", m, rates[m])
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// mimoTurn builds the real stack for the mimo-api entry as an install holds it
// and returns the request the endpoint received, the usage rate card the turn
// was priced with, and the entry as boot loaded it.
func mimoTurn(t *testing.T, shape mimoShape) (map[string]any, *pricing.CostQuote, *config.ProviderEntry) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("MIMO_API_KEY", "sk-test")
	capture := &millionTokenCapture{}
	srv := httptest.NewServer(capture)
	t.Cleanup(srv.Close)
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "mimo-api"

[codegraph]
enabled = false

[[providers]]
name = "mimo-api"
preset_id = "mimo-api"
kind = "openai"
base_url = "https://api.xiaomimimo.com/v1"
request_url = %q
api_key_env = "MIMO_API_KEY"
models = %s
vision_models = %s
default = %q
context_window = %d
prices = %s
`, srv.URL, tomlList(shape.models), tomlList(shape.vision), shape.def, shape.window, mimoPrices(shape.models)))
	approveWorkspace(t, dir)

	var (
		mu    sync.Mutex
		quote *pricing.CostQuote
	)
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage && e.CostQuote != nil {
			mu.Lock()
			quote = e.CostQuote
			mu.Unlock()
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	entry, ok := cfg.Provider("mimo-api")
	if !ok {
		t.Fatal("mimo-api missing after load")
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if len(capture.seen) == 0 {
		t.Fatal("the turn sent no request")
	}
	mu.Lock()
	defer mu.Unlock()
	return capture.seen[len(capture.seen)-1], quote, entry
}

func TestEffectMimoV26ReachesRequestPricingAndPicker(t *testing.T) {
	preset, _ := config.CuratedProviderPreset("mimo-api")
	e := preset.Entries[0]
	req, price, entry := mimoTurn(t, mimoShape{e.Models, e.VisionModels, e.Default, e.ContextWindow})
	if req["model"] != "mimo-v2.6-pro" {
		t.Fatalf("request model = %v, want mimo-v2.6-pro", req["model"])
	}
	if price == nil || price.Original.Amount != "9" || price.CatalogSource == "" {
		t.Fatalf("turn quoted %+v, want 3+6 for a million tokens each on the V2.6 pro card", price)
	}
	for _, m := range []string{"mimo-v2.6-pro", "mimo-v2.6-flash", "mimo-v2.6-pro-ultraspeed", "mimo-v2.5-pro", "mimo-v2.5"} {
		if !entry.HasModel(m) {
			t.Fatalf("picker catalog %v lacks %s", entry.Models, m)
		}
	}
	if entry.ContextWindow != 1_000_000 {
		t.Fatalf("window = %d, want 1000000", entry.ContextWindow)
	}
	if entry.HasVisionModel("mimo-v2.6-pro") || entry.HasVisionModel("mimo-v2.6-flash") || !entry.HasVisionModel("mimo-v2.5") {
		t.Fatalf("vision_models = %v", entry.VisionModels)
	}
}

func TestEffectShippedMimoV25ShapeMovesToV26(t *testing.T) {
	req, price, entry := mimoTurn(t, mimoShape{[]string{"mimo-v2.5-pro", "mimo-v2.5"}, []string{"mimo-v2.5"}, "mimo-v2.5-pro", 1_048_576})
	if req["model"] != "mimo-v2.6-pro" {
		t.Fatalf("an install holding the shipped V2.5 shape sent %v, want mimo-v2.6-pro", req["model"])
	}
	if price == nil || price.Original.Amount != "9" {
		t.Fatalf("turn quoted %+v, want the V2.6 pro card", price)
	}
	if entry.ContextWindow != 1_000_000 {
		t.Fatalf("upgraded window = %d, want 1000000", entry.ContextWindow)
	}
	if !entry.HasModel("mimo-v2.6-flash") || !entry.HasModel("mimo-v2.5-pro") || entry.HasVisionModel("mimo-v2.6-pro") || entry.HasVisionModel("mimo-v2.5-pro") {
		t.Fatalf("upgraded entry = models %v vision %v", entry.Models, entry.VisionModels)
	}
}

func TestEffectCuratedMimoShapeIsLeftAlone(t *testing.T) {
	req, _, entry := mimoTurn(t, mimoShape{[]string{"mimo-v2.5-pro", "mimo-v2.5"}, []string{"mimo-v2.5"}, "mimo-v2.5-pro", 256_000})
	if req["model"] != "mimo-v2.5-pro" {
		t.Fatalf("a user-narrowed window was treated as ours: sent %v", req["model"])
	}
	if entry.HasModel("mimo-v2.6-pro") || entry.ContextWindow != 256_000 {
		t.Fatalf("curated entry changed: %v window %d", entry.Models, entry.ContextWindow)
	}
}
