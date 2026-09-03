package config

import (
	"fmt"
	"strings"

	"reasonix/internal/billing"
)

func renderProviderAccounts(b *strings.Builder, c *Config, scope RenderScope) {
	if c == nil || scope == RenderScopeProject || len(c.ProviderAccounts) == 0 {
		return
	}
	b.WriteString("# Provider accounts are user-global. Project reasonix.toml may reference\n")
	b.WriteString("# generated provider names but must not declare accounts or API keys.\n")
	for _, a := range c.ProviderAccounts {
		b.WriteString("[[provider_accounts]]\n")
		fmt.Fprintf(b, "provider_id = %q\n", a.ProviderID)
		if a.PresetID != "" {
			fmt.Fprintf(b, "preset_id   = %q\n", a.PresetID)
		}
		fmt.Fprintf(b, "id          = %q\n", a.ID)
		fmt.Fprintf(b, "label       = %q\n", a.Label)
		fmt.Fprintf(b, "api_key_env = %q\n", a.APIKeyEnv)
		if a.Enabled != nil {
			fmt.Fprintf(b, "enabled     = %t\n", *a.Enabled)
		}
		if a.Default {
			b.WriteString("default     = true\n")
		}
		if a.Retired {
			b.WriteString("retired     = true\n")
		}
		if routes := normalizeProviderAccountRoutes(a.DisabledRoutes); len(routes) > 0 {
			fmt.Fprintf(b, "disabled_routes = %s\n", renderStringArray(routes))
		}
		b.WriteString("\n")
	}
}

func renderProviderEntries(b *strings.Builder, c *Config) {
	if c == nil {
		return
	}
	for _, p := range c.Providers {
		renderOneProviderEntry(b, p)
	}
}

func renderOneProviderEntry(b *strings.Builder, p ProviderEntry) {
	b.WriteString("[[providers]]\n")
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
	fmt.Fprintf(b, "api_key_env = %q\n", p.APIKeyEnv)
	if p.PresetID != "" {
		fmt.Fprintf(b, "preset_id   = %q   # curated preset identity; settings UI uses it to avoid duplicate installs\n", p.PresetID)
	}
	if p.PresetVersion > 0 {
		fmt.Fprintf(b, "preset_version = %d\n", p.PresetVersion)
	}
	if p.AccountProviderID != "" {
		fmt.Fprintf(b, "account_provider_id = %q\n", p.AccountProviderID)
	}
	if p.AccountID != "" {
		fmt.Fprintf(b, "account_id = %q\n", p.AccountID)
	}
	if p.AccountRouteID != "" {
		fmt.Fprintf(b, "account_route_id = %q\n", p.AccountRouteID)
	}
	if p.AccountLabel != "" {
		fmt.Fprintf(b, "account_label = %q\n", p.AccountLabel)
	}
	renderProviderEntryOptions(b, p)
	b.WriteString("\n")
}

func renderProviderEntryOptions(b *strings.Builder, p ProviderEntry) {
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
	if p.ContextWindow > 0 {
		fmt.Fprintf(b, "context_window = %d   # tokens; compaction triggers near this limit\n", p.ContextWindow)
	}
	if p.MaxOutputTokens != 0 {
		fmt.Fprintf(b, "max_output_tokens = %d   # per-turn total output; 0 = provider auto (official DeepSeek 384K, omit when safe); positive = cost cap; negative = force-omit; never affects compact_ratio\n", p.MaxOutputTokens)
	} else {
		b.WriteString("# max_output_tokens = 0       # recommended: official DeepSeek omits the field (server 384K ceiling)\n")
		b.WriteString("# max_output_tokens = 32768   # optional cost cap\n")
		b.WriteString("# max_output_tokens = 65536   # optional cost cap\n")
		b.WriteString("# max_output_tokens = 131072  # optional cost cap\n")
	}
	if p.Price != nil {
		fmt.Fprintf(b, "price       = %s   # provider-wide fallback, per 1M tokens\n", renderPricingInline(p.Price))
	}
	if len(p.Prices) > 0 {
		fmt.Fprintf(b, "prices      = %s   # per-model prices, per 1M tokens\n", renderPricingMap(p.Prices))
	}
	if cur := strings.TrimSpace(p.BillingCurrency); cur != "" {
		fmt.Fprintf(b, "billing_currency = %q   # frozen list-price currency; independent of display_currency\n", billing.NormalizeCurrency(cur))
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
		fmt.Fprintf(b, "vision_detail = %q   # openai image detail hint: low|high; empty = auto\n", p.VisionDetail)
	}
	if p.WebSearch != nil {
		fmt.Fprintf(b, "web_search  = %t   # provider-executed web_search tool; omitted defaults on for supported official DeepSeek APIs\n", *p.WebSearch)
	}
	if p.ReasoningProtocol != "" {
		fmt.Fprintf(b, "reasoning_protocol = %q   # auto|deepseek|glm|kimi-k3|openai|none; overrides model/endpoint reasoning detection\n", p.ReasoningProtocol)
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
