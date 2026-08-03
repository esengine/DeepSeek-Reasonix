package config

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"reasonix/internal/provider"
)

type RenderScope string

const (
	RenderScopeFull    RenderScope = "full"
	RenderScopeUser    RenderScope = "user"
	RenderScopeProject RenderScope = "project"
)

// RenderTOML renders the config as annotated TOML in the `reasonix setup` house style:
// comments preserved, system_prompt as a multi-line string, helpful hints. The
// output round-trips back through Load (see render_test.go).
func RenderTOML(c *Config) string {
	return RenderTOMLForScope(c, RenderScopeFull)
}

// RenderTOMLForScope renders an annotated TOML file for a specific persistence
// target. User configs can carry desktop and account-level preferences; project
// reasonix.toml stays focused on project behavior and intentionally excludes
// desktop-only preferences.
//
// The returned string is empty if a value could not be TOML-encoded (for
// example invalid UTF-8); write paths must use renderTOMLForScopeErr so the
// encoding failure can be surfaced instead of persisted.
func RenderTOMLForScope(c *Config, scope RenderScope) string {
	out, err := renderTOMLForScopeErr(c, scope)
	if err != nil {
		return ""
	}
	return out
}

// renderTOMLForScopeErr renders the same TOML as RenderTOMLForScope and also
// reports encoding failures. The validated config write pipeline uses this
// entry point so it never persists output that would not parse back.
func renderTOMLForScopeErr(c *Config, scope RenderScope) (string, error) {
	r := &tomlRenderer{}
	body := renderTOMLInto(r, c, scope)
	if r.err != nil {
		return "", r.err
	}
	return body, nil
}

