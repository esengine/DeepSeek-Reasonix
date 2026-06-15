// fetch.go — model auto-discovery via the OpenAI-compatible GET /models API.
package config

import (
	"context"
	"fmt"
	"strings"

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
	if e.BaseURL == "" {
		return nil, fmt.Errorf("fetch models: provider %q has no base_url", e.Name)
	}
	key := e.APIKey()
	if key == "" {
		return nil, fmt.Errorf("fetch models: provider %q has no API key (set %s in .env)", e.Name, e.APIKeyEnv)
	}
	candidates, err := BuildModelFetchURLs(e.BaseURL, e.ModelsURL)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, u := range candidates {
		models, err := openai.FetchModels(ctx, u, key)
		if err == nil {
			return models, nil
		}
		lastErr = err
		if !openai.IsModelFetchEndpointMiss(err) {
			break
		}
	}
	return nil, lastErr
}

// BuildModelFetchURLs derives likely OpenAI-compatible model-list endpoints.
// It keeps Reasonix's historical {base}/models path first, then tries the common
// {base}/v1/models shape used by many aggregators.
func BuildModelFetchURLs(baseURL, override string) ([]string, error) {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return []string{trimmed}, nil
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
		seen := false
		for _, existing := range out {
			if existing == s {
				seen = true
				break
			}
		}
		if !seen {
			out = append(out, s)
		}
	}
	return out
}
