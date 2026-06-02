// fetch.go — model auto-discovery via the OpenAI-compatible GET /models API.
package config

import (
	"context"
	"fmt"
	"time"

	"reasonix/internal/provider/openai"
)

// FetchModels queries the provider's OpenAI-compatible GET /models endpoint and
// returns the available model IDs, sorted alphabetically.
func (e *ProviderEntry) FetchModels(ctx context.Context) ([]string, error) {
	if e.BaseURL == "" {
		return nil, fmt.Errorf("fetch models: provider %q has no base_url", e.Name)
	}
	key := e.APIKey()
	if key == "" {
		return nil, fmt.Errorf("fetch models: provider %q has no API key (set %s in .env)", e.Name, e.APIKeyEnv)
	}
	url := e.ModelsURL
	if url == "" {
		url = e.BaseURL + "/models"
	}
	return openai.FetchModels(ctx, url, key)
}

// fetchModelsFromURL fetches models from a URL and returns the model IDs.
func fetchModelsFromURL(url, apiKey string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return openai.FetchModels(ctx, url, apiKey)
}
