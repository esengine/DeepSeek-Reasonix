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
)

type FetchModelsOptions struct {
	Headers  map[string]string
	AuthMode ModelFetchAuthMode
}

func (e modelFetchStatusError) Error() string {
	return fmt.Sprintf("fetch models: status %d: %s", e.status, strings.TrimSpace(e.body))
}

// IsModelFetchEndpointMiss reports whether a model-list request reached a
// plausible endpoint path that the provider does not implement.
func IsModelFetchEndpointMiss(err error) bool {
	var statusErr modelFetchStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.status == http.StatusNotFound || statusErr.status == http.StatusMethodNotAllowed
}

// FetchModels calls the OpenAI-compatible GET /models endpoint and returns the
// available model IDs.
func FetchModels(ctx context.Context, baseURL, apiKey string, headers map[string]string) ([]string, error) {
	return FetchModelsWithOptions(ctx, baseURL, apiKey, FetchModelsOptions{Headers: headers})
}

// FetchModelsWithOptions calls the OpenAI-compatible GET /models endpoint and
// returns the available model IDs.
func FetchModelsWithOptions(ctx context.Context, baseURL, apiKey string, opts FetchModelsOptions) ([]string, error) {
	cli := &http.Client{Timeout: 10 * time.Second}
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
		return nil, modelFetchStatusError{status: resp.StatusCode, body: truncateFetchBody(string(body))}
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
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
