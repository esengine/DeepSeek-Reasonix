// Package config loads Reasonix's runtime configuration from TOML. Resolution order:
// flag > project ./reasonix.toml > user ~/.config/reasonix/config.toml > built-in defaults.
// Secrets come from the environment via api_key_env and are never stored in
// config files.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"reasonix/internal/netclient"
	"reasonix/internal/provider"
)

var validSkillName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// IsValidSkillName reports whether name is a usable skill identifier.
func IsValidSkillName(name string) bool { return validSkillName.MatchString(name) }

// SkillNameKey normalizes a skill identifier for config comparisons.
func SkillNameKey(name string) string {
	name = strings.TrimSpace(name)
	if !IsValidSkillName(name) {
		return ""
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

// Config is Reasonix's runtime configuration.
type Config struct {
	ConfigVersion int                 `toml:"config_version"`
	DefaultModel  string              `toml:"default_model"`
	Language      string              `toml:"language"` // ui/model language tag (e.g. "zh"); empty = auto-detect from $LANG / $REASONIX_LANG
	UI            UIConfig            `toml:"ui"`
	Desktop       DesktopConfig       `toml:"desktop"`
	Notifications NotificationsConfig `toml:"notifications"`
	Agent         AgentConfig         `toml:"agent"`
	Providers     []ProviderEntry     `toml:"providers"`
	Tools         ToolsConfig         `toml:"tools"`
	Permissions   PermissionsConfig   `toml:"permissions"`
	Sandbox       SandboxConfig       `toml:"sandbox"`
	Network       NetworkConfig       `toml:"network"`
	Plugins       []PluginEntry       `toml:"plugins"`
	Skills        SkillsConfig        `toml:"skills"`
	Codegraph     CodegraphConfig     `toml:"codegraph"`
	BuiltInMCP    BuiltInMCPConfig    `toml:"builtin_mcp"`
	Statusline    StatuslineConfig    `toml:"statusline"`
	LSP           LSPConfig           `toml:"lsp"`
	Bot           BotConfig           `toml:"bot"`
}

// UIConfig controls CLI presentation-only settings. Desktop appearance is kept in
// DesktopConfig so desktop preferences cannot alter terminal output or prompts.
type UIConfig struct {
	Theme          string `toml:"theme"`           // auto|dark|light; empty resolves to auto
	ThemeStyle     string `toml:"theme_style"`     // graphite|aurora|slate|carbon|nocturne|amber and legacy aliases
	ShortcutLayout string `toml:"shortcut_layout"` // classic|desktop; accepted for compatibility
	CloseBehavior  string `toml:"close_behavior"`  // legacy desktop close behavior; prefer desktop.close_behavior
	ShowReasoning  bool   `toml:"show_reasoning"`  // Ctrl+O / /verbose: show thinking text in CLI; false = collapsed
}

// DesktopConfig controls desktop-only UI preferences. It is intentionally
// separate from top-level language and [ui] so desktop choices do not affect CLI
// language, terminal colours, or provider-visible prompt/request data.
type DesktopConfig struct {
	Language       string   `toml:"language"`        // auto|en|zh; empty/auto = browser/OS auto-detect
	Theme          string   `toml:"theme"`           // auto|dark|light; empty resolves to dark
	ThemeStyle     string   `toml:"theme_style"`     // graphite|aurora|slate|carbon|nocturne|amber and legacy aliases
	CloseBehavior  string   `toml:"close_behavior"`  // quit|background; desktop window close behavior
	DisplayMode    string   `toml:"display_mode"`    // standard|compact|minimal; transcript display mode
	CheckUpdates   *bool    `toml:"check_updates"`   // startup update checks; nil keeps the default enabled
	Telemetry      *bool    `toml:"telemetry"`       // anonymous launch ping (install id + version + OS); nil keeps the default enabled
	Metrics        *bool    `toml:"metrics"`         // opt-in aggregate agent metrics (anonymous signal/bucket counts; no content); nil = disabled
	ProviderAccess []string `toml:"provider_access"` // desktop-only list of provider entries shown in Settings > Model > Access
	ExpandThinking bool     `toml:"expand_thinking"` // true = show reasoning text expanded by default; false = collapsed
}

// NotificationsConfig controls optional system notifications for CLI chat/run.
type NotificationsConfig struct {
	Enabled         bool `toml:"enabled"`
	TurnDone        bool `toml:"turn_done"`
	ApprovalRequest bool `toml:"approval_request"`
	AskRequest      bool `toml:"ask_request"`
}

// UITheme normalizes ui.theme to a supported value.
func (c *Config) UITheme() string {
	switch strings.ToLower(strings.TrimSpace(c.UI.Theme)) {
	case "dark":
		return "dark"
	case "light":
		return "light"
	default:
		return "auto"
	}
}

// UIThemeStyle normalizes ui.theme_style. Empty means "pick the default style
// for the resolved light/dark shell".
func (c *Config) UIThemeStyle() string {
	return normalizeThemeStyle(c.UI.ThemeStyle)
}

// UIShortcutLayout normalizes the legacy CLI shortcut layout setting. It is kept
// for compatibility; Shift+Tab toggles Plan and Ctrl+Y toggles YOLO in both
// layouts.
func (c *Config) UIShortcutLayout() string {
	switch strings.ToLower(strings.TrimSpace(c.UI.ShortcutLayout)) {
	case "desktop", "dual", "dual-axis", "dual_axis":
		return "desktop"
	default:
		return "classic"
	}
}

func normalizeThemeStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "graphite", "aurora", "slate", "carbon", "nocturne", "amber", "ember", "midnight", "sandstone", "porcelain", "linen", "glacier":
		return strings.ToLower(strings.TrimSpace(style))
	default:
		return ""
	}
}

func normalizeCloseBehavior(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "quit", "exit":
		return "quit"
	default:
		return "background"
	}
}

// DesktopLanguage normalizes the desktop UI language. Empty means auto-detect
// from the browser/OS locale; it deliberately does not read top-level language,
// which is used by the CLI/model-facing runtime.
func (c *Config) DesktopLanguage() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.Language)) {
	case "en":
		return "en"
	case "zh":
		return "zh"
	default:
		return ""
	}
}

// DesktopTheme normalizes desktop.theme. New desktop users default to the light
// graphite product look; an explicit auto/light/dark is preserved.
func (c *Config) DesktopTheme() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.Theme)) {
	case "auto":
		return "auto"
	case "light":
		return "light"
	case "dark":
		return "dark"
	default:
		return "light"
	}
}

// DesktopThemeStyle normalizes desktop.theme_style. Empty means the frontend
// chooses the default style for the resolved desktop theme.
func (c *Config) DesktopThemeStyle() string {
	return normalizeThemeStyle(c.Desktop.ThemeStyle)
}

// DesktopCloseBehavior normalizes the desktop close-window preference. It falls
// back to the legacy ui.close_behavior value for configs written before [desktop]
// existed.
func (c *Config) DesktopCloseBehavior() string {
	if strings.TrimSpace(c.Desktop.CloseBehavior) != "" {
		return normalizeCloseBehavior(c.Desktop.CloseBehavior)
	}
	return normalizeCloseBehavior(c.UI.CloseBehavior)
}

// UICloseBehavior is the legacy name for DesktopCloseBehavior.
func (c *Config) UICloseBehavior() string {
	return c.DesktopCloseBehavior()
}

// DesktopDisplayMode normalizes the transcript display mode. Default is
// "minimal" (collapsed model-generated intermediate items).
func (c *Config) DesktopDisplayMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.DisplayMode)) {
	case "standard":
		return "standard"
	case "compact":
		return "compact"
	case "minimal":
		return "minimal"
	default:
		return "minimal"
	}
}

// DesktopCheckUpdates reports whether the desktop should check for updates on
// startup. Missing configs default to true so existing users keep update notices.
func (c *Config) DesktopCheckUpdates() bool {
	if c == nil || c.Desktop.CheckUpdates == nil {
		return true
	}
	return *c.Desktop.CheckUpdates
}

// DesktopTelemetry reports whether the desktop sends the anonymous launch ping.
// It carries no conversation, key, or file data — see desktop/README.md.
func (c *Config) DesktopTelemetry() bool {
	if c == nil || c.Desktop.Telemetry == nil {
		return true
	}
	return *c.Desktop.Telemetry
}

// DesktopMetrics reports whether the desktop sends opt-in aggregate agent
// metrics — anonymous (signal, bucket) counters, never content. Default off.
func (c *Config) DesktopMetrics() bool {
	if c == nil || c.Desktop.Metrics == nil {
		return false
	}
	return *c.Desktop.Metrics
}

