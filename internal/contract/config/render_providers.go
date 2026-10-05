package config

import (
	"fmt"
	"strings"

	"reasonix/internal/contract/pricing"
)

// renderProviderEntryAnnotated writes one [[providers]] table for the annotated
// scaffold, where each optional field carries an explanatory comment.
func renderProviderEntryAnnotated(b *strings.Builder, p ProviderEntry) {
	fmt.Fprintf(b, "name        = %q\n", p.Name)
	fmt.Fprintf(b, "kind        = %q\n", p.Kind)
	fmt.Fprintf(b, "base_url    = %q\n", p.BaseURL)
	if p.ChatURL != "" {
		fmt.Fprintf(b, "chat_url    = %q   # legacy OpenAI chat endpoint override\n", p.ChatURL)
	}
	if p.RequestURL != "" {
		fmt.Fprintf(b, "request_url = %q   # exact provider request URL; no path completion\n", p.RequestURL)
	}
	if len(p.Models) > 0 {
		fmt.Fprintf(b, "models      = %s\n", renderStringArray(p.Models))
		if p.Default != "" {
			fmt.Fprintf(b, "default     = %q\n", p.Default)
		}
	} else if p.Model != "" {
		fmt.Fprintf(b, "model       = %q\n", p.Model)
	}
	if p.ModelsURL != "" {
		fmt.Fprintf(b, "models_url  = %q   # auto-fetch models from this URL on startup\n", p.ModelsURL)
	}
	renderProviderIdentity(b, p.APIKeyEnv, p.DisplayName)
	if p.PresetID != "" {
		fmt.Fprintf(b, "preset_id   = %q   # curated preset identity; settings UI uses it to avoid duplicate installs\n", p.PresetID)
	}
	if p.PresetVersion > 0 {
		fmt.Fprintf(b, "preset_version = %d\n", p.PresetVersion)
	}
	if len(p.Headers) > 0 {
		fmt.Fprintf(b, "headers     = %s   # extra static request headers; keep secrets in api_key_env\n", renderStringMap(p.Headers))
	}
	if len(p.ExtraBody) > 0 {
		fmt.Fprintf(b, "extra_body  = %s   # extra top-level JSON request body fields for compatible gateways\n", renderAnyMap(p.ExtraBody))
	}
	if p.AuthHeader {
		b.WriteString("auth_header = true   # Anthropic-compatible: send Authorization: Bearer <api_key> instead of x-api-key\n")
	}
	if p.ResponsesMode != "" {
		fmt.Fprintf(b, "responses_mode = %q   # responses provider: stateless|stateful\n", p.ResponsesMode)
	}
	if p.ResponsesStateful != nil {
		fmt.Fprintf(b, "responses_stateful = %t   # legacy responses mode switch\n", *p.ResponsesStateful)
	}
	if p.BalanceURL != "" {
		fmt.Fprintf(b, "balance_url = %q   # optional; wallet-balance endpoint shown in the status bar\n", p.BalanceURL)
	}
	renderProviderEntryAnnotatedTuning(b, p)
}

