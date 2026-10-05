package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/termrender"
	"reasonix/internal/model/catalog"
	"reasonix/internal/model/openai"
)

// fetchOrFallback lists the entry's live models, and on any failure — no
// address, no key yet, a vendor without /models — falls back to the preset's
// static list, so the wizard always has something to offer. Best-effort, 10s.
func fetchOrFallback(probe *config.ProviderEntry, famName string) []string {
	static := probe.ModelList()
	if probe.BaseURL == "" {
		return static
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := catalog.FetchModels(ctx, probe)
	if err != nil || len(models) == 0 {
		if len(static) > 0 {
			fmt.Fprintf(os.Stderr, "  %s\n", termrender.Dim(fmt.Sprintf(i18n.M.FetchModelsUsingPresetsFmt, famName)))
		}
		return static
	}
	fmt.Printf("  %s\n", termrender.Green(fmt.Sprintf(i18n.M.FetchModelsSuccessFmt, len(models), famName)))
	return models
}

// fetchModelListCompat probes every model-list URL a pasted base URL can mean
// (root, /v1, the compat suffixes), the same candidates the chat client uses,
// and returns the first that answers. A full miss is an empty list, not an
// error, so the wizard falls through to typing a model name.
func fetchModelListCompat(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	candidates, err := catalog.BuildModelFetchURLs(baseURL, "")
	if err != nil {
		return nil, err
	}
	var lastErr error
	var firstHardErr error
	for _, u := range candidates {
		models, err := openai.FetchModels(ctx, u, apiKey, nil)
		if err == nil {
			return models, nil
		}
		lastErr = err
		if !openai.IsModelFetchEndpointMiss(err) && firstHardErr == nil {
			firstHardErr = err
		}
	}
	if firstHardErr != nil {
		return nil, firstHardErr
	}
	if lastErr != nil {
		slog.Debug("model-list probe: all candidates missed", "base_url", baseURL, "err", lastErr)
	}
	return nil, nil
}