// LSPConfig governs the optional Language Server Protocol tools (lsp_definition,
// lsp_references, lsp_hover, lsp_diagnostics). Enabled defaults to true; the
// servers themselves are never bundled — each resolves on PATH and the tool
// returns an install hint when it is missing, so the capability is dormant until
// the user installs a server. Servers overrides or extends the built-in language
// → server map, keyed by language id (e.g. "go", "rust", "python").
type LSPConfig struct {
	Enabled bool                 `toml:"enabled"`
	Servers map[string]LSPServer `toml:"servers"`
}

// LSPServer overrides a built-in language's server or, when keyed by a new
// language, adds one. An empty field falls back to the built-in default for that
// language; Extensions is required when adding a language the built-ins don't
// cover (e.g. ".ex" for Elixir) so files route to it.
type LSPServer struct {
	Command     string            `toml:"command"`
	Args        []string          `toml:"args"`
	Env         map[string]string `toml:"env"`
	LanguageID  string            `toml:"language_id"`
	Extensions  []string          `toml:"extensions"`
	InstallHint string            `toml:"install_hint"`
}

// StatuslineConfig configures a custom status line. Command, when set, is run at
// startup and after each turn; its first line of stdout replaces the built-in
// status data row. A JSON payload (model, context tokens, cwd) is fed on stdin.
type StatuslineConfig struct {
	Command string `toml:"command"`
}

// CodegraphConfig governs the built-in CodeGraph MCP server — symbol/call-graph
// code intelligence (tree-sitter + SQLite) that gives the agent codegraph_*
// search / context / explore / trace / node tools. Enabled defaults to true so
// upgrades keep it for existing configs; first-run scaffolds write enabled =
// false so only brand-new users start without it. AutoInstall (default true)
// lets reasonix fetch the CodeGraph runtime into its cache when CodeGraph is
// enabled but missing; set false to require an explicit `reasonix codegraph
// install` (e.g. for air-gapped or headless runs). Path overrides binary
// resolution; empty resolves the cache, then a `codegraph` on PATH, then a
// bundle beside the executable. CodeGraph always starts in the background when
// enabled; legacy tier values are ignored and removed during config load.
type CodegraphConfig struct {
	Enabled     bool   `toml:"enabled"`
	AutoInstall bool   `toml:"auto_install"`
	Path        string `toml:"path"`
	Tier        string `toml:"tier"`
}

func (c CodegraphConfig) ShouldAutoStart() bool {
	return c.Enabled
}

func (c CodegraphConfig) ResolvedTier() string {
	return "background"
}

// BuiltInMCPConfig controls Reasonix-shipped MCP servers that require no user
// server definition. They are off by default and become provider-visible only
// after the user enables them.
type BuiltInMCPConfig struct {
	TimeEnabled     bool `toml:"time_enabled"`
	Context7Enabled bool `toml:"context7_enabled"`
}

func (c BuiltInMCPConfig) Enabled(name string) bool {
	switch name {
	case "time":
		return c.TimeEnabled
	case "context7":
		return c.Context7Enabled
	default:
		return false
	}
}

func (c *BuiltInMCPConfig) SetEnabled(name string, enabled bool) bool {
	switch name {
	case "time":
		c.TimeEnabled = enabled
		return true
	case "context7":
		c.Context7Enabled = enabled
		return true
	default:
		return false
	}
}

func (c BuiltInMCPConfig) EnabledNames() []string {
	var out []string
	if c.TimeEnabled {
		out = append(out, "time")
	}
	if c.Context7Enabled {
		out = append(out, "context7")
	}
	return out
}

// BotConfig 控制多渠道 IM bot 消息网关。
type BotConfig struct {
	Enabled     bool                  `toml:"enabled"`
	Model       string                `toml:"model"` // 用于 bot 的模型名，空则用 default_model
	MaxSteps    int                   `toml:"max_steps"`
	DebounceMs  int                   `toml:"debounce_ms"` // 消息合并窗口，毫秒
	Allowlist   BotAllowlist          `toml:"allowlist"`
	QQ          QQBotConfig           `toml:"qq"`
	Feishu      FeishuBotConfig       `toml:"feishu"`
	Weixin      WeixinBotConfig       `toml:"weixin"`
	Connections []BotConnectionConfig `toml:"connections"`
}

// BotAllowlist 控制哪些用户可以使用 bot。
type BotAllowlist struct {
	Enabled      bool     `toml:"enabled"`
	AllowAll     bool     `toml:"allow_all"`
	QQUsers      []string `toml:"qq_users"`
	FeishuUsers  []string `toml:"feishu_users"`
	WeixinUsers  []string `toml:"weixin_users"`
	QQGroups     []string `toml:"qq_groups"`
	FeishuGroups []string `toml:"feishu_groups"`
	WeixinGroups []string `toml:"weixin_groups"`
}

// QQBotConfig QQ 官方 Bot API v2 配置。
type QQBotConfig struct {
	Enabled      bool   `toml:"enabled"`
	AppID        string `toml:"app_id"`
	AppSecretEnv string `toml:"app_secret_env"` // 环境变量名，如 QQ_BOT_APP_SECRET
}

// FeishuBotConfig 飞书自建应用 Bot 配置。
type FeishuBotConfig struct {
	Enabled           bool   `toml:"enabled"`
	Domain            string `toml:"domain"` // feishu（默认）| lark
	AppID             string `toml:"app_id"`
	AppSecretEnv      string `toml:"app_secret_env"`     // 如 FEISHU_BOT_APP_SECRET
	VerificationToken string `toml:"verification_token"` // 事件订阅验证 token
	Mode              string `toml:"mode"`               // webhook（默认）| websocket
	WebhookPort       int    `toml:"webhook_port"`       // webhook 模式端口
	RequireMention    bool   `toml:"require_mention"`
}

// WeixinBotConfig 微信 iLink Bot 配置。
type WeixinBotConfig struct {
	Enabled   bool   `toml:"enabled"`
	AccountID string `toml:"account_id"`
	TokenEnv  string `toml:"token_env"` // 环境变量名，如 WEIXIN_BOT_TOKEN
	APIBase   string `toml:"api_base"`  // iLink API base URL
}

// BotConnectionConfig is the desktop-friendly connection record for IM bot
// channels. It keeps install/runtime state separate from legacy per-provider
// knobs so the UI can expose a simple "connect first" flow while old configs
// keep working.
type BotConnectionConfig struct {
	ID              string                        `toml:"id"`
	Provider        string                        `toml:"provider"` // qq|feishu|weixin
	Domain          string                        `toml:"domain"`   // feishu|lark|weixin|qq
	Label           string                        `toml:"label"`
	Enabled         bool                          `toml:"enabled"`
	Status          string                        `toml:"status"` // disconnected|pending|connected|error
	Model           string                        `toml:"model"`
	WorkspaceRoot   string                        `toml:"workspace_root"`
	Credential      BotConnectionCredential       `toml:"credential"`
	SessionMappings []BotConnectionSessionMapping `toml:"session_mappings"`
	LastError       string                        `toml:"last_error"`
	CreatedAt       string                        `toml:"created_at"`
	UpdatedAt       string                        `toml:"updated_at"`
}

type BotConnectionCredential struct {
	AppID        string `toml:"app_id"`
	AppSecretEnv string `toml:"app_secret_env"`
	AccountID    string `toml:"account_id"`
	TokenEnv     string `toml:"token_env"`
}

type BotConnectionSessionMapping struct {
	RemoteID      string `toml:"remote_id"`
	SessionID     string `toml:"session_id"`
	Scope         string `toml:"scope"`
	WorkspaceRoot string `toml:"workspace_root"`
	UpdatedAt     string `toml:"updated_at"`
}

// NetworkConfig controls ordinary outbound HTTP traffic such as model providers,
// wallet-balance lookups, updater checks, CodeGraph downloads, and web_fetch.
// web_fetch reuses these proxy settings while keeping its own SSRF-guarded
// dialer.
type NetworkConfig struct {
	// ProxyMode is "auto" (default; environment proxy for now), "env", "custom",
	// or "off". auto leaves room for OS proxy detection later without changing the
	// config shape.
	ProxyMode string `toml:"proxy_mode"`
	// ProxyURL is an advanced custom override such as "socks5://127.0.0.1:7890".
	// When set and proxy_mode = "custom", it wins over the structured proxy table.
	ProxyURL string `toml:"proxy_url"`
	// NoProxy is honored for custom proxies. Env/auto modes use NO_PROXY from the
	// process environment instead.
	NoProxy string             `toml:"no_proxy"`
	Proxy   NetworkProxyConfig `toml:"proxy"`
}

