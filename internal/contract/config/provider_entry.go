package config

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/provider"
)

// ProviderEntry declares a model provider instance. ContextWindow is the model's
// token budget; the harness compacts older history as a turn's prompt approaches
// it (see agent compaction). 0 disables compaction for the instance.
type ProviderEntry struct {
	Name          string            `toml:"name"`
	DisplayName   string            `toml:"display_name"` // what the UI calls this entry; Name stays the identity refs point at
	Kind          string            `toml:"kind"`
	BaseURL       string            `toml:"base_url"`
	ChatURL       string            `toml:"chat_url"`    // legacy OpenAI chat endpoint override; retained with its historical semantics
	RequestURL    string            `toml:"request_url"` // exact provider request URL written by current settings UI
	Model         string            `toml:"model"`       // a single model (back-compat)
	Models        []string          `toml:"models"`      // a vendor's model list (one base_url/key, many models)
	ModelsURL     string            `toml:"models_url"`  // auto-fetch models from this URL on startup
	Default       string            `toml:"default"`     // default model when Models is set (else Models[0])
	APIKeyEnv     string            `toml:"api_key_env"`
	PresetID      string            `toml:"preset_id"`      // curated preset provenance: UI dedupe, and the vetting a wire contract reads.
	PresetVersion int               `toml:"preset_version"` // curated preset schema version for future migrations.
	Headers       map[string]string `toml:"headers"`        // optional extra HTTP headers for compatible gateways; secrets should stay in api_key_env.
	ExtraBody     map[string]any    `toml:"extra_body"`     // optional extra top-level JSON request body fields for OpenAI-compatible gateways.
	AuthHeader    bool              `toml:"auth_header"`    // for Anthropic-compatible gateways that expect Authorization: Bearer instead of x-api-key.
	// ResponsesMode selects the Responses API context strategy. Empty preserves
	// vendor detection; DeepSeek is stateless while compatible endpoints may use
	// stateful previous_response_id continuation.
	ResponsesMode string `toml:"responses_mode"`
	// ResponsesStateful is the legacy boolean form retained for config
	// compatibility. ResponsesMode wins when both are present.
	ResponsesStateful *bool `toml:"responses_stateful"`
	resolvedAPIKey    string
	resolvedSource    CredentialSource
	roots             Roots
	BalanceURL        string `toml:"balance_url"` // optional; a provider-specific wallet-balance endpoint (DeepSeek: https://api.deepseek.com/user/balance). Empty = no balance readout.
	ContextWindow     int    `toml:"context_window"`
	// MaxOutputTokens is a protocol-neutral total output budget for one turn.
	// Zero means automatic (ordinary 16K, reasoning 32K, high/max 64K); 32768
	// suits cost control and 65536 heavy reasoning. Negative omits wire limits.
	MaxOutputTokens int `toml:"max_output_tokens"`
	// PerseverationRetries overrides [progress_watch].perseveration_retries; nil inherits the global default.
	PerseverationRetries *int                         `toml:"perseveration_retries"`
	Price                *provider.Pricing            `toml:"price"`  // legacy/provider-wide fallback
	Prices               map[string]*provider.Pricing `toml:"prices"` // optional per-model prices; keys are model ids
	// BillingCurrency is the frozen list-price currency (ISO-4217). Independent
	// of [billing].display_currency; switching display never rewrites this.
	BillingCurrency string `toml:"billing_currency"`
	// BillingMode is payg (default) or subscription_equivalent (e.g. MiMo Token Plan).
	BillingMode string `toml:"billing_mode"`

	persistedOfficialCurrency string

	// Thinking / Effort are provider-kind-specific knobs forwarded via Config.Extra.
	// The anthropic provider reads Thinking="adaptive" for extended thinking and
	// Effort ("low".."max") for depth; openai forwards Effort as reasoning_effort.
	Thinking string `toml:"thinking"`
	Effort   string `toml:"effort"`
	// Vision marks the model as accepting image input: attached images are then
	// embedded in the request. Off by default, since text-only models 400 on
	// image input and the prompt prefix stays byte-identical without one.
	Vision bool `toml:"vision"`
	// VisionModels narrows image input support to specific models in a multi-model
	// provider, so one provider can expose text-only and multimodal chat models
	// without enabling image payloads for every model.
	VisionModels []string `toml:"vision_models"`
	// VisionDetail sets the openai image_url detail hint: low|high, plus DeepSeek's
	// "original"; empty = auto. "low" caps an image at ~85 tokens for a cheap read.
	VisionDetail string `toml:"vision_detail"`
	// WebSearch controls the provider-executed web_search tool for compatible
	// Anthropic and Responses endpoints. Nil keeps the official DeepSeek default;
	// non-nil preserves an explicit user choice across config rewrites.
	WebSearch *bool `toml:"web_search"`
	// ReasoningProtocol selects the request shape for OpenAI-compatible reasoning
	// models. Empty/auto uses the capability registry plus endpoint heuristics;
	// explicit values select DeepSeek, GLM, Kimi K3 or standard OpenAI.
	ReasoningProtocol string `toml:"reasoning_protocol"`
	// SupportedEfforts lists the /effort levels this provider/model exposes.
	// Non-empty overrides the built-in Kind/BaseURL default; "auto" is implicit.
	// DefaultEffort resolves it, else SupportedEfforts[0].
	SupportedEfforts []string `toml:"supported_efforts"`
	// DefaultEffort is the /effort level used when the user picks "auto" or
	// has not set Effort. Ignored for empty SupportedEfforts or fixed Kimi K3.
	DefaultEffort string `toml:"default_effort"`
	// ModelOverrides customizes capability metadata after ResolveModel selects a
	// concrete model from a multi-model provider, for gateways exposing mixed
	// reasoning/vision models under one base_url/key.
	ModelOverrides map[string]ProviderModelOverride `toml:"model_overrides"`
	visionOverride *bool
	// NoProxy reaches this provider's base_url directly, never through the proxy.
	// For China-only endpoints a foreign-exit proxy resets the TLS handshake (#2803).
	NoProxy bool `toml:"no_proxy"`
	// CacheTTLMinutes overrides the vendor-default prefix-cache retention used by
	// cold-resume prune. Zero uses the vendor default (DeepSeek/unknown 24h, DashScope/Anthropic 5m).
	CacheTTLMinutes int `toml:"cache_ttl_minutes"`
	// IdleTimeoutSeconds overrides the stream idle watchdog: how long a call may
	// send nothing — before headers or between events — before it is dropped.
	// Zero keeps the 300s default; a silent pre-header wait is retried once.
	IdleTimeoutSeconds int `toml:"idle_timeout_seconds"`
}

