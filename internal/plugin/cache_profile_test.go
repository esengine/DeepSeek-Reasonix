package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestProfileCacheIsolation pins the cross-version contract from the MCP 2026
// PR plan: the legacy profile keeps the shared v2 file; capability-declaring
// profiles write an isolated v3 file and treat the legacy file as a miss, so
// catalogs negotiated under different client capabilities never masquerade as
// each other.
func TestProfileCacheIsolation(t *testing.T) {
	dir := redirectCache(t)
	spec := sampleSpec()
	key := SchemaCacheKey(spec)

	legacy := CachedSchema{
		CacheKey:     key,
		Capabilities: map[string]bool{"tools": true},
		Tools:        []CachedTool{{Name: "legacy_tool", Schema: []byte(`{"type":"object"}`)}},
	}
	if err := SaveCachedSchema(spec.Name, legacy); err != nil {
		t.Fatalf("save legacy: %v", err)
	}

	if _, ok := LoadCachedSchemaForSpecProfile(spec, HostProfileInteractive); ok {
		t.Fatal("interactive profile read the legacy shared cache; must be a miss")
	}
	if _, ok := LoadCachedSchemaForSpecProfile(spec, HostProfileDesktopApps); ok {
		t.Fatal("desktop profile read the legacy shared cache; must be a miss")
	}
	if cs, ok := LoadCachedSchemaForSpecProfile(spec, HostProfileCore); !ok || len(cs.Tools) != 1 {
		t.Fatal("core profile must keep reading the legacy shared cache")
	}

	enhanced := CachedSchema{
		CacheKey:     key,
		Capabilities: map[string]bool{"tools": true},
		Tools: []CachedTool{{
			Name: "app_tool", Schema: []byte(`{"type":"object"}`),
			Visibility: []string{"model", "app"}, UIResourceURI: "ui://app/index.html",
		}},
	}
	if err := SaveCachedSchemaForProfile(HostProfileDesktopApps, spec.Name, enhanced); err != nil {
		t.Fatalf("save enhanced: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(dir, "mcp", "my-server*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected exactly legacy + enhanced cache files, got %v", files)
	}
	var sawEnhanced bool
	for _, f := range files {
		if filepath.Base(f) == "my-server.json" {
			continue
		}
		sawEnhanced = true
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var cs CachedSchema
		if err := json.Unmarshal(b, &cs); err != nil {
			t.Fatal(err)
		}
		if cs.Version != enhancedCacheVersion || cs.Profile != HostProfileDesktopApps.String() {
			t.Fatalf("enhanced cache version/profile = %d/%q", cs.Version, cs.Profile)
		}
	}
	if !sawEnhanced {
		t.Fatal("enhanced cache file not created")
	}

	cs, ok := LoadCachedSchemaForSpecProfile(spec, HostProfileDesktopApps)
	if !ok || len(cs.Tools) != 1 || cs.Tools[0].Name != "app_tool" {
		t.Fatalf("desktop profile cannot load its own enhanced cache: %+v", cs)
	}
	if !cs.Tools[0].ToolIsModelVisible() {
		t.Fatal("model+app visibility must stay model-visible")
	}
	if cs, ok := LoadCachedSchemaForSpecProfile(spec, HostProfileInteractive); ok {
		t.Fatalf("interactive profile read the desktop cache: %+v", cs)
	}
	if cs, ok := LoadCachedSchemaForSpecProfile(spec, HostProfileCore); !ok || cs.Tools[0].Name != "legacy_tool" {
		t.Fatal("legacy writer's cache was disturbed by the enhanced writer")
	}
}

// TestEnhancedCacheFutureVersionIsMiss ensures a newer layout is a miss, not a
// crash or a silent reuse.
func TestEnhancedCacheFutureVersionIsMiss(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	path := cachePathForProfile(spec.Name, HostProfileDesktopApps)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	future := CachedSchema{
		Version:      enhancedCacheVersion + 1,
		Profile:      HostProfileDesktopApps.String(),
		CacheKey:     SchemaCacheKey(spec),
		Capabilities: map[string]bool{"tools": true},
		Tools:        []CachedTool{{Name: "future", Schema: []byte(`{"type":"object"}`)}},
	}
	b, err := json.Marshal(future)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCachedSchemaForSpecProfile(spec, HostProfileDesktopApps); ok {
		t.Fatal("future cache version must be a miss")
	}
}

// TestEnhancedCacheLegacyToolVisibility pins the default visibility rule:
// a tool with no visibility metadata stays model-visible.
func TestEnhancedCacheLegacyToolVisibility(t *testing.T) {
	redirectCache(t)
	tool := CachedTool{Name: "plain", Schema: []byte(`{"type":"object"}`)}
	if !tool.ToolIsModelVisible() {
		t.Fatal("absent visibility must default to model-visible")
	}
	appOnly := CachedTool{Name: "app-only", Visibility: []string{"app"}}
	if appOnly.ToolIsModelVisible() {
		t.Fatal("app-only visibility must be hidden from the model catalog")
	}
}

// TestProfileHashStableAndDistinct guards the cache filename identity.
func TestProfileHashStableAndDistinct(t *testing.T) {
	interactive := HostProfileInteractive.ProfileCacheHash()
	desktop := HostProfileDesktopApps.ProfileCacheHash()
	core := HostProfileCore.ProfileCacheHash()
	if interactive == desktop || desktop == core || interactive == core {
		t.Fatalf("profile hashes collided: %s %s %s", core, interactive, desktop)
	}
	if HostProfileInteractive.ProfileCacheHash() != interactive {
		t.Fatal("profile hash not stable")
	}
	if len(interactive) != 12 {
		t.Fatalf("hash length = %d, want 12", len(interactive))
	}
}

// TestSaveEnhancedSetsTimestamp ensures LastValidated is stamped exactly once.
func TestSaveEnhancedSetsTimestamp(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	before := time.Now().UTC().Add(-time.Second)
	if err := SaveCachedSchemaForProfile(HostProfileInteractive, spec.Name, CachedSchema{
		CacheKey:     SchemaCacheKey(spec),
		Capabilities: map[string]bool{"tools": true},
	}); err != nil {
		t.Fatal(err)
	}
	cs, ok := LoadCachedSchemaForSpecProfile(spec, HostProfileInteractive)
	if !ok {
		t.Fatal("load after save failed")
	}
	if cs.LastValidated.Before(before) {
		t.Fatalf("LastValidated = %v, want >= %v", cs.LastValidated, before)
	}
}