// NetworkProxyConfig is the structured custom-proxy editor shape. Password is
// optional and supports ${VAR} expansion, so users can avoid storing it literally.
type NetworkProxyConfig struct {
	Type     string `toml:"type"` // http|https|socks5|socks5h
	Server   string `toml:"server"`
	Port     int    `toml:"port"`
	Username string `toml:"username"`
	Password string `toml:"password"`
}

// NetworkProxySpec returns the expanded proxy settings used by netclient.
func (c *Config) NetworkProxySpec() netclient.ProxySpec {
	return netclient.ProxySpec{
		Mode:        c.Network.ProxyMode,
		URL:         ExpandVars(c.Network.ProxyURL),
		NoProxy:     ExpandVars(c.Network.NoProxy),
		Type:        c.Network.Proxy.Type,
		Server:      ExpandVars(c.Network.Proxy.Server),
		Port:        c.Network.Proxy.Port,
		Username:    ExpandVars(c.Network.Proxy.Username),
		Password:    ExpandVars(c.Network.Proxy.Password),
		DirectHosts: c.directProxyHosts(),
	}
}

// directProxyHosts collects the base_url hosts of providers marked no_proxy, so
// netclient bypasses the proxy for them without knowing any provider by name.
//
// Only for an auto-detected proxy (auto/env): that proxy is typically a
// GFW-circumvention one not meant for domestic endpoints (e.g. mimo), so keep
// them direct. An explicit proxy_mode = "custom" is the user saying "route
// everything through this" — e.g. a mandatory corporate proxy — so honor it for
// every provider; a custom-proxy user who wants a host direct uses
// network.no_proxy instead (#3635).
func (c *Config) directProxyHosts() []string {
	if c.NetworkProxyMode() == netclient.ModeCustom {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range c.Providers {
		if !p.NoProxy {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(p.BaseURL))
		if err != nil {
			continue
		}
		if h := u.Hostname(); h != "" && !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	return out
}

// NetworkProxyMode normalizes network.proxy_mode to a known value.
func (c *Config) NetworkProxyMode() string {
	return netclient.NormalizeMode(c.Network.ProxyMode)
}

// SkillsConfig configures skill discovery. Paths adds extra "custom"-scope skill
// roots — each a directory of SKILL.md / <name>.md playbooks — scanned between
// the project roots (.reasonix/.agents/.agent/.claude under the workspace) and
// the global roots. ExcludedPaths hides matching discovery roots without deleting
// folders. ~, relative paths, and ${VAR} expansion are supported. DisabledSkills
// hides named skills from the agent prompt, slash invocation, and skill tools
// while keeping them manageable.
type SkillsConfig struct {
	Paths          []string `toml:"paths"`
	ExcludedPaths  []string `toml:"excluded_paths"`
	DisabledSkills []string `toml:"disabled_skills"`
	MaxDepth       int      `toml:"max_depth"`
}

// SkillCustomPaths returns the configured custom skill roots with ${VAR}
// expanded; empty entries are dropped.
func (c *Config) SkillCustomPaths() []string {
	var out []string
	for _, p := range c.Skills.Paths {
		if p = ExpandVars(p); strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// SkillExcludedPaths returns configured skill roots that should be hidden from
// discovery, with ${VAR} expanded and empty entries dropped.
func (c *Config) SkillExcludedPaths() []string {
	var out []string
	for _, p := range c.Skills.ExcludedPaths {
		if p = ExpandVars(p); strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// SkillMaxDepth bounds nested skill discovery. Depth 3 favors bundled skill
// packs while Store keeps nested markdown safe by requiring descriptions.
func (c *Config) SkillMaxDepth() int {
	const (
		defaultDepth = 3
		maxDepth     = 5
	)
	if c == nil || c.Skills.MaxDepth == 0 {
		return defaultDepth
	}
	if c.Skills.MaxDepth < 1 {
		return 1
	}
	if c.Skills.MaxDepth > maxDepth {
		return maxDepth
	}
	return c.Skills.MaxDepth
}

// DisabledSkillNames returns valid disabled skill identifiers, preserving the
// first spelling and dropping duplicates/empty entries.
func (c *Config) DisabledSkillNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, name := range c.Skills.DisabledSkills {
		name = strings.TrimSpace(name)
		if !IsValidSkillName(name) {
			continue
		}
		key := SkillNameKey(name)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// IsSkillDisabled reports whether name is configured as disabled.
func (c *Config) IsSkillDisabled(name string) bool {
	key := SkillNameKey(name)
	if key == "" {
		return false
	}
	for _, disabled := range c.DisabledSkillNames() {
		if SkillNameKey(disabled) == key {
			return true
		}
	}
	return false
}

// SandboxConfig bounds the blast radius of tool calls (Phase 0: file-writer
// confinement). WorkspaceRoot is the directory the built-in file writers
// (write_file / edit_file / multi_edit) may modify; empty means the current
// working directory, so writes stay inside the project by default. AllowWrite
// lists extra directories writers may also touch (e.g. a sibling repo or a temp
// dir). Both support ${VAR} / ${VAR:-default} expansion. Reads are unrestricted;
// confining `bash` is Phase 1 (OS-level sandbox).
type SandboxConfig struct {
	WorkspaceRoot string   `toml:"workspace_root"`
	AllowWrite    []string `toml:"allow_write"`
	// Bash is the OS-sandbox mode for the bash tool: "enforce" (default) jails
	// each command, "off" runs it unconfined. Phase 1; macOS only for now, with
	// a graceful fallback elsewhere (see internal/sandbox).
	Bash string `toml:"bash"`
	// Network allows network egress from inside the bash sandbox. Defaults true
	// so module/package downloads keep working; the boundary is then writes.
	Network bool `toml:"network"`
}

// WriteRoots returns the directories file-writer tools may modify: the
// workspace root (defaulting to the current working directory when unset) plus
// any AllowWrite extras, with ${VAR} expanded. The roots are returned as given
// (relative or absolute); the confiner resolves them to absolute, symlink-free
// paths. The result is always non-empty, so confinement is on by default.
func (c *Config) WriteRoots() []string {
	return c.WriteRootsForRoot(".")
}

// WriteRootsForRoot is like WriteRoots but falls back to fallbackRoot when the
// config doesn't explicitly set a workspace_root. Desktop tabs pass their
// project root here so tool confinement is correct without changing cwd.
func (c *Config) WriteRootsForRoot(fallbackRoot string) []string {
	root := ExpandVars(c.Sandbox.WorkspaceRoot)
	if root == "" {
		root = fallbackRoot
		if root == "" || root == "." {
			if wd, err := os.Getwd(); err == nil {
				root = wd
			} else {
				root = "."
			}
		}
	}
	roots := []string{root}
	for _, d := range c.Sandbox.AllowWrite {
		if d = ExpandVars(d); d != "" {
			roots = append(roots, d)
		}
	}
	return roots
}

// BashMode normalises the bash-sandbox mode: only an explicit "off" disables
// it; empty or any other value resolves to "enforce", so the sandbox is on by
// default and fails safe.
func (c *Config) BashMode() string {
	if c.Sandbox.Bash == "off" {
		return "off"
	}
	return "enforce"
}

// AgentConfig configures the harness loop. PlannerModel is optional: when set
// to another provider's name it enables two-model collaboration, where the
// planner handles low-frequency planning in its own session (kept separate so
// each model's prompt prefix stays cache-stable). SubagentModel is the optional
// default for runAs=subagent skills; SubagentModels overrides it per skill name.
type AgentConfig struct {
	SystemPrompt     string            `toml:"system_prompt"`
	SystemPromptFile string            `toml:"system_prompt_file"`
	MaxSteps         int               `toml:"max_steps"`         // tool-call rounds per turn; 0 = unlimited
	PlannerMaxSteps  int               `toml:"planner_max_steps"` // planner read-only tool-call rounds; 0 = unlimited
	Temperature      float64           `toml:"temperature"`
	PlannerModel     string            `toml:"planner_model"`
	SubagentModel    string            `toml:"subagent_model"`
	SubagentModels   map[string]string `toml:"subagent_models"`
	SubagentEffort   string            `toml:"subagent_effort"`
	SubagentEfforts  map[string]string `toml:"subagent_efforts"`
	// OutputStyle selects a persona/tone block folded into the system prompt at
	// startup (a built-in like "explanatory"/"learning"/"concise", or a custom
	// .reasonix/output-styles/<name>.md). Empty = the unmodified prompt.
	OutputStyle string `toml:"output_style"`
	// AutoPlan controls whether interactive turns that look multi-step start in
	// plan mode automatically: "off" keeps plan mode manual, "on" enables the
	// approval gate. Legacy "ask" is treated as "on".
	AutoPlan string `toml:"auto_plan"`
	// AutoPlanClassifier optionally names a provider/model used to classify
	// borderline auto-plan decisions. Empty keeps the zero-cost heuristic path.
	AutoPlanClassifier string `toml:"auto_plan_classifier"`
	// Compaction window fractions: soft = notice only, compact = trigger, force = hard ceiling.
	SoftCompactRatio  float64 `toml:"soft_compact_ratio"`
	CompactRatio      float64 `toml:"compact_ratio"`
	CompactForceRatio float64 `toml:"compact_force_ratio"`
}

// ProviderEntry declares a model provider instance. ContextWindow is the model's
// token budget; the harness compacts older history as a turn's prompt approaches
// it (see agent compaction). 0 disables compaction for the instance.
type ProviderEntry struct {
	Name          string            `toml:"name"`
	Kind          string            `toml:"kind"`
	BaseURL       string            `toml:"base_url"`
	Model         string            `toml:"model"`      // a single model (back-compat)
	Models        []string          `toml:"models"`     // a vendor's model list (one base_url/key, many models)
	ModelsURL     string            `toml:"models_url"` // auto-fetch models from this URL on startup
	Default       string            `toml:"default"`    // default model when Models is set (else Models[0])
	APIKeyEnv     string            `toml:"api_key_env"`
	BalanceURL    string            `toml:"balance_url"` // optional; a provider-specific wallet-balance endpoint (DeepSeek: https://api.deepseek.com/user/balance). Empty = no balance readout.
	ContextWindow int               `toml:"context_window"`
	Price         *provider.Pricing `toml:"price"`
	// Thinking / Effort are provider-kind-specific knobs forwarded to the provider
	// via Config.Extra. The anthropic provider reads Thinking="adaptive" to enable
	// extended thinking and Effort ("low".."max") to tune depth. The
	// openai-compatible provider forwards Effort as reasoning_effort for
	// thinking-capable models; DeepSeek accepts high|max.
	// Empty = provider default.
	Thinking string `toml:"thinking"`
	Effort   string `toml:"effort"`
	// ReasoningProtocol selects the request shape for OpenAI-compatible reasoning
	// models. Empty/auto uses the model capability registry plus endpoint
	// heuristics; none disables automatic reasoning controls for this provider.
	ReasoningProtocol string `toml:"reasoning_protocol"`
	// SupportedEfforts lists the /effort levels this provider/model exposes.
	// When non-empty, it overrides the built-in defaults derived from
	// Kind/BaseURL and makes /effort configurable. "auto" is the implicit
	// prefix — always accepted. DefaultEffort resolves it; omit DefaultEffort
	// (or set one outside this list) to fall back to SupportedEfforts[0].
	SupportedEfforts []string `toml:"supported_efforts"`
	// DefaultEffort is the /effort level used when the user picks "auto" or
	// has not set Effort. Ignored when SupportedEfforts is empty.
	DefaultEffort string `toml:"default_effort"`
	// NoProxy reaches this provider's base_url directly, never through the proxy.
	// For China-only endpoints a foreign-exit proxy resets the TLS handshake (#2803).
	NoProxy bool `toml:"no_proxy"`
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

// IsLikelyChatModel reports whether a model ID looks like a chat/completion
// model rather than a specialised audio/vision/embedding model. It applies a
// conservative name-based heuristic — the OpenAI-compatible /models API does
// not return capability/modality metadata, so this is the most reliable
// fallback until providers add such fields.
//
// The heuristic works in two passes:
//  1. Multi-word substring check for compound terms that span separators
//     (e.g. "text-embedding", "text-to-speech").
//  2. Token-level check: the model ID is split on common separators (- _ . / :)
//     and each token is compared against a set of known non-chat keywords.
//
// "voice" is intentionally absent from the non-chat set because it is too
// broad — legitimate future chat models may include it in their name.
func IsLikelyChatModel(model string) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	lower := strings.ToLower(model)

	// Pass 1: compound terms that span separator boundaries.
	var compoundNonChat = []string{
		"text-embedding", "text-to-speech", "speech-to-text",
	}
	for _, c := range compoundNonChat {
		if strings.Contains(lower, c) {
			return false
		}
	}

	// Pass 2: token-level check.
	tokens := strings.FieldsFunc(lower, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/' || r == ':'
	})
	var nonChatTokens = map[string]bool{
		"asr": true, "stt": true, "tts": true,
		"whisper": true, "embedding": true,
		"moderation": true, "rerank": true, "dall": true,
		"transcription": true,
	}
	for _, tok := range tokens {
		if nonChatTokens[tok] {
			return false
		}
	}
	return true
}

// ChatModelList returns ModelList filtered to likely chat/completion models.
// Non-chat models (TTS, STT, ASR, embedding, etc.) are excluded so they do
// not appear in the chat model picker. Use ModelList() only when the full
// raw provider model list is needed, such as config serialization, provider
// diagnostics, or model-fetch editing.
func (e *ProviderEntry) ChatModelList() []string {
	raw := e.ModelList()
	if len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, m := range raw {
		if IsLikelyChatModel(m) {
			out = append(out, m)
		}
	}
	return out
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

// HasModel reports whether m is one of the provider's models.
func (e *ProviderEntry) HasModel(m string) bool {
	for _, x := range e.ModelList() {
		if x == m {
			return true
		}
	}
	return false
}

// ToolsConfig selects which built-in tools are enabled. Empty means all of them.
type ToolsConfig struct {
	Enabled            []string     `toml:"enabled"`
	BashTimeoutSeconds *int         `toml:"bash_timeout_seconds"`
	Search             SearchConfig `toml:"search"`
	Shell              ShellConfig  `toml:"shell"`
}

const defaultBashTimeoutSeconds = 120

// BashTimeoutSeconds returns the foreground bash timeout in seconds. An omitted
// config keeps the historical 120s safety cap, explicit 0 disables the
// tool-local cap, and positive values set a custom cap. Negative values fall
// back to the default so a typo cannot silently remove the safety net.
func (c *Config) BashTimeoutSeconds() int {
	if c.Tools.BashTimeoutSeconds == nil || *c.Tools.BashTimeoutSeconds < 0 {
		return defaultBashTimeoutSeconds
	}
	return *c.Tools.BashTimeoutSeconds
}

// SearchConfig tunes the grep tool's engine. Engine is "auto" (default — use
// ripgrep when it's on PATH, else the native Go scanner), "native" (always Go),
// or "rg" (require ripgrep; warn at startup and fall back to native if absent).
// RgPath optionally points at a specific ripgrep binary instead of a PATH lookup.
type SearchConfig struct {
	Engine string `toml:"engine"`
	RgPath string `toml:"rg_path"`
}

// ShellConfig chooses the interpreter the bash tool runs commands under. Prefer
// is "auto" (default — real bash when present, else PowerShell on Windows),
// "bash", or "powershell"/"pwsh" (force it; warn at startup and fall back to
// auto if absent). Path optionally points at a specific shell executable.
type ShellConfig struct {
	Prefer string `toml:"prefer"`
	Path   string `toml:"path"`
}

// PermissionsConfig declares the per-call permission policy (see
// internal/permission). Mode is the fallback decision for writer tools when no
// rule matches ("ask" | "allow" | "deny"; default "ask"); read-only tools always
// fall back to allow. Allow/Ask/Deny are rule lists of the form "ToolName" or
// "ToolName(glob)". Precedence: deny > ask > allow > fallback.
type PermissionsConfig struct {
	Mode  string   `toml:"mode"`
	Allow []string `toml:"allow"`
	Ask   []string `toml:"ask"`
	Deny  []string `toml:"deny"`
}

// PluginEntry declares an external MCP server. Type selects the transport:
// "stdio" (default) launches Command/Args/Env as a subprocess; "http"
// (a.k.a. streamable-http) and "sse" connect to a remote URL with optional
// static Headers. String fields support ${VAR} / ${VAR:-default} expansion so
// secrets (bearer tokens, keys) come from the environment, not the file. The
// fields mirror Claude Code's mcpServers spec, so entries can come from either
// reasonix.toml's [[plugins]] or a project-root .mcp.json (see loadMCPJSON).
type PluginEntry struct {
	Name    string            `toml:"name"`
	Type    string            `toml:"type"` // "stdio" (default) | "http" | "sse"
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	URL     string            `toml:"url"`
	Headers map[string]string `toml:"headers"`
	// AutoStart controls whether the server connects during session startup.
	// Nil preserves historical behavior: configured servers start automatically.
	AutoStart *bool `toml:"auto_start"`
	// Tier selects how aggressively the server is connected at boot:
	//   "eager"      — blocks startup until the handshake completes; required for
	//                  servers whose tools the system prompt depends on.
	//   "lazy"       — registers placeholder tools immediately (from on-disk
	//                  schema cache when available) and only spawns the real
	//                  subprocess on first model use. Kept for legacy configs.
	//   "background" — placeholder + spawn fired at boot but not waited on;
	//                  swap happens once the spawn finishes.
	// Empty defaults to "background" so enabled MCPs connect automatically
	// without blocking chat. Unknown non-empty values fall back to "lazy".
	Tier string `toml:"tier"`
}

func (e PluginEntry) ShouldAutoStart() bool {
	return e.AutoStart == nil || *e.AutoStart
}

// ResolvedTier returns the normalized tier ("eager"|"lazy"|"background") with
// the project default applied. Unknown values fall back to "lazy" so a typo
// never forces a slow boot.
func (e PluginEntry) ResolvedTier() string {
	return resolvedMCPTier(e.Tier)
}

func resolvedMCPTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background":
		return "background"
	case "":
		return "background"
	default:
		return "lazy"
	}
}

func (c *Config) AutoStartPlugins() []PluginEntry {
	out := make([]PluginEntry, 0, len(c.Plugins))
	for _, p := range c.Plugins {
		if p.ShouldAutoStart() {
			out = append(out, p)
		}
	}
	return out
}

// DefaultSystemPrompt is used when config provides none.
const DefaultSystemPrompt = `You are Reasonix, a coding agent focused on executing code tasks.
Use the provided tools to read and write files and run shell commands.
Principles: understand the request before acting; verify with tools instead of
guessing; keep changes minimal and correct; briefly summarize what you did.
When the request leaves a real choice to the user — which approach or library,
the scope, or a consequential or ambiguous decision — call the ask tool to offer
2-4 concrete options rather than guessing or burying the question in prose. Skip
it when there's an obvious default; don't ask just to confirm. Approval-bypass
modes do not answer ask questions or approve plans for the user. If no
interactive user is available, the ask tool returns a model-assumption fallback;
state the assumption you made before proceeding.
For multi-step work, track progress with the todo_write tool: lay out the steps,
keep exactly one in_progress, and flip each to completed as you finish it — update
the list as you go, not just at the end.
In plan mode the harness blocks writer tools: do read-only research, then write a
concise plan as your reply and stop. The user is asked to approve before anything
is changed; once approved, work through the steps, updating the task list as you go.`

// LanguagePolicy is the auto fallback appended to the system prompt when no
// concrete UI language is resolved. It is static English text, so it stays part
// of the cache-stable prefix and avoids per-turn language injection.
const LanguagePolicy = `Reply in the same language the user is using in their most recent message: ` +
	`if they write in Chinese answer in Chinese, in English answer in English, and switch ` +
	`whenever they switch. Let this also guide the language you think in. Always keep code, ` +
	`identifiers, file paths, shell commands, and technical terms in their original form — never translate them.`

// Default returns the built-in default configuration (DeepSeek + MiMo presets).
func Default() *Config {
	return &Config{
		ConfigVersion: 2,
		DefaultModel:  "deepseek-flash",
		UI:            UIConfig{Theme: "auto"},
		Notifications: NotificationsConfig{
			Enabled:         false,
			TurnDone:        true,
			ApprovalRequest: true,
			AskRequest:      true,
		},
		Agent: AgentConfig{
			SystemPrompt: DefaultSystemPrompt,
			// 0 = no step cap: the agent loops until the model gives a final answer,
			// the user cancels, or the provider errors. Context stays bounded by
			// compaction, not by a round count. Set a positive agent.max_steps only
			// if you want a hard guard against runaway.
			MaxSteps:          0,
			PlannerMaxSteps:   12,
			AutoPlan:          "off",
			SoftCompactRatio:  0.5,
			CompactRatio:      0.8,
			CompactForceRatio: 0.9,
		},
		// Mode "ask" with no rules keeps `reasonix run` autonomous (no TTY → ask
		// resolves to allow) while `reasonix chat` prompts before writers. Users add
		// deny/allow rules to harden or quiet specific tools.
		Permissions: PermissionsConfig{Mode: "ask"},
		// Sandbox on by default: bash is jailed (macOS), network allowed so
		// builds/downloads work. Set bash = "off" to disable. Network=true here
		// so an absent [sandbox] in a user's file keeps egress (zero value would
		// wrongly deny it).
		Sandbox: SandboxConfig{Bash: "enforce", Network: true},
		// CodeGraph code-intelligence defaults on so existing configs (which never
		// wrote a [codegraph] section) keep it after an upgrade. First-run scaffolds
		// write enabled = false instead, so only brand-new users start without it.
		// AutoInstall fetches the runtime into the cache when enabled and missing.
		Codegraph: CodegraphConfig{Enabled: true, AutoInstall: true},
		// Time is dependency-free and bundled, so expose it by default. Context7
		// can invoke a package runner and remains opt-in.
		BuiltInMCP: BuiltInMCPConfig{TimeEnabled: true},
		// LSP tools on by default, but dormant until a language server is on PATH;
		// a missing server yields an install hint rather than an error.
		LSP:     LSPConfig{Enabled: true},
		Network: NetworkConfig{ProxyMode: netclient.ModeAuto},
		Bot: BotConfig{
			MaxSteps:   25,
			DebounceMs: 1500,
			Allowlist:  BotAllowlist{Enabled: true},
			QQ:         QQBotConfig{AppSecretEnv: "QQ_BOT_APP_SECRET"},
			Feishu:     FeishuBotConfig{Domain: "feishu", AppSecretEnv: "FEISHU_BOT_APP_SECRET", Mode: "webhook", WebhookPort: 8080, RequireMention: true},
			Weixin:     WeixinBotConfig{AccountID: "default", TokenEnv: "WEIXIN_BOT_TOKEN", APIBase: "https://ilinkai.weixin.qq.com"},
		},
		Providers: []ProviderEntry{
			{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance", ContextWindow: 1_000_000, Price: &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"}},
			{Name: "deepseek-pro", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro", APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance", ContextWindow: 1_000_000, Price: &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"}},
			{Name: "mimo-pro", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5-pro", APIKeyEnv: "MIMO_API_KEY", ContextWindow: 1_000_000, Price: &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"}, NoProxy: true},
			{Name: "mimo-flash", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5", APIKeyEnv: "MIMO_API_KEY", ContextWindow: 1_000_000, Price: &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"}, NoProxy: true},
		},
	}
}

// Load builds the configuration: defaults, then user config, then project
// config, then MCP servers from Claude Code's .mcp.json, then (lowest priority)
// the v0.x ~/.reasonix/config.json's mcpServers. A .env in the working directory
// is loaded first so api_key_env can resolve.
func Load() (*Config, error) {
	return LoadForRoot(".")
}

// LoadForRoot builds the configuration with project files resolved from root
// instead of the current working directory. When root is "" or ".", it behaves
// like Load(). This is the workspace-aware entry point: desktop tabs use it so
// each project's reasonix.toml + .env + .mcp.json are resolved independently
// without changing the process cwd.
func LoadForRoot(root string) (*Config, error) {
	root = resolveRoot(root)
	loadDotEnvForRoot(root)
	cfg := Default()

	projectTOML := "reasonix.toml"
	if root != "." {
		projectTOML = filepath.Join(root, "reasonix.toml")
	}

	var tomlSources []string
	if uc := userConfigPath(); uc != "" {
		tomlSources = append(tomlSources, uc)
	}
	tomlSources = append(tomlSources, projectTOML)
	sawConfigFile := false
	for _, path := range tomlSources {
		if _, err := os.Stat(path); err == nil {
			sawConfigFile = true
			if err := migrateLegacyMCPTiersFile(path); err != nil {
				slog.Warn("config: legacy mcp tier migration failed", "path", path, "err", err)
			}
		}
		if err := mergeFile(cfg, path); err != nil {
			return nil, err
		}
	}
	// toml.DecodeFile replaces [[plugins]] wholesale, so cfg.Plugins now holds
	// only the last file's. Re-merge by name across all sources (later wins) so a
	// project reasonix.toml doesn't drop the global config's MCP servers.
	plugins, err := mergeTOMLPlugins(tomlSources)
	if err != nil {
		return nil, err
	}
	cfg.Plugins = plugins

	// Claude Code's .mcp.json (project root) is read last and merged into
	// [[plugins]], so a server configured for Claude works here unchanged.
	// reasonix.toml wins on a name collision (see mergeMCPJSON).
	mcpFile := mcpJSONFile
	if root != "." {
		mcpFile = filepath.Join(root, mcpJSONFile)
	}
	entries, err := loadMCPJSON(mcpFile)
	if err != nil {
		return nil, err
	}
	cfg.mergeMCPJSON(entries)

	// Lowest priority: the v0.x ~/.reasonix/config.json's mcpServers, so upgrading
	// from the TypeScript line keeps MCP servers without rewriting them. Anything
	// the v2 config or .mcp.json already declared wins on a name collision.
	cfg.mergeMCPJSON(loadLegacyMCP(legacyConfigPath()))
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeEffortConfig(cfg)
	backfillDeepSeekPro(cfg)
	// First run (no config file anywhere): keep CodeGraph off until the user opts
	// in. An existing config — even one without a [codegraph] section — keeps the
	// built-in default (on), so an upgrade never silently drops code intelligence.
	if !sawConfigFile {
		cfg.Codegraph.Enabled = false
	}
	return cfg, nil
}

// backfillDeepSeekPro restores deepseek-pro for configs the pre-fix setup wizard
// wrote with only deepseek-v4-flash: a keyless /models probe used to drop the Pro
// SKU, leaving users unable to switch to it. In-memory only — the user's file is
// untouched. Narrowly scoped to the official DeepSeek endpoint (which is known to
// serve pro) so a custom flash-only deployment isn't given an entry that 404s.
func backfillDeepSeekPro(c *Config) {
	const flashModel, proModel = "deepseek-v4-flash", "deepseek-v4-pro"
	var flash *ProviderEntry
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == "deepseek-pro" {
			return
		}
		for _, m := range p.ModelList() {
			switch m {
			case proModel:
				return // pro already reachable
			case flashModel:
				if strings.Contains(p.BaseURL, "api.deepseek.com") {
					flash = p
				}
			}
		}
	}
	if flash == nil {
		return
	}
	for _, bp := range Default().Providers {
		if bp.Name == "deepseek-pro" {
			bp.APIKeyEnv = flash.APIKeyEnv
			c.Providers = append(c.Providers, bp)
			return
		}
	}
}

func resolveRoot(root string) string {
	if root == "" || root == "." {
		return "."
	}
	return filepath.Clean(root)
}

// normalizeLegacyEffort migrates the retired DeepSeek effort="off" (the old
// /thinking off that disabled thinking) to the provider default, so a config
// written by an older version keeps loading instead of erroring on a value the
// provider no longer accepts.
func normalizeLegacyEffort(c *Config) {
	for i := range c.Providers {
		if strings.EqualFold(strings.TrimSpace(c.Providers[i].Effort), "off") {
			c.Providers[i].Effort = ""
		}
	}
}

// mergeTOMLPlugins merges [[plugins]] across TOML sources by name (later source wins).
func mergeTOMLPlugins(paths []string) ([]PluginEntry, error) {
	var merged []PluginEntry
	index := map[string]int{}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Config
		if _, err := toml.DecodeFile(path, &f); err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
		for _, p := range f.Plugins {
			p, _ = NormalizePluginCommandLine(p)
			if i, ok := index[p.Name]; ok {
				merged[i] = p
				continue
			}
			index[p.Name] = len(merged)
			merged = append(merged, p)
		}
	}
	return merged, nil
}

// LoadForEdit returns a config to seed the `reasonix setup` wizard when reconfiguring:
// the built-in defaults with the file at path (if present) decoded on top, so a
// reconfigure preserves the user's existing providers and agent settings instead
// of resetting to defaults. .env is loaded so api_key_env resolution works while
// the wizard decides which keys are still missing.
func LoadForEdit(path string) *Config {
	loadDotEnv()
	cfg := Default()
	if _, err := os.Stat(path); err == nil {
		if err := migrateLegacyMCPTiersFile(path); err != nil {
			slog.Warn("config: legacy mcp tier migration failed", "path", path, "err", err)
		}
	}
	if err := mergeFile(cfg, path); err != nil {
		slog.Warn("config: load for edit failed, using defaults", "path", path, "err", err)
	}
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	normalizeLegacyMCPTiers(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeEffortConfig(cfg)
	return cfg
}

// mergeFile decodes a TOML file onto cfg if it exists. An absent file is not an error.
func mergeFile(cfg *Config, path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return fmt.Errorf("config %s: %w", path, err)
	}
	return nil
}

// normalizeLegacyMCPTiers keeps loaded legacy config files on the new product
// behavior: enabled MCP servers connect in the background by default, and the
// retired per-server startup tier is no longer a user-facing setting.
func normalizeLegacyMCPTiers(c *Config) {
	if c == nil {
		return
	}
	c.Codegraph.Tier = ""
	for i := range c.Plugins {
		c.Plugins[i].Tier = ""
	}
}

func migrateLegacyMCPTiersFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next, changed := stripLegacyMCPTierLines(string(raw))
	if !changed {
		return nil
	}
	return os.WriteFile(path, []byte(next), info.Mode().Perm())
}

