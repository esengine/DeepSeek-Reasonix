package config

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"reasonix/internal/contract/provider"
)

type RenderScope string

const (
	RenderScopeFull    RenderScope = "full"
	RenderScopeUser    RenderScope = "user"
	RenderScopeProject RenderScope = "project"
)

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
	fmt.Fprintf(&b, "# Resolution order: flag > ./reasonix.toml > %s > built-in defaults.\n", processRoots().userConfigDisplayPath())
	b.WriteString("# Fields marked user/global only are not overridden by ./reasonix.toml.\n")
	b.WriteString("# Secrets are named via api_key_env and stored in Reasonix's global .env; never put keys here.\n\n")

	fmt.Fprintf(&b, "config_version = %d   # schema marker for diagnostics; old versions may ignore it\n", configVersion(c))
	fmt.Fprintf(&b, "default_model = %q\n", c.DefaultModel)
	if c.Language != "" {
		fmt.Fprintf(&b, "language      = %q   # ui/model language; empty = auto-detect from $LANG / $REASONIX_LANG\n", c.Language)
	} else {
		b.WriteString("# language      = \"zh\"   # ui/model language; empty = auto-detect from $LANG / $REASONIX_LANG\n")
	}
	if scope != RenderScopeProject {
		if c.AutoSubmit {
			b.WriteString("auto_submit = true   # CLI ask: commit a multi-question batch once its last question is answered; user/global only\n")
		}
		fmt.Fprintf(&b, "credentials_store = %q   # legacy compatibility; provider keys are saved in Reasonix's global .env\n", normalizeCredentialsStore(c.CredentialsStore))
	}
	b.WriteString("\n")

	if shouldRenderUI(c, defaults, scope) {
		renderUISection(&b, c, scope)
	}

	if scope != RenderScopeProject {
		renderCLIConfig(&b, c, scope)
		renderDesktopSection(&b, c)
	} else if c.Desktop.ProviderAccess != nil {
		// provider_access is intentionally mergeable across user and project
		// configs. It is the only desktop field written to reasonix.toml: local
		// providers then appear in that workspace's desktop model switcher without
		// copying user-global appearance or security preferences into the project.
		b.WriteString("[desktop]\n")
		fmt.Fprintf(&b, "provider_access = %s   # providers available to this workspace in the desktop model switcher\n\n", renderStringArray(c.Desktop.ProviderAccess))
	}

	if scope != RenderScopeProject {
		if c.CLITelemetryConfigured() {
			b.WriteString("[telemetry]\n")
			fmt.Fprintf(&b, "cli_metrics = %q   # CLI content-free usage metrics: auto|on|off; auto requires a local interactive terminal\n\n", c.CLITelemetryMode())
		}

		renderProgressWatchSection(&b, c)
		renderServeSection(&b, c)
		b.WriteString("[notifications]\n")
		fmt.Fprintf(&b, "enabled = %v   # system notifications for CLI and desktop turns; default off\n", c.Notifications.Enabled)
		fmt.Fprintf(&b, "turn_done = %v   # notify when a turn finishes\n", c.Notifications.TurnDone)
		fmt.Fprintf(&b, "approval_request = %v   # notify when a tool approval is waiting\n", c.Notifications.ApprovalRequest)
		fmt.Fprintf(&b, "ask_request = %v   # notify when a question is waiting\n", c.Notifications.AskRequest)
		fmt.Fprintf(&b, "feedback_reply = %v   # notify when a feedback report gets a reply\n", c.Notifications.FeedbackReply)
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
	if shouldRenderEnvironment(c, defaults, scope) {
		renderEnvironmentConfig(&b, c.Environment)
	}

	b.WriteString("[agent]\n")
	if shouldRenderSystemPrompt(c, defaults, scope) {
		b.WriteString("system_prompt = " + tomlMultilineBasicString(c.Agent.SystemPrompt) + "\n")
	} else {
		b.WriteString("# system_prompt = \"\"\"...\"\"\"   # omit to use the built-in prompt for this version\n")
	}
	if c.Agent.SystemPromptFile != "" {
		fmt.Fprintf(&b, "system_prompt_file = %q\n", c.Agent.SystemPromptFile)
	} else {
		b.WriteString("# system_prompt_file = \"prompts/system.md\"   # project paths stay in <workspace>; user paths may fall back to <reasonix home>\n")
	}
	fmt.Fprintf(&b, "temperature       = %s\n", formatFloat(c.Agent.Temperature))
	if strings.TrimSpace(c.Agent.RecoveryModel) != "" {
		fmt.Fprintf(&b, "recovery_model = %q   # optional independent reviewer for low-risk automatic recovery\n", c.Agent.RecoveryModel)
	} else {
		b.WriteString("# recovery_model = \"deepseek-pro\"   # optional; falls back to guardian then main model\n")
	}
	if lang := c.ReasoningLanguage(); lang != "auto" {
		fmt.Fprintf(&b, "reasoning_language = %q   # visible reasoning language: auto|zh|en\n", lang)
	} else {
		b.WriteString("# reasoning_language = \"zh\"   # visible reasoning language: auto|zh|en\n")
	}
	fmt.Fprintf(&b, "compact_ratio       = %s   # fold at this share of the model window (default 0.85)\n", formatFloat(c.Agent.CompactRatio))
	if c.Agent.Keep != nil {
		fmt.Fprintf(&b, "keep                = %s   # compaction keep policy: errors, user_marked\n", renderStringArray(c.Agent.Keep))
	} else {
		b.WriteString("# keep                = [\"errors\"]   # compaction keep policy: errors, user_marked\n")
	}
	if c.Agent.UserTurnKeepTokens > 0 {
		fmt.Fprintf(&b, "user_turn_keep_tokens = %d   # your own turns kept verbatim through a fold\n", c.Agent.UserTurnKeepTokens)
	} else {
		b.WriteString("# user_turn_keep_tokens = 8192   # your own turns kept verbatim through a fold; unset = 5% of the window\n")
	}
	if c.Agent.FirstTurnPinTokens > 0 {
		fmt.Fprintf(&b, "first_turn_pin_tokens = %d   # first turn pinned verbatim into the cached prefix\n", c.Agent.FirstTurnPinTokens)
	} else {
		b.WriteString("# first_turn_pin_tokens = 1500   # first turn pinned verbatim into the cached prefix; it is paid for every request\n")
	}
	if c.Agent.CheckpointCeilingRatio > 0 {
		fmt.Fprintf(&b, "checkpoint_ceiling_ratio = %s   # how small a fold's result must be to be accepted\n", formatFloat(c.Agent.CheckpointCeilingRatio))
	} else {
		b.WriteString("# checkpoint_ceiling_ratio = 0.50   # how small a fold's result must be to be accepted\n")
	}
	fmt.Fprintf(&b, "context_soft_limit_tokens = %d   # optional earlier absolute fold size; 0 or negative = use model capacity\n", c.Agent.ContextSoftLimitTokens)
	if c.Agent.RecentKeep > 0 {
		fmt.Fprintf(&b, "recent_keep         = %d   # minimum recent messages kept verbatim\n", c.Agent.RecentKeep)
	} else {
		b.WriteString("# recent_keep         = 2   # minimum recent messages kept verbatim\n")
	}
	if len(c.Agent.PlanModeReadOnlyCommands) > 0 {
		fmt.Fprintf(&b, "plan_mode_read_only_commands = %s   # legacy compatibility only; Plan bash uses Permissions\n", renderStringArray(c.Agent.PlanModeReadOnlyCommands))
	} else {
		b.WriteString("# plan_mode_read_only_commands = [\"gh issue view\"]   # legacy compatibility only; Plan bash uses Permissions\n")
	}
	renderEmbeddedDiffDetection(&b, c)
	renderAgentDelegation(&b, c)
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
		fmt.Fprintf(&b, "output_style = %q   # persona/tone folded into the prompt\n", c.Agent.OutputStyle)
	} else {
		b.WriteString("# output_style = \"explanatory\"   # explanatory | learning | concise | custom; empty = default\n")
	}
	b.WriteString("\n")

	if shouldRenderProviders(c, defaults, scope) {
		for _, p := range c.Providers {
			b.WriteString("[[providers]]\n")
			renderProviderEntryAnnotated(&b, p)
			b.WriteString("\n")
		}
	}

	b.WriteString("[tools]\n")
	if len(c.Tools.Enabled) == 0 {
		b.WriteString("enabled = []   # empty = all built-in tools\n")
	} else {
		fmt.Fprintf(&b, "enabled = %s\n", renderStringArray(c.Tools.Enabled))
	}
	fmt.Fprintf(&b, "bash_timeout_seconds = %d   # foreground safety cap; set 0 for no tool-local cap\n", c.BashTimeoutSeconds())
	fmt.Fprintf(&b, "mcp_startup_timeout_seconds = %d   # background initialize + tools/list safety cap; per-plugin overrides may raise it\n", c.MCPStartupTimeoutSeconds())
	fmt.Fprintf(&b, "mcp_call_timeout_seconds = %d   # default MCP call safety cap; per-plugin/tool overrides may raise it\n", c.MCPCallTimeoutSeconds())
	fmt.Fprintf(&b, "protect_changed_files = %v   # refuse a whole-file write over a change made since the agent last saw the file\n", c.Tools.ChangedFilesProtected())
	fmt.Fprintf(&b, "browser_tools = %v   # built-in browser tools; the browser starts on first use\n\n", c.Tools.BrowserToolsEnabled())
	renderMCPLoadSection(&b, c.Tools.MCPLoad)

	b.WriteString("[tools.background_jobs]\n")
	fmt.Fprintf(&b, "stalled_warning_seconds = %d   # warn once per background job after this many quiet seconds; 0 disables\n\n", c.BackgroundJobStalledWarningSeconds())

	renderToolsShell(&b, c.Tools.Shell, scope)

	renderLSPConfig(&b, c.LSP)

	renderBrowserConfig(&b, c.Browser)

	renderSkillsConfig(&b, c)

	renderMemoryConfig(&b, c.Memory)

	b.WriteString("[permissions]\n")
	b.WriteString("# Per-call gating. mode = writer fallback when no rule matches: ask|allow|deny.\n")
	b.WriteString("# Readers always default to allow. Precedence: deny > ask > allow > fallback.\n")
	b.WriteString("# Rules are \"Tool\" or \"Tool(specifier)\"; e.g. Bash(go test:*), Edit(src/**).\n")
	mode := c.Permissions.Mode
	if mode == "" {
		mode = "ask"
	}
	fmt.Fprintf(&b, "mode  = %q\n", mode)
	if c.Permissions.AllowDynamicBash {
		b.WriteString("allow_dynamic_bash = true   # advanced: let mode=allow cover command substitution and interpreter -c/-e\n")
	} else {
		b.WriteString("# allow_dynamic_bash = false   # advanced opt-in; deny/ask and exact rules still take precedence\n")
	}
	b.WriteString(renderRuleList("deny", c.Permissions.Deny, `["Bash(rm -rf*)", "Bash(git push*)"]   # hard-blocked in every mode`))
	b.WriteString(renderRuleList("allow", c.Permissions.Allow, `["Bash(go test:*)", "Bash(git status:*)"]   # never prompted`))
	b.WriteString(renderRuleList("ask", c.Permissions.Ask, `["Edit(src/**)"]   # force a prompt even if otherwise allowed`))
	b.WriteString("\n")

	b.WriteString("[sandbox]\n")
	b.WriteString("# Confine tool blast radius. File-writers (write_file/edit_file/multi_edit/move_file)\n")
	b.WriteString("# may only write under workspace_root (empty = current dir) and allow_write extras.\n")
	b.WriteString("# bash = \"enforce\" jails each command in an OS sandbox when available;\n")
	b.WriteString("# without one, bash execution is refused. Empty defaults to enforce on macOS/Linux.\n")
	b.WriteString("# Windows has no OS-level Bash sandbox and fixes bash = \"off\".\n")
	b.WriteString("# network allows sandboxed bash egress.\n")
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
	if len(c.Sandbox.ForbidRead) > 0 {
		fmt.Fprintf(&b, "forbid_read = %s\n", renderStringArray(c.Sandbox.ForbidRead))
	} else {
		b.WriteString("# forbid_read = []                  # dirs the agent cannot read or list\n")
	}
	fmt.Fprintf(&b, "bash    = %q\n", c.BashMode())
	fmt.Fprintf(&b, "network = %v\n", c.Sandbox.Network)
	fmt.Fprintf(&b, "host_authorities = %s   # host services bash may call; user-global\n", renderStringArray(c.Sandbox.HostAuthorities))
	if len(c.Sandbox.AllowedDomains) > 0 {
		fmt.Fprintf(&b, "allowed_domains = %s\n", renderStringArray(c.Sandbox.AllowedDomains))
	} else {
		b.WriteString("# allowed_domains = [\"github.com\", \"*.npmjs.org\"]   # limit bash egress to these hosts (macOS, Linux)\n")
	}
	if len(c.Sandbox.DeniedDomains) > 0 {
		fmt.Fprintf(&b, "denied_domains = %s\n", renderStringArray(c.Sandbox.DeniedDomains))
	}
	b.WriteString("\n")

	if scope != RenderScopeProject {
		b.WriteString("[statusline]   # user/global only, ./reasonix.toml cannot set it\n")
		b.WriteString("# A custom status line: a command whose first stdout line replaces the built-in\n")
		b.WriteString("# data row. It receives {\"model\",\"contextUsed\",\"contextWindow\",\"cwd\"} as JSON on stdin.\n")
		if c.Statusline.Command != "" {
			fmt.Fprintf(&b, "command = %q\n", c.Statusline.Command)
		} else {
			b.WriteString("# command = \"my-statusline.sh\"\n")
		}
		b.WriteString("\n")
	}

	renderPassthroughTable(&b, "bot", c.Bot)

	renderStorage(&b, c, scope)

	// [secrets] is user/global only: LoadForRoot discards project values, so the
	// project scope never renders it. Rendering it here keeps saved toggles across rewrites.
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
		if !c.Secrets.ProtectCredentialFiles {
			b.WriteString("protect_credential_files = false   # opt-out; lets bash read raw SSH private keys and cloud credential files\n")
		}
		b.WriteString("\n")

		// [remote] is user/global only like [secrets]: LoadForRoot discards project
		// values so a cloned repo can never inject SSH hosts. Rendered so saved hosts
		// survive full-file config rewrites.
		renderRemoteSection(&b, c.Remote)
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
			renderPluginOverrides(&b, pl)
			renderPluginPolicy(&b, pl)
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
	if c == nil {
		return ""
	}
	d := Default()
	var b strings.Builder

	if v := configVersion(c); v != d.ConfigVersion {
		fmt.Fprintf(&b, "config_version = %d\n", v)
	}
	if c.DefaultModel != d.DefaultModel {
		fmt.Fprintf(&b, "default_model = %q\n", c.DefaultModel)
	}
	if c.Language != "" && c.Language != d.Language {
		fmt.Fprintf(&b, "language = %q\n", c.Language)
	}

	// [ui] section — whole-section comparison
	if !reflect.DeepEqual(c.UI, d.UI) {
		b.WriteString("[ui]\n")
		if c.UI.Theme != d.UI.Theme {
			fmt.Fprintf(&b, "theme = %q\n", c.UITheme())
		}
		if s := c.UIThemeStyle(); s != "" && s != d.UIThemeStyle() {
			fmt.Fprintf(&b, "theme_style = %q\n", s)
		}
		if l := c.UIShortcutLayout(); l != "classic" {
			fmt.Fprintf(&b, "shortcut_layout = %q\n", l)
		}
		if strings.TrimSpace(c.UI.CursorShape) != "" {
			fmt.Fprintf(&b, "cursor_shape = %q\n", c.UICursorShape())
		}
		if c.UI.CloseBehavior != d.UI.CloseBehavior {
			fmt.Fprintf(&b, "close_behavior = %q\n", c.DesktopCloseBehavior())
		}
		if c.UI.ShowReasoning != d.UI.ShowReasoning {
			fmt.Fprintf(&b, "show_reasoning = %v\n", c.UI.ShowReasoning)
		}
		if c.UI.ShowTurnUsage != d.UI.ShowTurnUsage {
			fmt.Fprintf(&b, "show_turn_usage = %v\n", c.UI.ShowTurnUsage)
		}
		b.WriteString("\n")
	}

	// [network] section
	if !reflect.DeepEqual(c.Network, d.Network) {
		b.WriteString("[network]\n")
		if c.Network.ProxyMode != d.Network.ProxyMode {
			fmt.Fprintf(&b, "proxy_mode = %q\n", c.NetworkProxyMode())
		}
		if c.Network.ProxyURL != "" {
			fmt.Fprintf(&b, "proxy_url = %q\n", c.Network.ProxyURL)
		}
		if c.Network.NoProxy != "" {
			fmt.Fprintf(&b, "no_proxy = %q\n", c.Network.NoProxy)
		}
		if c.Network.Proxy.Type != "" || c.Network.Proxy.Server != "" || c.Network.Proxy.Port > 0 || c.Network.Proxy.Username != "" || c.Network.Proxy.Password != "" {
			b.WriteString("[network.proxy]\n")
			pt := c.Network.Proxy.Type
			if pt == "" {
				pt = "socks5"
			}
			fmt.Fprintf(&b, "type = %q\n", pt)
			if c.Network.Proxy.Server != "" {
				fmt.Fprintf(&b, "server = %q\n", c.Network.Proxy.Server)
			}
			if c.Network.Proxy.Port > 0 {
				fmt.Fprintf(&b, "port = %d\n", c.Network.Proxy.Port)
			}
			if c.Network.Proxy.Username != "" {
				fmt.Fprintf(&b, "username = %q\n", c.Network.Proxy.Username)
			}
			if c.Network.Proxy.Password != "" {
				fmt.Fprintf(&b, "password = %q\n", c.Network.Proxy.Password)
			}
		}
		b.WriteString("\n")
	}

	// [agent] section — per-field comparison
	var agentBuf strings.Builder
	anyAgent := renderAgentDelta(&agentBuf, c, d)
	anyAgent = renderAgentTail(&agentBuf, c, d) || anyAgent

	if anyAgent {
		b.WriteString("[agent]\n")
		b.WriteString(agentBuf.String())
		b.WriteString("\n")
	}

	// [[providers]] — include user-defined providers that aren't built-in
	proj := projectScopedConfigForRender(c)
	if proj != nil && len(proj.Providers) > 0 && projectProvidersDifferFromDefaults(proj.Providers, d) {
		for _, p := range proj.Providers {
			b.WriteString("[[providers]]\n")
			renderProviderEntryPlain(&b, p)
			b.WriteString("\n")
		}
	}

	// [tools]
	if len(c.Tools.Enabled) > 0 ||
		(c.Tools.BashTimeoutSeconds != nil && *c.Tools.BashTimeoutSeconds != 0) ||
		(c.Tools.MCPStartupTimeoutSeconds != nil && *c.Tools.MCPStartupTimeoutSeconds > 0) ||
		(c.Tools.MCPCallTimeoutSeconds != nil && *c.Tools.MCPCallTimeoutSeconds > 0) ||
		c.Tools.ProtectChangedFiles != nil ||
		c.Tools.BrowserTools != nil {
		b.WriteString("[tools]\n")
		if len(c.Tools.Enabled) > 0 {
			fmt.Fprintf(&b, "enabled = %s\n", renderStringArray(c.Tools.Enabled))
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
		if c.Tools.ProtectChangedFiles != nil {
			fmt.Fprintf(&b, "protect_changed_files = %v\n", *c.Tools.ProtectChangedFiles)
		}
		if c.Tools.BrowserTools != nil {
			fmt.Fprintf(&b, "browser_tools = %v\n", *c.Tools.BrowserTools)
		}
		b.WriteString("\n")
	}
	renderMCPLoadSection(&b, c.Tools.MCPLoad)

	// [tools.background_jobs]
	if c.Tools.BackgroundJobs != d.Tools.BackgroundJobs {
		if c.Tools.BackgroundJobs.StalledWarningSeconds != nil && *c.Tools.BackgroundJobs.StalledWarningSeconds > 0 {
			b.WriteString("[tools.background_jobs]\n")
			fmt.Fprintf(&b, "stalled_warning_seconds = %d\n", *c.Tools.BackgroundJobs.StalledWarningSeconds)
			b.WriteString("\n")
		}
	}

	// [tools.shell]
	// Env is user-global, so it is never part of a project delta; comparing the
	// whole struct would write an empty [tools.shell] header on every save.
	if c.Tools.Shell.Prefer != d.Tools.Shell.Prefer || c.Tools.Shell.Path != d.Tools.Shell.Path {
		b.WriteString("[tools.shell]\n")
		if c.Tools.Shell.Prefer != d.Tools.Shell.Prefer {
			fmt.Fprintf(&b, "prefer = %q\n", c.Tools.Shell.Prefer)
		}
		if c.Tools.Shell.Path != d.Tools.Shell.Path {
			fmt.Fprintf(&b, "path = %q\n", c.Tools.Shell.Path)
		}
		b.WriteString("\n")
	}

	// [lsp]
	if !reflect.DeepEqual(c.LSP, d.LSP) {
		renderLSPConfig(&b, c.LSP)
	}
	renderChangedBrowserConfig(&b, c.Browser, d.Browser)

	renderProjectSkillsConfig(&b, c, d)

	// [permissions]
	if !reflect.DeepEqual(c.Permissions, d.Permissions) {
		b.WriteString("[permissions]\n")
		mode := c.Permissions.Mode
		if mode == "" {
			mode = "ask"
		}
		if mode != "ask" {
			fmt.Fprintf(&b, "mode = %q\n", mode)
		}
		if c.Permissions.AllowDynamicBash {
			b.WriteString("allow_dynamic_bash = true\n")
		}
		if len(c.Permissions.Deny) > 0 {
			fmt.Fprintf(&b, "deny = %s\n", renderStringArray(c.Permissions.Deny))
		}
		if len(c.Permissions.Allow) > 0 {
			fmt.Fprintf(&b, "allow = %s\n", renderStringArray(c.Permissions.Allow))
		}
		if len(c.Permissions.Ask) > 0 {
			fmt.Fprintf(&b, "ask = %s\n", renderStringArray(c.Permissions.Ask))
		}
		b.WriteString("\n")
	}

	// [sandbox]
	if !reflect.DeepEqual(c.Sandbox, d.Sandbox) {
		var sandboxBuf strings.Builder
		if c.Sandbox.WorkspaceRoot != "" {
			fmt.Fprintf(&sandboxBuf, "workspace_root = %q\n", c.Sandbox.WorkspaceRoot)
		}
		if len(c.Sandbox.AllowWrite) > 0 {
			fmt.Fprintf(&sandboxBuf, "allow_write = %s\n", renderStringArray(c.Sandbox.AllowWrite))
		}
		// Only persist a bash mode when its effective value differs from the
		// platform default. On Windows, even explicit "enforce" currently
		// resolves to "off", so project configs should not imply otherwise.
		if strings.TrimSpace(c.Sandbox.Bash) != "" && c.BashMode() != d.BashModeForGOOS(runtimeGOOS) {
			fmt.Fprintf(&sandboxBuf, "bash = %q\n", c.BashMode())
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

	// [[plugins]] — always include when set; replaces all existing entries
	for _, pl := range tomlPluginsForScope(c.Plugins, RenderScopeProject) {
		b.WriteString("[[plugins]]\n")
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
		if pl.StartupTimeoutSeconds > 0 {
			fmt.Fprintf(&b, "startup_timeout_seconds = %d\n", pl.StartupTimeoutSeconds)
		}
		if pl.CallTimeoutSeconds > 0 {
			b.WriteString("# Per-server MCP call timeout; 0 keeps the global/default cap.\n")
			fmt.Fprintf(&b, "call_timeout_seconds = %d\n", pl.CallTimeoutSeconds)
		}
		if hasPositiveIntMap(pl.ToolTimeoutSeconds) {
			b.WriteString("# Raw MCP tool names with per-tool call timeouts.\n")
			fmt.Fprintf(&b, "tool_timeout_seconds = %s\n", renderIntMap(pl.ToolTimeoutSeconds))
		}
		renderPluginPolicy(&b, pl)
		b.WriteString("\n")
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
	slices.Sort(keys)
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

func shouldRenderEnvironment(c, defaults *Config, scope RenderScope) bool {
	if scope != RenderScopeProject {
		return true
	}
	return !reflect.DeepEqual(c.Environment, defaults.Environment)
}

func renderEnvironmentConfig(b *strings.Builder, cfg EnvironmentConfig) {
	b.WriteString("[environment]\n")
	enabled := true
	if cfg.Enabled != nil {
		enabled = *cfg.Enabled
	}
	fmt.Fprintf(b, "enabled = %v   # inject a stable startup environment summary into the model prompt\noffline = %v   # declare that outbound network access is unavailable; prevents futile retries\n", enabled, cfg.Offline)
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
	slices.Sort(names)
	for _, name := range names {
		fmt.Fprintf(b, "%s = %q\n", renderTOMLKeyPart(name), cfg.Tools[name])
	}
	b.WriteString("\n")
}

func shouldRenderProviders(c, defaults *Config, scope RenderScope) bool {
	if scope != RenderScopeProject {
		return true
	}
	return projectProvidersDifferFromDefaults(c.Providers, defaults)
}

func projectProvidersDifferFromDefaults(providers []ProviderEntry, defaults *Config) bool {
	if reflect.DeepEqual(providers, defaults.Providers) {
		return false
	}
	// Project edits normalize the shipped provider before rendering. Compare
	// against the same normalized baseline so pricing/catalog backfills do not
	// turn an unrelated edit into an explicit project provider override.
	normalized := Default()
	normalizeConfigForEdit(normalized)
	return !reflect.DeepEqual(providers, normalized.Providers)
}

func projectScopedConfigForRender(c *Config) *Config {
	if c == nil || len(c.providerSources) == 0 && c.projectProviderEditBaseline == nil {
		return c
	}
	cp := *c
	cp.Providers = make([]ProviderEntry, 0, len(c.Providers)+len(c.shadowedProjectProviders))
	for _, p := range c.Providers {
		if c.providerSources[providerMergeKey(p)] == providerSourceUser {
			continue
		}
		if c.projectProviderEditBaseline != nil && !c.explicitProjectProviderNames[providerMergeKey(p)] &&
			slices.ContainsFunc(c.projectProviderEditBaseline, func(before ProviderEntry) bool {
				return providerMergeKey(before) == providerMergeKey(p) && reflect.DeepEqual(before, p)
			}) {
			continue
		}
		cp.Providers = append(cp.Providers, p)
	}
	cp.Providers = append(cp.Providers, c.shadowedProjectProviders...)
	return &cp
}

func shouldRenderSystemPrompt(c, defaults *Config, scope RenderScope) bool {
	if scope == RenderScopeFull {
		return true
	}
	return strings.TrimSpace(c.Agent.SystemPrompt) != "" && c.Agent.SystemPrompt != defaults.Agent.SystemPrompt
}

func renderStorage(b *strings.Builder, c *Config, scope RenderScope) {
	if scope == RenderScopeProject || len(c.Storage) == 0 {
		return
	}
	b.WriteString("[storage]   # where data lives; user/global only, ./reasonix.toml cannot override\n")
	for _, id := range RootIDs() {
		dir := strings.TrimSpace(c.Storage[string(id)])
		if dir == "" || !RootRelocatable(id) {
			continue
		}
		fmt.Fprintf(b, "%s = %q\n", renderTOMLKeyPart(string(id)), dir)
	}
	b.WriteString("\n")
}

func renderTOMLKeyPart(key string) string {
	if isBareTOMLKey(key) {
		return key
	}
	return strconv.Quote(key)
}

func renderTOMLTablePath(parts ...string) string {
	rendered := make([]string, 0, len(parts))
	for _, part := range parts {
		rendered = append(rendered, renderTOMLKeyPart(part))
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
	keys := slices.Sorted(maps.Keys(m))
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = %q", renderTOMLKeyPart(k), m[k])
	}
	b.WriteString(" }")
	return b.String()
}

func renderAnyMap(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if strings.TrimSpace(k) == "" {
			continue
		}
		if _, ok := renderAnyValue(v); ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		value, _ := renderAnyValue(m[k])
		fmt.Fprintf(&b, "%s = %s", strconv.Quote(k), value)
	}
	b.WriteString(" }")
	return b.String()
}

func renderAnyValue(v any) (string, bool) {
	switch x := v.(type) {
	case nil:
		return "", false
	case string:
		return strconv.Quote(x), true
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
			part, ok := renderAnyValue(item)
			if !ok {
				return "", false
			}
			parts = append(parts, part)
		}
		return "[" + strings.Join(parts, ", ") + "]", true
	case []string:
		return renderStringArray(x), true
	case map[string]any:
		return renderAnyMap(x), true
	case map[string]string:
		return renderStringMap(x), true
	default:
		return "", false
	}
}

func renderModelOverrides(m map[string]ProviderModelOverride) string {
	keys := make([]string, 0, len(m))
	for k, ov := range m {
		if k == "" || modelOverrideEmpty(ov) {
			continue
		}
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q = %s", k, renderModelOverride(m[k]))
	}
	b.WriteString(" }")
	return b.String()
}

func renderModelOverride(ov ProviderModelOverride) string {
	var parts []string
	if ov.ReasoningProtocol != "" {
		parts = append(parts, fmt.Sprintf("reasoning_protocol = %q", ov.ReasoningProtocol))
	}
	if len(ov.SupportedEfforts) > 0 {
		parts = append(parts, "supported_efforts = "+renderStringArray(ov.SupportedEfforts))
	}
	if ov.DefaultEffort != "" {
		parts = append(parts, fmt.Sprintf("default_effort = %q", ov.DefaultEffort))
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
func renderIntMap(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if strings.TrimSpace(k) != "" && v > 0 {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString("{ ")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q = %d", k, m[k])
	}
	b.WriteString(" }")
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
