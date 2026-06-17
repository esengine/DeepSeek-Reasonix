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
func RenderTOMLForScope(c *Config, scope RenderScope) string {
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
	b.WriteString("# Secrets come from the environment via api_key_env; never put keys here.\n\n")

	fmt.Fprintf(&b, "config_version = %d   # schema marker for diagnostics; old versions may ignore it\n", configVersion(c))
	fmt.Fprintf(&b, "default_model = %q\n", c.DefaultModel)
	if c.Language != "" {
		fmt.Fprintf(&b, "language      = %q   # ui/model language; empty = auto-detect from $LANG / $REASONIX_LANG\n", c.Language)
	} else {
		b.WriteString("# language      = \"zh\"   # ui/model language; empty = auto-detect from $LANG / $REASONIX_LANG\n")
	}
	if scope != RenderScopeProject {
		fmt.Fprintf(&b, "credentials_store = %q   # auto|keyring|file; auto prefers the OS keychain and falls back to ~/.reasonix/credentials\n", normalizeCredentialsStore(c.CredentialsStore))
	}
	b.WriteString("\n")

	if shouldRenderUI(c, defaults, scope) {
		b.WriteString("[ui]\n")
		fmt.Fprintf(&b, "theme = %q   # auto|dark|light; CLI colors only; REASONIX_THEME can override per run\n", c.UITheme())
		if style := c.UIThemeStyle(); style != "" {
			fmt.Fprintf(&b, "theme_style = %q   # CLI accent palette; REASONIX_THEME_STYLE can override per run\n", style)
		} else {
			b.WriteString("# theme_style = \"graphite\"   # graphite|aurora|slate|carbon|nocturne|amber and legacy aliases\n")
		}
		if layout := c.UIShortcutLayout(); layout != "classic" {
			fmt.Fprintf(&b, "shortcut_layout = %q   # classic|desktop; compatibility setting; Shift+Tab toggles Plan, Ctrl+Y toggles YOLO\n", layout)
		} else {
			b.WriteString("# shortcut_layout = \"desktop\"   # classic|desktop; compatibility setting; Shift+Tab toggles Plan, Ctrl+Y toggles YOLO\n")
		}
		if strings.TrimSpace(c.UI.CloseBehavior) != "" && scope == RenderScopeProject {
			fmt.Fprintf(&b, "close_behavior = %q   # legacy desktop close behavior; prefer [desktop].close_behavior in user config\n", c.DesktopCloseBehavior())
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
			fmt.Fprintf(&b, "language = %q   # desktop UI language; empty/auto = browser/OS auto-detect\n", lang)
		} else {
			b.WriteString("# language = \"zh\"   # desktop UI language; empty/auto = browser/OS auto-detect\n")
		}
		fmt.Fprintf(&b, "layout_style = %q   # desktop layout: classic|workbench\n", c.DesktopLayoutStyle())
		fmt.Fprintf(&b, "theme = %q   # desktop only: auto|dark|light\n", c.DesktopTheme())
		if style := c.DesktopThemeStyle(); style != "" {
			fmt.Fprintf(&b, "theme_style = %q   # desktop accent palette\n", style)
		} else {
			b.WriteString("# theme_style = \"graphite\"   # graphite|aurora|slate|carbon|nocturne|amber and legacy aliases\n")
		}
		fmt.Fprintf(&b, "close_behavior = %q   # desktop: quit|background when the window close button is clicked\n", c.DesktopCloseBehavior())
		fmt.Fprintf(&b, "status_bar_style = %q   # desktop: icon|text metric labels in the bottom status bar\n", c.DesktopStatusBarStyle())
		fmt.Fprintf(&b, "status_bar_items = %s   # desktop: ordered visible bottom status bar items\n", renderStringArray(c.DesktopStatusBarItems()))
		fmt.Fprintf(&b, "check_updates = %v   # desktop: check for new versions on startup\n", c.DesktopCheckUpdates())
		fmt.Fprintf(&b, "telemetry = %v   # desktop: anonymous launch ping (install id + version + OS); never content\n", c.DesktopTelemetry())
		fmt.Fprintf(&b, "metrics = %v   # desktop: aggregate desktop metrics (anonymous signal/bucket counts); never content\n", c.DesktopMetrics())
		if len(c.Desktop.ProviderAccess) > 0 {
			fmt.Fprintf(&b, "provider_access = %s   # desktop settings: providers shown on Settings > Model > Access\n", renderStringArray(c.Desktop.ProviderAccess))
		}
		fmt.Fprintf(&b, "expand_thinking = %v   # desktop: show reasoning text expanded by default; false = collapsed\n", c.Desktop.ExpandThinking)
		b.WriteString("\n")

		b.WriteString("[notifications]\n")
		fmt.Fprintf(&b, "enabled = %v   # system notifications for CLI chat/run; default off\n", c.Notifications.Enabled)
		fmt.Fprintf(&b, "turn_done = %v   # notify when a turn finishes\n", c.Notifications.TurnDone)
		fmt.Fprintf(&b, "approval_request = %v   # notify when a tool approval is waiting\n", c.Notifications.ApprovalRequest)
		fmt.Fprintf(&b, "ask_request = %v   # notify when a question is waiting\n", c.Notifications.AskRequest)
		b.WriteString("\n")
	}

	if shouldRenderNetwork(c, defaults, scope) {
		b.WriteString("[network]\n")
		fmt.Fprintf(&b, "proxy_mode = %q   # auto|env|custom|off; auto currently uses env proxy\n", c.NetworkProxyMode())
		if c.Network.ProxyURL != "" {
			fmt.Fprintf(&b, "proxy_url  = %q   # custom override, e.g. socks5://127.0.0.1:7890\n", c.Network.ProxyURL)
		} else {
			b.WriteString("# proxy_url  = \"socks5://127.0.0.1:7890\"   # optional custom override\n")
		}
		if c.Network.NoProxy != "" {
			fmt.Fprintf(&b, "no_proxy   = %q   # honored for proxy_mode = \"custom\"\n", c.Network.NoProxy)
		} else {
			b.WriteString("# no_proxy   = \"localhost,127.0.0.1,.local\"   # honored for proxy_mode = \"custom\"\n")
		}
		b.WriteString("\n[network.proxy]\n")
		proxyType := c.Network.Proxy.Type
		if proxyType == "" {
			proxyType = "socks5"
		}
		fmt.Fprintf(&b, "type = %q   # http|https|socks5|socks5h\n", proxyType)
		if c.Network.Proxy.Server != "" {
			fmt.Fprintf(&b, "server = %q\n", c.Network.Proxy.Server)
		} else {
			b.WriteString("# server = \"127.0.0.1\"\n")
		}
		if c.Network.Proxy.Port > 0 {
			fmt.Fprintf(&b, "port = %d\n", c.Network.Proxy.Port)
		} else {
			b.WriteString("# port = 7890\n")
		}
		if c.Network.Proxy.Username != "" {
			fmt.Fprintf(&b, "username = %q\n", c.Network.Proxy.Username)
		} else {
			b.WriteString("# username = \"\"\n")
		}
		if c.Network.Proxy.Password != "" {
			fmt.Fprintf(&b, "password = %q   # supports ${VAR} expansion\n", c.Network.Proxy.Password)
		} else {
			b.WriteString("# password = \"${REASONIX_PROXY_PASSWORD}\"   # optional; supports ${VAR} expansion\n")
		}
		b.WriteString("\n")
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
		fmt.Fprintf(&b, "system_prompt_file = %q\n", c.Agent.SystemPromptFile)
	} else {
		b.WriteString("# system_prompt_file = \"prompts/system.md\"   # overrides system_prompt when set\n")
	}
	if c.Agent.MaxSteps != defaults.Agent.MaxSteps {
		fmt.Fprintf(&b, "max_steps         = %d   # executor tool-call rounds; 0 = no limit\n", c.Agent.MaxSteps)
	} else {
		b.WriteString("# max_steps         = 0   # executor tool-call rounds; 0 = no limit\n")
	}
	if c.Agent.PlannerMaxSteps != defaults.Agent.PlannerMaxSteps {
		fmt.Fprintf(&b, "planner_max_steps = %d   # planner read-only tool-call rounds; 0 = no limit\n", c.Agent.PlannerMaxSteps)
	} else {
		b.WriteString("# planner_max_steps = 12   # planner read-only tool-call rounds; 0 = no limit\n")
	}
	fmt.Fprintf(&b, "temperature       = %s\n", formatFloat(c.Agent.Temperature))
	autoPlan := c.Agent.AutoPlan
	switch strings.ToLower(strings.TrimSpace(autoPlan)) {
	case "on", "ask":
		autoPlan = "on"
	default:
		autoPlan = "off"
	}
	fmt.Fprintf(&b, "auto_plan   = %q   # off|on; off keeps plan mode manual\n", autoPlan)
	if lang := c.ReasoningLanguage(); lang != "auto" {
		fmt.Fprintf(&b, "reasoning_language = %q   # visible reasoning language: auto|zh|en\n", lang)
	} else {
		b.WriteString("# reasoning_language = \"zh\"   # visible reasoning language: auto|zh|en\n")
	}
	if c.Agent.AutoPlanClassifier != "" {
		fmt.Fprintf(&b, "auto_plan_classifier = %q   # optional provider/model for borderline auto-plan decisions\n", c.Agent.AutoPlanClassifier)
	} else {
		b.WriteString("# auto_plan_classifier = \"deepseek-flash\"   # optional; only used for borderline tasks\n")
	}
	fmt.Fprintf(&b, "soft_compact_ratio  = %s   # notice only; keeps cache-first prefix intact\n", formatFloat(c.Agent.SoftCompactRatio))
	fmt.Fprintf(&b, "compact_ratio       = %s   # try compacting when prompt reaches this fraction\n", formatFloat(c.Agent.CompactRatio))
	fmt.Fprintf(&b, "compact_force_ratio = %s   # force compacting at this high-water mark\n", formatFloat(c.Agent.CompactForceRatio))
	if c.Agent.Keep != nil {
		fmt.Fprintf(&b, "keep                = %s   # compaction keep policy: errors, user_marked\n", renderStringArray(c.Agent.Keep))
	} else {
		b.WriteString("# keep                = [\"errors\"]   # compaction keep policy: errors, user_marked\n")
	}
	if c.Agent.RecentKeep > 0 {
		fmt.Fprintf(&b, "recent_keep         = %d   # minimum recent messages kept verbatim\n", c.Agent.RecentKeep)
	} else {
		b.WriteString("# recent_keep         = 2   # minimum recent messages kept verbatim\n")
	}
	fmt.Fprintf(&b, "cold_resume_prune   = %v   # elide stale tool results when reopening a session past the provider cache window\n", c.ColdResumePruneEnabled())
	if c.Agent.PlannerModel != "" {
		fmt.Fprintf(&b, "planner_model = %q   # low-frequency planner (two-model collaboration)\n", c.Agent.PlannerModel)
	} else {
		b.WriteString("# planner_model = \"mimo\"   # optional: enable two-model collaboration\n")
	}
	if c.Agent.SubagentModel != "" {
		fmt.Fprintf(&b, "subagent_model = %q   # default model for runAs=subagent skills\n", c.Agent.SubagentModel)
	} else {
		b.WriteString("# subagent_model = \"deepseek-pro\"   # optional default for runAs=subagent skills\n")
	}
	if len(c.Agent.SubagentModels) > 0 {
		fmt.Fprintf(&b, "subagent_models = %s   # per-skill overrides\n", renderStringMap(c.Agent.SubagentModels))
	} else {
		b.WriteString("# subagent_models = { review = \"deepseek-pro\", security_review = \"deepseek-pro\" }   # per-skill overrides\n")
	}
	if c.Agent.SubagentEffort != "" {
		fmt.Fprintf(&b, "subagent_effort = %q   # default effort for subagent entry points\n", c.Agent.SubagentEffort)
	} else {
		b.WriteString("# subagent_effort = \"high\"   # optional default effort for subagents\n")
	}
	if len(c.Agent.SubagentEfforts) > 0 {
		fmt.Fprintf(&b, "subagent_efforts = %s   # per-tool/skill effort overrides\n", renderStringMap(c.Agent.SubagentEfforts))
	} else {
		b.WriteString("# subagent_efforts = { review = \"max\", task = \"high\" }   # per-tool/skill effort overrides\n")
	}
	if c.Agent.OutputStyle != "" {
		fmt.Fprintf(&b, "output_style = %q   # persona/tone folded into the prompt\n", c.Agent.OutputStyle)
	} else {
		b.WriteString("# output_style = \"explanatory\"   # explanatory | learning | concise | custom; empty = default\n")
	}
	b.WriteString("\n")

	if shouldRenderProviders(c, defaults, scope) {
		for _, p := range c.Providers {
			b.WriteString("[[providers]]\n")
			fmt.Fprintf(&b, "name        = %q\n", p.Name)
			fmt.Fprintf(&b, "kind        = %q\n", p.Kind)
			fmt.Fprintf(&b, "base_url    = %q\n", p.BaseURL)
			if len(p.Models) > 0 {
				fmt.Fprintf(&b, "models      = %s\n", renderStringArray(p.Models))
				if p.Default != "" {
					fmt.Fprintf(&b, "default     = %q\n", p.Default)
				}
			} else if p.Model != "" {
				fmt.Fprintf(&b, "model       = %q\n", p.Model)
			}
			if p.ModelsURL != "" {
				fmt.Fprintf(&b, "models_url  = %q   # auto-fetch models from this URL on startup\n", p.ModelsURL)
			}
			fmt.Fprintf(&b, "api_key_env = %q\n", p.APIKeyEnv)
			if p.BalanceURL != "" {
				fmt.Fprintf(&b, "balance_url = %q   # optional; wallet-balance endpoint shown in the status bar\n", p.BalanceURL)
			}
			if p.ContextWindow > 0 {
				fmt.Fprintf(&b, "context_window = %d   # tokens; compaction triggers near this limit\n", p.ContextWindow)
			}
			if p.Price != nil {
				fmt.Fprintf(&b, "price       = %s   # provider-wide fallback, per 1M tokens\n", renderPricingInline(p.Price))
			}
			if len(p.Prices) > 0 {
				fmt.Fprintf(&b, "prices      = %s   # per-model prices, per 1M tokens\n", renderPricingMap(p.Prices))
			}
			if p.Thinking != "" {
				fmt.Fprintf(&b, "thinking    = %q\n", p.Thinking)
			}
			if p.Effort != "" {
				fmt.Fprintf(&b, "effort      = %q\n", p.Effort)
			}
			if p.Vision {
				b.WriteString("vision      = true   # provider accepts image input for all listed models\n")
			}
			if p.VisionModels != nil {
				fmt.Fprintf(&b, "vision_models = %s   # models in this provider that accept image input\n", renderStringArray(p.VisionModels))
			}
			if p.VisionDetail != "" {
				fmt.Fprintf(&b, "vision_detail = %q   # openai image detail hint: low|high; empty = auto\n", p.VisionDetail)
			}
			if p.ReasoningProtocol != "" {
				fmt.Fprintf(&b, "reasoning_protocol = %q   # auto|deepseek|openai|none; overrides model/endpoint reasoning detection\n", p.ReasoningProtocol)
			}
			if len(p.SupportedEfforts) > 0 {
				fmt.Fprintf(&b, "supported_efforts = %s   # custom /effort levels exposed by this provider; overrides the built-in Kind/BaseURL default\n", renderStringArray(p.SupportedEfforts))
			}
			if p.DefaultEffort != "" {
				fmt.Fprintf(&b, "default_effort    = %q   # used when /effort is auto or unset; must be one of supported_efforts\n", p.DefaultEffort)
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
			fmt.Fprintf(&b, "%q", t)
		}
		b.WriteString("]\n")
	}
	fmt.Fprintf(&b, "bash_timeout_seconds = %d   # foreground safety cap; set 0 for no tool-local cap\n\n", c.BashTimeoutSeconds())

	b.WriteString("[tools.background_jobs]\n")
	fmt.Fprintf(&b, "stalled_warning_seconds = %d   # warn once per background job after this many quiet seconds; 0 disables\n\n", c.BackgroundJobStalledWarningSeconds())

	renderLSPConfig(&b, c.LSP)

	b.WriteString("[skills]\n")
	if len(c.Skills.Paths) > 0 {
		fmt.Fprintf(&b, "paths = %s   # extra custom skill roots\n", renderStringArray(c.Skills.Paths))
	} else {
		b.WriteString("# paths = [\"~/my-skills\", \"../shared/skills\"]   # extra custom skill roots\n")
	}
	if len(c.Skills.ExcludedPaths) > 0 {
		fmt.Fprintf(&b, "excluded_paths = %s   # skill roots hidden from discovery\n", renderStringArray(c.Skills.ExcludedPaths))
	} else {
		b.WriteString("# excluded_paths = [\"~/.agents/skills\"]   # hide convention roots without deleting folders\n")
	}
	if c.Skills.MaxDepth != 0 {
		fmt.Fprintf(&b, "max_depth = %d   # nested scan depth; default 3, set 1 for legacy root-only discovery\n", c.SkillMaxDepth())
	} else {
		b.WriteString("# max_depth = 3   # nested scan depth; set 1 for legacy root-only discovery\n")
	}
	if disabled := c.DisabledSkillNames(); len(disabled) > 0 {
		fmt.Fprintf(&b, "disabled_skills = %s   # hidden from the prompt, slash invocation, and skill tools\n\n", renderStringArray(disabled))
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
	fmt.Fprintf(&b, "mode  = %q\n", mode)
	b.WriteString(renderRuleList("deny", c.Permissions.Deny, `["Bash(rm -rf*)", "Bash(git push*)"]   # hard-blocked in every mode`))
	b.WriteString(renderRuleList("allow", c.Permissions.Allow, `["Bash(go test:*)", "Bash(git status:*)"]   # never prompted`))
	b.WriteString(renderRuleList("ask", c.Permissions.Ask, `["Edit(src/**)"]   # force a prompt even if otherwise allowed`))
	b.WriteString("\n")

	b.WriteString("[sandbox]\n")
	b.WriteString("# Confine tool blast radius. File-writers (write_file/edit_file/multi_edit/move_file)\n")
	b.WriteString("# may only write under workspace_root (empty = current dir) and allow_write extras.\n")
	b.WriteString("# bash = \"enforce\" (default) jails each command in an OS sandbox (macOS now;\n")
	b.WriteString("# graceful fallback elsewhere); \"off\" disables it. network allows egress.\n")
	if c.Sandbox.WorkspaceRoot != "" {
		fmt.Fprintf(&b, "workspace_root = %q\n", c.Sandbox.WorkspaceRoot)
	} else {
		b.WriteString("# workspace_root = \"\"            # default: current working directory\n")
	}
	if len(c.Sandbox.AllowWrite) > 0 {
		fmt.Fprintf(&b, "allow_write = %s\n", renderStringArray(c.Sandbox.AllowWrite))
	} else {
		b.WriteString("# allow_write = [\"/tmp\"]          # extra dirs writers may also modify\n")
	}
	fmt.Fprintf(&b, "bash    = %q\n", c.BashMode())
	fmt.Fprintf(&b, "network = %v\n", c.Sandbox.Network)
	b.WriteString("\n")

	b.WriteString("[statusline]\n")
	b.WriteString("# A custom status line: a command whose first stdout line replaces the built-in\n")
	b.WriteString("# data row. It receives {\"model\",\"contextUsed\",\"contextWindow\",\"cwd\"} as JSON on stdin.\n")
	if c.Statusline.Command != "" {
		fmt.Fprintf(&b, "command = %q\n", c.Statusline.Command)
	} else {
		b.WriteString("# command = \"my-statusline.sh\"\n")
	}
	b.WriteString("\n")

	if shouldRenderBot(c, defaults, scope) {
		b.WriteString("# Bot gateway: multi-channel IM bot for QQ, Feishu/Lark, and WeChat.\n")
		b.WriteString("[bot]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.Enabled)
		if c.Bot.Model != "" {
			fmt.Fprintf(&b, "model = %q\n", c.Bot.Model)
		} else {
			b.WriteString("# model = \"\"   # empty = default_model\n")
		}
		if c.Bot.ToolApprovalMode != "" {
			fmt.Fprintf(&b, "tool_approval_mode = %q   # ask|auto|yolo; yolo skips tool approvals only\n", c.Bot.ToolApprovalMode)
		} else {
			b.WriteString("# tool_approval_mode = \"ask\"   # ask|auto|yolo; ask and plan decisions still wait\n")
		}
		fmt.Fprintf(&b, "max_steps = %d\n", c.Bot.MaxSteps)
		fmt.Fprintf(&b, "debounce_ms = %d\n", c.Bot.DebounceMs)
		b.WriteString("\n[bot.allowlist]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.Allowlist.Enabled)
		fmt.Fprintf(&b, "allow_all = %v\n", c.Bot.Allowlist.AllowAll)
		fmt.Fprintf(&b, "qq_users = %s\n", renderStringArray(c.Bot.Allowlist.QQUsers))
		fmt.Fprintf(&b, "feishu_users = %s\n", renderStringArray(c.Bot.Allowlist.FeishuUsers))
		fmt.Fprintf(&b, "weixin_users = %s\n", renderStringArray(c.Bot.Allowlist.WeixinUsers))
		fmt.Fprintf(&b, "qq_groups = %s\n", renderStringArray(c.Bot.Allowlist.QQGroups))
		fmt.Fprintf(&b, "feishu_groups = %s\n", renderStringArray(c.Bot.Allowlist.FeishuGroups))
		fmt.Fprintf(&b, "weixin_groups = %s\n", renderStringArray(c.Bot.Allowlist.WeixinGroups))
		b.WriteString("\n[bot.qq]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.QQ.Enabled)
		fmt.Fprintf(&b, "app_id = %q\n", c.Bot.QQ.AppID)
		fmt.Fprintf(&b, "app_secret_env = %q\n", c.Bot.QQ.AppSecretEnv)
		fmt.Fprintf(&b, "sandbox = %v\n", c.Bot.QQ.Sandbox)
		b.WriteString("\n[bot.feishu]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.Feishu.Enabled)
		fmt.Fprintf(&b, "app_id = %q\n", c.Bot.Feishu.AppID)
		fmt.Fprintf(&b, "domain = %q\n", c.Bot.Feishu.Domain)
		fmt.Fprintf(&b, "app_secret_env = %q\n", c.Bot.Feishu.AppSecretEnv)
		fmt.Fprintf(&b, "verification_token = %q\n", c.Bot.Feishu.VerificationToken)
		fmt.Fprintf(&b, "mode = %q\n", c.Bot.Feishu.Mode)
		fmt.Fprintf(&b, "webhook_port = %d\n", c.Bot.Feishu.WebhookPort)
		fmt.Fprintf(&b, "require_mention = %v\n", c.Bot.Feishu.RequireMention)
		b.WriteString("\n[bot.weixin]\n")
		fmt.Fprintf(&b, "enabled = %v\n", c.Bot.Weixin.Enabled)
		fmt.Fprintf(&b, "account_id = %q\n", c.Bot.Weixin.AccountID)
		fmt.Fprintf(&b, "token_env = %q\n", c.Bot.Weixin.TokenEnv)
		fmt.Fprintf(&b, "api_base = %q\n", c.Bot.Weixin.APIBase)
		for _, conn := range c.Bot.Connections {
			b.WriteString("\n[[bot.connections]]\n")
			fmt.Fprintf(&b, "id = %q\n", conn.ID)
			fmt.Fprintf(&b, "provider = %q\n", conn.Provider)
			fmt.Fprintf(&b, "domain = %q\n", conn.Domain)
			fmt.Fprintf(&b, "label = %q\n", conn.Label)
			fmt.Fprintf(&b, "enabled = %v\n", conn.Enabled)
			fmt.Fprintf(&b, "status = %q\n", conn.Status)
			if conn.Model != "" {
				fmt.Fprintf(&b, "model = %q\n", conn.Model)
			}
			if conn.ToolApprovalMode != "" {
				fmt.Fprintf(&b, "tool_approval_mode = %q\n", conn.ToolApprovalMode)
			}
			if conn.WorkspaceRoot != "" {
				fmt.Fprintf(&b, "workspace_root = %q\n", conn.WorkspaceRoot)
			}
			if conn.LastError != "" {
				fmt.Fprintf(&b, "last_error = %q\n", conn.LastError)
			}
			if conn.CreatedAt != "" {
				fmt.Fprintf(&b, "created_at = %q\n", conn.CreatedAt)
			}
			if conn.UpdatedAt != "" {
				fmt.Fprintf(&b, "updated_at = %q\n", conn.UpdatedAt)
			}
			if parts := renderBotCredential(conn.Credential); parts != "" {
				fmt.Fprintf(&b, "credential = %s\n", parts)
			}
			if len(conn.SessionMappings) > 0 {
				fmt.Fprintf(&b, "session_mappings = %s\n", renderBotSessionMappings(conn.SessionMappings))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("# External MCP servers. type: \"stdio\" (default, a subprocess) | \"http\" | \"sse\".\n")
	b.WriteString("# ${VAR} / ${VAR:-default} are expanded from the environment in command/args/env/url/headers.\n")
	if len(c.Plugins) == 0 {
		b.WriteString("# [[plugins]]\n")
		b.WriteString("# name    = \"example\"\n")
		b.WriteString("# command = \"reasonix-plugin-example\"\n")
		b.WriteString("# [[plugins]]                                  # a remote server over Streamable HTTP\n")
		b.WriteString("# name    = \"stripe\"\n")
		b.WriteString("# type    = \"http\"\n")
		b.WriteString("# url     = \"https://mcp.stripe.com\"\n")
		b.WriteString("# headers = { Authorization = \"Bearer ${STRIPE_KEY}\" }\n")
	} else {
		for _, pl := range c.Plugins {
			b.WriteString("\n[[plugins]]\n")
			fmt.Fprintf(&b, "name    = %q\n", pl.Name)
			if pl.Type != "" {
				fmt.Fprintf(&b, "type    = %q\n", pl.Type)
			}
			if pl.Command != "" {
				fmt.Fprintf(&b, "command = %q\n", pl.Command)
			}
			if len(pl.Args) > 0 {
				fmt.Fprintf(&b, "args    = %s\n", renderStringArray(pl.Args))
			}
			if pl.URL != "" {
				fmt.Fprintf(&b, "url     = %q\n", pl.URL)
			}
			if len(pl.Headers) > 0 {
				fmt.Fprintf(&b, "headers = %s\n", renderStringMap(pl.Headers))
			}
			if len(pl.Env) > 0 {
				fmt.Fprintf(&b, "env     = %s\n", renderStringMap(pl.Env))
			}
			if pl.AutoStart != nil {
				fmt.Fprintf(&b, "auto_start = %v\n", *pl.AutoStart)
			}
		}
	}

	return b.String()
}

func renderPricingInline(p *provider.Pricing) string {
	if p == nil {
		return "{}"
	}
	return fmt.Sprintf("{ cache_hit = %v, input = %v, output = %v, currency = %q }",
		p.CacheHit, p.Input, p.Output, p.Symbol())
}

func renderPricingMap(prices map[string]*provider.Pricing) string {
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
		fmt.Fprintf(&b, "%s = %s", strconv.Quote(model), renderPricingInline(prices[model]))
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
	cp.Providers = make([]ProviderEntry, 0, len(c.Providers))
	for _, p := range c.Providers {
		if c.providerSources[providerMergeKey(p)] == providerSourceUser {
			continue
		}
		cp.Providers = append(cp.Providers, p)
	}
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

func renderLSPConfig(b *strings.Builder, cfg LSPConfig) {
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
		fmt.Fprintf(b, "[lsp.servers.%s]\n", renderTOMLKeyPart(lang))
		if srv.Command != "" {
			fmt.Fprintf(b, "command = %q\n", srv.Command)
		}
		if len(srv.Args) > 0 {
			fmt.Fprintf(b, "args = %s\n", renderStringArray(srv.Args))
		}
		if len(srv.Env) > 0 {
			fmt.Fprintf(b, "env = %s\n", renderStringMap(srv.Env))
		}
		if srv.LanguageID != "" {
			fmt.Fprintf(b, "language_id = %q\n", srv.LanguageID)
		}
		if len(srv.Extensions) > 0 {
			fmt.Fprintf(b, "extensions = %s\n", renderStringArray(srv.Extensions))
		}
		if srv.InstallHint != "" {
			fmt.Fprintf(b, "install_hint = %q\n", srv.InstallHint)
		}
		b.WriteString("\n")
	}
}

func renderTOMLKeyPart(key string) string {
	if isBareTOMLKey(key) {
		return key
	}
	return strconv.Quote(key)
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

// renderStringArray renders a []string as a TOML inline array.
func renderStringArray(ss []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range ss {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", s)
	}
	b.WriteByte(']')
	return b.String()
}

// renderStringMap renders a map[string]string as a TOML inline table with keys
// in sorted order so output is deterministic (round-trips cleanly).
func renderStringMap(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = %q", k, m[k])
	}
	b.WriteString(" }")
	return b.String()
}

func renderBotCredential(cred BotConnectionCredential) string {
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
	return renderStringMap(parts)
}

func renderBotSessionMappings(mappings []BotConnectionSessionMapping) string {
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
		b.WriteString(renderStringMap(parts))
	}
	b.WriteByte(']')
	return b.String()
}

// renderRuleList emits a permission rule list. A populated list renders as an
// active TOML array; an empty one renders as a commented example so `reasonix setup`
// scaffolds discoverable guidance without imposing surprising rules.
func renderRuleList(key string, rules []string, example string) string {
	if len(rules) == 0 {
		return fmt.Sprintf("# %s = %s\n", key, example)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s = [", key)
	for i, r := range rules {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", r)
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