func stripLegacyMCPTierLines(raw string) (string, bool) {
	lines := strings.Split(raw, "\n")
	section := ""
	changed := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if header := tomlSectionHeader(line); header != "" {
			section = header
		}
		if (section == "codegraph" || section == "plugins") && isTOMLKeyAssignment(line, "tier") {
			changed = true
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), changed
}

func tomlSectionHeader(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "[") {
		return ""
	}
	if i := strings.Index(trimmed, "#"); i >= 0 {
		trimmed = strings.TrimSpace(trimmed[:i])
	}
	switch trimmed {
	case "[codegraph]":
		return "codegraph"
	case "[[plugins]]":
		return "plugins"
	default:
		return "other"
	}
}

func isTOMLKeyAssignment(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") || !strings.HasPrefix(trimmed, key) {
		return false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, key))
	return strings.HasPrefix(rest, "=")
}

// normalizeLegacyProviderModels repairs provider entries written by older
// desktop builds that carried the official provider name/endpoint but omitted the
// model field. The repair is intentionally narrow: valid user-provided model
// lists are left untouched, while known official aliases get the model implied by
// their preset name so model pickers and provider validation have an option.
func normalizeLegacyProviderModels(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if providerHasAnyModel(*p) {
			continue
		}
		if model := legacyOfficialProviderModel(p.Name); model != "" {
			p.Model = model
		}
	}
}

