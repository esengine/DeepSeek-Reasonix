// provider_wizard.go — adding a model source from the setup wizard.
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"reasonix/internal/base/i18n"
	"reasonix/internal/contract/config"
	"reasonix/internal/frontend/termrender"
)

// protocolMenuText names a wire for a person. A kind the translators have not
// reached prints as itself rather than as a blank row.
func protocolMenuText(kind string) (string, string) {
	switch kind {
	case "openai":
		return i18n.M.ProtocolOpenAIName, i18n.M.ProtocolOpenAIDesc
	case "responses":
		return i18n.M.ProtocolResponsesName, i18n.M.ProtocolResponsesDesc
	case "anthropic":
		return i18n.M.ProtocolAnthropicName, i18n.M.ProtocolAnthropicDesc
	}
	return kind, ""
}

// promptProtocolKind asks which wire drives an endpoint whose model listing
// several of them answer. One candidate is not a question, so none is asked.
func promptProtocolKind(discovery string) (string, error) {
	kinds := config.ProtocolsDiscoveredAs(discovery)
	if len(kinds) <= 1 {
		return discovery, nil
	}
	items := make([]menuItem, len(kinds))
	for i, kind := range kinds {
		items[i].name, items[i].desc = protocolMenuText(kind)
	}
	idx, err := selectOne(i18n.M.ProtocolChooseLabel, items)
	if err != nil {
		return "", err
	}
	return kinds[idx], nil
}

// promptCustomProvider handles the custom provider entry flow.
func promptCustomProvider() (providerPromptResult, error) {
	kind, err := promptProtocolKind("openai")
	if err != nil {
		return providerPromptResult{}, err
	}
	methodIdx, err := selectOne(i18n.M.CustomAddMethodLabel, []menuItem{
		{name: i18n.M.CustomMethodManual},
		{name: i18n.M.CustomMethodURL},
	})
	if err != nil {
		return providerPromptResult{}, err
	}
	if methodIdx == 0 {
		return promptCustomProviderManual(kind)
	}
	return promptCustomProviderFromURL(kind)
}

// promptCustomProviderManual handles manual model entry.
func promptCustomProviderManual(kind string) (providerPromptResult, error) {
	return promptCustomProviderManualWith(bufio.NewScanner(os.Stdin), kind, "", "", "")
}

// promptCustomProviderManualWith is the shared backend for manual entry.
// Pre-filled values (baseURL, keyEnv, apiKey) are reused as-is when non-empty
// so the URL-fetch flow can fall through to manual entry without re-asking
// the user for information they've already typed. An empty apiKey is allowed
// — the key step happens later in the wizard and Reasonix's global .env is updated then.
func promptCustomProviderManualWith(in *bufio.Scanner, kind, baseURL, keyEnv, apiKey string) (providerPromptResult, error) {
	fmt.Println()
	if baseURL == "" {
		baseURL = ask(in, os.Stdout, i18n.M.CustomPromptBaseURL, "")
		if baseURL == "" {
			return providerPromptResult{}, fmt.Errorf("base URL is required")
		}
	}
	providerName := providerSlug("custom", baseURL)
	modelName := ask(in, os.Stdout, i18n.M.CustomPromptModel, "")
	if modelName == "" {
		return providerPromptResult{}, fmt.Errorf("model name is required")
	}
	if keyEnv == "" {
		keyEnv = promptAPIKeyEnvName(in, os.Stdout, i18n.M.CustomPromptKeyEnv, apiKeyEnvFromProviderName(providerName))
	} else if !config.IsValidCredentialKey(keyEnv) {
		return providerPromptResult{}, fmt.Errorf("invalid API key variable name %q", keyEnv)
	}
	if apiKey == "" {
		apiKey = ask(in, os.Stdout, i18n.M.CustomPromptAPIKey, "")
	}
	entry := config.ProviderEntry{
		Name: providerName, Kind: kind, BaseURL: baseURL,
		Model: modelName, APIKeyEnv: keyEnv, ContextWindow: askContextWindow(in, os.Stdout),
	}
	fmt.Printf("  %s\n", termrender.Green(fmt.Sprintf(i18n.M.CustomAddedFmt, entry.Name+"/"+modelName)))
	return newProviderPromptResult([]config.ProviderEntry{entry}, keyEnv, apiKey), nil
}

