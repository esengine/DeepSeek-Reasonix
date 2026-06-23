// Package config loads Reasonix's runtime configuration from TOML. Resolution order:
// flag > project ./reasonix.toml > user config.toml (in the OS user-config dir) > built-in defaults.
// User-global runtime controls, such as agent step limits, are documented exceptions.
// Secrets come from the environment via api_key_env and are never stored in
// config files.
package config

import (
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"

	"reasonix/internal/fileutil"
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
	ConfigVersion    int                 `toml:"config_version"`
	DefaultModel     string              `toml:"default_model"`
	Language         string              `toml:"language"` // ui/model language tag (e.g. "zh"); empty = auto-detect from $LANG / $REASONIX_LANG
	CredentialsStore string              `toml:"credentials_store"`
	UI               UIConfig            `toml:"ui"`
	Desktop          DesktopConfig       `toml:"desktop"`
	Notifications    NotificationsConfig `toml:"notifications"`
	Agent            AgentConfig         `toml:"agent"`
	Providers        []ProviderEntry     `toml:"providers"`
	Tools            ToolsConfig         `toml:"tools"`
	Permissions      PermissionsConfig   `toml:"permissions"`
	Sandbox          SandboxConfig       `toml:"sandbox"`
	Network          NetworkConfig       `toml:"network"`
	Plugins          []PluginEntry       `toml:"plugins"`
	Skills           SkillsConfig        `toml:"skills"`
	Statusline       StatuslineConfig    `toml:"statusline"`
	LSP              LSPConfig           `toml:"lsp"`
	Bot              BotConfig           `toml:"bot"`

	providerSources          map[string]providerSourceScope
	shadowedProjectProviders []ProviderEntry
}

type providerSourceScope string

const (
	providerSourceUser    providerSourceScope = "user"
	providerSourceProject providerSourceScope = "project"
)

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
	Language       string   `toml:"language"`         // auto|en|zh; empty/auto = browser/OS auto-detect
	LayoutStyle    string   `toml:"layout_style"`     // classic|workbench|creation; desktop layout style
	Theme          string   `toml:"theme"`            // auto|dark|light; empty resolves to auto
	ThemeStyle     string   `toml:"theme_style"`      // graphite|aurora|slate|carbon|nocturne|amber and legacy aliases
	CloseBehavior  string   `toml:"close_behavior"`   // quit|background; desktop window close behavior
	DisplayMode    string   `toml:"display_mode"`     // standard|compact (legacy "minimal" maps to compact); transcript display mode
	StatusBarStyle string   `toml:"status_bar_style"` // icon|text; desktop status bar metric labels
	StatusBarItems []string `toml:"status_bar_items"` // ordered visible desktop status bar items
	CheckUpdates   *bool    `toml:"check_updates"`    // startup update checks; nil keeps the default enabled
	Telemetry      *bool    `toml:"telemetry"`        // anonymous launch ping (install id + version + OS); nil keeps the default enabled
	Metrics        *bool    `toml:"metrics"`          // aggregate desktop metrics (anonymous signal/bucket counts; no content); nil keeps the default enabled
	ProviderAccess []string `toml:"provider_access"`  // desktop-only list of provider entries shown in Settings > Model > Access
	ExpandThinking bool     `toml:"expand_thinking"`  // true = show reasoning text expanded by default; false = collapsed
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

func normalizeDesktopLayoutStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "classic":
		return "classic"
	case "workbench", "workspace":
		return "workbench"
	case "creation":
		return "creation"
	default:
		return "workbench"
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

// DesktopTheme normalizes desktop.theme. New desktop users default to the OS
// automatic graphite product look; an explicit auto/light/dark is preserved.
func (c *Config) DesktopTheme() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.Theme)) {
	case "auto":
		return "auto"
	case "light":
		return "light"
	case "dark":
		return "dark"
	default:
		return "auto"
	}
}

// DesktopThemeStyle normalizes desktop.theme_style. Empty means the frontend
// chooses the default style for the resolved desktop theme.
func (c *Config) DesktopThemeStyle() string {
	return normalizeThemeStyle(c.Desktop.ThemeStyle)
}

// DesktopLayoutStyle normalizes the desktop layout style. New installs default
// to workbench; explicit classic remains respected.
func (c *Config) DesktopLayoutStyle() string {
	if strings.EqualFold(strings.TrimSpace(c.Desktop.ThemeStyle), "workbench") && strings.TrimSpace(c.Desktop.LayoutStyle) == "" {
		return "workbench"
	}
	return normalizeDesktopLayoutStyle(c.Desktop.LayoutStyle)
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
// "standard" (flat rendering, no folding).
func (c *Config) DesktopDisplayMode() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.DisplayMode)) {
	case "standard":
		return "standard"
	case "compact", "minimal":
		return "compact"
	default:
		return "standard"
	}
}

// DesktopStatusBarStyle normalizes the desktop status bar metric label style.
// Default is "text"; explicit "icon" preserves the user's compact choice.
func (c *Config) DesktopStatusBarStyle() string {
	switch strings.ToLower(strings.TrimSpace(c.Desktop.StatusBarStyle)) {
	case "icon":
		return "icon"
	case "text":
		return "text"
	default:
		return "text"
	}
}

var defaultDesktopStatusBarItems = []string{
	"model",
	"workspace",
	"git_branch",
	"cache",
	"cache_avg",
	"session_tokens",
	"turn_tokens",
	"turn_cost",
	"session_turns",
	"context",
	"compact",
	"cost",
	"balance",
}

var knownDesktopStatusBarItems = desktopStatusBarItemSet(defaultDesktopStatusBarItems)

func desktopStatusBarItemSet(items []string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

// DefaultDesktopStatusBarItems returns the default ordered visible desktop
// status bar items.
func DefaultDesktopStatusBarItems() []string {
	return append([]string(nil), defaultDesktopStatusBarItems...)
}

// DesktopStatusBarItems normalizes the ordered visible desktop status bar items.
// An unset or empty list uses the default full set; explicit non-empty lists
// preserve user order and omit hidden items.
func (c *Config) DesktopStatusBarItems() []string {
	return normalizeDesktopStatusBarItems(c.Desktop.StatusBarItems)
}

func normalizeDesktopStatusBarItems(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, raw := range items {
		id := strings.TrimSpace(raw)
		if !knownDesktopStatusBarItems[id] || seen[id] {
			continue
		}
		out = append(out, id)
		seen[id] = true
	}
	if len(out) == 0 {
		return DefaultDesktopStatusBarItems()
	}
	return out
}

// DesktopCheckUpdates reports whether the desktop should check for updates on
// startup. Missing configs default to true so existing users keep update notices.
func (c *Config) DesktopCheckUpdates() bool {
	if c == nil || c.Desktop.CheckUpdates == nil {
		return true
	}
	return *c.Desktop.CheckUpdates
}

// ColdResumePruneEnabled reports whether stale tool results are elided when a
// session resumes past the provider cache window. Default true (cheaper cold
// restart); users keep full history by disabling it.
func (c *Config) ColdResumePruneEnabled() bool {
	if c == nil || c.Agent.ColdResumePrune == nil {
		return true
	}
	return *c.Agent.ColdResumePrune
}

// ReasoningLanguage normalizes agent.reasoning_language. Empty means auto:
// visible reasoning follows the conversation language already described by the
// stable LanguagePolicy. Legacy "default" is treated as auto.
func (c *Config) ReasoningLanguage() string {
	if c == nil {
		return "auto"
	}
	return NormalizeReasoningLanguage(c.Agent.ReasoningLanguage)
}

// NormalizeReasoningLanguage returns one of auto|zh|en.
func NormalizeReasoningLanguage(lang string) string {
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "", "auto", "follow", "conversation", "detect", "default", "model", "model-default", "model_default", "provider":
		return "auto"
	case "zh", "cn", "chinese", "中文":
		return "zh"
	case "en", "english":
		return "en"
	default:
		return "auto"
	}
}

