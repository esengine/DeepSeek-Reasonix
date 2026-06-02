package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// FetchModels calls the OpenAI-compatible GET /models endpoint and returns the
// available model IDs. It works with DeepSeek, MiMo, and any backend that
// implements the standard models listing API. The caller provides baseURL
// (e.g. "https://api.deepseek.com" or "https://api.xiaomimimo.com/v1") and
// a valid API key. On success the IDs are sorted alphabetically.
//
// This function is intended for interactive use (setup wizard, /providers add)
// and uses a short timeout; it is NOT called during normal config loading.
func FetchModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	return fetchModelsWithClient(ctx, baseURL, apiKey, &http.Client{
		Timeout: 10 * time.Second,
	})
}

// fetchModelsWithClient is the testable core; callers can inject a mock client.
func fetchModelsWithClient(ctx context.Context, baseURL, apiKey string, cli *http.Client) ([]string, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("fetch models: base_url is required")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("fetch models: api_key is required")
	}

	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch models: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 256*1024))
	if err != nil {
		return nil, fmt.Errorf("fetch models: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch models: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result modelsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("fetch models: decode response: %w", err)
	}

	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// modelsResponse is the wire format for GET /models.
type modelsResponse struct {
	Object string       `json:"object"`
	Data   []modelEntry `json:"data"`
}

type modelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}