func legacyOfficialProviderModel(name string) string {
	switch strings.TrimSpace(name) {
	case "deepseek-flash":
		return "deepseek-v4-flash"
	case "deepseek-pro":
		return "deepseek-v4-pro"
	case "mimo-api", "mimo-pro":
		return "mimo-v2.5-pro"
	case "mimo-flash":
		return "mimo-v2.5"
	default:
		return ""
	}
}

func normalizeDesktopOfficialProviderAccess(c *Config) {
	if c == nil || len(c.Desktop.ProviderAccess) == 0 {
		return
	}
	seen := desktopProviderAccessMap(nil)
	next := make([]string, 0, len(c.Desktop.ProviderAccess))
	includeMimoFlash := false
	for _, name := range c.Desktop.ProviderAccess {
		if strings.TrimSpace(name) == "mimo-flash" {
			includeMimoFlash = true
		}
		name = canonicalDesktopOfficialProviderName(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		next = append(next, name)
	}
	c.Desktop.ProviderAccess = next
	if seen["deepseek"] {
		ensureDeepSeekOfficialProvider(c)
	}
	if seen["mimo-api"] {
		ensureMimoAPIProvider(c)
	}
	if seen["mimo-token-plan"] {
		ensureMimoTokenPlanProvider(c, includeMimoFlash)
	}
	retargetDesktopOfficialRefs(c, seen)
}

// NormalizeLegacyDesktopProviderAccess seeds the desktop provider-access list
// for configs written before Settings tracked explicit provider access. Callers
// should only use this when they know the TOML did not declare provider_access;
// an explicit empty list means the user removed all access entries.
func NormalizeLegacyDesktopProviderAccess(c *Config) {
	if c == nil || len(c.Desktop.ProviderAccess) > 0 {
		return
	}
	seen := desktopProviderAccessMap(nil)
	var access []string
	add := func(name string) {
		name = canonicalDesktopOfficialProviderName(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		access = append(access, name)
	}
	addRef := func(ref string) {
		if entry, ok := c.ResolveModel(ref); ok {
			if !entry.Configured() {
				return
			}
			add(entry.Name)
		}
	}
	addRef(c.DefaultModel)
	addRef(c.Agent.PlannerModel)
	addRef(c.Agent.SubagentModel)
	addRef(c.Agent.AutoPlanClassifier)
	for _, ref := range c.Agent.SubagentModels {
		addRef(ref)
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Configured() {
			add(p.Name)
		}
	}
	if len(access) == 0 {
		return
	}
	c.Desktop.ProviderAccess = access
	normalizeDesktopOfficialProviderAccess(c)
}

func canonicalDesktopOfficialProviderName(name string) string {
	switch strings.TrimSpace(name) {
	case "deepseek-flash", "deepseek-pro":
		return "deepseek"
	case "mimo", "xiaomi-mimo", "xiaomi_mimo":
		return "mimo-api"
	case "mimo-pro", "mimo-flash":
		return "mimo-token-plan"
	default:
		return strings.TrimSpace(name)
	}
}

// CanonicalDesktopOfficialProviderName returns the Settings Center provider ID
// for built-in official provider aliases.
func CanonicalDesktopOfficialProviderName(name string) string {
	return canonicalDesktopOfficialProviderName(name)
}

func desktopProviderAccessMap(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		name = canonicalDesktopOfficialProviderName(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func ensureDeepSeekOfficialProvider(c *Config) {
	if _, ok := c.Provider("deepseek"); ok {
		return
	}
	entry := ProviderEntry{
		Name:          "deepseek",
		Kind:          "openai",
		BaseURL:       "https://api.deepseek.com",
		Models:        []string{"deepseek-v4-flash", "deepseek-v4-pro"},
		Default:       "deepseek-v4-flash",
		APIKeyEnv:     "DEEPSEEK_API_KEY",
		BalanceURL:    "https://api.deepseek.com/user/balance",
		ContextWindow: 1_000_000,
	}
	if old, ok := c.Provider("deepseek-flash"); ok {
		entry = officialProviderFromLegacy(entry, old)
		entry.Models = mergeModelLists([]string{"deepseek-v4-flash", "deepseek-v4-pro"}, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, "deepseek-v4-flash")
	}
	c.Providers = append(c.Providers, entry)
}

func ensureMimoAPIProvider(c *Config) {
	if _, ok := c.Provider("mimo-api"); ok {
		return
	}
	c.Providers = append(c.Providers, ProviderEntry{
		Name:          "mimo-api",
		Kind:          "openai",
		BaseURL:       "https://api.xiaomimimo.com/v1",
		Models:        []string{"mimo-v2.5-pro"},
		Default:       "mimo-v2.5-pro",
		APIKeyEnv:     "MIMO_API_KEY",
		ContextWindow: 1_048_576,
		NoProxy:       true,
	})
}

func ensureMimoTokenPlanProvider(c *Config, includeMimoFlash bool) {
	if _, ok := c.Provider("mimo-token-plan"); ok {
		return
	}
	entry := ProviderEntry{
		Name:          "mimo-token-plan",
		Kind:          "openai",
		BaseURL:       "https://token-plan-cn.xiaomimimo.com/v1",
		Models:        []string{"mimo-v2.5-pro"},
		Default:       "mimo-v2.5-pro",
		APIKeyEnv:     "MIMO_API_KEY",
		ContextWindow: 1_048_576,
		NoProxy:       true,
	}
	if old, ok := c.Provider("mimo-pro"); ok {
		entry = officialProviderFromLegacy(entry, old)
		entry.Models = mergeModelLists([]string{"mimo-v2.5-pro"}, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, "mimo-v2.5-pro")
	}
	if old, ok := c.Provider("mimo-flash"); includeMimoFlash && ok {
		if !providerHasAnyModel(entry) {
			entry = officialProviderFromLegacy(entry, old)
		}
		entry.Models = mergeModelLists(entry.Models, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, entry.Default)
	}
	c.Providers = append(c.Providers, entry)
}

func officialProviderFromLegacy(entry ProviderEntry, old *ProviderEntry) ProviderEntry {
	entry.Kind = old.Kind
	entry.BaseURL = old.BaseURL
	entry.ModelsURL = old.ModelsURL
	entry.APIKeyEnv = old.APIKeyEnv
	entry.BalanceURL = old.BalanceURL
	entry.ContextWindow = old.ContextWindow
	entry.Price = old.Price
	entry.Thinking = old.Thinking
	entry.Effort = old.Effort
	entry.ReasoningProtocol = old.ReasoningProtocol
	entry.SupportedEfforts = append([]string(nil), old.SupportedEfforts...)
	entry.DefaultEffort = old.DefaultEffort
	entry.NoProxy = old.NoProxy
	return entry
}

func mergeModelLists(primary, extra []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(primary)+len(extra))
	for _, list := range [][]string{primary, extra} {
		for _, model := range list {
			model = strings.TrimSpace(model)
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			out = append(out, model)
		}
	}
	return out
}

func firstKnownModel(current string, models []string, fallback string) string {
	current = strings.TrimSpace(current)
	for _, model := range models {
		if model == current {
			return current
		}
	}
	for _, model := range models {
		if model == fallback {
			return fallback
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

func retargetDesktopOfficialRefs(c *Config, access map[string]bool) {
	c.DefaultModel = retargetDesktopOfficialRef(c.DefaultModel, access)
	c.Agent.PlannerModel = retargetDesktopOfficialRef(c.Agent.PlannerModel, access)
	c.Agent.SubagentModel = retargetDesktopOfficialRef(c.Agent.SubagentModel, access)
	c.Agent.AutoPlanClassifier = retargetDesktopOfficialRef(c.Agent.AutoPlanClassifier, access)
	for skill, ref := range c.Agent.SubagentModels {
		c.Agent.SubagentModels[skill] = retargetDesktopOfficialRef(ref, access)
	}
}

func retargetDesktopOfficialRef(ref string, access map[string]bool) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	provider, model, hasModel := strings.Cut(ref, "/")
	switch provider {
	case "deepseek-flash":
		if !access["deepseek"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "deepseek-v4-flash"
		}
		return "deepseek/" + model
	case "deepseek-pro":
		if !access["deepseek"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "deepseek-v4-pro"
		}
		return "deepseek/" + model
	case "mimo-pro":
		if !access["mimo-token-plan"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "mimo-v2.5-pro"
		}
		return "mimo-token-plan/" + model
	case "mimo", "xiaomi-mimo", "xiaomi_mimo":
		if !access["mimo-api"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "mimo-v2.5-pro"
		}
		return "mimo-api/" + model
	case "mimo-flash":
		if !access["mimo-token-plan"] {
			return ref
		}
		if !hasModel || strings.TrimSpace(model) == "" {
			model = "mimo-v2.5"
		}
		return "mimo-token-plan/" + model
	default:
		return ref
	}
}

func userConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "reasonix", "config.toml")
}

// UserConfigPath is the user-global config file (~/.config/reasonix/config.toml),
// or "" when the user config dir can't be resolved.
func UserConfigPath() string { return userConfigPath() }

// UserCredentialsPath is the reasonix-owned global secrets file, beside
// config.toml in the user config dir (e.g. ~/.config/reasonix/credentials). It
// holds KEY=value lines loaded into the environment by loadDotEnv. The setup
// wizard writes API keys here, deliberately NOT named .env: keys never land in a
// project's own .env (which can't be selectively gitignored), never get
// committed, and resolve from any working directory. "" when the user config dir
// can't be resolved.
func UserCredentialsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "reasonix", "credentials")
}

// ArchiveDir is where compacted conversation history is archived for
// traceability (one timestamped .jsonl per compaction). Empty if the user config
// directory cannot be resolved, in which case archiving is skipped.
func ArchiveDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "reasonix", "archive")
}

