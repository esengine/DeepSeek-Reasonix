package boot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"strings"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
)

// ProviderBuildIdentity is the resolved identity a controller was built with.
// A changed fingerprint requires a rebuild; Effort is compared separately so
// persistence can change without changing the request-level level.
type ProviderBuildIdentity struct {
	Fingerprint string
	Effort      string
}

// ResolveProviderBuildIdentity fingerprints the resolved provider inputs
// selectModel gives boot, excluding effort, which is compared separately.
// A non-nil override follows the ACP path, including adaptive thinking.
func ResolveProviderBuildIdentity(e *config.ProviderEntry, proxy netclient.ProxySpec, effortOverride *string) ProviderBuildIdentity {
	if e == nil {
		return ProviderBuildIdentity{}
	}
	entry := *e
	if effortOverride != nil {
		entry.Effort = strings.TrimSpace(*effortOverride)
		if entry.Kind == "anthropic" && entry.Effort != "" && strings.TrimSpace(entry.Thinking) == "" {
			entry.Thinking = "adaptive"
		}
	}
	return ProviderBuildIdentity{
		Fingerprint: providerFingerprint(&entry, proxy),
		Effort:      config.EffectiveEffort(&entry),
	}
}

// NewProviderWithProxy builds a provider.Provider with the configured ordinary
// network proxy settings.
func NewProviderWithProxy(e *config.ProviderEntry, proxy netclient.ProxySpec) (provider.Provider, error) {
	return provider.New(e.Kind, providerConfig(e, proxy))
}

func providerConfig(e *config.ProviderEntry, proxy netclient.ProxySpec) provider.Config {
	return provider.Config{
		Name:    e.Name,
		BaseURL: e.BaseURL,
		Model:   e.Model,
		APIKey:  e.APIKey(), APIKeyFunc: e.APIKey, // live: a replaced key reaches the next request
		// Pass the key's env var so auth failures can name where to fix it, plus
		// provider-kind-specific knobs. EffectiveEffort applies a configured
		// default_effort when the user has not explicitly selected /effort.
		Extra: map[string]any{
			"api_key_env":          e.APIKeyEnv,
			"api_key_source":       e.APIKeySourceLabel(),
			"user_id":              e.CacheContextValue(),
			"session_id":           e.SessionContextValue(),
			"thinking":             e.Thinking,
			"effort":               config.EffectiveEffort(e),
			"supported_efforts":    config.RequestEffortLevels(e),
			"reasoning_modes":      config.RequestReasoningModes(e),
			"reasoning_protocol":   config.ReasoningProtocolForEntry(e),
			"max_output_tokens":    e.MaxOutputTokens,
			"idle_timeout_seconds": e.IdleTimeoutSeconds,
			"chat_url":             e.ChatURL,
			"request_url":          e.RequestURL,
			"headers":              e.Headers,
			"extra_body":           e.ExtraBody,
			"auth_header":          e.AuthHeader,
			"proxy_spec":           proxy,
			"vision":               config.EffectiveVision(e),
			"vision_detail":        e.VisionDetail,
			"web_search":           config.EffectiveWebSearch(e),
			"mode":                 e.ResponsesMode,
			// Keep nil as nil so the responses provider can vendor-detect its
			// default instead of accidentally treating every endpoint as stateful.
			"stateful": e.ResponsesStateful,
		},
	}
}

// providerFingerprint hashes the resolved entry and constructor payload,
// excluding request-level effort. APIKeyFunc is omitted because its value is
// already in APIKey; Extra carries resolved endpoint/key/vision/proxy inputs.
func providerFingerprint(e *config.ProviderEntry, proxy netclient.ProxySpec) string {
	cfg := providerConfig(e, proxy)
	extra := maps.Clone(cfg.Extra)
	delete(extra, "effort")
	entry := *e
	entry.Effort = ""
	payload := struct {
		Entry  config.ProviderEntry
		APIKey string
		Extra  map[string]any
	}{
		Entry:  entry,
		APIKey: cfg.APIKey,
		Extra:  extra,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		// Fail closed: without a stable identity callers must rebuild rather
		// than risk serving a stale provider.
		return ""
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
