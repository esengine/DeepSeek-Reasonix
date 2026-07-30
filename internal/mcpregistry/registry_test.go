package mcpregistry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchNormalizesOfficialRegistryEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0.1/servers" || r.URL.Query().Get("version") != "latest" || r.URL.Query().Get("search") != "demo" {
			t.Fatalf("request = %s", r.URL)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"servers": []any{
			map[string]any{"server": map[string]any{
				"name": "io.example/remote", "title": "Remote", "version": "1.2.0",
				"remotes": []any{map[string]any{"type": "streamable-http", "url": "https://mcp.example/mcp"}},
			}},
			map[string]any{"server": map[string]any{
				"name": "io.example/package", "description": "Package server", "version": "2.0.0",
				"packages": []any{map[string]any{
					"registryType": "npm", "identifier": "@example/mcp", "version": "2.0.0",
					"transport": map[string]any{"type": "stdio"},
				}},
			}},
			map[string]any{"server": map[string]any{
				"name": "io.example/manual", "version": "1.0.0",
				"remotes": []any{map[string]any{
					"type": "streamable-http", "url": "https://mcp.example/{tenant}",
					"variables": map[string]any{"tenant": map[string]any{"isRequired": true}},
				}},
			}},
		}})
	}))
	defer server.Close()

	client := New(filepath.Join(t.TempDir(), "registry.json"))
	client.BaseURL = server.URL
	result, err := client.Search(context.Background(), "demo", 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Cached || len(result.Entries) != 3 {
		t.Fatalf("result = %+v", result)
	}
	remote := result.Entries[0]
	if !remote.Installable || remote.Transport != "http" || remote.URL != "https://mcp.example/mcp" {
		t.Fatalf("remote = %+v", remote)
	}
	pkg := result.Entries[1]
	if !pkg.Installable || pkg.Transport != "stdio" || pkg.Command != "npx" || len(pkg.Args) != 2 || pkg.Args[1] != "@example/mcp@2.0.0" {
		t.Fatalf("package = %+v", pkg)
	}
	if result.Entries[2].Installable || result.Entries[2].UnavailableReason == "" {
		t.Fatalf("manual entry = %+v", result.Entries[2])
	}
	entry, err := pkg.PluginEntry("")
	if err != nil || entry.Name != "package" || entry.Command != "npx" {
		t.Fatalf("PluginEntry = %+v, %v", entry, err)
	}
}

func TestSearchFallsBackToMatchingCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"servers": []any{map[string]any{"server": map[string]any{
			"name": "io.example/cached", "version": "1", "remotes": []any{map[string]any{"type": "sse", "url": "https://mcp.example/sse"}},
		}}}})
	}))
	cachePath := filepath.Join(t.TempDir(), "registry.json")
	client := New(cachePath)
	client.BaseURL = server.URL
	client.Now = func() time.Time { return time.Unix(1_000_000, 0) }
	if _, err := client.Search(context.Background(), "cached", 5); err != nil {
		t.Fatal(err)
	}
	server.Close()
	client.HTTP = &http.Client{Timeout: 100 * time.Millisecond}
	result, err := client.Search(context.Background(), "cached", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cached || result.Warning == "" || len(result.Entries) != 1 || result.Entries[0].Transport != "sse" {
		t.Fatalf("cached result = %+v", result)
	}
}

func TestSearchExpiresEachCachedQueryIndependently(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("search")
		_ = json.NewEncoder(w).Encode(map[string]any{"servers": []any{map[string]any{"server": map[string]any{
			"name":    "io.example/" + query,
			"remotes": []any{map[string]any{"type": "sse", "url": "https://mcp.example/" + query}},
		}}}})
	}))
	cachePath := filepath.Join(t.TempDir(), "registry.json")
	client := New(cachePath)
	client.BaseURL = server.URL
	client.Now = func() time.Time { return now }
	if _, err := client.Search(context.Background(), "old", 5); err != nil {
		t.Fatal(err)
	}
	now = now.Add(maxCacheAge + time.Hour)
	if _, err := client.Search(context.Background(), "fresh", 5); err != nil {
		t.Fatal(err)
	}
	server.Close()
	client.HTTP = &http.Client{Timeout: 100 * time.Millisecond}

	if _, err := client.Search(context.Background(), "old", 5); err == nil {
		t.Fatal("expired query reused after an unrelated query refreshed the cache")
	}
	result, err := client.Search(context.Background(), "fresh", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cached || len(result.Entries) != 1 || result.Entries[0].Name != "io.example/fresh" {
		t.Fatalf("fresh cached result = %+v", result)
	}
}

func TestSearchReadsLegacyGlobalTimestampCache(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	key := cacheKey("legacy", 5)
	data, err := json.Marshal(cacheFile{
		FetchedAt: now,
		Queries: map[string][]Entry{
			key: {{Name: "io.example/legacy", Transport: "sse", URL: "https://mcp.example/legacy"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(cachePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	client := New(cachePath)
	client.BaseURL = "http://127.0.0.1:1"
	client.HTTP = &http.Client{Timeout: 100 * time.Millisecond}
	client.Now = func() time.Time { return now.Add(time.Hour) }
	result, err := client.Search(context.Background(), "legacy", 5)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Cached || len(result.Entries) != 1 || result.Entries[0].Name != "io.example/legacy" {
		t.Fatalf("legacy cached result = %+v", result)
	}
}

func TestResolveRequiresLiveRegistryMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"servers": []any{map[string]any{"server": map[string]any{
			"name":    "io.example/demo",
			"remotes": []any{map[string]any{"type": "streamable-http", "url": "https://mcp.example/demo"}},
		}}}})
	}))
	client := New(filepath.Join(t.TempDir(), "registry.json"))
	client.BaseURL = server.URL
	if _, err := client.Search(context.Background(), "io.example/demo", maxLimit); err != nil {
		t.Fatal(err)
	}
	server.Close()
	client.HTTP = &http.Client{Timeout: 100 * time.Millisecond}

	if _, result, err := client.Resolve(context.Background(), "io.example/demo"); err == nil {
		t.Fatalf("Resolve used cached install metadata: %+v", result)
	}
}

func TestSuggestedName(t *testing.T) {
	if got := SuggestedName("io.github.Example/My MCP Server"); got != "my-mcp-server" {
		t.Fatalf("SuggestedName = %q", got)
	}
}
