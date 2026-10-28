package catalog

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"reasonix/internal/contract/config"
)

func TestMiniMaxDiscoveryPassesUnknownModelsThrough(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"MiniMax-M3"},{"id":"MiniMax-M3.1-Flash"},{"id":"MiniMax-M3.2"},{"id":"MiniMax-M4"}]}`)
	}))
	defer srv.Close()
	entry := &config.ProviderEntry{Name: "fixture", Kind: "openai", BaseURL: srv.URL}
	entryWithKey := entry.WithAPIKeyForProbe("test-key")
	got, err := FetchModelsVia(context.Background(), &entryWithKey, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"MiniMax-M3", "MiniMax-M3.1-Flash", "MiniMax-M3.2", "MiniMax-M4"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("discovered models = %v, want %v", got, want)
	}
}
