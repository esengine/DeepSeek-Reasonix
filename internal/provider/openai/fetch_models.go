package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"reasonix/internal/netclient"
	"reasonix/internal/provider"
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
	// Proxy routes the model-list request through the same transport policy as
	// chat requests, so a broken proxy surfaces at setup time instead of only
	// stalling the first chat turn later (#9560).
	Proxy netclient.ProxySpec
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

// FetchModelCatalog calls the OpenAI-compatible model endpoint and returns
// model-level capability metadata. The adapter deliberately uses a
// unknown capability when an endpoint omits capability fields;
// callers must never infer image support from a model name.
func FetchModelCatalog(ctx context.Context, baseURL, apiKey string, headers map[string]string) ([]provider.ModelInfo, error) {
	return FetchModelCatalogWithOptions(ctx, baseURL, apiKey, FetchModelsOptions{Headers: headers})
}

// FetchModelsWithOptions calls the OpenAI-compatible GET /models endpoint and
// returns the available model IDs.
func FetchModelsWithOptions(ctx context.Context, baseURL, apiKey string, opts FetchModelsOptions) ([]string, error) {
	catalog, err := FetchModelCatalogWithOptions(ctx, baseURL, apiKey, opts)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(catalog))
	for _, model := range catalog {
		ids = append(ids, model.ID)
	}
	return ids, nil
}

// FetchModelCatalogWithOptions is the metadata-preserving form of
// FetchModelsWithOptions. It keeps the existing request/auth/size behavior.
func FetchModelCatalogWithOptions(ctx context.Context, baseURL, apiKey string, opts FetchModelsOptions) ([]provider.ModelInfo, error) {
	transport, err := netclient.NewTransport(opts.Proxy, netclient.TransportOptions{})
	if err != nil {
		return nil, fmt.Errorf("fetch models: network: %w", err)
	}
	cli := &http.Client{Timeout: 10 * time.Second, Transport: transport}
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
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("fetch models: decode response: %w", err)
	}

	modelsByID := make(map[string]provider.ModelInfo, len(result.Data))
	conflicts := make(map[string]bool)
	for _, raw := range result.Data {
		model := parseModelInfo(baseURL, raw)
		if model.ID == "" {
			continue
		}
		if previous, exists := modelsByID[model.ID]; exists {
			switch {
			case conflicts[model.ID]:
				model.InputModalities = nil
			case model.InputModalities == nil:
				model.InputModalities = previous.InputModalities
			case previous.InputModalities != nil && !sameModalities(previous.InputModalities, model.InputModalities):
				// A conflict stays unknown for the rest of this response, regardless
				// of duplicate ordering. Missing metadata is not a negative fact.
				conflicts[model.ID] = true
				model.InputModalities = nil
			}
		}
		modelsByID[model.ID] = model
	}
	models := make([]provider.ModelInfo, 0, len(modelsByID))
	for _, model := range modelsByID {
		models = append(models, model)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models, nil
}

func sameModalities(a, b []provider.ModelModality) bool {
	return len(a) == len(b) && !slices.ContainsFunc(a, func(m provider.ModelModality) bool { return !slices.Contains(b, m) })
}

func parseModelInfo(baseURL string, raw json.RawMessage) provider.ModelInfo {
	var entry map[string]json.RawMessage
	if json.Unmarshal(raw, &entry) != nil {
		return provider.ModelInfo{}
	}
	var rawID string
	_ = json.Unmarshal(entry["id"], &rawID)
	id := normalizeModelID(baseURL, rawID)
	if id == "" {
		return provider.ModelInfo{}
	}
	modalities, _ := parseModalities(entry)
	return provider.ModelInfo{ID: id, InputModalities: modalities}
}

func parseModalities(entry map[string]json.RawMessage) ([]provider.ModelModality, bool) {
	// Canonical and nested array fields are ordered before compatibility
	// aliases. Presence with an invalid value is treated as an unsafe
	// declaration and therefore stays unknown.
	for _, key := range []string{"input_modalities"} {
		if raw, present := entry[key]; present {
			return decodeModalities(raw)
		}
	}
	if raw, present := entry["modalities"]; present {
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil {
			if input, ok := nested["input"]; ok {
				return decodeModalities(input)
			}
		}
		return nil, false
	}
	if raw, present := entry["capabilities"]; present {
		var nested map[string]json.RawMessage
		if json.Unmarshal(raw, &nested) == nil {
			if input, ok := nested["input_modalities"]; ok {
				return decodeModalities(input)
			}
			if vision, ok := nested["vision"]; ok {
				return decodeVisionBool(vision)
			}
		}
		return nil, false
	}
	for _, key := range []string{"supports_vision", "vision"} {
		if raw, present := entry[key]; present {
			return decodeVisionBool(raw)
		}
	}
	return nil, false
}

func decodeModalities(raw json.RawMessage) ([]provider.ModelModality, bool) {
	var values []string
	if json.Unmarshal(raw, &values) != nil || len(values) == 0 {
		return nil, false
	}
	seen := map[provider.ModelModality]bool{}
	out := make([]provider.ModelModality, 0, len(values))
	for _, value := range values {
		modality := provider.ModelModality(strings.ToLower(strings.TrimSpace(value)))
		if modality != provider.ModalityText && modality != provider.ModalityImage {
			return nil, false
		}
		if !seen[modality] {
			seen[modality] = true
			out = append(out, modality)
		}
	}
	// Stable order also makes duplicate merging independent of array order.
	if len(out) == 2 && out[0] == provider.ModalityImage {
		out[0], out[1] = out[1], out[0]
	}
	return out, len(out) > 0
}

func decodeVisionBool(raw json.RawMessage) ([]provider.ModelModality, bool) {
	var vision *bool
	if json.Unmarshal(raw, &vision) != nil || vision == nil {
		return nil, false
	}
	if *vision {
		return []provider.ModelModality{provider.ModalityText, provider.ModalityImage}, true
	}
	return []provider.ModelModality{provider.ModalityText}, true
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