type ProviderModelOverride struct {
	ReasoningProtocol string   `toml:"reasoning_protocol"`
	SupportedEfforts  []string `toml:"supported_efforts"`
	DefaultEffort     string   `toml:"default_effort"`
	Vision            *bool    `toml:"vision"`
	// ContextWindow overrides the provider-wide context budget for this model.
	// Zero inherits ProviderEntry.ContextWindow so existing configurations keep
	// their current compaction behavior.
	ContextWindow int `toml:"context_window"`
	// MaxOutputTokens overrides the provider-wide output budget. Zero inherits;
	// positive values set a cap and negative values omit optional wire limits.
	MaxOutputTokens int `toml:"max_output_tokens"`
}

// ModelList returns the models this provider exposes: the explicit `models` list,
// or the single `model` as a one-element list (back-compat). Empty if neither set.
func (e *ProviderEntry) ModelList() []string {
	if len(e.Models) > 0 {
		return e.Models
	}
	if e.Model != "" {
		return []string{e.Model}
	}
	return nil
}

// DefaultModel returns the provider's default model: the explicit `default`, else
// the first of ModelList.
func (e *ProviderEntry) DefaultModel() string {
	if e.Default != "" {
		return e.Default
	}
	if l := e.ModelList(); len(l) > 0 {
		return l[0]
	}
	return ""
}

func validateProvider(e ProviderEntry) error {
	switch {
	case strings.TrimSpace(e.Name) == "":
		return fmt.Errorf("provider: name is required")
	case strings.TrimSpace(e.Kind) == "":
		return fmt.Errorf("provider %q: kind is required", e.Name)
	case strings.TrimSpace(e.BaseURL) == "":
		return fmt.Errorf("provider %q: base_url is required", e.Name)
	case !providerHasAnyModel(e):
		return fmt.Errorf("provider %q: model is required", e.Name)
	case strings.TrimSpace(e.APIKeyEnv) != "" && !IsValidCredentialKey(e.APIKeyEnv):
		return fmt.Errorf("provider %q: api_key_env %q is not a valid environment variable name", e.Name, e.APIKeyEnv)
	case e.IdleTimeoutSeconds != 0 && (e.IdleTimeoutSeconds < provider.MinIdleTimeoutSeconds || e.IdleTimeoutSeconds > provider.MaxIdleTimeoutSeconds):
		return fmt.Errorf("provider %q: idle_timeout_seconds must be 0 (use the default) or between %d and %d", e.Name, provider.MinIdleTimeoutSeconds, provider.MaxIdleTimeoutSeconds)
	}
	return nil
}

func providerHasAnyModel(e ProviderEntry) bool {
	if strings.TrimSpace(e.Model) != "" {
		return true
	}
	for _, m := range e.Models {
		if strings.TrimSpace(m) != "" {
			return true
		}
	}
	return false
}