// SessionDir is where chat sessions are persisted (one .jsonl per session).
// Used by `reasonix chat --continue` / `--resume` to find the recent ones. Empty
// if the user config dir can't be resolved — sessions then aren't saved.
func SessionDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "reasonix", "sessions")
}

// ProjectSessionDir is the per-workspace session directory the desktop sidebar
// lists: <config root>/projects/<slug>/sessions. Empty when either the config
// root or workspaceRoot doesn't resolve.
func ProjectSessionDir(workspaceRoot string) string {
	base := MemoryUserDir()
	root := strings.TrimSpace(workspaceRoot)
	if base == "" || root == "" {
		return ""
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return filepath.Join(base, "projects", WorkspaceSlug(root), "sessions")
}

// WorkspaceSlug flattens an absolute workspace path into the directory name
// used under <config root>/projects.
func WorkspaceSlug(absPath string) string {
	return strings.NewReplacer(string(os.PathSeparator), "-", "/", "-", "\\", "-", ":", "-").Replace(absPath)
}

// CacheDir is the per-user cache root for derived/regenerable artefacts: MCP
// handshake snapshots, plugin startup-latency telemetry. Lives beside the
// existing dirs (UserConfigDir/reasonix/...) so the whole reasonix state tree
// shares one root the user can wipe in a single rm. Empty when the OS dir is
// unavailable — callers must tolerate that (caching is best-effort).
func CacheDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "reasonix", "cache")
}

