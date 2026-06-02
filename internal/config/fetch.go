// fetch.go — model auto-discovery via the OpenAI-compatible GET /models API.
//
// ProviderEntry.FetchModels calls the provider's /models endpoint and returns
// the available model IDs. This is used by the setup wizard and /providers add
// to auto-populate the models list instead of requiring users to type them.
//
// The function is NOT called during normal config loading (that would add
// latency and require a network connection). It is only invoked when the user
// interactively configures a provider.
package config

import (
	"context"
	"fmt"

	"reasonix/internal/provider/openai"
)

// FetchModels queries the provider's OpenAI-compatible GET /models endpoint and
// returns the available model IDs, sorted alphabetically. It uses the entry's
// base_url and resolves the API key from api_key_env. The caller should handle
// errors gracefully and fall back to hardcoded presets when the API is
// unreachable.
//
// Example usage:
//
//	ids, err := entry.FetchModels(ctx)
//	if err != nil {
//	    log.Printf("fetch models failed: %v — using presets", err)
//	    ids = presetModels
//	}
func (e *ProviderEntry) FetchModels(ctx context.Context) ([]string, error) {
	if e.BaseURL == "" {
		return nil, fmt.Errorf("fetch models: provider %q has no base_url", e.Name)
	}
	key := e.APIKey()
	if key == "" {
		return nil, fmt.Errorf("fetch models: provider %q has no API key (set %s in .env)", e.Name, e.APIKeyEnv)
	}
	return openai.FetchModels(ctx, e.BaseURL, key)
}

// RefreshModels fetches the latest model list from the provider's API and
// updates the entry's Models field in place. It returns the new model list
// and whether it changed from the previous value. The caller is responsible
// for persisting the config (cfg.Save()) after calling this.
func (e *ProviderEntry) RefreshModels(ctx context.Context) (models []string, changed bool, err error) {
	models, err = e.FetchModels(ctx)
	if err != nil {
		return nil, false, err
	}
	if len(models) == 0 {
		return nil, false, fmt.Errorf("fetch models: provider %q returned empty model list", e.Name)
	}
	prev := e.Models
	e.Models = models
	// Update default if it no longer exists in the new list
	if e.Default != "" && !e.HasModel(e.Default) {
		e.Default = models[0]
	}
	// Update single model for back-compat single-model entries
	if e.Model != "" && !e.HasModel(e.Model) {
		e.Model = models[0]
	}
	changed = !stringSliceEqual(prev, models)
	return models, changed, nil
}

// stringSliceEqual reports whether two string slices have the same elements in
// the same order.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