func renderProviderEntryAnnotatedTuning(b *strings.Builder, p ProviderEntry) {
	if p.ContextWindow > 0 {
		fmt.Fprintf(b, "context_window = %d   # tokens; compaction triggers near this limit\n", p.ContextWindow)
	}
	if p.MaxOutputTokens != 0 {
		fmt.Fprintf(b, "max_output_tokens = %d   # per-turn total output; 0 = auto (~64K on DeepSeek high), 32768/65536/131072 explicit; never affects compact_ratio\n", p.MaxOutputTokens)
	} else {
		b.WriteString("# max_output_tokens = 0       # recommended: automatic (DeepSeek default high → ~64K; not unlimited)\n")
		b.WriteString("# max_output_tokens = 32768   # ordinary coding / cost control\n")
		b.WriteString("# max_output_tokens = 65536   # heavy reasoning / long tool loops\n")
		b.WriteString("# max_output_tokens = 131072  # only after repeated finish_reason=length\n")
	}
	if p.IdleTimeoutSeconds > 0 {
		fmt.Fprintf(b, "idle_timeout_seconds = %d   # how long this provider may send nothing before the call is read as dropped\n", p.IdleTimeoutSeconds)
	} else {
		b.WriteString("# idle_timeout_seconds = 300   # per-provider stream idle watchdog; default 300s\n")
	}
	if p.Price != nil {
		fmt.Fprintf(b, "price       = %s   # provider-wide fallback, per 1M tokens\n", renderPricingInline(p.Price))
	}
	if len(p.Prices) > 0 {
		fmt.Fprintf(b, "prices      = %s   # per-model prices, per 1M tokens\n", renderPricingMap(p.Prices))
	}
	if cur := strings.TrimSpace(p.BillingCurrency); cur != "" {
		fmt.Fprintf(b, "billing_currency = %q   # frozen list-price currency; independent of display_currency\n", pricing.NormalizeCurrency(cur))
	}
	if mode := strings.TrimSpace(p.BillingMode); mode != "" && mode != "payg" {
		fmt.Fprintf(b, "billing_mode = %q   # payg|subscription_equivalent\n", mode)
	}
	if p.Thinking != "" {
		fmt.Fprintf(b, "thinking    = %q\n", p.Thinking)
	}
	if p.Effort != "" {
		fmt.Fprintf(b, "effort      = %q\n", p.Effort)
	}
	if p.Vision {
		b.WriteString("vision      = true   # provider accepts image input for all listed models\n")
	}
	if p.VisionModels != nil {
		fmt.Fprintf(b, "vision_models = %s   # models in this provider that accept image input\n", renderStringArray(p.VisionModels))
	}
	if p.VisionDetail != "" {
		fmt.Fprintf(b, "vision_detail = %q   # openai image detail hint: low|high (DeepSeek also original); empty = auto\n", p.VisionDetail)
	}
	if p.WebSearch != nil {
		fmt.Fprintf(b, "web_search  = %t   # provider-executed web_search tool; omitted defaults on for supported official DeepSeek APIs\n", *p.WebSearch)
	}
	if p.ReasoningProtocol != "" {
		fmt.Fprintf(b, "reasoning_protocol = %q   # auto|anthropic|deepseek|glm|kimi-k3|openai|none; overrides model/endpoint reasoning detection\n", p.ReasoningProtocol)
	}
	if len(p.SupportedEfforts) > 0 {
		fmt.Fprintf(b, "supported_efforts = %s   # custom /effort levels exposed by this provider; overrides the built-in Kind/BaseURL default\n", renderStringArray(p.SupportedEfforts))
	}
	if p.DefaultEffort != "" {
		fmt.Fprintf(b, "default_effort    = %q   # used when /effort is auto or unset; must be one of supported_efforts\n", p.DefaultEffort)
	}
	if len(p.ModelOverrides) > 0 {
		fmt.Fprintf(b, "model_overrides   = %s   # per-model context/output/reasoning/vision overrides for mixed gateways\n", renderModelOverrides(p.ModelOverrides))
	}
	if p.NoProxy {
		b.WriteString("no_proxy    = true   # reach this base_url directly, never via the proxy\n")
	}
}