// MemoryUserDir returns the reasonix user config root (…/reasonix), under which
// the user-global REASONIX.md and the per-project auto-memory store live. Empty
// when the user config dir can't be resolved, which disables user-scoped memory.
func MemoryUserDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "reasonix")
}

// ConventionDirs are the parent directories scanned for agent assets (skills,
// commands), in canonical-first order. .reasonix is ours; .agents / .agent /
// .claude let users drop in assets authored for other agent tools without moving
// files. Shared so skills (internal/skill) and commands (CommandDirs) discover
// the same set. Note: hooks are NOT scanned across these — a .claude/settings.json
// uses a different hook schema that can't be parsed as ours, so hooks stay in
// .reasonix/settings.json (see internal/hook).
var ConventionDirs = []string{".reasonix", ".agents", ".agent", ".claude"}

// conventionSubdirsAsc joins sub under each ConventionDir of base, in ascending
// priority (reverse of ConventionDirs) so the canonical .reasonix ends up the
// highest-priority entry — command.Load lets a later directory win on a clash.
func conventionSubdirsAsc(base, sub string) []string {
	out := make([]string, 0, len(ConventionDirs))
	for i := len(ConventionDirs) - 1; i >= 0; i-- {
		out = append(out, filepath.Join(base, ConventionDirs[i], sub))
	}
	return out
}

