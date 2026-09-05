package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/provider"
)

func TestModelCapabilityResolverHonorsExplicitConfigBeforeCache(t *testing.T) {
	r := &ModelCapabilityResolver{entries: map[string]ModelCapabilityCacheEntry{}}
	entry := ProviderEntry{Name: "custom", Kind: "openai", BaseURL: "https://example.test", Model: "mixed"}
	r.PutCatalog(entry, []provider.ModelInfo{{ID: "mixed", InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}})
	if got := r.Resolve(&entry); got.State != CapabilitySupported || got.Source != CapabilitySourceAdapter {
		t.Fatalf("adapter capability = %+v", got)
	}
	entry.VisionModels = []string{"mixed"}
	if got := r.Resolve(&entry); got.State != CapabilitySupported || got.Source != CapabilitySourceLegacy {
		t.Fatalf("legacy capability = %+v", got)
	}
	entry.visionOverride = capabilityBoolPtr(false)
	if got := r.Resolve(&entry); got.State != CapabilityUnsupported || got.Source != CapabilitySourceOverride {
		t.Fatalf("override capability = %+v", got)
	}
}

func TestModelCapabilityResolverDefaultsExactModelsToUnknown(t *testing.T) {
	r := &ModelCapabilityResolver{entries: map[string]ModelCapabilityCacheEntry{}}
	entry := ProviderEntry{Name: "custom", Kind: "openai", BaseURL: "https://example.test", Model: "new-model"}
	got := r.Resolve(&entry)
	if got.State != CapabilityUnknown || got.Source != CapabilitySourceUnknown || got.InputModalities != nil {
		t.Fatalf("default capability = %+v", got)
	}
}

func TestModelCapabilityResolverUsesBuiltinAdapterCatalog(t *testing.T) {
	r := &ModelCapabilityResolver{entries: map[string]ModelCapabilityCacheEntry{}}
	vision := ProviderEntry{Name: "opencode-go", Kind: "openai", BaseURL: "https://opencode.ai/zen/go/v1", Model: "kimi-k3"}
	if got := r.Resolve(&vision); got.State != CapabilitySupported || got.Source != CapabilitySourceAdapter {
		t.Fatalf("OpenCode Go vision capability = %+v", got)
	}
	if got := r.Resolve(&vision); got.ModelInfo.ContextWindow == 0 || got.ModelInfo.MaxOutputTokens == 0 || got.ModelInfo.API == "" {
		t.Fatalf("OpenCode Go catalog facts missing = %+v", got.ModelInfo)
	}
	text := vision
	text.Model = "glm-5.2"
	if got := r.Resolve(&text); got.State != CapabilityUnsupported || got.Source != CapabilitySourceAdapter {
		t.Fatalf("OpenCode Go text capability = %+v", got)
	}
	unknown := vision
	unknown.Model = "omen-alpha"
	if got := r.Resolve(&unknown); got.State != CapabilityUnknown || got.Source != CapabilitySourceUnknown {
		t.Fatalf("OpenCode Go unknown capability = %+v", got)
	}
	modelScope := ProviderEntry{Name: "modelscope", Kind: "openai", BaseURL: "https://api-inference.modelscope.cn/v1", Model: "Qwen/Qwen3.5-27B"}
	if got := r.Resolve(&modelScope); got.State != CapabilitySupported || got.Source != CapabilitySourceAdapter {
		t.Fatalf("ModelScope capability = %+v", got)
	}
}

func TestModelCapabilityResolverUsesOtherCuratedPresetCatalogs(t *testing.T) {
	r := &ModelCapabilityResolver{entries: map[string]ModelCapabilityCacheEntry{}}
	for _, id := range []string{"kimi-cn", "mimo-api", "minimax-cn-api", "glm-cn", "stepfun-api", "scnet", "ollama-cloud"} {
		preset, ok := CuratedProviderPreset(id)
		if !ok || len(preset.Entries) == 0 {
			t.Fatalf("missing preset %q", id)
		}
		entry := preset.Entries[0]
		var visionModel string
		for _, candidate := range entry.VisionModels {
			visionModel = candidate
			break
		}
		if visionModel == "" {
			continue
		}
		entry.Model = visionModel
		got := r.Resolve(&entry)
		if got.State != CapabilitySupported || got.Source != CapabilitySourcePreset {
			t.Fatalf("preset %q model %q capability = %+v", id, visionModel, got)
		}
	}
}

func TestModelCapabilityResolverLoadsIndependentCache(t *testing.T) {
	dir := t.TempDir()
	oldCache := os.Getenv("REASONIX_CACHE_HOME")
	if err := os.Setenv("REASONIX_CACHE_HOME", dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv("REASONIX_CACHE_HOME", oldCache) })

	entry := ProviderEntry{Name: "custom", Kind: "openai", BaseURL: "https://example.test", Model: "vision"}
	writer := NewModelCapabilityResolver()
	writer.PutCatalog(entry, []provider.ModelInfo{{ID: "vision", InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}})
	reader := NewModelCapabilityResolver()
	got := reader.Resolve(&entry)
	if got.State != CapabilitySupported || got.Source != CapabilitySourceCache {
		t.Fatalf("reloaded capability = %+v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "model-capabilities-v2.json")); err != nil {
		t.Fatalf("cache file missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "model-capabilities-v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" || string(data) == "\n" {
		t.Fatal("cache file is unexpectedly empty")
	}
	if string(data) != "" && containsAny(string(data), "super-secret-api-key", "Authorization") {
		t.Fatal("cache must not contain credential material")
	}
}

func TestModelCapabilityResolverIgnoresExpiredCache(t *testing.T) {
	r := &ModelCapabilityResolver{entries: map[string]ModelCapabilityCacheEntry{}}
	entry := ProviderEntry{Name: "custom", Kind: "openai", BaseURL: "https://example.test", Model: "vision"}
	key := r.entryKey(&entry, entry.Model)
	r.entries[key] = ModelCapabilityCacheEntry{
		ProviderFingerprint: r.providerFingerprint(entry), ModelID: entry.Model,
		InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage},
		ExpiresAt:       time.Now().Add(-time.Minute), Source: CapabilitySourceCache,
	}
	got := r.Resolve(&entry)
	if got.Source != CapabilitySourceUnknown || got.State != CapabilityUnknown {
		t.Fatalf("expired cache capability = %+v", got)
	}
}

func TestModelCapabilityResolverFingerprintIncludesCatalogHeaders(t *testing.T) {
	r := &ModelCapabilityResolver{}
	base := ProviderEntry{Name: "custom", Kind: "openai", BaseURL: "https://example.test", Model: "m"}
	withHeader := base
	withHeader.Headers = map[string]string{"X-Tenant": "tenant-a"}
	if r.providerFingerprint(base) == r.providerFingerprint(withHeader) {
		t.Fatal("catalog-affecting headers must invalidate capability cache identity")
	}
}

func TestReadModelCapabilityCacheFileRejectsOversizedData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model-capabilities-v1.json")
	data := []byte(strings.Repeat("x", modelCapabilityCacheMaxSize+1))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readModelCapabilityCacheFile(path); ok {
		t.Fatal("oversized capability cache must be ignored")
	}
}

func capabilityBoolPtr(value bool) *bool { return &value }

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