// renderProviderEntryPlain writes one [[providers]] table with no comments, for
// the project-delta rewrite the settings UI round-trips.
func renderProviderEntryPlain(b *strings.Builder, p ProviderEntry) {
	fmt.Fprintf(b, "name        = %q\n", p.Name)
	fmt.Fprintf(b, "kind        = %q\n", p.Kind)
	fmt.Fprintf(b, "base_url    = %q\n", p.BaseURL)
	if p.ChatURL != "" {
		fmt.Fprintf(b, "chat_url    = %q\n", p.ChatURL)
	}
	if p.RequestURL != "" {
		fmt.Fprintf(b, "request_url = %q\n", p.RequestURL)
	}
	if len(p.Models) > 0 {
		fmt.Fprintf(b, "models      = %s\n", renderStringArray(p.Models))
		if p.Default != "" {
			fmt.Fprintf(b, "default     = %q\n", p.Default)
		}
	} else if p.Model != "" {
		fmt.Fprintf(b, "model       = %q\n", p.Model)
	}
	if p.ModelsURL != "" {
		fmt.Fprintf(b, "models_url  = %q\n", p.ModelsURL)
	}
	renderProviderIdentity(b, p.APIKeyEnv, p.DisplayName)
	if p.PresetID != "" {
		fmt.Fprintf(b, "preset_id   = %q\n", p.PresetID)
	}
	if p.PresetVersion > 0 {
		fmt.Fprintf(b, "preset_version = %d\n", p.PresetVersion)
	}
	if len(p.Headers) > 0 {
		fmt.Fprintf(b, "headers     = %s\n", renderStringMap(p.Headers))
	}
	if len(p.ExtraBody) > 0 {
		fmt.Fprintf(b, "extra_body  = %s\n", renderAnyMap(p.ExtraBody))
	}
	if p.AuthHeader {
		b.WriteString("auth_header = true\n")
	}
	if p.ResponsesMode != "" {
		fmt.Fprintf(b, "responses_mode = %q\n", p.ResponsesMode)
	}
	if p.ResponsesStateful != nil {
		fmt.Fprintf(b, "responses_stateful = %t\n", *p.ResponsesStateful)
	}
	if p.BalanceURL != "" {
		fmt.Fprintf(b, "balance_url = %q\n", p.BalanceURL)
	}
	renderProviderEntryPlainTuning(b, p)
}

func renderProviderEntryPlainTuning(b *strings.Builder, p ProviderEntry) {
	if p.ContextWindow > 0 {
		fmt.Fprintf(b, "context_window = %d\n", p.ContextWindow)
	}
	if p.MaxOutputTokens != 0 {
		fmt.Fprintf(b, "max_output_tokens = %d\n", p.MaxOutputTokens)
	}
	if p.IdleTimeoutSeconds > 0 {
		fmt.Fprintf(b, "idle_timeout_seconds = %d\n", p.IdleTimeoutSeconds)
	}
	renderPerseverationRetries(b, p.PerseverationRetries, nil)
	if p.Price != nil {
		fmt.Fprintf(b, "price       = %s\n", renderPricingInline(p.Price))
	}
	if len(p.Prices) > 0 {
		fmt.Fprintf(b, "prices      = %s\n", renderPricingMap(p.Prices))
	}
	if cur := strings.TrimSpace(p.BillingCurrency); cur != "" {
		fmt.Fprintf(b, "billing_currency = %q\n", pricing.NormalizeCurrency(cur))
	}
	if mode := strings.TrimSpace(p.BillingMode); mode != "" && mode != "payg" {
		fmt.Fprintf(b, "billing_mode = %q\n", mode)
	}
	if p.Thinking != "" {
		fmt.Fprintf(b, "thinking    = %q\n", p.Thinking)
	}
	if p.Effort != "" {
		fmt.Fprintf(b, "effort      = %q\n", p.Effort)
	}
	if p.Vision {
		b.WriteString("vision      = true\n")
	}
	if p.VisionModels != nil {
		fmt.Fprintf(b, "vision_models = %s\n", renderStringArray(p.VisionModels))
	}
	if p.VisionDetail != "" {
		fmt.Fprintf(b, "vision_detail = %q\n", p.VisionDetail)
	}
	if p.WebSearch != nil {
		fmt.Fprintf(b, "web_search  = %t\n", *p.WebSearch)
	}
	if p.ReasoningProtocol != "" {
		fmt.Fprintf(b, "reasoning_protocol = %q\n", p.ReasoningProtocol)
	}
	if len(p.SupportedEfforts) > 0 {
		fmt.Fprintf(b, "supported_efforts = %s\n", renderStringArray(p.SupportedEfforts))
	}
	if p.DefaultEffort != "" {
		fmt.Fprintf(b, "default_effort    = %q\n", p.DefaultEffort)
	}
	if len(p.ModelOverrides) > 0 {
		fmt.Fprintf(b, "model_overrides   = %s\n", renderModelOverrides(p.ModelOverrides))
	}
	if p.NoProxy {
		b.WriteString("no_proxy    = true\n")
	}
}