func renderTOMLInto(r *tomlRenderer, c *Config, scope RenderScope) string {
	if c == nil {
		c = Default()
	}
	switch scope {
	case RenderScopeUser, RenderScopeProject:
	default:
		scope = RenderScopeFull
	}
	if scope == RenderScopeProject {
		c = projectScopedConfigForRender(c)
	}
	defaults := Default()
	var b strings.Builder

	b.WriteString("# Reasonix configuration.\n")
	fmt.Fprintf(&b, "# Resolution order: flag > ./reasonix.toml > %s > built-in defaults.\n", userConfigDisplayPath())
	b.WriteString("# Fields marked user/global only are not overridden by ./reasonix.toml.\n")
	b.WriteString("# Secrets are named via api_key_env and stored in Reasonix's global .env; never put keys here.\n\n")

	fmt.Fprintf(&b, "config_version = %d   # schema marker for diagnostics; old versions may ignore it\n", configVersion(c))
	fmt.Fprintf(&b, "default_model = %s\n", r.q(c.DefaultModel))
	if c.Language != "" {
		fmt.Fprintf(&b, "language      = %s   # ui/model language; empty = auto-detect from $LANG / $REASONIX_LANG\n", r.q(c.Language))
	} else {
		b.WriteString("# language      = \"zh\"   # ui/model language; empty = auto-detect from $LANG / $REASONIX_LANG\n")
	}
	if scope != RenderScopeProject {
		fmt.Fprintf(&b, "credentials_store = %s   # legacy compatibility; provider keys are saved in Reasonix's global .env\n", r.q(normalizeCredentialsStore(c.CredentialsStore)))
	}
	b.WriteString("\n")

	if shouldRenderUI(c, defaults, scope) {
		b.WriteString("[ui]\n")
		fmt.Fprintf(&b, "theme = %s   # auto|dark|light; CLI colors only; REASONIX_THEME can override per run\n", r.q(c.UITheme()))
		if style := c.UIThemeStyle(); style != "" {
			fmt.Fprintf(&b, "theme_style = %s   # CLI accent palette; REASONIX_THEME_STYLE can override per run\n", r.q(style))
		} else {
			b.WriteString("# theme_style = \"graphite\"   # graphite|aurora|slate|carbon|nocturne|amber and legacy aliases\n")
		}
		if layout := c.UIShortcutLayout(); layout != "classic" {
			fmt.Fprintf(&b, "shortcut_layout = %s   # classic|desktop; compatibility setting; Shift+Tab toggles Plan, Ctrl+Y toggles YOLO\n", r.q(layout))
		} else {
			b.WriteString("# shortcut_layout = \"desktop\"   # classic|desktop; compatibility setting; Shift+Tab toggles Plan, Ctrl+Y toggles YOLO\n")
		}
		if strings.TrimSpace(c.UI.CursorShape) != "" {
			fmt.Fprintf(&b, "cursor_shape = %s   # block|underline|bar; text input cursor shape\n", r.q(c.UICursorShape()))
		} else {
			b.WriteString("# cursor_shape = \"bar\"   # block|underline|bar; text input cursor shape\n")
		}
		if strings.TrimSpace(c.UI.CloseBehavior) != "" && scope == RenderScopeProject {
			fmt.Fprintf(&b, "close_behavior = %s   # legacy desktop close behavior; prefer [desktop].close_behavior in user config\n", r.q(c.DesktopCloseBehavior()))
		}
		if c.UI.ShowReasoning {
			b.WriteString("show_reasoning = true   # CLI: show thinking text by default; false = collapsed (toggle with Ctrl+O)\n")
		} else {
			b.WriteString("# show_reasoning = true   # CLI: show thinking text by default; false = collapsed (toggle with Ctrl+O)\n")
		}
		b.WriteString("\n")
	}

	if scope != RenderScopeProject {
		b.WriteString("[desktop]\n")
		if lang := c.DesktopLanguage(); lang != "" {
			fmt.Fprintf(&b, "language = %s   # desktop UI language; empty/auto = browser/OS auto-detect\n", r.q(lang))
		} else {
			b.WriteString("# language = \"zh\"   # desktop UI language; empty/auto = browser/OS auto-detect\n")
		}
		if currency := c.DesktopCurrency(); currency != "" {
			fmt.Fprintf(&b, "currency = %s   # official pricing currency: CNY|USD; empty/auto follows language\n", r.q(currency))
		} else {
			b.WriteString("# currency = \"USD\"   # official pricing currency: CNY|USD; empty/auto follows language\n")
		}
		fmt.Fprintf(&b, "layout_style = %s   # desktop layout: classic|workbench|creation\n", r.q(c.DesktopLayoutStyle()))
		fmt.Fprintf(&b, "theme = %s   # desktop only: auto|dark|light\n", r.q(c.DesktopTheme()))
		if style := c.DesktopThemeStyle(); style != "" {
			fmt.Fprintf(&b, "theme_style = %s   # desktop accent palette\n", r.q(style))
		} else {
			b.WriteString("# theme_style = \"graphite\"   # graphite|aurora|slate|carbon|nocturne|amber and legacy aliases\n")
		}
		if opener := c.DesktopExternalOpener(); opener != "" {
			fmt.Fprintf(&b, "external_opener = %s   # desktop Open control: installed application id\n", r.q(opener))
		} else {
			b.WriteString("# external_opener = \"vscode\"   # desktop Open control: installed application id\n")
		}
		fmt.Fprintf(&b, "close_behavior = %s   # desktop: quit|background when the window close button is clicked\n", r.q(c.DesktopCloseBehavior()))
		fmt.Fprintf(&b, "status_bar_style = %s   # desktop: icon|text metric labels in the bottom status bar\n", r.q(c.DesktopStatusBarStyle()))
		fmt.Fprintf(&b, "status_bar_items = %s   # desktop: ordered visible bottom status bar items\n", r.stringArray(c.DesktopStatusBarItems()))
		fmt.Fprintf(&b, "default_tool_approval_mode = %s   # desktop: Ask/Auto/YOLO default for newly-created sessions\n", r.q(c.DesktopDefaultToolApprovalMode()))
		fmt.Fprintf(&b, "check_updates = %v   # desktop: check for new versions on startup\n", c.DesktopCheckUpdates())
		fmt.Fprintf(&b, "telemetry = %v   # desktop: anonymous launch ping + scrubbed next-launch native crash diagnostics; never content\n", c.DesktopTelemetry())
		fmt.Fprintf(&b, "metrics = %v   # desktop: aggregate quality/lifecycle metrics (anonymous signal/bucket counts); never content\n", c.DesktopMetrics())
		// A non-nil empty slice is intentional: provider_access = [] means the
		// user removed every desktop access entry. Omitting it would make the next
		// load treat the config as legacy and infer access again.
		if c.Desktop.ProviderAccess != nil {
			fmt.Fprintf(&b, "provider_access = %s   # desktop settings: providers shown on Settings > Model > Access\n", r.stringArray(c.Desktop.ProviderAccess))
		}
		fmt.Fprintf(&b, "expand_thinking = %v   # desktop: show reasoning text expanded by default; false = collapsed\n", c.Desktop.ExpandThinking)
		fmt.Fprintf(&b, "display_mode = %s   # desktop: standard|compact transcript display mode\n", r.q(c.DesktopDisplayMode()))
		if width := c.DesktopConversationWidth(); width == "full" {
			fmt.Fprintf(&b, "conversation_width = %s   # desktop: standard|full transcript width; empty = standard\n", r.q(width))
		}
		b.WriteString("\n")
	} else if c.Desktop.ProviderAccess != nil {
		// provider_access is intentionally mergeable across user and project
		// configs. It is the only desktop field written to reasonix.toml: local
		// providers then appear in that workspace's desktop model switcher without
		// copying user-global appearance or security preferences into the project.
		b.WriteString("[desktop]\n")
		fmt.Fprintf(&b, "provider_access = %s   # providers available to this workspace in the desktop model switcher\n\n", r.stringArray(c.Desktop.ProviderAccess))
	}

	if scope != RenderScopeProject {
		if c.CLITelemetryConfigured() {
			b.WriteString("[telemetry]\n")
			fmt.Fprintf(&b, "cli_metrics = %s   # CLI content-free usage metrics: auto|on|off; auto requires a local interactive terminal\n\n", r.q(c.CLITelemetryMode()))
		}

		b.WriteString("[notifications]\n")
		fmt.Fprintf(&b, "enabled = %v   # system notifications for CLI and desktop turns; default off\n", c.Notifications.Enabled)
		fmt.Fprintf(&b, "turn_done = %v   # notify when a turn finishes\n", c.Notifications.TurnDone)
		fmt.Fprintf(&b, "approval_request = %v   # notify when a tool approval is waiting\n", c.Notifications.ApprovalRequest)
		fmt.Fprintf(&b, "ask_request = %v   # notify when a question is waiting\n", c.Notifications.AskRequest)
		b.WriteString("\n")
	}

	if shouldRenderNetwork(c, defaults, scope) {
		b.WriteString("[network]\n")
		fmt.Fprintf(&b, "proxy_mode = %s   # auto|env|custom|off; auto currently uses env proxy\n", r.q(c.NetworkProxyMode()))
		if c.Network.ProxyURL != "" {
			fmt.Fprintf(&b, "proxy_url  = %s   # custom override, e.g. socks5://127.0.0.1:7890\n", r.q(c.Network.ProxyURL))
		} else {
			b.WriteString("# proxy_url  = \"socks5://127.0.0.1:7890\"   # optional custom override\n")
		}
		if c.Network.NoProxy != "" {
			fmt.Fprintf(&b, "no_proxy   = %s   # honored for proxy_mode = \"custom\"\n", r.q(c.Network.NoProxy))
		} else {
			b.WriteString("# no_proxy   = \"localhost,127.0.0.1,.local\"   # honored for proxy_mode = \"custom\"\n")
		}
		b.WriteString("\n[network.proxy]\n")
		proxyType := c.Network.Proxy.Type
		if proxyType == "" {
			proxyType = "socks5"
		}
		fmt.Fprintf(&b, "type = %s   # http|https|socks5|socks5h\n", r.q(proxyType))
		if c.Network.Proxy.Server != "" {
			fmt.Fprintf(&b, "server = %s\n", r.q(c.Network.Proxy.Server))
		} else {
			b.WriteString("# server = \"127.0.0.1\"\n")
		}
		if c.Network.Proxy.Port > 0 {
			fmt.Fprintf(&b, "port = %d\n", c.Network.Proxy.Port)
		} else {
			b.WriteString("# port = 7890\n")
		}
		if c.Network.Proxy.Username != "" {
			fmt.Fprintf(&b, "username = %s\n", r.q(c.Network.Proxy.Username))
		} else {
			b.WriteString("# username = \"\"\n")
		}
		if c.Network.Proxy.Password != "" {
			fmt.Fprintf(&b, "password = %s   # supports ${VAR} expansion\n", r.q(c.Network.Proxy.Password))
		} else {
			b.WriteString("# password = \"${REASONIX_PROXY_PASSWORD}\"   # optional; supports ${VAR} expansion\n")
		}
		b.WriteString("\n")
	}
	if shouldRenderEnvironment(c, defaults, scope) {
		r.environmentConfig(&b, c.Environment)
	}

	b.WriteString("[agent]\n")
	if shouldRenderSystemPrompt(c, defaults, scope) {
		b.WriteString("system_prompt = \"\"\"\n")
		b.WriteString(c.Agent.SystemPrompt)
		b.WriteString("\"\"\"\n")
	} else {
		b.WriteString("# system_prompt = \"\"\"...\"\"\"   # omit to use the built-in prompt for this version\n")
	}
	if c.Agent.SystemPromptFile != "" {
		fmt.Fprintf(&b, "system_prompt_file = %s\n", r.q(c.Agent.SystemPromptFile))
	} else {
		b.WriteString("# system_prompt_file = \"prompts/system.md\"   # overrides system_prompt when set\n")
	}
	fmt.Fprintf(&b, "temperature       = %s\n", formatFloat(c.Agent.Temperature))
	if strings.TrimSpace(c.Agent.RecoveryModel) != "" {
		fmt.Fprintf(&b, "recovery_model = %s   # optional independent reviewer for low-risk automatic recovery\n", r.q(c.Agent.RecoveryModel))
	} else {
		b.WriteString("# recovery_model = \"deepseek-pro\"   # optional; falls back to guardian then main model\n")
	}
	if lang := c.ReasoningLanguage(); lang != "auto" {
		fmt.Fprintf(&b, "reasoning_language = %s   # visible reasoning language: auto|zh|en\n", r.q(lang))
	} else {
		b.WriteString("# reasoning_language = \"zh\"   # visible reasoning language: auto|zh|en\n")
	}
	fmt.Fprintf(&b, "soft_compact_ratio  = %s   # notice only; keeps cache-first prefix intact\n", formatFloat(c.Agent.SoftCompactRatio))
	fmt.Fprintf(&b, "tool_result_snip_ratio = %s   # snip stale tool results at this fraction before summary compaction\n", formatFloat(c.Agent.ToolResultSnipRatio))
	fmt.Fprintf(&b, "compact_ratio       = %s   # try compacting when prompt reaches this fraction\n", formatFloat(c.Agent.CompactRatio))
	fmt.Fprintf(&b, "compact_force_ratio = %s   # force compacting at this high-water mark\n", formatFloat(c.Agent.CompactForceRatio))
	if c.Agent.Keep != nil {
		fmt.Fprintf(&b, "keep                = %s   # compaction keep policy: errors, user_marked\n", r.stringArray(c.Agent.Keep))
	} else {
		b.WriteString("# keep                = [\"errors\"]   # compaction keep policy: errors, user_marked\n")
	}
	if c.Agent.RecentKeep > 0 {
		fmt.Fprintf(&b, "recent_keep         = %d   # minimum recent messages kept verbatim\n", c.Agent.RecentKeep)
	} else {
		b.WriteString("# recent_keep         = 2   # minimum recent messages kept verbatim\n")
	}
	fmt.Fprintf(&b, "cold_resume_prune   = %v   # elide stale tool results when reopening a session past the provider cache window\n", c.ColdResumePruneEnabled())
	if len(c.Agent.PlanModeReadOnlyCommands) > 0 {
		fmt.Fprintf(&b, "plan_mode_read_only_commands = %s   # legacy compatibility only; Plan bash uses Permissions\n", r.stringArray(c.Agent.PlanModeReadOnlyCommands))
	} else {
		b.WriteString("# plan_mode_read_only_commands = [\"gh issue view\"]   # legacy compatibility only; Plan bash uses Permissions\n")
	}
	if c.Agent.PlannerModel != "" {
		fmt.Fprintf(&b, "planner_model = %s   # low-frequency planner (two-model collaboration)\n", r.q(c.Agent.PlannerModel))
	} else {
		b.WriteString("# planner_model = \"deepseek-pro\"   # optional: enable two-model collaboration\n")
	}
	if c.Agent.SubagentModel != "" {
		fmt.Fprintf(&b, "subagent_model = %s   # default model for runAs=subagent skills\n", r.q(c.Agent.SubagentModel))
	} else {
		b.WriteString("# subagent_model = \"deepseek-pro\"   # optional default for runAs=subagent skills\n")
	}
	if len(c.Agent.SubagentModels) > 0 {
		fmt.Fprintf(&b, "subagent_models = %s   # per-skill overrides\n", r.stringMap(c.Agent.SubagentModels))
	} else {
		b.WriteString("# subagent_models = { review = \"deepseek-pro\", security_review = \"deepseek-pro\" }   # per-skill overrides\n")
	}
	if c.Agent.SubagentEffort != "" {
		fmt.Fprintf(&b, "subagent_effort = %s   # default effort for subagent entry points\n", r.q(c.Agent.SubagentEffort))
	} else {
		b.WriteString("# subagent_effort = \"high\"   # optional default effort for subagents\n")
	}
	if len(c.Agent.SubagentEfforts) > 0 {
		fmt.Fprintf(&b, "subagent_efforts = %s   # per-tool/skill effort overrides\n", r.stringMap(c.Agent.SubagentEfforts))
	} else {
		b.WriteString("# subagent_efforts = { review = \"max\", task = \"high\" }   # per-tool/skill effort overrides\n")
	}
	if c.Agent.MaxSubagentDepth != defaults.Agent.MaxSubagentDepth {
		fmt.Fprintf(&b, "max_subagent_depth = %d   # nested subagent delegation depth; 1 restores the old single-layer boundary\n", c.Agent.MaxSubagentDepth)
	} else {
		b.WriteString("# max_subagent_depth = 2   # nested subagent delegation depth; set 1 to disable nested delegation\n")
	}
	if c.Agent.MaxSubagentConcurrency != defaults.Agent.MaxSubagentConcurrency {
		fmt.Fprintf(&b, "max_subagent_concurrency = %d   # session-wide sub-agent concurrency (task/fleet/skills)\n", c.Agent.MaxSubagentConcurrency)
	} else {
		b.WriteString("# max_subagent_concurrency = 6   # session-wide sub-agent concurrency (task/fleet/skills)\n")
	}
	if c.Agent.MaxParallelWriters != defaults.Agent.MaxParallelWriters {
		fmt.Fprintf(&b, "max_parallel_writers = %d   # concurrent writers with non-overlapping write_paths\n", c.Agent.MaxParallelWriters)
	} else {
		b.WriteString("# max_parallel_writers = 3   # concurrent writers with non-overlapping write_paths\n")
	}
	if c.Agent.OutputStyle != "" {
		fmt.Fprintf(&b, "output_style = %s   # persona/tone folded into the prompt\n", r.q(c.Agent.OutputStyle))
	} else {
		b.WriteString("# output_style = \"explanatory\"   # explanatory | learning | concise | custom; empty = default\n")
	}
	b.WriteString("\n")

	if shouldRenderProviders(c, defaults, scope) {
		for _, p := range c.Providers {
			b.WriteString("[[providers]]\n")
			fmt.Fprintf(&b, "name        = %s\n", r.q(p.Name))
			fmt.Fprintf(&b, "kind        = %s\n", r.q(p.Kind))
			fmt.Fprintf(&b, "base_url    = %s\n", r.q(p.BaseURL))
			if p.ChatURL != "" {
				fmt.Fprintf(&b, "chat_url    = %s   # optional full chat completions URL; disables automatic /chat/completions suffix\n", r.q(p.ChatURL))
			}
			if len(p.Models) > 0 {
				fmt.Fprintf(&b, "models      = %s\n", r.stringArray(p.Models))
				if p.Default != "" {
					fmt.Fprintf(&b, "default     = %s\n", r.q(p.Default))
				}
			} else if p.Model != "" {
				fmt.Fprintf(&b, "model       = %s\n", r.q(p.Model))
			}
			if p.ModelsURL != "" {
				fmt.Fprintf(&b, "models_url  = %s   # auto-fetch models from this URL on startup\n", r.q(p.ModelsURL))
			}
			fmt.Fprintf(&b, "api_key_env = %s\n", r.q(p.APIKeyEnv))
			if p.PresetID != "" {
				fmt.Fprintf(&b, "preset_id   = %s   # curated preset identity; settings UI uses it to avoid duplicate installs\n", r.q(p.PresetID))
			}
			if p.PresetVersion > 0 {
				fmt.Fprintf(&b, "preset_version = %d\n", p.PresetVersion)
			}
			if len(p.Headers) > 0 {
				fmt.Fprintf(&b, "headers     = %s   # extra static request headers; keep secrets in api_key_env\n", r.stringMap(p.Headers))
			}
			if len(p.ExtraBody) > 0 {
				fmt.Fprintf(&b, "extra_body  = %s   # extra top-level JSON request body fields for compatible gateways\n", r.anyMap(p.ExtraBody))
			}
			if p.AuthHeader {
				b.WriteString("auth_header = true   # Anthropic-compatible: send Authorization: Bearer <api_key> instead of x-api-key\n")
			}
			if p.ResponsesMode != "" {
				fmt.Fprintf(&b, "responses_mode = %s   # responses provider: stateless|stateful\n", r.q(p.ResponsesMode))
			}
			if p.ResponsesStateful != nil {
				fmt.Fprintf(&b, "responses_stateful = %t   # legacy responses mode switch\n", *p.ResponsesStateful)
			}
			if p.BalanceURL != "" {
				fmt.Fprintf(&b, "balance_url = %s   # optional; wallet-balance endpoint shown in the status bar\n", r.q(p.BalanceURL))
			}
			if p.ContextWindow > 0 {
				fmt.Fprintf(&b, "context_window = %d   # tokens; compaction triggers near this limit\n", p.ContextWindow)
			}
			if p.MaxOutputTokens != 0 {
				fmt.Fprintf(&b, "max_output_tokens = %d   # total output cap; 0 = provider default, negative = omit when optional\n", p.MaxOutputTokens)
			}
			if p.Price != nil {
				fmt.Fprintf(&b, "price       = %s   # provider-wide fallback, per 1M tokens\n", r.pricingInline(p.Price))
			}
			if len(p.Prices) > 0 {
				fmt.Fprintf(&b, "prices      = %s   # per-model prices, per 1M tokens\n", r.pricingMap(p.Prices))
			}
			if p.Thinking != "" {
				fmt.Fprintf(&b, "thinking    = %s\n", r.q(p.Thinking))
			}
			if p.Effort != "" {
				fmt.Fprintf(&b, "effort      = %s\n", r.q(p.Effort))
			}
			if p.Vision {
				b.WriteString("vision      = true   # provider accepts image input for all listed models\n")
			}
			if p.VisionModels != nil {
				fmt.Fprintf(&b, "vision_models = %s   # models in this provider that accept image input\n", r.stringArray(p.VisionModels))
			}
			if p.VisionDetail != "" {
				fmt.Fprintf(&b, "vision_detail = %s   # openai image detail hint: low|high; empty = auto\n", r.q(p.VisionDetail))
			}
			if p.ReasoningProtocol != "" {
				fmt.Fprintf(&b, "reasoning_protocol = %s   # auto|deepseek|glm|openai|none; overrides model/endpoint reasoning detection\n", r.q(p.ReasoningProtocol))
			}
			if len(p.SupportedEfforts) > 0 {
				fmt.Fprintf(&b, "supported_efforts = %s   # custom /effort levels exposed by this provider; overrides the built-in Kind/BaseURL default\n", r.stringArray(p.SupportedEfforts))
			}
			if p.DefaultEffort != "" {
				fmt.Fprintf(&b, "default_effort    = %s   # used when /effort is auto or unset; must be one of supported_efforts\n", r.q(p.DefaultEffort))
			}
			if len(p.ModelOverrides) > 0 {
				fmt.Fprintf(&b, "model_overrides   = %s   # per-model context/output/reasoning/vision overrides for mixed gateways\n", r.modelOverrides(p.ModelOverrides))
			}
			if p.NoProxy {
				b.WriteString("no_proxy    = true   # reach this base_url directly, never via the proxy\n")
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("[tools]\n")
	if len(c.Tools.Enabled) == 0 {
		b.WriteString("enabled = []   # empty = all built-in tools\n")
	} else {
		b.WriteString("enabled = [")
		for i, t := range c.Tools.Enabled {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s", r.q(t))
		}
		b.WriteString("]\n")
	}
	fmt.Fprintf(&b, "bash_timeout_seconds = %d   # foreground safety cap; set 0 for no tool-local cap\n", c.BashTimeoutSeconds())
	fmt.Fprintf(&b, "mcp_startup_timeout_seconds = %d   # background initialize + tools/list safety cap; per-plugin overrides may raise it\n", c.MCPStartupTimeoutSeconds())
	fmt.Fprintf(&b, "mcp_call_timeout_seconds = %d   # default MCP call safety cap; per-plugin/tool overrides may raise it\n\n", c.MCPCallTimeoutSeconds())

	b.WriteString("[tools.background_jobs]\n")
	fmt.Fprintf(&b, "stalled_warning_seconds = %d   # warn once per background job after this many quiet seconds; 0 disables\n\n", c.BackgroundJobStalledWarningSeconds())

	b.WriteString("[tools.shell]\n")
	if c.Tools.Shell.Prefer != "" {
		fmt.Fprintf(&b, "prefer = %s   # auto|bash|powershell|pwsh; empty/default = auto-detect\n", r.q(c.Tools.Shell.Prefer))
	} else {
		b.WriteString("# prefer = \"auto\"   # auto|bash|powershell|pwsh; empty/default = auto-detect\n")
	}
	if c.Tools.Shell.Path != "" {
		fmt.Fprintf(&b, "path   = %s   # absolute path to the shell executable; empty = PATH lookup\n\n", r.q(c.Tools.Shell.Path))
	} else {
		b.WriteString("# path   = \"/opt/homebrew/bin/bash\"   # absolute path to the shell executable; empty = PATH lookup\n\n")
	}

	r.lspConfig(&b, c.LSP)

	b.WriteString("[skills]\n")
	if len(c.Skills.Paths) > 0 {
		fmt.Fprintf(&b, "paths = %s   # extra custom skill roots\n", r.stringArray(c.Skills.Paths))
	} else {
		b.WriteString("# paths = [\"~/my-skills\", \"../shared/skills\"]   # extra custom skill roots\n")
	}
	if len(c.Skills.ExcludedPaths) > 0 {
		fmt.Fprintf(&b, "excluded_paths = %s   # skill roots hidden from discovery\n", r.stringArray(c.Skills.ExcludedPaths))
	} else {
		b.WriteString("# excluded_paths = [\"~/.agents/skills\"]   # hide convention roots without deleting folders\n")
	}
	if c.Skills.MaxDepth != 0 {
		fmt.Fprintf(&b, "max_depth = %d   # nested scan depth; default 3, set 1 for legacy root-only discovery\n", c.SkillMaxDepth())
	} else {
		b.WriteString("# max_depth = 3   # nested scan depth; set 1 for legacy root-only discovery\n")
	}
	if disabled := c.DisabledSkillNames(); len(disabled) > 0 {
		fmt.Fprintf(&b, "disabled_skills = %s   # hidden from the prompt, slash invocation, and skill tools\n\n", r.stringArray(disabled))
	} else {
		b.WriteString("# disabled_skills = [\"review\"]   # hide noisy or unwanted skills\n\n")
	}

	b.WriteString("[permissions]\n")
	b.WriteString("# Per-call gating. mode = writer fallback when no rule matches: ask|allow|deny.\n")
	b.WriteString("# Readers always default to allow. Precedence: deny > ask > allow > fallback.\n")
	b.WriteString("# Rules are \"Tool\" or \"Tool(specifier)\"; e.g. Bash(go test:*), Edit(src/**).\n")
	mode := c.Permissions.Mode
	if mode == "" {
		mode = "ask"
	}
	fmt.Fprintf(&b, "mode  = %s\n", r.q(mode))
	if c.Permissions.AllowDynamicBash {
		b.WriteString("allow_dynamic_bash = true   # advanced: let mode=allow cover command substitution and interpreter -c/-e\n")
	} else {
		b.WriteString("# allow_dynamic_bash = false   # advanced opt-in; deny/ask and exact rules still take precedence\n")
	}
	b.WriteString(r.ruleList("deny", c.Permissions.Deny, `["Bash(rm -rf*)", "Bash(git push*)"]   # hard-blocked in every mode`))
	b.WriteString(r.ruleList("allow", c.Permissions.Allow, `["Bash(go test:*)", "Bash(git status:*)"]   # never prompted`))
	b.WriteString(r.ruleList("ask", c.Permissions.Ask, `["Edit(src/**)"]   # force a prompt even if otherwise allowed`))
	b.WriteString("\n")

	b.WriteString("[sandbox]\n")
	b.WriteString("# Confine tool blast radius. File-writers (write_file/edit_file/multi_edit/move_file)\n")
	b.WriteString("# may only write under workspace_root (empty = current dir) and allow_write extras.\n")
	b.WriteString("# bash = \"enforce\" jails each command in an OS sandbox when available;\n")
	b.WriteString("# without one, bash execution is refused. Empty defaults to enforce on macOS/Linux.\n")
	b.WriteString("# Windows has no OS-level Bash sandbox and fixes bash = \"off\".\n")
	b.WriteString("# network allows sandboxed bash egress.\n")
	if c.Sandbox.WorkspaceRoot != "" {
		fmt.Fprintf(&b, "workspace_root = %s\n", r.q(c.Sandbox.WorkspaceRoot))
	} else {
		b.WriteString("# workspace_root = \"\"            # default: current working directory\n")
	}
	if len(c.Sandbox.AllowWrite) > 0 {
		fmt.Fprintf(&b, "allow_write = %s\n", r.stringArray(c.Sandbox.AllowWrite))
	} else {
		b.WriteString("# allow_write = [\"/tmp\"]          # extra dirs writers may also modify\n")
	}
	if len(c.Sandbox.ForbidRead) > 0 {
		fmt.Fprintf(&b, "forbid_read = %s\n", r.stringArray(c.Sandbox.ForbidRead))
	} else {
		b.WriteString("# forbid_read = []                  # dirs the agent cannot read or list\n")
	}
	fmt.Fprintf(&b, "bash    = %s\n", r.q(c.BashMode()))
	fmt.Fprintf(&b, "network = %v\n", c.Sandbox.Network)
	b.WriteString("\n")

	b.WriteString("[statusline]\n")
	b.WriteString("# A custom status line: a command whose first stdout line replaces the built-in\n")
	b.WriteString("# data row. It receives {\"model\",\"contextUsed\",\"contextWindow\",\"cwd\"} as JSON on stdin.\n")
	if c.Statusline.Command != "" {
		fmt.Fprintf(&b, "command = %s\n", r.q(c.Statusline.Command))
	} else {
		b.WriteString("# command = \"my-statusline.sh\"\n")
	}
	b.WriteString("\n")

	if shouldRenderBot(c, defaults, scope) {
		b.WriteString("# Bot gateway: multi-channel IM bot for QQ, Feishu/Lark, and WeChat.\n")
		b.WriteString("[bot]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.Enabled)
		if c.Bot.Model != "" {
			fmt.Fprintf(&b, "model = %s\n", r.q(c.Bot.Model))
		} else {
			b.WriteString("# model = \"\"   # empty = default_model\n")
		}
		if c.Bot.ToolApprovalMode != "" {
			fmt.Fprintf(&b, "tool_approval_mode = %s   # ask|auto|yolo; yolo skips tool approvals only\n", r.q(c.Bot.ToolApprovalMode))
		} else {
			b.WriteString("# tool_approval_mode = \"ask\"   # ask|auto|yolo; ask and plan decisions still wait\n")
		}
		fmt.Fprintf(&b, "max_steps = %d\n", c.Bot.MaxSteps)
		fmt.Fprintf(&b, "debounce_ms = %d\n", c.Bot.DebounceMs)
		if c.Bot.QueueMode != "" {
			fmt.Fprintf(&b, "queue_mode = %s   # steer|followup|collect|interrupt\n", r.q(c.Bot.QueueMode))
		} else {
			b.WriteString("# queue_mode = \"steer\"   # steer|followup|collect|interrupt\n")
		}
		if c.Bot.QueueCap > 0 {
			fmt.Fprintf(&b, "queue_cap = %d\n", c.Bot.QueueCap)
		} else {
			b.WriteString("# queue_cap = 20\n")
		}
		if c.Bot.QueueDrop != "" {
			fmt.Fprintf(&b, "queue_drop = %s   # summarize|old|new\n", r.q(c.Bot.QueueDrop))
		} else {
			b.WriteString("# queue_drop = \"summarize\"   # summarize|old|new\n")
		}
		fmt.Fprintf(&b, "ignore_self_messages = %v   # ignore bot echo by returned message_id and configured self user ids\n", c.Bot.IgnoreSelfMessages)
		b.WriteString("\n[bot.self_user_ids]\n")
		fmt.Fprintf(&b, "qq = %s\n", r.stringArray(c.Bot.SelfUserIDs.QQ))
		fmt.Fprintf(&b, "feishu = %s\n", r.stringArray(c.Bot.SelfUserIDs.Feishu))
		fmt.Fprintf(&b, "weixin = %s\n", r.stringArray(c.Bot.SelfUserIDs.Weixin))
		b.WriteString("\n[bot.control]\n")
		fmt.Fprintf(&b, "enabled = %v   # local loopback HTTP API for status/send; requires Bearer token\n", c.Bot.Control.Enabled)
		if strings.TrimSpace(c.Bot.Control.Addr) != "" {
			fmt.Fprintf(&b, "addr = %s\n", r.q(c.Bot.Control.Addr))
		} else {
			b.WriteString("# addr = \"127.0.0.1:37913\"\n")
		}
		if strings.TrimSpace(c.Bot.Control.TokenEnv) != "" {
			fmt.Fprintf(&b, "token_env = %s\n", r.q(c.Bot.Control.TokenEnv))
		} else {
			b.WriteString("# token_env = \"REASONIX_BOT_CONTROL_TOKEN\"\n")
		}
		if len(c.Bot.Routes) > 0 {
			for _, route := range c.Bot.Routes {
				b.WriteString("\n[[bot.routes]]\n")
				r.botRoute(&b, route)
			}
		}
		if len(c.Bot.DesktopWatchers) > 0 {
			for _, watcher := range c.Bot.DesktopWatchers {
				b.WriteString("\n[[bot.desktop_watchers]]\n")
				r.botDesktopWatcher(&b, watcher)
			}
		}
		b.WriteString("\n[bot.pairing]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.Pairing.Enabled)
		if c.Bot.Pairing.RequestTTLMinutes > 0 {
			fmt.Fprintf(&b, "request_ttl_minutes = %d\n", c.Bot.Pairing.RequestTTLMinutes)
		} else {
			b.WriteString("# request_ttl_minutes = 60\n")
		}
		if c.Bot.Pairing.MaxPendingPerPlatform > 0 {
			fmt.Fprintf(&b, "max_pending_per_platform = %d\n", c.Bot.Pairing.MaxPendingPerPlatform)
		} else {
			b.WriteString("# max_pending_per_platform = 3\n")
		}
		b.WriteString("\n[bot.allowlist]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.Allowlist.Enabled)
		fmt.Fprintf(&b, "allow_all = %v\n", c.Bot.Allowlist.AllowAll)
		fmt.Fprintf(&b, "qq_users = %s\n", r.stringArray(c.Bot.Allowlist.QQUsers))
		fmt.Fprintf(&b, "feishu_users = %s\n", r.stringArray(c.Bot.Allowlist.FeishuUsers))
		fmt.Fprintf(&b, "weixin_users = %s\n", r.stringArray(c.Bot.Allowlist.WeixinUsers))
		fmt.Fprintf(&b, "qq_approvers = %s\n", r.stringArray(c.Bot.Allowlist.QQApprovers))
		fmt.Fprintf(&b, "feishu_approvers = %s\n", r.stringArray(c.Bot.Allowlist.FeishuApprovers))
		fmt.Fprintf(&b, "weixin_approvers = %s\n", r.stringArray(c.Bot.Allowlist.WeixinApprovers))
		fmt.Fprintf(&b, "qq_admins = %s\n", r.stringArray(c.Bot.Allowlist.QQAdmins))
		fmt.Fprintf(&b, "feishu_admins = %s\n", r.stringArray(c.Bot.Allowlist.FeishuAdmins))
		fmt.Fprintf(&b, "weixin_admins = %s\n", r.stringArray(c.Bot.Allowlist.WeixinAdmins))
		fmt.Fprintf(&b, "qq_groups = %s\n", r.stringArray(c.Bot.Allowlist.QQGroups))
		fmt.Fprintf(&b, "feishu_groups = %s\n", r.stringArray(c.Bot.Allowlist.FeishuGroups))
		fmt.Fprintf(&b, "weixin_groups = %s\n", r.stringArray(c.Bot.Allowlist.WeixinGroups))
		b.WriteString("\n[bot.qq]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.QQ.Enabled)
		fmt.Fprintf(&b, "app_id = %s\n", r.q(c.Bot.QQ.AppID))
		fmt.Fprintf(&b, "app_secret_env = %s\n", r.q(c.Bot.QQ.AppSecretEnv))
		fmt.Fprintf(&b, "sandbox = %v\n", c.Bot.QQ.Sandbox)
		if strings.TrimSpace(c.Bot.QQ.Model) != "" {
			fmt.Fprintf(&b, "model = %s\n", r.q(strings.TrimSpace(c.Bot.QQ.Model)))
		}
		if strings.TrimSpace(c.Bot.QQ.ToolApprovalMode) != "" {
			fmt.Fprintf(&b, "tool_approval_mode = %s\n", r.q(strings.TrimSpace(c.Bot.QQ.ToolApprovalMode)))
		}
		if strings.TrimSpace(c.Bot.QQ.WorkspaceRoot) != "" {
			fmt.Fprintf(&b, "workspace_root = %s\n", r.q(strings.TrimSpace(c.Bot.QQ.WorkspaceRoot)))
		}
		if parts := r.botAccess(c.Bot.QQ.Access); parts != "" {
			fmt.Fprintf(&b, "access = %s\n", parts)
		}
		b.WriteString("\n[bot.feishu]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.Feishu.Enabled)
		fmt.Fprintf(&b, "app_id = %s\n", r.q(c.Bot.Feishu.AppID))
		fmt.Fprintf(&b, "domain = %s\n", r.q(c.Bot.Feishu.Domain))
		fmt.Fprintf(&b, "app_secret_env = %s\n", r.q(c.Bot.Feishu.AppSecretEnv))
		fmt.Fprintf(&b, "verification_token = %s\n", r.q(c.Bot.Feishu.VerificationToken))
		fmt.Fprintf(&b, "mode = %s\n", r.q(c.Bot.Feishu.Mode))
		fmt.Fprintf(&b, "webhook_port = %d\n", c.Bot.Feishu.WebhookPort)
		fmt.Fprintf(&b, "require_mention = %v\n", c.Bot.Feishu.RequireMention)
		if len(c.Bot.Feishu.OutboundMediaRoots) > 0 {
			fmt.Fprintf(&b, "outbound_media_roots = %s\n", r.stringArray(c.Bot.Feishu.OutboundMediaRoots))
		}
		b.WriteString("\n[bot.weixin]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.Weixin.Enabled)
		fmt.Fprintf(&b, "account_id = %s\n", r.q(c.Bot.Weixin.AccountID))
		fmt.Fprintf(&b, "token_env = %s\n", r.q(c.Bot.Weixin.TokenEnv))
		fmt.Fprintf(&b, "api_base = %s\n", r.q(c.Bot.Weixin.APIBase))
		for _, conn := range c.Bot.Connections {
			b.WriteString("\n[[bot.connections]]\n")
			fmt.Fprintf(&b, "id = %s\n", r.q(conn.ID))
			fmt.Fprintf(&b, "provider = %s\n", r.q(conn.Provider))
			fmt.Fprintf(&b, "domain = %s\n", r.q(conn.Domain))
			fmt.Fprintf(&b, "label = %s\n", r.q(conn.Label))
			fmt.Fprintf(&b, "enabled = %v\n", conn.Enabled)
			fmt.Fprintf(&b, "status = %s\n", r.q(conn.Status))
			if conn.Model != "" {
				fmt.Fprintf(&b, "model = %s\n", r.q(conn.Model))
			}
			if conn.ToolApprovalMode != "" {
				fmt.Fprintf(&b, "tool_approval_mode = %s\n", r.q(conn.ToolApprovalMode))
			}
			if conn.WorkspaceRoot != "" {
				fmt.Fprintf(&b, "workspace_root = %s\n", r.q(conn.WorkspaceRoot))
			}
			if parts := r.botAccess(conn.Access); parts != "" {
				fmt.Fprintf(&b, "access = %s\n", parts)
			}
			if conn.LastError != "" {
				fmt.Fprintf(&b, "last_error = %s\n", r.q(conn.LastError))
			}
			if conn.CreatedAt != "" {
				fmt.Fprintf(&b, "created_at = %s\n", r.q(conn.CreatedAt))
			}
			if conn.UpdatedAt != "" {
				fmt.Fprintf(&b, "updated_at = %s\n", r.q(conn.UpdatedAt))
			}
			if parts := r.botCredential(conn.Credential); parts != "" {
				fmt.Fprintf(&b, "credential = %s\n", parts)
			}
			if len(conn.SessionMappings) > 0 {
				fmt.Fprintf(&b, "session_mappings = %s\n", r.botSessionMappings(conn.SessionMappings))
			}
		}
		b.WriteString("\n")
	}

	// [secrets] is user/global only: LoadForRoot discards project values, so
	// the project scope never renders it. Rendering it here is what lets a
	// user's saved toggles survive config rewrites (WriteFile re-renders the
	// whole file from the struct).
	if scope != RenderScopeProject {
		b.WriteString("[secrets]   # credential protection; user/global only, ./reasonix.toml cannot override\n")
		if c.Secrets.FilterSubprocessEnv {
			b.WriteString("filter_subprocess_env = true   # strip credential-named env vars from tool/hook/LSP/MCP subprocesses\n")
		} else {
			b.WriteString("# filter_subprocess_env = false   # opt-in; stripping tokens breaks gh, HTTPS git push, npm publish\n")
		}
		if c.Secrets.ProtectSensitiveFiles {
			b.WriteString("protect_sensitive_files = true   # hide .env/.git-credentials/key files/~/.ssh from read tools\n")
		} else {
			b.WriteString("# protect_sensitive_files = false   # opt-in; hiding credential files can break legitimate edit workflows\n")
		}
		b.WriteString("\n")
	}

	// [remote] is user/global only like [secrets]: LoadForRoot discards project
	// values so a cloned repo can never inject SSH hosts. Rendered here so
	// saved hosts survive full-file config rewrites.
	if scope != RenderScopeProject && (c.Remote.ImportSSHConfig || len(c.Remote.Hosts) > 0) {
		b.WriteString("[remote]   # SSH remote hosts; user/global only, ./reasonix.toml cannot override\n")
		if c.Remote.ImportSSHConfig {
			b.WriteString("import_ssh_config = true   # surface ~/.ssh/config aliases in `reasonix remote import`\n")
		}
		for _, h := range c.Remote.Hosts {
			b.WriteString("\n[[remote.hosts]]\n")
			fmt.Fprintf(&b, "name = %s\n", r.q(h.Name))
			fmt.Fprintf(&b, "host = %s\n", r.q(h.Host))
			if h.Port > 0 {
				fmt.Fprintf(&b, "port = %d\n", h.Port)
			}
			if h.User != "" {
				fmt.Fprintf(&b, "user = %s\n", r.q(h.User))
			}
			if h.IdentityFile != "" {
				fmt.Fprintf(&b, "identity_file = %s   # key file path; Reasonix never stores key material\n", r.q(h.IdentityFile))
			}
			if h.PassphraseEnv != "" {
				fmt.Fprintf(&b, "passphrase_env = %s   # env var name; value lives in Reasonix's global .env\n", r.q(h.PassphraseEnv))
			}
			if h.PasswordEnv != "" {
				fmt.Fprintf(&b, "password_env = %s   # env var name; value lives in Reasonix's global .env\n", r.q(h.PasswordEnv))
			}
			if h.ProxyJump != "" {
				fmt.Fprintf(&b, "proxy_jump = %s   # OpenSSH ProxyJump chain\n", r.q(h.ProxyJump))
			}
			if h.Workspace != "" {
				fmt.Fprintf(&b, "workspace = %s   # default remote workspace dir\n", r.q(h.Workspace))
			}
			if h.ServeInstall != "" {
				fmt.Fprintf(&b, "serve_install = %s   # auto|npm|upload|never\n", r.q(h.ServeInstall))
			}
			if h.UseSSHConfig {
				b.WriteString("use_ssh_config = true   # layer ~/.ssh/config values under unset fields\n")
			}
			for _, f := range h.Forwards {
				b.WriteString("\n[[remote.hosts.forwards]]\n")
				fmt.Fprintf(&b, "type = %s   # local (-L) | remote (-R)\n", r.q(f.Type))
				fmt.Fprintf(&b, "bind = %s\n", r.q(f.Bind))
				fmt.Fprintf(&b, "target = %s\n", r.q(f.Target))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("# External MCP servers. type: \"stdio\" (default, a subprocess) | \"http\" | \"sse\".\n")
	b.WriteString("# ${VAR} / ${VAR:-default} are expanded from the environment in command/args/env/url/headers.\n")
	plugins := tomlPluginsForScope(c.Plugins, scope)
	if len(plugins) == 0 {
		b.WriteString("# [[plugins]]\n")
		b.WriteString("# name    = \"example\"\n")
		b.WriteString("# command = \"reasonix-plugin-example\"\n")
		b.WriteString("# startup_timeout_seconds = 60    # optional initialize + tools/list cap\n")
		b.WriteString("# call_timeout_seconds = 600       # optional per-server MCP call timeout\n")
		b.WriteString("# tool_timeout_seconds = { \"generate_video\" = 1800 }   # raw MCP tool names\n")
		b.WriteString("# [[plugins]]                                  # a remote server over Streamable HTTP\n")
		b.WriteString("# name    = \"stripe\"\n")
		b.WriteString("# type    = \"http\"\n")
		b.WriteString("# url     = \"https://mcp.stripe.com\"\n")
		b.WriteString("# headers = { Authorization = \"Bearer ${STRIPE_KEY}\" }\n")
	} else {
		for _, pl := range plugins {
			b.WriteString("\n[[plugins]]\n")
			fmt.Fprintf(&b, "name    = %s\n", r.q(pl.Name))
			if pl.Type != "" {
				fmt.Fprintf(&b, "type    = %s\n", r.q(pl.Type))
			}
			if pl.Command != "" {
				fmt.Fprintf(&b, "command = %s\n", r.q(pl.Command))
			}
			if len(pl.Args) > 0 {
				fmt.Fprintf(&b, "args    = %s\n", r.stringArray(pl.Args))
			}
			if pl.URL != "" {
				fmt.Fprintf(&b, "url     = %s\n", r.q(pl.URL))
			}
			if len(pl.Headers) > 0 {
				fmt.Fprintf(&b, "headers = %s\n", r.stringMap(pl.Headers))
			}
			if len(pl.Env) > 0 {
				fmt.Fprintf(&b, "env     = %s\n", r.stringMap(pl.Env))
			}
			if pl.StartupTimeoutSeconds > 0 {
				b.WriteString("# Per-server MCP initialize + tools/list timeout; 0 keeps the global/default cap.\n")
				fmt.Fprintf(&b, "startup_timeout_seconds = %d\n", pl.StartupTimeoutSeconds)
			}
			if pl.CallTimeoutSeconds > 0 {
				b.WriteString("# Per-server MCP call timeout; 0 keeps the global/default cap.\n")
				fmt.Fprintf(&b, "call_timeout_seconds = %d\n", pl.CallTimeoutSeconds)
			}
			if hasPositiveIntMap(pl.ToolTimeoutSeconds) {
				b.WriteString("# Raw MCP tool names with per-tool call timeouts.\n")
				fmt.Fprintf(&b, "tool_timeout_seconds = %s\n", r.intMap(pl.ToolTimeoutSeconds))
			}
			if pl.AutoStart != nil {
				fmt.Fprintf(&b, "auto_start = %v\n", *pl.AutoStart)
			}
		}
	}

	return b.String()
}

// tomlPluginsForScope keeps merged runtime entries in their owning config
// source. Unknown provenance is retained for callers that construct a Config
// directly before saving it to a specific target.
func tomlPluginsForScope(plugins []PluginEntry, scope RenderScope) []PluginEntry {
	if scope == RenderScopeFull {
		return plugins
	}
	out := make([]PluginEntry, 0, len(plugins))
	for _, pl := range plugins {
		switch pl.Source {
		case MCPSourceUnknown:
			out = append(out, pl)
		case MCPSourceUserConfig:
			if scope == RenderScopeUser {
				out = append(out, pl)
			}
		case MCPSourceProjectConfig:
			if scope == RenderScopeProject {
				out = append(out, pl)
			}
		}
	}
	return out
}

// RenderTOMLProjectDelta generates TOML containing only the sections and fields
// that differ from built-in defaults. Unlike RenderTOMLForScope (which renders
// the full config with comments), this emits clean TOML that can be surgically
// merged into an existing project config file via replaceTOMLSection.
func RenderTOMLProjectDelta(c *Config) string {
	out, err := renderTOMLProjectDeltaErr(c)
	if err != nil {
		return ""
	}
	return out
}

// renderTOMLProjectDeltaErr is the error-reporting variant used by the
// validated write pipeline.
func renderTOMLProjectDeltaErr(c *Config) (string, error) {
	r := &tomlRenderer{}
	body := renderTOMLProjectDeltaInto(r, c)
	if r.err != nil {
		return "", r.err
	}
	return body, nil
}

func renderTOMLProjectDeltaInto(r *tomlRenderer, c *Config) string {
	if c == nil {
		return ""
	}
	d := Default()
	var b strings.Builder

	// Top-level scalar fields
	if v := configVersion(c); v != d.ConfigVersion {
		fmt.Fprintf(&b, "config_version = %d\n", v)
	}
	if c.DefaultModel != d.DefaultModel {
		fmt.Fprintf(&b, "default_model = %s\n", r.q(c.DefaultModel))
	}
	if c.Language != "" && c.Language != d.Language {
		fmt.Fprintf(&b, "language = %s\n", r.q(c.Language))
	}

	// [ui] section — whole-section comparison
	if !reflect.DeepEqual(c.UI, d.UI) {
		b.WriteString("[ui]\n")
		if c.UI.Theme != d.UI.Theme {
			fmt.Fprintf(&b, "theme = %s\n", r.q(c.UITheme()))
		}
		if s := c.UIThemeStyle(); s != "" && s != d.UIThemeStyle() {
			fmt.Fprintf(&b, "theme_style = %s\n", r.q(s))
		}
		if l := c.UIShortcutLayout(); l != "classic" {
			fmt.Fprintf(&b, "shortcut_layout = %s\n", r.q(l))
		}
		if strings.TrimSpace(c.UI.CursorShape) != "" {
			fmt.Fprintf(&b, "cursor_shape = %s\n", r.q(c.UICursorShape()))
		}
		if c.UI.CloseBehavior != d.UI.CloseBehavior {
			fmt.Fprintf(&b, "close_behavior = %s\n", r.q(c.DesktopCloseBehavior()))
		}
		if c.UI.ShowReasoning != d.UI.ShowReasoning {
			fmt.Fprintf(&b, "show_reasoning = %v\n", c.UI.ShowReasoning)
		}
		b.WriteString("\n")
	}

	// [network] section
	if !reflect.DeepEqual(c.Network, d.Network) {
		b.WriteString("[network]\n")
		if c.Network.ProxyMode != d.Network.ProxyMode {
			fmt.Fprintf(&b, "proxy_mode = %s\n", r.q(c.NetworkProxyMode()))
		}
		if c.Network.ProxyURL != "" {
			fmt.Fprintf(&b, "proxy_url = %s\n", r.q(c.Network.ProxyURL))
		}
		if c.Network.NoProxy != "" {
			fmt.Fprintf(&b, "no_proxy = %s\n", r.q(c.Network.NoProxy))
		}
		if c.Network.Proxy.Type != "" || c.Network.Proxy.Server != "" || c.Network.Proxy.Port > 0 || c.Network.Proxy.Username != "" || c.Network.Proxy.Password != "" {
			b.WriteString("[network.proxy]\n")
			pt := c.Network.Proxy.Type
			if pt == "" {
				pt = "socks5"
			}
			fmt.Fprintf(&b, "type = %s\n", r.q(pt))
			if c.Network.Proxy.Server != "" {
				fmt.Fprintf(&b, "server = %s\n", r.q(c.Network.Proxy.Server))
			}
			if c.Network.Proxy.Port > 0 {
				fmt.Fprintf(&b, "port = %d\n", c.Network.Proxy.Port)
			}
			if c.Network.Proxy.Username != "" {
				fmt.Fprintf(&b, "username = %s\n", r.q(c.Network.Proxy.Username))
			}
			if c.Network.Proxy.Password != "" {
				fmt.Fprintf(&b, "password = %s\n", r.q(c.Network.Proxy.Password))
			}
		}
		b.WriteString("\n")
	}

	// [agent] section — per-field comparison
	var agentBuf strings.Builder
	anyAgent := false

	if sp := strings.TrimSpace(c.Agent.SystemPrompt); sp != "" && sp != d.Agent.SystemPrompt {
		agentBuf.WriteString("system_prompt = \"\"\"\n")
		agentBuf.WriteString(sp)
		agentBuf.WriteString("\"\"\"\n")
		anyAgent = true
	}
	if c.Agent.SystemPromptFile != "" && c.Agent.SystemPromptFile != d.Agent.SystemPromptFile {
		fmt.Fprintf(&agentBuf, "system_prompt_file = %s\n", r.q(c.Agent.SystemPromptFile))
		anyAgent = true
	}
	if c.Agent.Temperature != d.Agent.Temperature {
		fmt.Fprintf(&agentBuf, "temperature = %s\n", formatFloat(c.Agent.Temperature))
		anyAgent = true
	}
	if c.Agent.RecoveryModel != "" && c.Agent.RecoveryModel != d.Agent.RecoveryModel {
		fmt.Fprintf(&agentBuf, "recovery_model = %s\n", r.q(c.Agent.RecoveryModel))
		anyAgent = true
	}
	if c.Agent.ReasoningLanguage != d.Agent.ReasoningLanguage {
		if l := c.ReasoningLanguage(); l != "auto" {
			fmt.Fprintf(&agentBuf, "reasoning_language = %s\n", r.q(l))
			anyAgent = true
		}
	}
	if c.Agent.SoftCompactRatio != d.Agent.SoftCompactRatio {
		fmt.Fprintf(&agentBuf, "soft_compact_ratio = %s\n", formatFloat(c.Agent.SoftCompactRatio))
		anyAgent = true
	}
	if c.Agent.ToolResultSnipRatio != d.Agent.ToolResultSnipRatio {
		fmt.Fprintf(&agentBuf, "tool_result_snip_ratio = %s\n", formatFloat(c.Agent.ToolResultSnipRatio))
		anyAgent = true
	}
	if c.Agent.CompactRatio != d.Agent.CompactRatio {
		fmt.Fprintf(&agentBuf, "compact_ratio = %s\n", formatFloat(c.Agent.CompactRatio))
		anyAgent = true
	}
	if c.Agent.CompactForceRatio != d.Agent.CompactForceRatio {
		fmt.Fprintf(&agentBuf, "compact_force_ratio = %s\n", formatFloat(c.Agent.CompactForceRatio))
		anyAgent = true
	}
	if c.Agent.Keep != nil && !reflect.DeepEqual(c.Agent.Keep, d.Agent.Keep) {
		fmt.Fprintf(&agentBuf, "keep = %s\n", r.stringArray(c.Agent.Keep))
		anyAgent = true
	}
	if c.Agent.RecentKeep > 0 && c.Agent.RecentKeep != d.Agent.RecentKeep {
		fmt.Fprintf(&agentBuf, "recent_keep = %d\n", c.Agent.RecentKeep)
		anyAgent = true
	}
	if c.Agent.ColdResumePrune != d.Agent.ColdResumePrune {
		fmt.Fprintf(&agentBuf, "cold_resume_prune = %v\n", c.ColdResumePruneEnabled())
		anyAgent = true
	}
	if len(c.Agent.PlanModeReadOnlyCommands) > 0 && !reflect.DeepEqual(c.Agent.PlanModeReadOnlyCommands, d.Agent.PlanModeReadOnlyCommands) {
		fmt.Fprintf(&agentBuf, "plan_mode_read_only_commands = %s\n", r.stringArray(c.Agent.PlanModeReadOnlyCommands))
		anyAgent = true
	}
	if c.Agent.PlannerModel != "" && c.Agent.PlannerModel != d.Agent.PlannerModel {
		fmt.Fprintf(&agentBuf, "planner_model = %s\n", r.q(c.Agent.PlannerModel))
		anyAgent = true
	}
	if c.Agent.SubagentModel != "" && c.Agent.SubagentModel != d.Agent.SubagentModel {
		fmt.Fprintf(&agentBuf, "subagent_model = %s\n", r.q(c.Agent.SubagentModel))
		anyAgent = true
	}
	if len(c.Agent.SubagentModels) > 0 && !reflect.DeepEqual(c.Agent.SubagentModels, d.Agent.SubagentModels) {
		fmt.Fprintf(&agentBuf, "subagent_models = %s\n", r.stringMap(c.Agent.SubagentModels))
		anyAgent = true
	}
	if c.Agent.SubagentEffort != "" && c.Agent.SubagentEffort != d.Agent.SubagentEffort {
		fmt.Fprintf(&agentBuf, "subagent_effort = %s\n", r.q(c.Agent.SubagentEffort))
		anyAgent = true
	}
	if len(c.Agent.SubagentEfforts) > 0 && !reflect.DeepEqual(c.Agent.SubagentEfforts, d.Agent.SubagentEfforts) {
		fmt.Fprintf(&agentBuf, "subagent_efforts = %s\n", r.stringMap(c.Agent.SubagentEfforts))
		anyAgent = true
	}
	if c.Agent.MaxSubagentDepth != d.Agent.MaxSubagentDepth {
		fmt.Fprintf(&agentBuf, "max_subagent_depth = %d\n", c.Agent.MaxSubagentDepth)
		anyAgent = true
	}
	if c.Agent.OutputStyle != "" && c.Agent.OutputStyle != d.Agent.OutputStyle {
		fmt.Fprintf(&agentBuf, "output_style = %s\n", r.q(c.Agent.OutputStyle))
		anyAgent = true
	}

	if anyAgent {
		b.WriteString("[agent]\n")
		b.WriteString(agentBuf.String())
		b.WriteString("\n")
	}

	// [[providers]] — include user-defined providers that aren't built-in
	proj := projectScopedConfigForRender(c)
	if proj != nil && len(proj.Providers) > 0 && !reflect.DeepEqual(proj.Providers, d.Providers) {
		for _, p := range proj.Providers {
			b.WriteString("[[providers]]\n")
			fmt.Fprintf(&b, "name        = %s\n", r.q(p.Name))
			fmt.Fprintf(&b, "kind        = %s\n", r.q(p.Kind))
			fmt.Fprintf(&b, "base_url    = %s\n", r.q(p.BaseURL))
			if p.ChatURL != "" {
				fmt.Fprintf(&b, "chat_url    = %s\n", r.q(p.ChatURL))
			}
			if len(p.Models) > 0 {
				fmt.Fprintf(&b, "models      = %s\n", r.stringArray(p.Models))
				if p.Default != "" {
					fmt.Fprintf(&b, "default     = %s\n", r.q(p.Default))
				}
			} else if p.Model != "" {
				fmt.Fprintf(&b, "model       = %s\n", r.q(p.Model))
			}
			if p.ModelsURL != "" {
				fmt.Fprintf(&b, "models_url  = %s\n", r.q(p.ModelsURL))
			}
			fmt.Fprintf(&b, "api_key_env = %s\n", r.q(p.APIKeyEnv))
			if p.PresetID != "" {
				fmt.Fprintf(&b, "preset_id   = %s\n", r.q(p.PresetID))
			}
			if p.PresetVersion > 0 {
				fmt.Fprintf(&b, "preset_version = %d\n", p.PresetVersion)
			}
			if len(p.Headers) > 0 {
				fmt.Fprintf(&b, "headers     = %s\n", r.stringMap(p.Headers))
			}
			if len(p.ExtraBody) > 0 {
				fmt.Fprintf(&b, "extra_body  = %s\n", r.anyMap(p.ExtraBody))
			}
			if p.AuthHeader {
				b.WriteString("auth_header = true\n")
			}
			if p.ResponsesMode != "" {
				fmt.Fprintf(&b, "responses_mode = %s\n", r.q(p.ResponsesMode))
			}
			if p.ResponsesStateful != nil {
				fmt.Fprintf(&b, "responses_stateful = %t\n", *p.ResponsesStateful)
			}
			if p.BalanceURL != "" {
				fmt.Fprintf(&b, "balance_url = %s\n", r.q(p.BalanceURL))
			}
			if p.ContextWindow > 0 {
				fmt.Fprintf(&b, "context_window = %d\n", p.ContextWindow)
			}
			if p.MaxOutputTokens != 0 {
				fmt.Fprintf(&b, "max_output_tokens = %d\n", p.MaxOutputTokens)
			}
			if p.Price != nil {
				fmt.Fprintf(&b, "price       = %s\n", r.pricingInline(p.Price))
			}
			if len(p.Prices) > 0 {
				fmt.Fprintf(&b, "prices      = %s\n", r.pricingMap(p.Prices))
			}
			if p.Thinking != "" {
				fmt.Fprintf(&b, "thinking    = %s\n", r.q(p.Thinking))
			}
			if p.Effort != "" {
				fmt.Fprintf(&b, "effort      = %s\n", r.q(p.Effort))
			}
			if p.Vision {
				b.WriteString("vision      = true\n")
			}
			if p.VisionModels != nil {
				fmt.Fprintf(&b, "vision_models = %s\n", r.stringArray(p.VisionModels))
			}
			if p.VisionDetail != "" {
				fmt.Fprintf(&b, "vision_detail = %s\n", r.q(p.VisionDetail))
			}
			if p.ReasoningProtocol != "" {
				fmt.Fprintf(&b, "reasoning_protocol = %s\n", r.q(p.ReasoningProtocol))
			}
			if len(p.SupportedEfforts) > 0 {
				fmt.Fprintf(&b, "supported_efforts = %s\n", r.stringArray(p.SupportedEfforts))
			}
			if p.DefaultEffort != "" {
				fmt.Fprintf(&b, "default_effort    = %s\n", r.q(p.DefaultEffort))
			}
			if len(p.ModelOverrides) > 0 {
				fmt.Fprintf(&b, "model_overrides   = %s\n", r.modelOverrides(p.ModelOverrides))
			}
			if p.NoProxy {
				b.WriteString("no_proxy    = true\n")
			}
			b.WriteString("\n")
		}
	}

	// [tools]
	if len(c.Tools.Enabled) > 0 ||
		(c.Tools.BashTimeoutSeconds != nil && *c.Tools.BashTimeoutSeconds != 0) ||
		(c.Tools.MCPStartupTimeoutSeconds != nil && *c.Tools.MCPStartupTimeoutSeconds > 0) ||
		(c.Tools.MCPCallTimeoutSeconds != nil && *c.Tools.MCPCallTimeoutSeconds > 0) {
		b.WriteString("[tools]\n")
		if len(c.Tools.Enabled) > 0 {
			fmt.Fprintf(&b, "enabled = %s\n", r.stringArray(c.Tools.Enabled))
		}
		if c.Tools.BashTimeoutSeconds != nil && *c.Tools.BashTimeoutSeconds != 0 {
			fmt.Fprintf(&b, "bash_timeout_seconds = %d\n", *c.Tools.BashTimeoutSeconds)
		}
		if c.Tools.MCPStartupTimeoutSeconds != nil && *c.Tools.MCPStartupTimeoutSeconds > 0 {
			fmt.Fprintf(&b, "mcp_startup_timeout_seconds = %d\n", *c.Tools.MCPStartupTimeoutSeconds)
		}
		if c.Tools.MCPCallTimeoutSeconds != nil && *c.Tools.MCPCallTimeoutSeconds > 0 {
			fmt.Fprintf(&b, "mcp_call_timeout_seconds = %d\n", *c.Tools.MCPCallTimeoutSeconds)
		}
		b.WriteString("\n")
	}

	// [tools.background_jobs]
	if c.Tools.BackgroundJobs != d.Tools.BackgroundJobs {
		if c.Tools.BackgroundJobs.StalledWarningSeconds != nil && *c.Tools.BackgroundJobs.StalledWarningSeconds > 0 {
			b.WriteString("[tools.background_jobs]\n")
			fmt.Fprintf(&b, "stalled_warning_seconds = %d\n", *c.Tools.BackgroundJobs.StalledWarningSeconds)
			b.WriteString("\n")
		}
	}

	// [tools.shell]
	if !reflect.DeepEqual(c.Tools.Shell, d.Tools.Shell) {
		b.WriteString("[tools.shell]\n")
		if c.Tools.Shell.Prefer != d.Tools.Shell.Prefer {
			fmt.Fprintf(&b, "prefer = %s\n", r.q(c.Tools.Shell.Prefer))
		}
		if c.Tools.Shell.Path != d.Tools.Shell.Path {
			fmt.Fprintf(&b, "path = %s\n", r.q(c.Tools.Shell.Path))
		}
		b.WriteString("\n")
	}

	// [lsp]
	if !reflect.DeepEqual(c.LSP, d.LSP) {
		r.lspConfig(&b, c.LSP)
	}

	// [skills]
	if !reflect.DeepEqual(c.Skills, d.Skills) {
		b.WriteString("[skills]\n")
		if len(c.Skills.Paths) > 0 {
			fmt.Fprintf(&b, "paths = %s\n", r.stringArray(c.Skills.Paths))
		}
		if len(c.Skills.ExcludedPaths) > 0 {
			fmt.Fprintf(&b, "excluded_paths = %s\n", r.stringArray(c.Skills.ExcludedPaths))
		}
		if c.Skills.MaxDepth != 0 {
			fmt.Fprintf(&b, "max_depth = %d\n", c.SkillMaxDepth())
		}
		if disabled := c.DisabledSkillNames(); len(disabled) > 0 {
			fmt.Fprintf(&b, "disabled_skills = %s\n\n", r.stringArray(disabled))
		}
	}

	// [permissions]
	if !reflect.DeepEqual(c.Permissions, d.Permissions) {
		b.WriteString("[permissions]\n")
		mode := c.Permissions.Mode
		if mode == "" {
			mode = "ask"
		}
		if mode != "ask" {
			fmt.Fprintf(&b, "mode = %s\n", r.q(mode))
		}
		if c.Permissions.AllowDynamicBash {
			b.WriteString("allow_dynamic_bash = true\n")
		}
		if len(c.Permissions.Deny) > 0 {
			fmt.Fprintf(&b, "deny = %s\n", r.stringArray(c.Permissions.Deny))
		}
		if len(c.Permissions.Allow) > 0 {
			fmt.Fprintf(&b, "allow = %s\n", r.stringArray(c.Permissions.Allow))
		}
		if len(c.Permissions.Ask) > 0 {
			fmt.Fprintf(&b, "ask = %s\n", r.stringArray(c.Permissions.Ask))
		}
		b.WriteString("\n")
	}

	// [sandbox]
	if !reflect.DeepEqual(c.Sandbox, d.Sandbox) {
		var sandboxBuf strings.Builder
		if c.Sandbox.WorkspaceRoot != "" {
			fmt.Fprintf(&sandboxBuf, "workspace_root = %s\n", r.q(c.Sandbox.WorkspaceRoot))
		}
		if len(c.Sandbox.AllowWrite) > 0 {
			fmt.Fprintf(&sandboxBuf, "allow_write = %s\n", r.stringArray(c.Sandbox.AllowWrite))
		}
		// Only persist a bash mode when its effective value differs from the
		// platform default. On Windows, even explicit "enforce" currently
		// resolves to "off", so project configs should not imply otherwise.
		if strings.TrimSpace(c.Sandbox.Bash) != "" && c.BashMode() != d.BashModeForGOOS(runtimeGOOS) {
			fmt.Fprintf(&sandboxBuf, "bash = %s\n", r.q(c.BashMode()))
		}
		if c.Sandbox.Network != d.Sandbox.Network {
			fmt.Fprintf(&sandboxBuf, "network = %v\n", c.Sandbox.Network)
		}
		if sandboxBuf.Len() > 0 {
			b.WriteString("[sandbox]\n")
			b.WriteString(sandboxBuf.String())
			b.WriteString("\n")
		}
	}

	// [statusline]
	if !reflect.DeepEqual(c.Statusline, d.Statusline) {
		b.WriteString("[statusline]\n")
		if c.Statusline.Command != "" {
			fmt.Fprintf(&b, "command = %s\n", r.q(c.Statusline.Command))
		}
		b.WriteString("\n")
	}

	// [[plugins]] — always include when set; replaces all existing entries
	for _, pl := range tomlPluginsForScope(c.Plugins, RenderScopeProject) {
		b.WriteString("[[plugins]]\n")
		fmt.Fprintf(&b, "name    = %s\n", r.q(pl.Name))
		if pl.Type != "" {
			fmt.Fprintf(&b, "type    = %s\n", r.q(pl.Type))
		}
		if pl.Command != "" {
			fmt.Fprintf(&b, "command = %s\n", r.q(pl.Command))
		}
		if len(pl.Args) > 0 {
			fmt.Fprintf(&b, "args    = %s\n", r.stringArray(pl.Args))
		}
		if pl.URL != "" {
			fmt.Fprintf(&b, "url     = %s\n", r.q(pl.URL))
		}
		if len(pl.Headers) > 0 {
			fmt.Fprintf(&b, "headers = %s\n", r.stringMap(pl.Headers))
		}
		if len(pl.Env) > 0 {
			fmt.Fprintf(&b, "env     = %s\n", r.stringMap(pl.Env))
		}
		if pl.StartupTimeoutSeconds > 0 {
			fmt.Fprintf(&b, "startup_timeout_seconds = %d\n", pl.StartupTimeoutSeconds)
		}
		if pl.CallTimeoutSeconds > 0 {
			b.WriteString("# Per-server MCP call timeout; 0 keeps the global/default cap.\n")
			fmt.Fprintf(&b, "call_timeout_seconds = %d\n", pl.CallTimeoutSeconds)
		}
		if hasPositiveIntMap(pl.ToolTimeoutSeconds) {
			b.WriteString("# Raw MCP tool names with per-tool call timeouts.\n")
			fmt.Fprintf(&b, "tool_timeout_seconds = %s\n", r.intMap(pl.ToolTimeoutSeconds))
		}
		if pl.AutoStart != nil {
			fmt.Fprintf(&b, "auto_start = %v\n", *pl.AutoStart)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func (r *tomlRenderer) pricingInline(p *provider.Pricing) string {
	if p == nil {
		return "{}"
	}
	return fmt.Sprintf("{ cache_hit = %v, input = %v, output = %v, currency = %s }",
		p.CacheHit, p.Input, p.Output, r.q(p.Symbol()))
}

func (r *tomlRenderer) pricingMap(prices map[string]*provider.Pricing) string {
	if len(prices) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(prices))
	for model := range prices {
		if strings.TrimSpace(model) != "" && prices[model] != nil {
			keys = append(keys, model)
		}
	}
	if len(keys) == 0 {
		return "{}"
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, model := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = %s", r.key(model), r.pricingInline(prices[model]))
	}
	b.WriteString(" }")
	return b.String()
}

func configVersion(c *Config) int {
	if c != nil && c.ConfigVersion > 0 {
		return c.ConfigVersion
	}
	return Default().ConfigVersion
}

func shouldRenderUI(c, defaults *Config, scope RenderScope) bool {
	if scope != RenderScopeProject {
		return true
	}
	return !reflect.DeepEqual(c.UI, defaults.UI)
}

func shouldRenderNetwork(c, defaults *Config, scope RenderScope) bool {
	if scope != RenderScopeProject {
		return true
	}
	return !reflect.DeepEqual(c.Network, defaults.Network)
}

func shouldRenderEnvironment(c, defaults *Config, scope RenderScope) bool {
	if scope != RenderScopeProject {
		return true
	}
	return !reflect.DeepEqual(c.Environment, defaults.Environment)
}

func (r *tomlRenderer) environmentConfig(b *strings.Builder, cfg EnvironmentConfig) {
	b.WriteString("[environment]\n")
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	fmt.Fprintf(b, "enabled = %v   # inject a stable startup environment summary into the model prompt\n", enabled)
	if len(cfg.Tools) == 0 {
		b.WriteString("# [environment.tools]\n")
		b.WriteString("# go = \"/opt/homebrew/bin/go\"   # trusted executable path; workspace-local paths are not auto-executed\n\n")
		return
	}
	b.WriteString("\n[environment.tools]\n")
	names := make([]string, 0, len(cfg.Tools))
	for name := range cfg.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(b, "%s = %s\n", r.keyPart(name), r.q(cfg.Tools[name]))
	}
	b.WriteString("\n")
}

func shouldRenderProviders(c, defaults *Config, scope RenderScope) bool {
	if scope != RenderScopeProject {
		return true
	}
	return !reflect.DeepEqual(c.Providers, defaults.Providers)
}

func projectScopedConfigForRender(c *Config) *Config {
	if c == nil || len(c.providerSources) == 0 {
		return c
	}
	cp := *c
	cp.Providers = make([]ProviderEntry, 0, len(c.Providers)+len(c.shadowedProjectProviders))
	for _, p := range c.Providers {
		if c.providerSources[providerMergeKey(p)] == providerSourceUser {
			continue
		}
		cp.Providers = append(cp.Providers, p)
	}
	cp.Providers = append(cp.Providers, c.shadowedProjectProviders...)
	return &cp
}

func shouldRenderBot(c, defaults *Config, scope RenderScope) bool {
	if scope != RenderScopeProject {
		return true
	}
	return !reflect.DeepEqual(c.Bot, defaults.Bot)
}

func shouldRenderSystemPrompt(c, defaults *Config, scope RenderScope) bool {
	if scope == RenderScopeFull {
		return true
	}
	return strings.TrimSpace(c.Agent.SystemPrompt) != "" && c.Agent.SystemPrompt != defaults.Agent.SystemPrompt
}

func (r *tomlRenderer) lspConfig(b *strings.Builder, cfg LSPConfig) {
	b.WriteString("[lsp]\n")
	fmt.Fprintf(b, "enabled = %v   # language server tools; servers launch lazily when used\n", cfg.Enabled)
	if len(cfg.Servers) == 0 {
		b.WriteString("# [lsp.servers.go]\n")
		b.WriteString("# command = \"gopls\"\n")
		b.WriteString("# args = []\n")
		b.WriteString("# extensions = [\".go\"]\n\n")
		return
	}
	b.WriteString("\n")

	langs := make([]string, 0, len(cfg.Servers))
	for lang := range cfg.Servers {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		srv := cfg.Servers[lang]
		fmt.Fprintf(b, "[%s]\n", r.tablePath("lsp", "servers", lang))
		if srv.Command != "" {
			fmt.Fprintf(b, "command = %s\n", r.q(srv.Command))
		}
		if len(srv.Args) > 0 {
			fmt.Fprintf(b, "args = %s\n", r.stringArray(srv.Args))
		}
		if len(srv.Env) > 0 {
			fmt.Fprintf(b, "env = %s\n", r.stringMap(srv.Env))
		}
		if srv.LanguageID != "" {
			fmt.Fprintf(b, "language_id = %s\n", r.q(srv.LanguageID))
		}
		if len(srv.Extensions) > 0 {
			fmt.Fprintf(b, "extensions = %s\n", r.stringArray(srv.Extensions))
		}
		if srv.InstallHint != "" {
			fmt.Fprintf(b, "install_hint = %s\n", r.q(srv.InstallHint))
		}
		b.WriteString("\n")
	}
}

func (r *tomlRenderer) keyPart(key string) string {
	return r.key(key)
}

func (r *tomlRenderer) tablePath(parts ...string) string {
	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		rendered = append(rendered, r.keyPart(part))
	}
	return strings.Join(rendered, ".")
}

func isBareTOMLKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func (r *tomlRenderer) anyMap(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if strings.TrimSpace(k) == "" {
			continue
		}
		if _, ok := r.anyValue(v); ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		value, _ := r.anyValue(m[k])
		fmt.Fprintf(&b, "%s = %s", r.key(k), value)
	}
	b.WriteString(" }")
	return b.String()
}

func (r *tomlRenderer) anyValue(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return r.q(x), true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case int:
		return strconv.Itoa(x), true
	case int8:
		return strconv.FormatInt(int64(x), 10), true
	case int16:
		return strconv.FormatInt(int64(x), 10), true
	case int32:
		return strconv.FormatInt(int64(x), 10), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case uint:
		return strconv.FormatUint(uint64(x), 10), true
	case uint8:
		return strconv.FormatUint(uint64(x), 10), true
	case uint16:
		return strconv.FormatUint(uint64(x), 10), true
	case uint32:
		return strconv.FormatUint(uint64(x), 10), true
	case uint64:
		return strconv.FormatUint(x, 10), true
	case float32:
		return formatFloat(float64(x)), true
	case float64:
		return formatFloat(x), true
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			part, ok := r.anyValue(item)
			if !ok {
				return "", false
			}
			parts = append(parts, part)
		}
		return "[" + strings.Join(parts, ", ") + "]", true
	case []string:
		return r.stringArray(x), true
	case map[string]any:
		return r.anyMap(x), true
	case map[string]string:
		return r.stringMap(x), true
	default:
		return "", false
	}
}

func (r *tomlRenderer) modelOverrides(m map[string]ProviderModelOverride) string {
	keys := make([]string, 0, len(m))
	for k, ov := range m {
		if k == "" || modelOverrideEmpty(ov) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = %s", r.key(k), r.modelOverride(m[k]))
	}
	b.WriteString(" }")
	return b.String()
}

func (r *tomlRenderer) modelOverride(ov ProviderModelOverride) string {
	var parts []string
	if ov.ReasoningProtocol != "" {
		parts = append(parts, fmt.Sprintf("reasoning_protocol = %s", r.q(ov.ReasoningProtocol)))
	}
	if len(ov.SupportedEfforts) > 0 {
		parts = append(parts, "supported_efforts = "+r.stringArray(ov.SupportedEfforts))
	}
	if ov.DefaultEffort != "" {
		parts = append(parts, fmt.Sprintf("default_effort = %s", r.q(ov.DefaultEffort)))
	}
	if ov.Vision != nil {
		parts = append(parts, fmt.Sprintf("vision = %t", *ov.Vision))
	}
	if ov.ContextWindow > 0 {
		parts = append(parts, fmt.Sprintf("context_window = %d", ov.ContextWindow))
	}
	if ov.MaxOutputTokens != 0 {
		parts = append(parts, fmt.Sprintf("max_output_tokens = %d", ov.MaxOutputTokens))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func modelOverrideEmpty(ov ProviderModelOverride) bool {
	return ov.ReasoningProtocol == "" && len(ov.SupportedEfforts) == 0 && ov.DefaultEffort == "" && ov.Vision == nil && ov.ContextWindow <= 0 && ov.MaxOutputTokens == 0
}

func hasPositiveIntMap(m map[string]int) bool {
	for k, v := range m {
		if strings.TrimSpace(k) != "" && v > 0 {
			return true
		}
	}
	return false
}

// renderIntMap renders a map[string]int as a TOML inline table with positive
// values only, preserving deterministic key order.
func (r *tomlRenderer) intMap(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if strings.TrimSpace(k) != "" && v > 0 {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = %d", r.key(k), m[k])
	}
	b.WriteString(" }")
	return b.String()
}

func (r *tomlRenderer) botCredential(cred BotConnectionCredential) string {
	parts := make(map[string]string)
	if cred.AppID != "" {
		parts["app_id"] = cred.AppID
	}
	if cred.AppSecretEnv != "" {
		parts["app_secret_env"] = cred.AppSecretEnv
	}
	if cred.AccountID != "" {
		parts["account_id"] = cred.AccountID
	}
	if cred.TokenEnv != "" {
		parts["token_env"] = cred.TokenEnv
	}
	if len(parts) == 0 {
		return ""
	}
	return r.stringMap(parts)
}

func (r *tomlRenderer) botAccess(access BotAccessConfig) string {
	hasList := len(access.Users) > 0 || len(access.Groups) > 0 || len(access.Approvers) > 0 || len(access.Admins) > 0
	if !access.Enabled && !access.AllowAll && !access.PairingEnabled && !hasList {
		return ""
	}
	var parts []string
	parts = append(parts, fmt.Sprintf("enabled = %v", access.Enabled))
	parts = append(parts, fmt.Sprintf("allow_all = %v", access.AllowAll))
	parts = append(parts, fmt.Sprintf("pairing_enabled = %v", access.PairingEnabled))
	if len(access.Users) > 0 {
		parts = append(parts, "users = "+r.stringArray(access.Users))
	}
	if len(access.Groups) > 0 {
		parts = append(parts, "groups = "+r.stringArray(access.Groups))
	}
	if len(access.Approvers) > 0 {
		parts = append(parts, "approvers = "+r.stringArray(access.Approvers))
	}
	if len(access.Admins) > 0 {
		parts = append(parts, "admins = "+r.stringArray(access.Admins))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func (r *tomlRenderer) botSessionMappings(mappings []BotConnectionSessionMapping) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, mapping := range mappings {
		if i > 0 {
			b.WriteString(", ")
		}
		parts := map[string]string{
			"remote_id":  mapping.RemoteID,
			"session_id": mapping.SessionID,
		}
		if mapping.SessionSource != "" {
			parts["session_source"] = mapping.SessionSource
		}
		if mapping.ChatType != "" {
			parts["chat_type"] = mapping.ChatType
		}
		if mapping.UserID != "" {
			parts["user_id"] = mapping.UserID
		}
		if mapping.ThreadID != "" {
			parts["thread_id"] = mapping.ThreadID
		}
		if mapping.Scope != "" {
			parts["scope"] = mapping.Scope
		}
		if mapping.WorkspaceRoot != "" {
			parts["workspace_root"] = mapping.WorkspaceRoot
		}
		if mapping.UpdatedAt != "" {
			parts["updated_at"] = mapping.UpdatedAt
		}
		b.WriteString(r.stringMap(parts))
	}
	b.WriteByte(']')
	return b.String()
}

func (r *tomlRenderer) botRoute(b *strings.Builder, route BotRouteConfig) {
	if strings.TrimSpace(route.ConnectionID) != "" {
		fmt.Fprintf(b, "connection_id = %s\n", r.q(strings.TrimSpace(route.ConnectionID)))
	}
	if strings.TrimSpace(route.Platform) != "" {
		fmt.Fprintf(b, "platform = %s\n", r.q(strings.TrimSpace(route.Platform)))
	}
	if strings.TrimSpace(route.ChatType) != "" {
		fmt.Fprintf(b, "chat_type = %s\n", r.q(strings.TrimSpace(route.ChatType)))
	}
	if strings.TrimSpace(route.ChatID) != "" {
		fmt.Fprintf(b, "chat_id = %s\n", r.q(strings.TrimSpace(route.ChatID)))
	}
	if strings.TrimSpace(route.UserID) != "" {
		fmt.Fprintf(b, "user_id = %s\n", r.q(strings.TrimSpace(route.UserID)))
	}
	if strings.TrimSpace(route.ThreadID) != "" {
		fmt.Fprintf(b, "thread_id = %s\n", r.q(strings.TrimSpace(route.ThreadID)))
	}
	if strings.TrimSpace(route.Model) != "" {
		fmt.Fprintf(b, "model = %s\n", r.q(strings.TrimSpace(route.Model)))
	}
	if strings.TrimSpace(route.ToolApprovalMode) != "" {
		fmt.Fprintf(b, "tool_approval_mode = %s\n", r.q(strings.TrimSpace(route.ToolApprovalMode)))
	}
	if strings.TrimSpace(route.WorkspaceRoot) != "" {
		fmt.Fprintf(b, "workspace_root = %s\n", r.q(strings.TrimSpace(route.WorkspaceRoot)))
	}
}

func (r *tomlRenderer) botDesktopWatcher(b *strings.Builder, watcher BotDesktopWatcherConfig) {
	if strings.TrimSpace(watcher.Platform) != "" {
		fmt.Fprintf(b, "platform = %s\n", r.q(strings.TrimSpace(watcher.Platform)))
	}
	if strings.TrimSpace(watcher.ConnectionID) != "" {
		fmt.Fprintf(b, "connection_id = %s\n", r.q(strings.TrimSpace(watcher.ConnectionID)))
	}
	if strings.TrimSpace(watcher.Domain) != "" {
		fmt.Fprintf(b, "domain = %s\n", r.q(strings.TrimSpace(watcher.Domain)))
	}
	if strings.TrimSpace(watcher.ChatType) != "" {
		fmt.Fprintf(b, "chat_type = %s\n", r.q(strings.TrimSpace(watcher.ChatType)))
	}
	if strings.TrimSpace(watcher.ChatID) != "" {
		fmt.Fprintf(b, "chat_id = %s\n", r.q(strings.TrimSpace(watcher.ChatID)))
	}
}

// renderRuleList emits a permission rule list. A populated list renders as an
// active TOML array; an empty one renders as a commented example so `reasonix setup`
// scaffolds discoverable guidance without imposing surprising rules.
func (r *tomlRenderer) ruleList(key string, rules []string, example string) string {
	if len(rules) == 0 {
		return fmt.Sprintf("# %s = %s\n", key, example)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s = [", key)
	for i, rule := range rules {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s", r.q(rule))
	}
	b.WriteString("]\n")
	return b.String()
}

// formatFloat ensures a float renders with a decimal point so TOML types it as a
// float, not an integer (e.g. 0 -> "0.0").
func formatFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
