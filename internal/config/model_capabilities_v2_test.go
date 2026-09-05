package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/provider"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestCapabilityOverrideDirectCatalogResolution(t *testing.T) {
	r := &ModelCapabilityResolver{entries: map[string]ModelCapabilityCacheEntry{}}
	e := ProviderEntry{Name: "opencode-go", Kind: "openai", BaseURL: "https://opencode.ai/zen/go/v1", Model: "kimi-k3"}
	auto := r.Resolve(&e)
	e.ModelOverrides = map[string]ProviderModelOverride{"KIMI-K3": {Vision: capabilityBoolPtr(false), ContextWindow: 123456}}
	got := r.Resolve(&e)
	if got.State != CapabilityUnsupported || got.AutomaticState != CapabilitySupported || got.Source != CapabilitySourceOverride {
		t.Fatalf("override = %+v", got)
	}
	facts := got.ModelInfo
	facts.InputModalities = auto.ModelInfo.InputModalities
	if !reflect.DeepEqual(facts, auto.ModelInfo) {
		t.Fatalf("override erased catalog facts: %+v vs %+v", got, auto)
	}
	if e.Model != "kimi-k3" || e.visionOverride != nil {
		t.Fatal("shared entry was mutated")
	}
	e.Model = "uncatalogued"
	if got := r.Resolve(&e); got.State != CapabilityUnknown {
		t.Fatalf("other model inherited override: %+v", got)
	}
	e.ModelOverrides["uncatalogued"] = ProviderModelOverride{Vision: capabilityBoolPtr(true)}
	if got := r.Resolve(&e); got.State != CapabilitySupported || got.AutomaticState != CapabilityUnknown {
		t.Fatalf("manual enable: %+v", got)
	}
	delete(e.ModelOverrides, "uncatalogued")
	if got := r.Resolve(&e); got.State != CapabilityUnknown {
		t.Fatalf("auto: %+v", got)
	}
}

func TestCapabilityOfficialHardLimitAndExplicitOff(t *testing.T) {
	r := &ModelCapabilityResolver{}
	for _, kind := range []string{"openai", "anthropic", "responses"} {
		e := ProviderEntry{Name: "deepseek", Kind: kind, BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", Vision: true, ModelOverrides: map[string]ProviderModelOverride{"deepseek-v4-flash": {Vision: capabilityBoolPtr(true)}}}
		if got := r.Resolve(&e); got.State != CapabilityUnsupported || got.ImageInputEnableAllowed || got.ImageInputBlockReason == "" {
			t.Fatalf("%s hard limit: %+v", kind, got)
		}
		e.BaseURL, e.RequestURL = "https://relay.test", "https://api.deepseek.com/v1/messages"
		if got := r.Resolve(&e); got.State != CapabilityUnsupported || got.ImageInputEnableAllowed {
			t.Fatalf("%s exact request URL bypassed hard limit: %+v", kind, got)
		}
		e.Model = "deepseek-v4-flash-vision-exp"
		e.ModelOverrides[e.Model] = ProviderModelOverride{Vision: capabilityBoolPtr(false)}
		if got := r.Resolve(&e); got.State != CapabilityUnsupported || !got.ImageInputEnableAllowed || got.Source != CapabilitySourceOverride {
			t.Fatalf("%s vision off: %+v", kind, got)
		}
	}
}

func TestCapabilityV2IgnoresV1AndPersistsUnknown(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	v1 := filepath.Join(CacheDir(), "model-capabilities-v1.json")
	old := []byte(`{"version":1,"entries":[]}`)
	if err := os.WriteFile(v1, old, 0600); err != nil {
		t.Fatal(err)
	}
	e := ProviderEntry{Name: "relay", Kind: "openai", BaseURL: "https://relay.test", Model: "x"}
	r := NewModelCapabilityResolver()
	now := time.Now()
	r.PutCatalogAt(e, []provider.ModelInfo{{ID: "x", InputModalities: []provider.ModelModality{provider.ModalityText, provider.ModalityImage}}}, now)
	r.PutCatalogAt(e, []provider.ModelInfo{{ID: "x"}}, now.Add(time.Second))
	if got := NewModelCapabilityResolver().Resolve(&e); got.State != CapabilityUnknown || got.InputModalities != nil {
		t.Fatalf("unknown roundtrip: %+v", got)
	}
	if data, _ := os.ReadFile(v1); string(data) != string(old) {
		t.Fatal("v1 was modified")
	}
	info, err := os.Stat(r.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cache permissions: %v %v", info, err)
	}
	for _, content := range []string{"broken", `{"version":999}`, `{"version":1}`} {
		if err := os.WriteFile(r.path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if got := NewModelCapabilityResolver().Resolve(&e); got.State != CapabilityUnknown {
			t.Fatalf("invalid cache: %+v", got)
		}
	}
}

func TestCapabilityCacheNewestSuccessWinsAcrossResolvers(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	e := ProviderEntry{Name: "relay", Kind: "openai", BaseURL: "https://relay.test", Model: "x"}
	old, newer := NewModelCapabilityResolver(), NewModelCapabilityResolver()
	now := time.Now()
	newer.PutCatalogAt(e, []provider.ModelInfo{{ID: "x"}}, now.Add(time.Second))
	old.PutCatalogAt(e, []provider.ModelInfo{{ID: "x", InputModalities: []provider.ModelModality{provider.ModalityImage}}}, now)
	if got := old.Resolve(&e); got.State != CapabilityUnknown {
		t.Fatalf("late response resurrected images: %+v", got)
	}
	if got := NewModelCapabilityResolver().Resolve(&e); got.State != CapabilityUnknown {
		t.Fatalf("disk lost newer success: %+v", got)
	}
	// Concurrent cache reads and writes share no mutable Provider state.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			old.PutCatalogAt(e, []provider.ModelInfo{{ID: "x"}}, now)
			_ = old.Resolve(&e)
		})
	}
	wg.Wait()
}

func TestCapabilityCacheRouteIdentity(t *testing.T) {
	r := &ModelCapabilityResolver{}
	e := ProviderEntry{Name: "relay", Kind: "openai", BaseURL: "https://relay.test", Model: "x"}
	for _, mutate := range []func(*ProviderEntry){func(p *ProviderEntry) { p.NoProxy = true }, func(p *ProviderEntry) { p.ChatURL = "https://a.test/chat" }, func(p *ProviderEntry) { p.RequestURL = "https://b.test/responses" }} {
		other := e
		mutate(&other)
		if r.providerFingerprint(e) == r.providerFingerprint(other) {
			t.Fatal("route omitted from cache identity")
		}
	}
}

func TestCapabilityCacheMergeValidatesDiskModalities(t *testing.T) {
	t.Setenv("REASONIX_CACHE_HOME", t.TempDir())
	r := NewModelCapabilityResolver()
	e := ProviderEntry{Name: "relay", Kind: "openai", BaseURL: "https://relay.test", Model: "malformed"}
	file := ModelCapabilityCacheFile{Version: 2, Entries: []ModelCapabilityCacheEntry{{ProviderFingerprint: r.providerFingerprint(e), ModelID: e.Model, InputModalities: []provider.ModelModality{provider.ModalityImage, "invalid"}, FetchedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}}}
	data, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(r.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	r.PutCatalog(e, []provider.ModelInfo{{ID: "other"}})
	if got := r.Resolve(&e); got.State != CapabilityUnknown {
		t.Fatalf("merge trusted invalid disk metadata: %+v", got)
	}
}
