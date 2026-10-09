package mcpregistry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/testenv"
)

func registryPaginationItem(name string) map[string]any {
	return map[string]any{"server": map[string]any{
		"name": name, "version": "1.0.0",
		"remotes": []any{map[string]any{"type": "streamable-http", "url": "https://mcp.example/mcp"}},
	}}
}

func TestResolveFindsExactNameAfterFirstPage(t *testing.T) {
	const name = "io.example/demo"
	const cursor = "opaque cursor+/=?&"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		q := r.URL.Query()
		if r.URL.Path != "/v0.1/servers" || q.Get("search") != name || q.Get("version") != "latest" || q.Get("limit") != "100" {
			t.Errorf("request = %s", r.URL)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var items []any
		next := ""
		switch q.Get("cursor") {
		case "":
			for i := range 100 {
				items = append(items, registryPaginationItem(fmt.Sprintf("%s-addon-%03d", name, i)))
			}
			next = cursor
		case cursor:
			items = []any{registryPaginationItem(name)}
		default:
			t.Errorf("cursor changed: %q", q.Get("cursor"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"servers": items, "metadata": map[string]any{"count": len(items), "nextCursor": next},
		})
	}))
	t.Cleanup(server.Close)
	client := New(filepath.Join(testenv.TempDir(t), "registry.json"))
	client.BaseURL = server.URL
	entry, result, err := client.Resolve(t.Context(), "  "+name+"  ")
	if err != nil || entry.Name != name || !entry.Installable || result.Cached || result.Warning != "" {
		t.Fatalf("Resolve name=%q installable=%t cached=%t entries=%d warning=%q err=%v", entry.Name, entry.Installable, result.Cached, len(result.Entries), result.Warning, err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want two pages", requests.Load())
	}
	configured, err := entry.PluginEntry("manual")
	if err != nil || configured.Name != "manual" || configured.URL != entry.URL || configured.Type != "http" {
		t.Fatalf("PluginEntry = %+v, %v", configured, err)
	}
	server.Close()
	browse, err := client.Search(t.Context(), name, 100)
	if err != nil || !browse.Cached || len(browse.Entries) != 100 || browse.Entries[0].Name != name+"-addon-000" {
		t.Fatalf("browse cached=%t entries=%d err=%v", browse.Cached, len(browse.Entries), err)
	}
	if _, _, err := client.Resolve(t.Context(), name); err == nil {
		t.Fatal("Resolve used cached metadata after the registry stopped")
	}
}

func TestResolvePaginationStopsAtMatchEndOrFailure(t *testing.T) {
	const name = "io.example/demo"
	for _, tc := range []struct {
		name     string
		requests int32
		found    bool
	}{
		{"first-page", 1, true},
		{"missing", 2, false},
		{"failed-page", 2, false},
		{"cursor-cycle", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				cursor := r.URL.Query().Get("cursor")
				if cursor != "" && tc.name == "failed-page" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				items := []any{registryPaginationItem(name + "-other")}
				next := ""
				switch {
				case tc.found:
					items = []any{registryPaginationItem(name)}
					next = "unused"
				case cursor == "", cursor == "page-b":
					next = "page-a"
				case tc.name == "cursor-cycle":
					next = "page-b"
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"servers": items, "metadata": map[string]any{"nextCursor": next},
				})
			}))
			t.Cleanup(server.Close)
			client := New("")
			client.BaseURL = server.URL
			entry, result, err := client.Resolve(t.Context(), name)
			if (err == nil) != tc.found || result.Cached || (tc.found && entry.Name != name) {
				t.Errorf("Resolve = %+v, %+v, %v", entry, result, err)
			}
			if requests.Load() != tc.requests {
				t.Errorf("requests = %d, want %d", requests.Load(), tc.requests)
			}
		})
	}
}

func TestSearchRemainsBoundedToFirstRegistryPage(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Query().Get("limit") != "1" || r.URL.Query().Get("cursor") != "" {
			t.Errorf("browse request = %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"servers":  []any{registryPaginationItem("io.example/demo")},
			"metadata": map[string]any{"nextCursor": "unused"},
		})
	}))
	t.Cleanup(server.Close)
	client := New("")
	client.BaseURL = server.URL
	result, err := client.Search(t.Context(), "demo", 1)
	if err != nil || len(result.Entries) != 1 || requests.Load() != 1 {
		t.Fatalf("Search = %+v, %v, requests=%d", result, err, requests.Load())
	}
}
