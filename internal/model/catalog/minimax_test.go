package catalog

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"reasonix/internal/contract/config"
)

func TestMiniMaxDiscoveryFiltersTextAndPreservesSavedAndMedia(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"other-model"},{"id":"MiniMax-M2.7"},{"id":"MiniMax-M3"},{"id":"MiniMax-M3.1-Flash"},{"id":"minimax/minimax-m2.5"},{"id":"minimax-music-2.5"},{"id":"speech-2.8-hd"},{"id":"image-01"}]}`)
	}))
	defer srv.Close()
	for _, saved := range []string{"", "MiniMax-M2.7"} {
		entry := &config.ProviderEntry{Name: "fixture", Kind: "openai", BaseURL: srv.URL, Model: saved}
		entryWithKey := entry.WithAPIKeyForProbe("test-key")
		got, err := FetchModelsVia(context.Background(), &entryWithKey, srv.Client())
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(got, "minimax/minimax-m2.5") || slices.Contains(got, "MiniMax-M2.7") != (saved != "") {
			t.Fatalf("discovery did not distinguish ordinary and saved models: %v", got)
		}
		if slices.Index(got, "MiniMax-M3.1-Flash") > slices.Index(got, "MiniMax-M3") {
			t.Fatalf("Flash must precede M3: %v", got)
		}
		for _, model := range []string{"other-model", "minimax-music-2.5", "speech-2.8-hd", "image-01"} {
			if !slices.Contains(got, model) {
				t.Errorf("discovery removed unrelated model %s", model)
			}
		}
	}
}