// DesktopTelemetry reports whether the desktop sends the anonymous launch ping.
// It carries no conversation, key, or file data — see desktop/README.md.
func (c *Config) DesktopTelemetry() bool {
	if c == nil || c.Desktop.Telemetry == nil {
		return true
	}
	return *c.Desktop.Telemetry
}

// DesktopMetrics reports whether the desktop sends aggregate desktop metrics —
// anonymous (signal, bucket) counters, never content. Default on.
func (c *Config) DesktopMetrics() bool {
	if c == nil || c.Desktop.Metrics == nil {
		return true
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

// BotConfig 控制多渠道 IM bot 消息网关。
type BotConfig struct {
	Enabled          bool                  `toml:"enabled"`
	Model            string                `toml:"model"` // 用于 bot 的模型名，空则用 default_model
	ToolApprovalMode string                `toml:"tool_approval_mode"`
	MaxSteps         int                   `toml:"max_steps"`
	DebounceMs       int                   `toml:"debounce_ms"` // 消息合并窗口，毫秒
	Allowlist        BotAllowlist          `toml:"allowlist"`
	QQ               QQBotConfig           `toml:"qq"`
	Feishu           FeishuBotConfig       `toml:"feishu"`
	Weixin           WeixinBotConfig       `toml:"weixin"`
	Connections      []BotConnectionConfig `toml:"connections"`
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
	Sandbox      bool   `toml:"sandbox"`        // true 使用 QQ 沙箱 API / gateway
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
	ID               string                        `toml:"id"`
	Provider         string                        `toml:"provider"` // qq|feishu|weixin
	Domain           string                        `toml:"domain"`   // feishu|lark|weixin|qq
	Label            string                        `toml:"label"`
	Enabled          bool                          `toml:"enabled"`
	Status           string                        `toml:"status"` // disconnected|pending|connected|error
	Model            string                        `toml:"model"`
	ToolApprovalMode string                        `toml:"tool_approval_mode"`
	WorkspaceRoot    string                        `toml:"workspace_root"`
	Credential       BotConnectionCredential       `toml:"credential"`
	SessionMappings  []BotConnectionSessionMapping `toml:"session_mappings"`
	LastError        string                        `toml:"last_error"`
	CreatedAt        string                        `toml:"created_at"`
	UpdatedAt        string                        `toml:"updated_at"`
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
	SessionSource string `toml:"session_source"`
	ChatType      string `toml:"chat_type"`
	UserID        string `toml:"user_id"`
	ThreadID      string `toml:"thread_id"`
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
// (write_file / edit_file / multi_edit / move_file) may modify; empty means the
// current working directory, so writes stay inside the project by default.
// AllowWrite lists extra directories writers may also touch (e.g. a sibling repo
// or a temp dir). Both support ${VAR} / ${VAR:-default} expansion. Reads are
// unrestricted; confining `bash` is Phase 1 (OS-level sandbox).
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
// workspace root (defaulting to the current working directory when unset), plus
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
	// ReasoningLanguage controls the preferred language for visible reasoning
	// text. Empty/auto follows the conversation language. Applied as transient
	// turn context, not the stable prompt.
	ReasoningLanguage string `toml:"reasoning_language"`
	// AutoPlanClassifier optionally names a provider/model used to classify
	// borderline auto-plan decisions. Empty keeps the zero-cost heuristic path.
	AutoPlanClassifier string `toml:"auto_plan_classifier"`
	// Compaction window fractions: soft = notice only, compact = trigger, force = hard ceiling.
	SoftCompactRatio  float64 `toml:"soft_compact_ratio"`
	CompactRatio      float64 `toml:"compact_ratio"`
	CompactForceRatio float64 `toml:"compact_force_ratio"`
	// Keep controls which compactable messages stay verbatim beyond the current
	// user-fact/digest floor and recent tail. Empty uses the conservative default
	// of keeping error tool results.
	Keep       []string `toml:"keep"`
	RecentKeep int      `toml:"recent_keep"`
	// ColdResumePrune elides stale tool results when a session reopens past the
	// provider cache window. nil = default enabled.
	ColdResumePrune *bool `toml:"cold_resume_prune"`
	// PlanModeAllowedTools names tools that are exempt from the plan-mode read-only
	// gate. When a tool named here is called while in plan mode, it executes without
	// the "plan mode is read-only" block. Use sparingly — prefer the built-in safe
	// bash commands for read-only exploration.
	PlanModeAllowedTools []string `toml:"plan_mode_allowed_tools"`
}

// ProviderEntry declares a model provider instance. ContextWindow is the model's
// token budget; the harness compacts older history as a turn's prompt approaches
// it (see agent compaction). 0 disables compaction for the instance.
type ProviderEntry struct {
	Name           string   `toml:"name"`
	Kind           string   `toml:"kind"`
	BaseURL        string   `toml:"base_url"`
	Model          string   `toml:"model"`      // a single model (back-compat)
	Models         []string `toml:"models"`     // a vendor's model list (one base_url/key, many models)
	ModelsURL      string   `toml:"models_url"` // auto-fetch models from this URL on startup
	Default        string   `toml:"default"`    // default model when Models is set (else Models[0])
	APIKeyEnv      string   `toml:"api_key_env"`
	resolvedAPIKey string
	resolvedSource CredentialSource
	BalanceURL     string                       `toml:"balance_url"` // optional; a provider-specific wallet-balance endpoint (DeepSeek: https://api.deepseek.com/user/balance). Empty = no balance readout.
	ContextWindow  int                          `toml:"context_window"`
	Price          *provider.Pricing            `toml:"price"`  // legacy/provider-wide fallback
	Prices         map[string]*provider.Pricing `toml:"prices"` // optional per-model prices; keys are model ids
	// Thinking / Effort are provider-kind-specific knobs forwarded to the provider
	// via Config.Extra. The anthropic provider reads Thinking="adaptive" to enable
	// extended thinking and Effort ("low".."max") to tune depth. The
	// openai-compatible provider forwards Effort as reasoning_effort for
	// thinking-capable models; DeepSeek accepts high|max.
	// Empty = provider default.
	Thinking string `toml:"thinking"`
	Effort   string `toml:"effort"`
	// Vision marks the model as accepting image input. When set, images the user
	// attaches are embedded in the request (image_url for openai-kind, base64
	// blocks for anthropic). Off by default: text-only models 400 on image input,
	// and image tokens are heavy — gating keeps text-only flows cheap (the prompt
	// prefix is byte-identical with no image, so the cache is unaffected either way).
	Vision bool `toml:"vision"`
	// VisionModels narrows image input support to specific models in a multi-model
	// provider. This lets one provider expose both text-only and multimodal chat
	// models without enabling image payloads for every model.
	VisionModels []string `toml:"vision_models"`
	// VisionDetail sets the openai image_url detail hint (low|high); empty = auto
	// (the field is omitted). "low" caps an image to a fixed ~85 tokens for cheap
	// coarse reads; ignored by providers without the knob (e.g. anthropic).
	VisionDetail string `toml:"vision_detail"`
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

// PriceForModel returns the configured per-1M-token price for model. Per-model
// prices win; the legacy provider-wide price is a fallback for older configs.
func (e *ProviderEntry) PriceForModel(model string) *provider.Pricing {
	if e == nil {
		return nil
	}
	if e.Prices != nil {
		if p := e.Prices[strings.TrimSpace(model)]; p != nil {
			return clonePricing(p)
		}
	}
	return clonePricing(e.Price)
}

func (e *ProviderEntry) applyModelPrice() {
	if e == nil {
		return
	}
	e.Price = e.PriceForModel(e.Model)
}

func clonePricing(p *provider.Pricing) *provider.Pricing {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// ToolsConfig selects which built-in tools are enabled. Empty means all of them.
type ToolsConfig struct {
	Enabled            []string             `toml:"enabled"`
	BashTimeoutSeconds *int                 `toml:"bash_timeout_seconds"`
	BackgroundJobs     BackgroundJobsConfig `toml:"background_jobs"`
	Search             SearchConfig         `toml:"search"`
	Shell              ShellConfig          `toml:"shell"`
}

const (
	defaultBashTimeoutSeconds             = 120
	defaultBackgroundJobStalledWarningSec = 900
	maxBackgroundJobStalledWarningSec     = 86400
)

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

// BackgroundJobsConfig tunes parent-created background jobs.
type BackgroundJobsConfig struct {
	StalledWarningSeconds *int `toml:"stalled_warning_seconds"`
}

// BackgroundJobStalledWarningSeconds returns the stalled warning threshold in
// seconds. Omitted/negative values keep the default, explicit 0 disables the
// notice, and oversized values clamp to one day so a typo cannot become
// effectively invisible.
func (c *Config) BackgroundJobStalledWarningSeconds() int {
	if c.Tools.BackgroundJobs.StalledWarningSeconds == nil || *c.Tools.BackgroundJobs.StalledWarningSeconds < 0 {
		return defaultBackgroundJobStalledWarningSec
	}
	if *c.Tools.BackgroundJobs.StalledWarningSeconds > maxBackgroundJobStalledWarningSec {
		return maxBackgroundJobStalledWarningSec
	}
	return *c.Tools.BackgroundJobs.StalledWarningSeconds
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
	// Tier is a legacy compatibility field. New config rendering omits it; enabled
	// MCP servers connect automatically in the background unless auto_start=false.
	// Historical values are accepted for old files:
	//   "eager"      — blocks startup until the handshake completes; required for
	//                  servers whose tools the system prompt depends on.
	//   "lazy"       — legacy alias for background.
	//   "background" — placeholder + spawn fired at boot but not waited on;
	//                  swap happens once the spawn finishes.
	// Empty defaults to "background" so enabled MCPs connect automatically
	// without blocking chat. Unknown non-empty values fall back to "background".
	Tier string `toml:"tier"`
}

func (e PluginEntry) ShouldAutoStart() bool {
	return e.AutoStart == nil || *e.AutoStart
}

// ResolvedTier returns the normalized tier ("eager"|"background") with the
// project default applied. Legacy lazy and unknown values fall back to
// background so enabled MCPs are available without manual connection.
func (e PluginEntry) ResolvedTier() string {
	return resolvedMCPTier(e.Tier)
}

func resolvedMCPTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background", "lazy":
		return "background"
	case "":
		return "background"
	default:
		return "background"
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
For multi-step work, track progress with the todo_write tool: lay out the steps,
keep exactly one in_progress, and flip each to completed as you finish it — update
the list as you go, not just at the end.
In plan mode the harness blocks writer tools: do read-only research, then write a
concise plan as your reply and stop. The user is asked to approve before anything
is changed; once approved, work through the steps, updating the task list as you go.`

// UserDecisionPolicy is appended to every system prompt, including user-custom
// prompts, so custom personas cannot accidentally remove the `ask` UI contract.
const UserDecisionPolicy = `User-owned choices: when a real decision belongs to the user — scope, approach, library, risk, manual validation, or any ambiguous or consequential path — and there is no obvious safe default, call the ask tool with 2-4 concrete options so the UI shows a choice. Do not ask in prose, infer a choice from silence, or continue by choosing for the user; do not choose for the user. Tool-approval bypass modes do not answer ask questions or approve plans. If no interactive user is available, the ask tool returns a model-assumption fallback; state that assumption and choose the safest reversible path.`

// LanguagePolicy is the auto fallback appended to the system prompt when no
// concrete UI language is resolved. It is static English text, so it stays part
// of the cache-stable prefix and avoids per-turn language injection.
const LanguagePolicy = `Reply in the same language the user is using in their most recent message: ` +
	`if they write in Chinese answer in Chinese, in English answer in English, and switch ` +
	`whenever they switch. Let this also guide the language you think in. Always keep code, ` +
	`identifiers, file paths, shell commands, and technical terms in their original form — never translate them.`

// Default returns the built-in default configuration.
func Default() *Config {
	return &Config{
		ConfigVersion:    3,
		DefaultModel:     "deepseek-flash",
		CredentialsStore: CredentialsStoreAuto,
		UI:               UIConfig{Theme: "auto"},
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
			PlannerMaxSteps:   0,
			AutoPlan:          "off",
			SoftCompactRatio:  0.5,
			CompactRatio:      0.8,
			CompactForceRatio: 0.9,
		},
		// Mode "ask" with no rules keeps `reasonix run` autonomous (no TTY → ask
		// resolves to allow) while `reasonix` prompts before writers. Users add
		// deny/allow rules to harden or quiet specific tools.
		Permissions: PermissionsConfig{Mode: "ask"},
		// Sandbox on by default: bash is jailed (macOS), network allowed so
		// builds/downloads work. Set bash = "off" to disable. Network=true here
		// so an absent [sandbox] in a user's file keeps egress (zero value would
		// wrongly deny it).
		Sandbox: SandboxConfig{Bash: "enforce", Network: true},
		// LSP tools on by default, but dormant until a language server is on PATH;
		// a missing server yields an install hint rather than an error.
		LSP:     LSPConfig{Enabled: true},
		Network: NetworkConfig{ProxyMode: netclient.ModeAuto},
		Bot: BotConfig{
			ToolApprovalMode: "ask",
			MaxSteps:         25,
			DebounceMs:       1500,
			Allowlist:        BotAllowlist{Enabled: true},
			QQ:               QQBotConfig{AppSecretEnv: "QQ_BOT_APP_SECRET"},
			Feishu:           FeishuBotConfig{Domain: "feishu", AppSecretEnv: "FEISHU_BOT_APP_SECRET", Mode: "webhook", WebhookPort: 8080, RequireMention: true},
			Weixin:           WeixinBotConfig{AccountID: "default", TokenEnv: "WEIXIN_BOT_TOKEN", APIBase: "https://ilinkai.weixin.qq.com"},
		},
		Providers: []ProviderEntry{
			{Name: "deepseek-flash", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-flash", APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance", ContextWindow: 1_000_000, Price: deepSeekV4FlashPrice()},
			{Name: "deepseek-pro", Kind: "openai", BaseURL: "https://api.deepseek.com", Model: "deepseek-v4-pro", APIKeyEnv: "DEEPSEEK_API_KEY", BalanceURL: "https://api.deepseek.com/user/balance", ContextWindow: 1_000_000, Price: deepSeekV4ProPrice()},
		},
	}
}

func deepSeekV4FlashPrice() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"}
}

func deepSeekV4ProPrice() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"}
}

func deepSeekV4Prices() map[string]*provider.Pricing {
	return map[string]*provider.Pricing{
		"deepseek-v4-flash": deepSeekV4FlashPrice(),
		"deepseek-v4-pro":   deepSeekV4ProPrice(),
	}
}

func deepSeekV4FlashPriceUSD() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.0028, Input: 0.14, Output: 0.28, Currency: "$"}
}

func deepSeekV4ProPriceUSD() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.003625, Input: 0.435, Output: 0.87, Currency: "$"}
}

func deepSeekV4PricesUSD() map[string]*provider.Pricing {
	return map[string]*provider.Pricing{
		"deepseek-v4-flash": deepSeekV4FlashPriceUSD(),
		"deepseek-v4-pro":   deepSeekV4ProPriceUSD(),
	}
}

// DeepSeekV4PricesForLanguage keeps the settings/template call site stable while
// official DeepSeek defaults move to RMB. Persisted prices still win; this is
// only used for templates and missing-default backfills.
func DeepSeekV4PricesForLanguage(lang string) map[string]*provider.Pricing {
	_ = lang
	return deepSeekV4Prices()
}

func deepSeekV4PricesForConfig(c *Config) map[string]*provider.Pricing {
	_ = c
	return deepSeekV4Prices()
}

func deepSeekV4PriceForModel(lang, model string) *provider.Pricing {
	_ = lang
	return clonePricing(deepSeekV4Prices()[strings.TrimSpace(model)])
}

// DeepSeekOfficialPricingLanguage is retained for settings/template compatibility.
// Official DeepSeek providers now seed RMB prices by default; explicit user
// prices in config still override these defaults.
func (c *Config) DeepSeekOfficialPricingLanguage() string {
	_ = c
	return "zh"
}

// ApplyDeepSeekOfficialDefaultPricing refreshes built-in/official DeepSeek
// prices that still match known official defaults. Custom user prices are left
// untouched.
func (c *Config) ApplyDeepSeekOfficialDefaultPricing() {
	applyDeepSeekOfficialDefaultPricing(c)
}

func applyDeepSeekOfficialDefaultPricing(c *Config) {
	if c == nil {
		return
	}
	lang := c.DeepSeekOfficialPricingLanguage()
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderKind(p) != "deepseek" {
			continue
		}
		if isKnownDeepSeekOfficialPricing(p.Model, p.Price) {
			p.Price = deepSeekV4PriceForModel(lang, p.Model)
		}
		for model, price := range p.Prices {
			if isKnownDeepSeekOfficialPricing(model, price) {
				p.Prices[model] = deepSeekV4PriceForModel(lang, model)
			}
		}
	}
}

func mimoV25ProPrice() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"}
}

func mimoV25Price() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"}
}

func mimoV2FlashPrice() *provider.Pricing {
	return &provider.Pricing{CacheHit: 0.07, Input: 0.70, Output: 2.10, Currency: "¥"}
}

func mimoDomesticPrices(models []string) map[string]*provider.Pricing {
	prices := map[string]*provider.Pricing{}
	for _, model := range models {
		switch strings.TrimSpace(model) {
		case "mimo-v2.5-pro", "mimo-v2-pro":
			prices[model] = mimoV25ProPrice()
		case "mimo-v2.5", "mimo-v2-omni":
			prices[model] = mimoV25Price()
		case "mimo-v2-flash":
			prices[model] = mimoV2FlashPrice()
		}
	}
	return prices
}

// ResetOfficialProviderPricingOnUpgrade resets official DeepSeek prices to
// the current built-in RMB defaults once for desktop upgrades. It intentionally
// runs from the desktop app startup path, not every config Load(), so user edits
// made after the upgrade are preserved.
func ResetOfficialProviderPricingOnUpgrade(path string) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var header Config
	if _, err := toml.DecodeFile(path, &header); err != nil {
		return false, fmt.Errorf("config %s: %w", path, err)
	}
	if header.ConfigVersion >= Default().ConfigVersion {
		return false, nil
	}
	cfg := LoadForEdit(path)
	resetOfficialProviderPricingDefaults(cfg)
	cfg.ConfigVersion = Default().ConfigVersion
	if err := cfg.SaveTo(path); err != nil {
		return false, err
	}
	return true, nil
}

func resetOfficialProviderPricingDefaults(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		switch {
		case officialProviderKind(p) == "deepseek":
			resetDeepSeekOfficialPricing(p)
		}
	}
}

func resetDeepSeekOfficialPricing(p *ProviderEntry) {
	if p == nil {
		return
	}
	defaults := deepSeekV4Prices()
	p.Price = nil
	if strings.TrimSpace(p.Model) != "" && len(p.Models) == 0 {
		if price := defaults[strings.TrimSpace(p.Model)]; price != nil {
			p.Price = clonePricing(price)
			p.Prices = nil
			return
		}
	}
	if p.Prices == nil {
		p.Prices = map[string]*provider.Pricing{}
	}
	for model, price := range defaults {
		if p.HasModel(model) {
			p.Prices[model] = clonePricing(price)
		}
	}
}

func isKnownDeepSeekOfficialPricing(model string, price *provider.Pricing) bool {
	model = strings.TrimSpace(model)
	if model == "" || price == nil {
		return false
	}
	for _, prices := range []map[string]*provider.Pricing{deepSeekV4Prices(), deepSeekV4PricesUSD()} {
		if samePricing(price, prices[model]) {
			return true
		}
	}
	return false
}

func samePricing(a, b *provider.Pricing) bool {
	if a == nil || b == nil {
		return false
	}
	return a.CacheHit == b.CacheHit && a.Input == b.Input && a.Output == b.Output && a.Currency == b.Currency
}

// Load builds the configuration: defaults, then user config, then project
// config, then MCP servers from Claude Code's .mcp.json, then (lowest priority)
// the v0.x ~/.reasonix/config.json's mcpServers. Provider api_key_env values
// resolve from Reasonix's global .env, not from project .env files.
func Load() (*Config, error) {
	return LoadForRoot(".")
}

// LoadForRoot builds the configuration with project files resolved from root
// instead of the current working directory. When root is "" or ".", it behaves
// like Load(). This is the workspace-aware entry point: desktop tabs use it so
// each project's reasonix.toml + .mcp.json are resolved independently without
// changing the process cwd, while provider keys stay rooted in Reasonix home.
func LoadForRoot(root string) (*Config, error) {
	root = resolveRoot(root)
	loadDotEnvForRoot(root)
	cfg := Default()
	cfg.CredentialsStore = credentialsStoreMode()

	projectTOML := "reasonix.toml"
	if root != "." {
		projectTOML = filepath.Join(root, "reasonix.toml")
	}

	var tomlSources []string
	if uc := userConfigLoadPath(); uc != "" {
		tomlSources = append(tomlSources, uc)
		if err := mergeRuntimeTOMLFile(cfg, uc); err != nil {
			return nil, err
		}
	}
	globalMaxSteps := cfg.Agent.MaxSteps
	globalPlannerMaxSteps := cfg.Agent.PlannerMaxSteps

	tomlSources = append(tomlSources, projectTOML)
	if err := mergeRuntimeTOMLFile(cfg, projectTOML); err != nil {
		return nil, err
	}
	// Runtime step caps are user/global controls, not project policy. Keep the
	// project config's other fields, but do not let ./reasonix.toml override
	// the user's execution and planner round limits.
	cfg.Agent.MaxSteps = globalMaxSteps
	cfg.Agent.PlannerMaxSteps = globalPlannerMaxSteps
	// toml.DecodeFile replaces [[plugins]] wholesale, so cfg.Plugins now holds
	// only the last file's. Re-merge by name across all sources (later wins) so a
	// project reasonix.toml doesn't drop the global config's MCP servers.
	plugins, err := mergeTOMLPlugins(tomlSources)
	if err != nil {
		return nil, err
	}
	cfg.Plugins = plugins
	if providers, providerSources, shadowedProjectProviders, ok, err := mergeTOMLProviders(tomlSources); err != nil {
		return nil, err
	} else if ok {
		cfg.Providers = providers
		cfg.providerSources = providerSources
		cfg.shadowedProjectProviders = shadowedProjectProviders
	}
	if access, ok, err := mergeTOMLProviderAccess(tomlSources); err != nil {
		return nil, err
	} else if ok {
		cfg.Desktop.ProviderAccess = access
	}

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
	normalizeLegacyMimoCustomProviders(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	normalizeOfficialDeepSeekModels(cfg)
	applyDeepSeekOfficialDefaultPricing(cfg)
	backfillDeepSeekOfficialPrices(cfg)
	normalizeEffortConfig(cfg)
	backfillDeepSeekPro(cfg)
	cfg.Agent.AutoPlan = userAutoPlanMode()
	cfg.CredentialsStore = credentialsStoreMode()
	resolveProviderCredentialsForRoot(root, cfg)
	return cfg, nil
}

func userAutoPlanMode() string {
	cfg := Default()
	if uc := userConfigLoadPath(); uc != "" {
		_ = mergeFile(cfg, uc)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Agent.AutoPlan)) {
	case "on", "ask":
		return "on"
	default:
		return "off"
	}
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
	// If the user has explicitly curated a model list for the flash provider
	// (e.g. unchecked pro in Settings), respect that choice and do not backfill.
	if len(flash.Models) > 0 {
		return
	}
	for _, bp := range Default().Providers {
		if bp.Name == "deepseek-pro" {
			bp.APIKeyEnv = flash.APIKeyEnv
			bp.Price = deepSeekV4PriceForModel(c.DeepSeekOfficialPricingLanguage(), proModel)
			c.Providers = append(c.Providers, bp)
			return
		}
	}
}

func backfillDeepSeekOfficialPrices(c *Config) {
	if c == nil {
		return
	}
	defaults := deepSeekV4PricesForConfig(c)
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderKind(p) != "deepseek" {
			continue
		}
		if p.Price != nil {
			continue
		}
		if p.Prices == nil {
			p.Prices = map[string]*provider.Pricing{}
		}
		for model, price := range defaults {
			if p.HasModel(model) && p.Prices[model] == nil {
				p.Prices[model] = clonePricing(price)
			}
		}
	}
}

func officialProviderKind(p *ProviderEntry) string {
	if p == nil {
		return ""
	}
	u, err := url.Parse(strings.TrimSpace(p.BaseURL))
	if err != nil {
		return ""
	}
	if strings.EqualFold(u.Hostname(), "api.deepseek.com") {
		return "deepseek"
	}
	return ""
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

// mergeTOMLProviders merges [[providers]] across TOML sources by provider name.
// User-global providers win over same-named project providers; project providers
// only fill names the global config does not define. Keep official legacy aliases
// distinct here: they can carry different default models and effort capabilities,
// and the later desktop normalization layer handles canonical Settings access.
func mergeTOMLProviders(paths []string) ([]ProviderEntry, map[string]providerSourceScope, []ProviderEntry, bool, error) {
	var merged []ProviderEntry
	var shadowedProject []ProviderEntry
	index := map[string]int{}
	sources := map[string]providerSourceScope{}
	saw := false
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Config
		if _, err := toml.DecodeFile(path, &f); err != nil {
			return nil, nil, nil, false, fmt.Errorf("config %s: %w", path, err)
		}
		if len(f.Providers) == 0 {
			continue
		}
		saw = true
		source := providerSourceForPath(path)
		for _, p := range f.Providers {
			normalizeProviderEffortFields(&p)
			key := providerMergeKey(p)
			if i, ok := index[key]; ok {
				if sources[key] == providerSourceProject && source == providerSourceUser {
					shadowedProject = append(shadowedProject, merged[i])
					merged[i] = p
					sources[key] = source
				} else if sources[key] == providerSourceUser && source == providerSourceProject {
					shadowedProject = append(shadowedProject, p)
				}
				continue
			} else {
				index[key] = len(merged)
				merged = append(merged, p)
				sources[key] = source
			}
		}
	}
	return merged, sources, shadowedProject, saw, nil
}

func providerSourceForPath(path string) providerSourceScope {
	if isUserConfigPath(path) {
		return providerSourceUser
	}
	return providerSourceProject
}

func providerMergeKey(p ProviderEntry) string {
	return strings.TrimSpace(p.Name)
}

// mergeTOMLProviderAccess merges desktop.provider_access across TOML sources so
// project desktop settings do not hide account-level providers from the desktop
// model switcher.
func mergeTOMLProviderAccess(paths []string) ([]string, bool, error) {
	var merged []string
	seen := map[string]bool{}
	saw := false
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		var f Config
		meta, err := toml.DecodeFile(path, &f)
		if err != nil {
			return nil, false, fmt.Errorf("config %s: %w", path, err)
		}
		if !meta.IsDefined("desktop", "provider_access") {
			continue
		}
		saw = true
		for _, name := range f.Desktop.ProviderAccess {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			merged = append(merged, name)
		}
	}
	return merged, saw, nil
}

// LoadForEdit returns a config to seed the `reasonix setup` wizard when reconfiguring:
// the built-in defaults with the file at path (if present) decoded on top, so a
// reconfigure preserves the user's existing providers and agent settings instead
// of resetting to defaults. Reasonix's global .env is loaded so api_key_env
// resolution works while the wizard decides which keys are still missing.
func LoadForEdit(path string) *Config {
	cfg, err := loadForEditStrict(path, true)
	if err == nil {
		return cfg
	}
	slog.Warn("config: load for edit failed, using defaults", "path", path, "err", err)
	loadDotEnvForEditPath(path)
	cfg = Default()
	normalizeConfigForEdit(cfg)
	return cfg
}

func LoadForEditWithoutCredentials(path string) *Config {
	cfg, err := loadForEditStrict(path, false)
	if err == nil {
		return cfg
	}
	slog.Warn("config: load for edit failed, using defaults", "path", path, "err", err)
	cfg = Default()
	normalizeConfigForEdit(cfg)
	return cfg
}

func loadForEditStrict(path string, loadCredentials bool) (*Config, error) {
	if loadCredentials {
		loadDotEnvForEditPath(path)
	}
	cfg := Default()
	if _, err := os.Stat(path); err == nil {
		if err := migrateLegacyMCPTiersFile(path); err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
	}
	if err := mergeFile(cfg, path); err != nil {
		return nil, err
	}
	migratedMimo := normalizeConfigForEdit(cfg)
	if migratedMimo && strings.TrimSpace(path) != "" {
		if _, err := os.Stat(path); err == nil {
			if err := cfg.SaveTo(path); err != nil {
				return nil, err
			}
		}
	}
	return cfg, nil
}

func normalizeConfigForEdit(cfg *Config) bool {
	normalizePluginCommandLines(cfg)
	normalizeLegacyEffort(cfg)
	normalizeLegacyMCPTiers(cfg)
	migratedMimo := normalizeLegacyMimoCustomProviders(cfg)
	normalizeLegacyProviderModels(cfg)
	normalizeDesktopOfficialProviderAccess(cfg)
	applyDeepSeekOfficialDefaultPricing(cfg)
	backfillDeepSeekOfficialPrices(cfg)
	normalizeEffortConfig(cfg)
	return migratedMimo
}

func loadDotEnvForEditPath(path string) {
	path = strings.TrimSpace(path)
	if path == "" || isUserConfigPath(path) {
		loadDotEnv()
		return
	}
	loadDotEnvForRoot(filepath.Dir(path))
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

func mergeRuntimeTOMLFile(cfg *Config, path string) error {
	if _, err := os.Stat(path); err == nil {
		if err := migrateLegacyMCPTiersFile(path); err != nil {
			slog.Warn("config: legacy mcp tier migration failed", "path", path, "err", err)
		}
	}
	return mergeFile(cfg, path)
}

// normalizeLegacyMCPTiers keeps loaded legacy config files on the new product
// behavior: enabled MCP servers connect in the background by default, and the
// retired per-server startup tier is no longer a user-facing setting.
func normalizeLegacyMCPTiers(c *Config) {
	if c == nil {
		return
	}
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
		if section == "plugins" && isTOMLKeyAssignment(line, "tier") {
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

func normalizeLegacyMimoProviderCatalogs(c *Config) bool {
	if c == nil {
		return false
	}
	changed := false
	for i := range c.Providers {
		p := &c.Providers[i]
		if legacyMimoProviderName(p.Name) == "" || len(p.Models) > 0 {
			continue
		}
		switch officialProviderHost(p.BaseURL) {
		case "api.xiaomimimo.com":
			if applyLegacyMimoCatalog(p, legacyMimoAPIModels(), []string{"mimo-v2.5", "mimo-v2-omni"}, "mimo-v2.5-pro") {
				changed = true
			}
		case "token-plan-cn.xiaomimimo.com":
			if applyLegacyMimoCatalog(p, legacyMimoTokenPlanModels(), []string{"mimo-v2.5"}, "mimo-v2.5-pro") {
				changed = true
			}
		}
	}
	return changed
}

func applyLegacyMimoCatalog(p *ProviderEntry, models, visionModels []string, fallbackDefault string) bool {
	if p == nil || len(models) == 0 {
		return false
	}
	beforeModels := append([]string(nil), p.Models...)
	beforeVision := append([]string(nil), p.VisionModels...)
	beforeDefault := p.Default
	beforeModel := p.Model
	beforeWindow := p.ContextWindow
	beforeNoProxy := p.NoProxy
	beforePricesLen := len(p.Prices)

	currentDefault := strings.TrimSpace(p.Default)
	if currentDefault == "" {
		currentDefault = strings.TrimSpace(p.Model)
	}
	p.Models = mergeModelLists(models, p.ModelList())
	p.Model = p.Models[0]
	p.Default = firstKnownModel(currentDefault, p.Models, fallbackDefault)
	p.VisionModels = mergeModelLists(visionModels, p.VisionModels)
	backfillOfficialContextWindow(p, 1_048_576)
	p.NoProxy = true
	if p.Prices == nil {
		p.Prices = mimoDomesticPrices(models)
	} else {
		for model, price := range mimoDomesticPrices(models) {
			if p.Prices[model] == nil {
				p.Prices[model] = price
			}
		}
	}

	return !stringSlicesEqual(beforeModels, p.Models) ||
		!stringSlicesEqual(beforeVision, p.VisionModels) ||
		beforeDefault != p.Default ||
		beforeModel != p.Model ||
		beforeWindow != p.ContextWindow ||
		beforeNoProxy != p.NoProxy ||
		beforePricesLen != len(p.Prices)
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func normalizeOfficialDeepSeekModels(c *Config) {
	if c == nil {
		return
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if officialProviderHost(p.BaseURL) != "api.deepseek.com" {
			continue
		}
		switch strings.TrimSpace(p.Name) {
		case "deepseek":
			ensureProviderModels(p, []string{"deepseek-v4-flash", "deepseek-v4-pro"}, "deepseek-v4-flash")
		case "deepseek-flash":
			ensureProviderModels(p, []string{"deepseek-v4-flash"}, "deepseek-v4-flash")
		case "deepseek-pro":
			ensureProviderModels(p, []string{"deepseek-v4-pro"}, "deepseek-v4-pro")
		}
	}
}

func officialProviderHost(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func ensureProviderModels(p *ProviderEntry, required []string, fallbackDefault string) {
	if p == nil {
		return
	}
	// If the user has explicitly curated a model list (via Settings), respect
	// that choice and do not merge additional required models.
	if len(p.Models) > 0 {
		return
	}
	models := mergeModelLists(required, p.ModelList())
	if len(models) == 0 {
		return
	}
	p.Model = models[0]
	if len(models) > 1 {
		p.Models = models
		p.Default = firstKnownModel(p.Default, models, fallbackDefault)
		return
	}
	p.Models = nil
	p.Default = ""
}

func legacyOfficialProviderModel(name string) string {
	switch strings.TrimSpace(name) {
	case "deepseek-flash":
		return "deepseek-v4-flash"
	case "deepseek-pro":
		return "deepseek-v4-pro"
	case "mimo", "xiaomi-mimo", "xiaomi_mimo", "mimo-api", "mimo-token-plan", "mimo-pro":
		return "mimo-v2.5-pro"
	case "mimo-flash":
		return "mimo-v2.5"
	default:
		return ""
	}
}

func normalizeLegacyMimoCustomProviders(c *Config) bool {
	return normalizeLegacyMimoCustomProvidersForRefs(c, legacyMimoConfigRefs(c)...)
}

// NormalizeLegacyMimoCustomProvidersForRefs appends custom OpenAI-compatible
// MiMo providers needed by legacy refs that live outside reasonix.toml, such as
// restored desktop tab state.
func NormalizeLegacyMimoCustomProvidersForRefs(c *Config, refs ...string) bool {
	return normalizeLegacyMimoCustomProvidersForRefs(c, refs...)
}

func normalizeLegacyMimoCustomProvidersForRefs(c *Config, refs ...string) bool {
	if c == nil {
		return false
	}
	needed := map[string]bool{}
	addRef := func(ref string) {
		if name := legacyMimoProviderNameForRef(ref); name != "" {
			needed[name] = true
		}
	}
	for _, ref := range refs {
		addRef(ref)
	}
	changed := normalizeLegacyMimoProviderCatalogs(c)
	for name := range needed {
		if _, ok := c.Provider(name); ok {
			continue
		}
		c.Providers = append(c.Providers, legacyMimoCustomProvider(name))
		changed = true
	}
	if normalizeLegacyMimoProviderCatalogs(c) {
		changed = true
	}
	return changed
}

func legacyMimoConfigRefs(c *Config) []string {
	if c == nil {
		return nil
	}
	refs := []string{
		c.DefaultModel,
		c.Agent.PlannerModel,
		c.Agent.SubagentModel,
		c.Agent.AutoPlanClassifier,
		c.Bot.Model,
	}
	for _, ref := range c.Agent.SubagentModels {
		refs = append(refs, ref)
	}
	for _, conn := range c.Bot.Connections {
		refs = append(refs, conn.Model)
	}
	refs = append(refs, c.Desktop.ProviderAccess...)
	return refs
}

func legacyMimoProviderName(ref string) string {
	switch strings.TrimSpace(ref) {
	case "mimo", "xiaomi-mimo", "xiaomi_mimo", "mimo-api", "mimo-token-plan", "mimo-pro", "mimo-flash":
		return strings.TrimSpace(ref)
	default:
		return ""
	}
}

func legacyMimoProviderNameForRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	providerName, _, hasModel := strings.Cut(ref, "/")
	if name := legacyMimoProviderName(providerName); name != "" {
		return name
	}
	if hasModel {
		return ""
	}
	switch ref {
	case "mimo-v2.5-pro":
		return "mimo-pro"
	case "mimo-v2.5":
		return "mimo-flash"
	case "mimo-v2-omni":
		return "mimo-api"
	default:
		return ""
	}
}

func legacyMimoAPIModels() []string {
	return []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"}
}

func legacyMimoTokenPlanModels() []string {
	return []string{"mimo-v2.5-pro", "mimo-v2.5"}
}

func legacyMimoCustomProvider(name string) ProviderEntry {
	switch strings.TrimSpace(name) {
	case "mimo", "xiaomi-mimo", "xiaomi_mimo", "mimo-api":
		models := legacyMimoAPIModels()
		return ProviderEntry{
			Name:          strings.TrimSpace(name),
			Kind:          "openai",
			BaseURL:       "https://api.xiaomimimo.com/v1",
			Models:        models,
			VisionModels:  []string{"mimo-v2.5", "mimo-v2-omni"},
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_API_KEY",
			ContextWindow: 1_048_576,
			Prices:        mimoDomesticPrices(models),
			NoProxy:       true,
		}
	case "mimo-token-plan":
		models := legacyMimoTokenPlanModels()
		return ProviderEntry{
			Name:          "mimo-token-plan",
			Kind:          "openai",
			BaseURL:       "https://token-plan-cn.xiaomimimo.com/v1",
			Models:        models,
			VisionModels:  []string{"mimo-v2.5"},
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_API_KEY",
			ContextWindow: 1_048_576,
			Prices:        mimoDomesticPrices(models),
			NoProxy:       true,
		}
	case "mimo-flash":
		return ProviderEntry{Name: "mimo-flash", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5", APIKeyEnv: "MIMO_API_KEY", ContextWindow: 1_000_000, Price: mimoV25Price(), NoProxy: true}
	default:
		return ProviderEntry{Name: "mimo-pro", Kind: "openai", BaseURL: "https://token-plan-cn.xiaomimimo.com/v1", Model: "mimo-v2.5-pro", APIKeyEnv: "MIMO_API_KEY", ContextWindow: 1_000_000, Price: mimoV25ProPrice(), NoProxy: true}
	}
}

func normalizeDesktopOfficialProviderAccess(c *Config) {
	if c == nil || len(c.Desktop.ProviderAccess) == 0 {
		return
	}
	seen := desktopProviderAccessMap(nil)
	next := make([]string, 0, len(c.Desktop.ProviderAccess))
	for _, name := range c.Desktop.ProviderAccess {
		name = desktopProviderAccessNameForConfig(c, name)
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
	normalizeLegacyMimoProviderCatalogs(c)
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
		name = desktopProviderAccessNameForConfig(c, name)
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
	addRef(c.Bot.Model)
	for _, conn := range c.Bot.Connections {
		addRef(conn.Model)
	}
	for i := range c.Providers {
		p := &c.Providers[i]
		if legacyMimoProviderName(p.Name) != "" && len(p.ModelList()) > 0 {
			add(p.Name)
			continue
		}
		if p.Configured() && len(p.ModelList()) > 0 {
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
	default:
		return strings.TrimSpace(name)
	}
}

func desktopProviderAccessNameForConfig(c *Config, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	canonical := canonicalDesktopOfficialProviderName(name)
	if canonical == name {
		return name
	}
	if c == nil {
		return canonical
	}
	if p, ok := c.Provider(name); ok && !providerEntryMatchesCanonicalOfficialAccess(p, canonical) {
		return name
	}
	return canonical
}

func providerEntryMatchesCanonicalOfficialAccess(p *ProviderEntry, canonical string) bool {
	if p == nil {
		return false
	}
	switch canonical {
	case "deepseek":
		return officialProviderKind(p) == "deepseek"
	default:
		return false
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
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func ensureDeepSeekOfficialProvider(c *Config) {
	if p, ok := c.Provider("deepseek"); ok {
		if officialProviderKind(p) == "deepseek" {
			backfillOfficialContextWindow(p, 1_000_000)
		}
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
		Prices:        deepSeekV4PricesForConfig(c),
	}
	if old, ok := c.Provider("deepseek-flash"); ok {
		entry = officialProviderFromLegacy(entry, old)
		entry.Prices = deepSeekV4PricesForConfig(c)
		entry.Models = mergeModelLists([]string{"deepseek-v4-flash", "deepseek-v4-pro"}, old.ModelList())
		entry.Default = firstKnownModel(entry.Default, entry.Models, "deepseek-v4-flash")
	}
	backfillOfficialContextWindow(&entry, 1_000_000)
	c.Providers = append(c.Providers, entry)
}

func isOpenAIProviderKind(e *ProviderEntry) bool {
	return e != nil && strings.EqualFold(strings.TrimSpace(e.Kind), "openai")
}

func mergeCuratedModelsIntoProvider(e *ProviderEntry, models []string, fallback string) {
	// If the user has explicitly curated a model list (via Settings), respect
	// that choice and do not merge additional curated models.
	if len(e.Models) > 0 {
		return
	}
	currentDefault := e.Default
	if strings.TrimSpace(currentDefault) == "" {
		currentDefault = e.Model
	}
	e.Models = mergeModelLists(models, e.ModelList())
	e.Default = firstKnownModel(currentDefault, e.Models, fallback)
}

func backfillOfficialContextWindow(e *ProviderEntry, fallback int) {
	if e != nil && e.ContextWindow <= 0 {
		e.ContextWindow = fallback
	}
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
	default:
		return ref
	}
}

func userConfigPath() string {
	dir := userConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.toml")
}

func userConfigDir() string {
	return reasonixHomeDir()
}

func reasonixHomeDir() string {
	if dir := cleanEnvDir("REASONIX_HOME"); dir != "" {
		return dir
	}
	if runtime.GOOS != "windows" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, ".reasonix")
		}
		return ""
	}
	dir := osUserConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "reasonix")
}

func userConfigLoadPath() string {
	primary := userConfigPath()
	if primary == "" {
		return legacyUserConfigPath()
	}
	if _, err := os.Stat(primary); err == nil {
		return primary
	}
	if legacy := legacyUserConfigPath(); legacy != "" {
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	for _, legacy := range legacyXDGConfigPaths() {
		if legacy == "" || samePath(legacy, primary) {
			continue
		}
		if _, err := os.Stat(legacy); err == nil {
			return legacy
		}
	}
	return primary
}

func legacyUserConfigPath() string {
	dir := legacyOSSupportDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "config.toml")
	if primary := userConfigPath(); primary != "" && samePath(path, primary) {
		return ""
	}
	return path
}

func userConfigCandidatePaths() []string {
	var paths []string
	if p := userConfigPath(); p != "" {
		paths = append(paths, p)
	}
	if p := legacyUserConfigPath(); p != "" {
		paths = append(paths, p)
	}
	paths = append(paths, legacyXDGConfigPaths()...)
	return paths
}

func legacyXDGConfigPaths() []string {
	if runtime.GOOS == "windows" {
		return nil
	}
	seen := map[string]bool{}
	var paths []string
	add := func(path string) {
		if path == "" {
			return
		}
		path = filepath.Clean(path)
		if seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	if dir := cleanEnvDir("XDG_CONFIG_HOME"); dir != "" {
		add(filepath.Join(dir, "reasonix", "config.toml"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		add(filepath.Join(home, ".config", "reasonix", "config.toml"))
	}
	return paths
}

func userSupportDir() string {
	if dir := cleanEnvDir("REASONIX_STATE_HOME"); dir != "" {
		return dir
	}
	return reasonixHomeDir()
}

func legacyOSSupportDir() string {
	dir := osUserConfigDir()
	if dir == "" {
		return ""
	}
	path := filepath.Join(dir, "reasonix")
	if current := reasonixHomeDir(); current != "" && samePath(path, current) {
		return ""
	}
	return path
}

func userCacheDir() string {
	if dir := cleanEnvDir("REASONIX_CACHE_HOME"); dir != "" {
		return dir
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "reasonix")
}

func osUserConfigDir() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return dir
}

func cleanEnvDir(name string) string {
	dir := strings.TrimSpace(os.Getenv(name))
	if dir == "" {
		return ""
	}
	dir = ExpandVars(dir)
	if dir == "~" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			dir = home
		}
	} else if strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			dir = filepath.Join(home, dir[2:])
		}
	}
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return filepath.Clean(dir)
}

func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	aa, aerr := filepath.Abs(a)
	bb, berr := filepath.Abs(b)
	if aerr == nil {
		a = aa
	}
	if berr == nil {
		b = bb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// userConfigDisplayPath is userConfigPath collapsed to a ~-relative form for
// comments rendered into the user's own config.toml, so Windows users see the
// real location instead of a hardcoded ~/.reasonix path.
func userConfigDisplayPath() string {
	p := userConfigPath()
	if p == "" {
		return "<os-config-dir>/reasonix/config.toml"
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return p
}

// UserConfigPath is the user-global config.toml. It lives under Reasonix home:
// REASONIX_HOME/config.toml, then ~/.reasonix/config.toml on Unix-like systems,
// or %AppData%/reasonix/config.toml on Windows. "" when the user config dir
// can't be resolved.
func UserConfigPath() string { return userConfigPath() }

// LegacyUserConfigPath is the old OS app-support config.toml path when it
// differs from UserConfigPath. It is read as a compatibility fallback when the
// primary user config does not exist.
func LegacyUserConfigPath() string { return legacyUserConfigPath() }

// LegacyUserConfigPaths returns every known legacy user config path that differs
// from the current v1.8.1 Reasonix-home config path.
func LegacyUserConfigPaths() []string {
	primary := userConfigPath()
	var out []string
	add := func(path string) {
		if path == "" || samePath(path, primary) {
			return
		}
		for _, existing := range out {
			if samePath(existing, path) {
				return
			}
		}
		out = append(out, path)
	}
	add(legacyUserConfigPath())
	for _, path := range legacyXDGConfigPaths() {
		add(path)
	}
	return out
}

// ReasonixHomeDir is the current Reasonix home directory. It honors
// REASONIX_HOME, then uses ~/.reasonix on macOS/Linux or %APPDATA%/reasonix on
// Windows.
func ReasonixHomeDir() string { return reasonixHomeDir() }

// UserCredentialsPath is the reasonix-owned global .env file under Reasonix
// home. It is the single source for provider credentials saved by Reasonix, so
// stale shell, Windows, project, or home env vars cannot silently override keys
// the user saved through setup or settings. "" when Reasonix home can't be
// resolved.
func UserCredentialsPath() string {
	dir := userSupportDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, ".env")
}

// ArchiveDir is where compacted conversation history is archived for
// traceability (one timestamped .jsonl per compaction). Empty if the user state
// directory cannot be resolved, in which case archiving is skipped.
func ArchiveDir() string {
	dir := userSupportDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "archive")
}

// SessionDir is where chat sessions are persisted (one .jsonl per session).
// Used by `reasonix --continue` / `--resume` to find the recent ones. Empty
// if the user state dir can't be resolved — sessions then aren't saved.
func SessionDir() string {
	dir := userSupportDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "sessions")
}

// ProjectSessionDir is the per-workspace session directory the desktop sidebar
// lists: <state root>/projects/<slug>/sessions. Empty when either the state root
// or workspaceRoot doesn't resolve.
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
// handshake snapshots, plugin startup-latency telemetry. Empty when the OS dir is
// unavailable — callers must tolerate that (caching is best-effort).
func CacheDir() string {
	dir := userCacheDir()
	if dir == "" {
		return ""
	}
	return dir
}

// MemoryUserDir returns the reasonix user state root (…/reasonix), under which
// the user-global REASONIX.md and the per-project auto-memory store live. Empty
// when the user state dir can't be resolved, which disables user-scoped memory.
func MemoryUserDir() string {
	return userSupportDir()
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
// on a name clash. Order: home-dir convention dirs (~/.claude/commands …
// ~/.reasonix/commands), the Reasonix home commands dir, the legacy OS
// app-support dir if different, then the project's
// convention dirs (.claude/commands … .reasonix/commands). Scanning the .claude /
// .agents / .agent dirs lets commands authored for other agent tools (same .md +
// frontmatter format) work here unchanged.
func CommandDirs() []string {
	return CommandDirsForRoot(".")
}

// CommandDirsForRoot is like CommandDirs but resolves the project convention
// dirs under root instead of the current working directory. Global dirs are
// unchanged — they are always user-scoped.
func CommandDirsForRoot(root string) []string {
	root = resolveRoot(root)
	var dirs []string
	add := func(dir string) {
		if dir == "" {
			return
		}
		for _, existing := range dirs {
			if samePath(existing, dir) {
				return
			}
		}
		dirs = append(dirs, dir)
	}
	if dir := legacyOSSupportDir(); dir != "" {
		add(filepath.Join(dir, "commands"))
	}
	for _, legacy := range legacyXDGConfigPaths() {
		add(filepath.Join(filepath.Dir(legacy), "commands"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, dir := range conventionSubdirsAsc(home, "commands") {
			add(dir)
		}
	}
	if dir := userConfigDir(); dir != "" {
		add(filepath.Join(dir, "commands"))
	}
	if dir := userSupportDir(); dir != "" && !samePath(dir, userConfigDir()) {
		add(filepath.Join(dir, "commands"))
	}
	for _, dir := range conventionSubdirsAsc(root, "commands") {
		add(dir)
	}
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
	if uc := userConfigLoadPath(); uc != "" {
		if _, err := os.Stat(uc); err == nil {
			return uc
		}
	}
	return ""
}

// WriteFile writes the configuration to path as annotated TOML. The write is
// atomic + fsynced so an interrupted write or power loss can never truncate the
// main config into an unparseable state that leaves the app with no usable
// models (#4615, #4708).
func (c *Config) WriteFile(path string) error {
	return fileutil.AtomicWriteFile(path, []byte(RenderTOMLForScope(c, renderScopeForPath(path))), configFilePerm(path))
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
			cp.applyModelPrice()
			return &cp, true
		}
	}
	// a provider name → its default model
	if e, found := c.Provider(ref); found {
		cp := *e
		cp.Model = e.DefaultModel()
		cp.applyModelPrice()
		return &cp, true
	}
	// a bare model name → the provider that lists it
	for i := range c.Providers {
		if c.Providers[i].HasModel(ref) {
			cp := c.Providers[i]
			cp.Model = ref
			cp.applyModelPrice()
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
	if e == nil {
		return ""
	}
	if e.resolvedAPIKey != "" {
		return e.resolvedAPIKey
	}
	if e.APIKeyEnv == "" {
		return ""
	}
	value, _, ok := storedCredentialValue(e.APIKeyEnv)
	if !ok {
		return ""
	}
	return value
}

func (e *ProviderEntry) APIKeySourceLabel() string {
	if e == nil || strings.TrimSpace(e.APIKeyEnv) == "" {
		return ""
	}
	if e.resolvedAPIKey != "" {
		return credentialSourceLabel(e.resolvedSource)
	}
	return ResolveCredentialForRootGlobalFirst(".", e.APIKeyEnv).Source.Label
}

// RequiresAPIKey reports whether this provider should be hidden/validated when
// its configured api_key_env is empty. A blank api_key_env means the provider is
// intentionally no-auth. Local OpenAI-compatible gateways often keep a legacy
// api_key_env in config even though they accept unauthenticated requests, so
// loopback/private endpoints are also allowed to run without a resolved key.
func (e *ProviderEntry) RequiresAPIKey() bool {
	if e == nil {
		return false
	}
	if strings.TrimSpace(e.APIKeyEnv) == "" {
		return providerBaseURLRequiresAPIKey(e.BaseURL)
	}
	return !providerBaseURLAllowsMissingAPIKey(e.BaseURL)
}

func providerBaseURLRequiresAPIKey(raw string) bool {
	switch officialProviderHost(raw) {
	case "api.deepseek.com", "api.xiaomimimo.com", "token-plan-cn.xiaomimimo.com", "api.minimaxi.com", "api.openai.com":
		return true
	default:
		return false
	}
}

func providerBaseURLAllowsMissingAPIKey(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.Trim(strings.ToLower(u.Hostname()), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

// Configured reports whether the provider is selectable. Providers that do not
// require an API key are configured by definition; providers that name an env var
// require that variable to resolve unless their endpoint is local/private.
func (e *ProviderEntry) Configured() bool {
	return e != nil && (!e.RequiresAPIKey() || e.APIKey() != "")
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
	if e.RequiresAPIKey() && e.APIKey() == "" {
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