// promptCustomProviderFromURL tries the OpenAI-compatible GET /models
// endpoint and shows a checkbox of the returned models. If the call fails
// (network error, auth failure, or a vendor without /models) it falls
// through to manual entry, reusing the URL and key the user already typed.
func promptCustomProviderFromURL(kind string) (providerPromptResult, error) {
	in := bufio.NewScanner(os.Stdin)
	fmt.Println()

	baseURL := ask(in, os.Stdout, i18n.M.CustomPromptBaseURL, "")
	if baseURL == "" {
		return providerPromptResult{}, fmt.Errorf("base URL is required")
	}
	providerName := providerSlug("custom", baseURL)
	keyEnv := promptAPIKeyEnvName(in, os.Stdout, i18n.M.CustomPromptKeyEnv, apiKeyEnvFromProviderName(providerName))
	apiKey := ask(in, os.Stdout, i18n.M.CustomPromptAPIKey, "")

	fmt.Printf("  %s\n", termrender.Dim(fmt.Sprintf(i18n.M.FetchingModelsFmt, "custom")))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := fetchModelListCompat(ctx, baseURL, apiKey)
	if err != nil || len(models) == 0 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s\n", termrender.Dim(fmt.Sprintf(i18n.M.FetchModelsFailedFmt, "custom", err)))
		} else {
			fmt.Fprintf(os.Stderr, "  %s\n", termrender.Dim(i18n.M.CustomFetchEmpty))
		}
		return promptCustomProviderManualWith(in, kind, baseURL, keyEnv, apiKey)
	}
	fmt.Printf("  %s\n", termrender.Green(fmt.Sprintf(i18n.M.FetchModelsSuccessFmt, len(models), "custom")))

	items := make([]menuItem, len(models))
	for i, m := range models {
		items[i] = menuItem{name: m}
	}
	idxs, err := selectMany(fmt.Sprintf(i18n.M.SelectModelsLabel, "custom"), items)
	if err != nil || len(idxs) == 0 {
		return providerPromptResult{}, fmt.Errorf("no models selected")
	}
	var selected []string
	for _, i := range idxs {
		selected = append(selected, models[i])
	}
	entry := config.ProviderEntry{
		Name: providerName, Kind: kind, BaseURL: baseURL,
		Models: selected, Model: selected[0], APIKeyEnv: keyEnv, ContextWindow: askContextWindow(in, os.Stdout),
	}
	fmt.Printf("  %s\n", termrender.Green(fmt.Sprintf(i18n.M.CustomAddedFmt, entry.Name+"/"+selected[0])))
	return newProviderPromptResult([]config.ProviderEntry{entry}, keyEnv, apiKey), nil
}

// addProviderToSession runs the entry flow for a model-listing shape. The two
// flows differ in how models are discovered, not in which wire drives them —
// the protocol chooser inside each one answers that from the catalog.
func addProviderToSession(s *providerSetupSession, discovery string) bool {
	var result providerPromptResult
	var err error
	if discovery == "anthropic" {
		result, err = promptAnthropicProvider()
	} else {
		result, err = promptCustomProvider()
	}
	if err != nil {
		if !errors.Is(err, errCancelled) {
			fmt.Fprintln(os.Stderr, err)
		}
		return false
	}
	for _, entry := range result.entries {
		if !confirmSharedCredential(s.cfg, entry, "") {
			return false
		}
	}
	if err := s.add(result.entries); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return false
	}
	s.addProviderAccess(result.entries)
	for key, value := range result.credentials {
		if err := s.setCredential(key, value); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return false
		}
	}
	// After the new keys are staged, so usability sees them.
	s.promoteDefaultToNewProviders(result.entries)
	return true
}
