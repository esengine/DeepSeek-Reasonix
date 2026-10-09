// fetch.go — model auto-discovery via the OpenAI-compatible GET /models API.
package catalog

import (
	"context"
	"fmt"
	"net/http"
	"reasonix/internal/contract/config"
	"slices"
	"strings"

	"reasonix/internal/model/openai"
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
func FetchModels(ctx context.Context, e *config.ProviderEntry) ([]string, error) {
	return FetchModelsVia(ctx, e, nil)
}

// FetchModelsVia is FetchModels over a caller-supplied client. A caller that
// holds the user's proxy settings must use it: listing models over a plain
// client while chatting over a proxied one reports an empty catalog for an
// endpoint that works.
func FetchModelsVia(ctx context.Context, e *config.ProviderEntry, client *http.Client) ([]string, error) {
	listed, err := FetchModelListingVia(ctx, e, client)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(listed))
	for _, m := range listed {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// FetchModelListingVia is FetchModelsVia without discarding what each row
// declares about itself — the probe reads the wires a relay names there rather
// than inferring them from the shape of the listing.
func FetchModelListingVia(ctx context.Context, e *config.ProviderEntry, client *http.Client) ([]openai.ListedModel, error) {
	listed, _, err := fetchModelListingAt(ctx, e, client)
	return listed, err
}

// fetchModelListingAt also names the candidate URL that answered, which is how
// the probe learns which base a listing was served under.
func fetchModelListingAt(ctx context.Context, e *config.ProviderEntry, client *http.Client) ([]openai.ListedModel, string, error) {
	if e.BaseURL == "" {
		return nil, "", fmt.Errorf("fetch models: provider %q has no base_url", e.Name)
	}
	key := e.APIKey()
	if e.RequiresAPIKey() && key == "" {
		return nil, "", fmt.Errorf("fetch models: provider %q has no API key (set %s in .env)", e.Name, e.APIKeyEnv)
	}
	candidates, err := BuildModelFetchURLs(e.BaseURL, e.ModelsURL)
	if err != nil {
		return nil, "", err
	}
	var lastErr error
	var firstHardErr error
	authMode := modelFetchAuthMode(e)
	for _, u := range candidates {
		models, err := openai.FetchModelListing(ctx, u, key, openai.FetchModelsOptions{
			Headers:  e.Headers,
			AuthMode: authMode,
			Client:   client,
		})
		if err == nil {
			return models, u, nil
		}
		lastErr = err
		if !openai.IsModelFetchEndpointMiss(err) && firstHardErr == nil {
			firstHardErr = err
		}
	}
	if firstHardErr != nil {
		return nil, "", firstHardErr
	}
	return nil, "", lastErr
}

// ResolveChatBase is the base URL a chat call must be rooted at, given the base
// the user typed and the model-list URL that answered. A listing found only
// under {base}/v1 means the typed base is missing its version segment: chat
// posts to {base}/chat/completions, so the completed base is the one that was
// proven. Any other answering URL leaves the typed base as it is.
func ResolveChatBase(typed, answeredURL string) string {
	base := strings.TrimRight(strings.TrimSpace(typed), "/")
	if base != "" && answeredURL == base+"/v1/models" && !endsWithVersionSegment(base) {
		return base + "/v1"
	}
	return strings.TrimSpace(typed)
}

func modelFetchAuthMode(e *config.ProviderEntry) openai.ModelFetchAuthMode {
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
		seen := slices.Contains(out, s)
		if !seen {
			out = append(out, s)
		}
	}
	return out
}
