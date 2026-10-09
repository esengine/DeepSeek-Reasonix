package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"reasonix/internal/contract/provider"
)

type modelFetchStatusError struct {
	status int
	body   string
}

type ModelFetchAuthMode string

const (
	ModelFetchAuthAuto    ModelFetchAuthMode = ""
	ModelFetchAuthBearer  ModelFetchAuthMode = "bearer"
	ModelFetchAuthXAPIKey ModelFetchAuthMode = "x-api-key"

	// fetchModelsMaxBody caps the response body read from a model-list
	// endpoint. Large providers like OpenRouter return ~530 KB for 338
	// models; 2 MiB leaves headroom while keeping memory bounded.
	fetchModelsMaxBody = 2 << 20 // 2 MiB
)

type FetchModelsOptions struct {
	Headers  map[string]string
	AuthMode ModelFetchAuthMode
	// Client reaches the endpoint the way a real request will. Nil honours only
	// the HTTP_PROXY environment, so a user whose proxy lives in Reasonix's own
	// settings would list nothing while chatting fine.
	Client *http.Client
}

func (e modelFetchStatusError) Error() string {
	return fmt.Sprintf("fetch models: status %d: %s", e.status, strings.TrimSpace(e.body))
}

// ModelFetchStatus reports the status a model-list request came back with. A
// caller that must tell a refused key from a path the endpoint does not serve
// reads it here rather than out of the message. false when the request never
// reached a response at all.
func ModelFetchStatus(err error) (int, bool) {
	var statusErr modelFetchStatusError
	if !errors.As(err, &statusErr) {
		return 0, false
	}
	return statusErr.status, true
}

// ModelFetchBody is what the endpoint said alongside a refused model-list
// request, for a reader to see and never for a caller to classify by.
func ModelFetchBody(err error) (string, bool) {
	var statusErr modelFetchStatusError
	if !errors.As(err, &statusErr) {
		return "", false
	}
	return strings.TrimSpace(statusErr.body), true
}

// IsModelFetchEndpointMiss reports whether a model-list request reached a
// plausible endpoint path that the provider does not implement.
func IsModelFetchEndpointMiss(err error) bool {
	status, ok := ModelFetchStatus(err)
	return ok && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed)
}

// FetchModels calls the OpenAI-compatible GET /models endpoint and returns the
// available model IDs.
func FetchModels(ctx context.Context, baseURL, apiKey string, headers map[string]string) ([]string, error) {
	return FetchModelsWithOptions(ctx, baseURL, apiKey, FetchModelsOptions{Headers: headers})
}

// FetchModelsWithOptions calls the OpenAI-compatible GET /models endpoint and
// returns the available model IDs.
func FetchModelsWithOptions(ctx context.Context, baseURL, apiKey string, opts FetchModelsOptions) ([]string, error) {
	listed, err := FetchModelListing(ctx, baseURL, apiKey, opts)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(listed))
	for _, m := range listed {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// ListedModel is one row of a model listing: its id, and the wires the endpoint
// says it serves for that model. Endpoints is empty for the listings that say
// nothing, which is most of them.
type ListedModel struct {
	ID        string
	Endpoints []string
}

// FetchModelListing is FetchModelsWithOptions without discarding what each row
// declares about itself.
func FetchModelListing(ctx context.Context, baseURL, apiKey string, opts FetchModelsOptions) ([]ListedModel, error) {
	cli := opts.Client
	if cli == nil {
		cli = &http.Client{Timeout: 10 * time.Second}
	}
	url := strings.TrimRight(baseURL, "/")
	if !strings.HasSuffix(url, "/models") {
		url += "/models"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch models: build request: %w", err)
	}
	applyModelFetchAPIKeyHeader(req.Header, baseURL, apiKey, opts.AuthMode)
	req.Header.Set("Accept", "application/json")
	applyCustomHeaders(req.Header, opts.Headers)
	provider.ApplyClientIdentity(req)

	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch models: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchModelsMaxBody+1))
	if err != nil {
		return nil, fmt.Errorf("fetch models: read response: %w", err)
	}
	if len(body) > fetchModelsMaxBody {
		return nil, fmt.Errorf("fetch models: response too large (exceeds %d bytes)", fetchModelsMaxBody)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, modelFetchStatusError{status: resp.StatusCode, body: truncateFetchBody(string(body))}
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
			// Some relays name the wires they serve per model. It is the
			// endpoint answering for itself, which beats inferring the answer
			// from the shape of this listing — see ListedModel.Endpoints.
			SupportedEndpointTypes []string `json:"supported_endpoint_types"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("fetch models: decode response: %w", err)
	}

	out := make([]ListedModel, 0, len(result.Data))
	for _, m := range result.Data {
		id := normalizeModelID(baseURL, m.ID)
		if id == "" {
			continue
		}
		out = append(out, ListedModel{ID: id, Endpoints: trimmedLower(m.SupportedEndpointTypes)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// trimmedLower normalizes a declared wire list; unknown spellings stay as they
// are so the caller, not this decoder, decides what it recognises.
func trimmedLower(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func applyModelFetchAPIKeyHeader(h http.Header, baseURL, apiKey string, mode ModelFetchAuthMode) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return
	}
	switch mode {
	case ModelFetchAuthBearer:
		h.Set("Authorization", "Bearer "+apiKey)
	case ModelFetchAuthXAPIKey:
		h.Set("x-api-key", apiKey)
	default:
		applyAPIKeyHeader(h, baseURL, apiKey)
	}
}

func truncateFetchBody(body string) string {
	body = strings.TrimSpace(body)
	const max = 512
	if len([]rune(body)) <= max {
		return body
	}
	r := []rune(body)
	return string(r[:max]) + "..."
}
