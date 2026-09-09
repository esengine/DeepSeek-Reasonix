// fetch.go — model auto-discovery via the OpenAI-compatible GET /models API.
package config

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reasonix/internal/provider/openai"
)

var knownModelFetchCompatSuffixes = []string{
	"/api/claudecode",
	"/api/anthropic",
	"/apps/anthropic",
	"/api/coding",
	"/claudecode",
	"/anthropic",
	"/step_plan",
	"/coding",
	"/claude",
}

// FetchModels queries the provider's OpenAI-compatible GET /models endpoint and
// returns the available model IDs, sorted alphabetically.
func (e *ProviderEntry) FetchModels(ctx context.Context) ([]string, error) {
	return e.FetchModelsWithProxy(ctx, netclient.ProxySpec{})
}

// FetchModelCatalog discovers model IDs together with adapter-owned input
// modality metadata. FetchModelsWithProxy remains the compatibility wrapper
// used by older callers.
func (e *ProviderEntry) FetchModelCatalog(ctx context.Context) ([]provider.ModelInfo, error) {
	return e.FetchModelCatalogWithProxy(ctx, netclient.ProxySpec{})
}

// FetchModelsWithProxy is FetchModels routed through the same network policy as
// chat requests. Passing cfg.NetworkProxySpec() makes model discovery fail at
// setup time when the proxy path is broken, instead of succeeding here and
// stalling the first chat turn later (#9560).
func (e *ProviderEntry) FetchModelsWithProxy(ctx context.Context, proxy netclient.ProxySpec) ([]string, error) {
	catalog, err := e.FetchModelCatalogWithProxy(ctx, proxy)
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(catalog))
	for _, model := range catalog {
		models = append(models, model.ID)
	}
	return models, nil
}

// FetchModelCatalogWithProxy is FetchModelsWithProxy with model capability
// metadata preserved through the config/provider boundary.
func (e *ProviderEntry) FetchModelCatalogWithProxy(ctx context.Context, proxy netclient.ProxySpec) ([]provider.ModelInfo, error) {
	if e.BaseURL == "" {
		return nil, fmt.Errorf("fetch models: provider %q has no base_url", e.Name)
	}
	key := e.APIKey()
	if e.RequiresAPIKey() && key == "" {
		return nil, fmt.Errorf("fetch models: provider %q has no API key (set %s in .env)", e.Name, e.APIKeyEnv)
	}
	candidates, err := BuildModelFetchURLs(e.BaseURL, e.ModelsURL)
	if err != nil {
		return nil, err
	}
	var lastErr error
	var firstHardErr error
	authMode := modelFetchAuthMode(e)
	for _, u := range candidates {
		models, err := openai.FetchModelCatalogWithOptions(ctx, u, key, openai.FetchModelsOptions{
			Headers:  e.Headers,
			AuthMode: authMode,
			Proxy:    proxy,
		})
		if err == nil {
			allowed := provider.FilterOfficialOpenCodeGoModels(e.Kind, e.BaseURL, modelInfoIDs(models))
			keep := make(map[string]bool, len(allowed))
			for _, id := range allowed {
				keep[id] = true
			}
			filtered := make([]provider.ModelInfo, 0, len(models))
			for _, model := range models {
				if keep[model.ID] {
					filtered = append(filtered, model)
				}
			}
			return filtered, nil
		}
		lastErr = err
		if !openai.IsModelFetchEndpointMiss(err) && firstHardErr == nil {
			firstHardErr = err
		}
	}
	if firstHardErr != nil {
		return nil, firstHardErr
	}
	return nil, lastErr
}

func modelInfoIDs(models []provider.ModelInfo) []string {
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

func modelFetchAuthMode(e *ProviderEntry) openai.ModelFetchAuthMode {
	if e == nil || !strings.EqualFold(strings.TrimSpace(e.Kind), "anthropic") {
		return openai.ModelFetchAuthAuto
	}
	if e.AuthHeader {
		return openai.ModelFetchAuthBearer
	}
	return openai.ModelFetchAuthXAPIKey
}

// BuildModelFetchURLs derives likely OpenAI-compatible model-list endpoints.
// It keeps Reasonix's historical {base}/models path first, then tries the common
// {base}/v1/models shape used by many aggregators. Known official Token Rhythm
// URLs collapse to a single /v1/models candidate because that /v1 route is complete.
func BuildModelFetchURLs(baseURL, override string) ([]string, error) {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		if canonical, ok := canonicalVendorModelsURL(trimmed); ok {
			return []string{canonical}, nil
		}
		return []string{trimmed}, nil
	}
	if canonical, ok := canonicalVendorModelsURL(baseURL); ok {
		return []string{canonical}, nil
	}
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("fetch models: base_url is required")
	}
	var candidates []string
	if endsWithVersionSegment(base) {
		candidates = append(candidates, base+"/models")
		if !strings.HasSuffix(base, "/v1") {
			candidates = append(candidates, base+"/v1/models")
		}
	} else {
		candidates = append(candidates, base+"/models", base+"/v1/models")
	}
	if stripped := stripModelFetchCompatSuffix(base); stripped != "" {
		root := strings.TrimRight(stripped, "/")
		candidates = append(candidates, root+"/models", root+"/v1/models")
	}
	return uniqueStrings(candidates), nil
}

// canonicalVendorModelsURL rewrites official vendor bases whose documented
// form differs from the OpenAI-compatible shape (Token Rhythm, StepFun step_plan).
func canonicalVendorModelsURL(raw string) (string, bool) {
	if canonical, ok := openai.CanonicalTokenRhythmModelsURL(raw); ok {
		return canonical, true
	}
	return openai.CanonicalStepFunPlanModelsURL(raw)
}

func endsWithVersionSegment(raw string) bool {
	last := raw
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		last = raw[i+1:]
	}
	if len(last) < 2 || last[0] != 'v' {
		return false
	}
	for _, r := range last[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func stripModelFetchCompatSuffix(base string) string {
	for _, suffix := range knownModelFetchCompatSuffixes {
		if strings.HasSuffix(base, suffix) {
			return base[:len(base)-len(suffix)]
		}
	}
	return ""
}

func uniqueStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		seen := slices.Contains(out, s)
		if !seen {
			out = append(out, s)
		}
	}
	return out
}