// CommandDirs returns the directories scanned for custom slash commands, lowest
// priority first, so a later (more specific) directory overrides an earlier one
// on a name clash. Order: home-dir convention dirs (~/.claude/commands … ~/.reasonix/commands),
// the legacy XDG user dir (~/.config/reasonix/commands), then the project's
// convention dirs (.claude/commands … .reasonix/commands). Scanning the .claude /
// .agents / .agent dirs lets commands authored for other agent tools (same .md +
// frontmatter format) work here unchanged.
func CommandDirs() []string {
	return CommandDirsForRoot(".")
}

// CommandDirsForRoot is like CommandDirs but resolves the project convention
// dirs under root instead of the current working directory. Global (home/XDG)
// dirs are unchanged — they are always user-scoped.
func CommandDirsForRoot(root string) []string {
	root = resolveRoot(root)
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, conventionSubdirsAsc(home, "commands")...)
	}
	if dir, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(dir, "reasonix", "commands")) // legacy XDG user dir
	}
	dirs = append(dirs, conventionSubdirsAsc(root, "commands")...)
	return dirs
}

// SourcePath returns the highest-priority config file that exists, or "" if none.
func SourcePath() string {
	return SourcePathForRoot(".")
}

// SourcePathForRoot returns the highest-priority config file that exists under
// root, or "" if none. Equivalent to SourcePath() when root is ".".
func SourcePathForRoot(root string) string {
	root = resolveRoot(root)
	projectTOML := "reasonix.toml"
	if root != "." {
		projectTOML = filepath.Join(root, "reasonix.toml")
	}
	if _, err := os.Stat(projectTOML); err == nil {
		return projectTOML
	}
	if uc := userConfigPath(); uc != "" {
		if _, err := os.Stat(uc); err == nil {
			return uc
		}
	}
	return ""
}

// WriteFile writes the configuration to path as annotated TOML.
func (c *Config) WriteFile(path string) error {
	return os.WriteFile(path, []byte(RenderTOMLForScope(c, renderScopeForPath(path))), 0o644)
}

// Provider returns the named provider entry.
func (c *Config) Provider(name string) (*ProviderEntry, bool) {
	for i := range c.Providers {
		if c.Providers[i].Name == name {
			return &c.Providers[i], true
		}
	}
	return nil, false
}

// ResolveModel resolves a model reference to a provider entry whose Model is the
// selected model string (a copy, so the config's lists stay intact). It accepts:
//   - "provider/model" — that exact model under that provider;
//   - a provider name   — the provider's default model;
//   - a bare model name — the (first) provider that lists it.
//
// The returned entry is ready to build a provider from (NewProvider reads .Model),
// so a single "vendor with many models" entry yields one instance per model
// without duplicating base_url/api_key_env. Single-`model` entries still resolve
// by provider name, keeping older configs working unchanged.
func (c *Config) ResolveModel(ref string) (*ProviderEntry, bool) {
	if ref == "" {
		return nil, false
	}
	if access := desktopProviderAccessMap(c.Desktop.ProviderAccess); len(access) > 0 {
		ref = retargetDesktopOfficialRef(ref, access)
	}
	// "provider/model"
	if prov, model, ok := strings.Cut(ref, "/"); ok {
		if e, found := c.Provider(prov); found && e.HasModel(model) {
			cp := *e
			cp.Model = model
			return &cp, true
		}
	}
	// a provider name → its default model
	if e, found := c.Provider(ref); found {
		cp := *e
		cp.Model = e.DefaultModel()
		return &cp, true
	}
	// a bare model name → the provider that lists it
	for i := range c.Providers {
		if c.Providers[i].HasModel(ref) {
			cp := c.Providers[i]
			cp.Model = ref
			return &cp, true
		}
	}
	return nil, false
}

// ResolveModelWithFallback resolves a model reference to the canonical
// "provider/model" form used by the desktop runtime. If ref is stale or empty,
// it tries the user's configured default_model before falling back to the first
// configured provider — so preference isn't overwritten by iteration order.
func (c *Config) ResolveModelWithFallback(ref string) (resolvedRef string, fallback bool, ok bool) {
	ref = strings.TrimSpace(ref)
	if ref != "" {
		if e, found := c.ResolveModel(ref); found {
			return e.Name + "/" + e.Model, false, true
		}
	}
	// Before falling back to the first configured provider (which may not be the
	// user's preferred choice), try the configured default_model.  Skip when ref
	// already WAS the DefaultModel (it already failed above, so retrying won't
	// help) or when the default provider has no API key configured.
	if ref != c.DefaultModel && c.DefaultModel != "" {
		if e, found := c.ResolveModel(c.DefaultModel); found && e.Configured() {
			return e.Name + "/" + e.Model, true, true
		}
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		// Skip providers with no models or no API key: falling back onto a keyless
		// provider just boots the tab onto something that fails on first use. Mirrors
		// the Configured() gate the provider-removal/selection paths already apply.
		if len(p.ModelList()) == 0 || !p.Configured() {
			continue
		}
		return p.Name + "/" + p.DefaultModel(), true, true
	}
	return "", false, false
}

// APIKey resolves the entry's API key from its api_key_env.
func (e *ProviderEntry) APIKey() string {
	if e.APIKeyEnv == "" {
		return ""
	}
	return os.Getenv(e.APIKeyEnv)
}

// Configured reports whether the provider's api_key_env is set — the same check
// Validate enforces, so pickers can filter on it.
func (e *ProviderEntry) Configured() bool {
	return e.APIKey() != ""
}

// ResolveSystemPrompt returns the system prompt, reading system_prompt_file if set.
func (c *Config) ResolveSystemPrompt() (string, error) {
	return c.ResolveSystemPromptForRoot(".")
}

// ResolveSystemPromptForRoot is like ResolveSystemPrompt but resolves a relative
// system_prompt_file against root. Desktop tabs pass their workspace root here so
// prompt files are project-scoped even when the process cwd is elsewhere.
func (c *Config) ResolveSystemPromptForRoot(root string) (string, error) {
	if c.Agent.SystemPromptFile != "" {
		path := c.Agent.SystemPromptFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(resolveRoot(root), path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("system_prompt_file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	if strings.TrimSpace(c.Agent.SystemPrompt) == "" {
		return DefaultSystemPrompt, nil
	}
	return c.Agent.SystemPrompt, nil
}

// Validate checks that the selected model's provider is usable.
func (c *Config) Validate(model string) error {
	e, ok := c.ResolveModel(model)
	if !ok {
		return fmt.Errorf("unknown model %q (configured: %s)", model, c.providerNames())
	}
	if e.Kind == "" {
		return fmt.Errorf("provider %q: kind is required", model)
	}
	if e.BaseURL == "" {
		return fmt.Errorf("provider %q: base_url is required", model)
	}
	if e.APIKey() == "" {
		return fmt.Errorf("provider %q: missing env %s", model, e.APIKeyEnv)
	}
	return nil
}

func (c *Config) providerNames() string {
	names := make([]string, len(c.Providers))
	for i, p := range c.Providers {
		names[i] = p.Name
	}
	return strings.Join(names, ", ")
}
