package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/provider"
)

// settings_app.go is the desktop Settings panel's command surface: it reads the
// resolved config and applies edits through internal/config/edit.go (the
// purpose-built mutation API), then rebuilds the controller so the change takes
// effect live — the same snapshot→reload→resume pattern as SetModel. Secrets are
// the exception: they go to the global credential store (upsertDotEnv), since
// config stores only the env-var name, not the key.

// --- read ---

type ProviderView struct {
	Name              string   `json:"name"`
	BuiltIn           bool     `json:"builtIn"`
	Added             bool     `json:"added"`
	Kind              string   `json:"kind"`
	BaseURL           string   `json:"baseUrl"`
	Models            []string `json:"models"`
	VisionModels      []string `json:"visionModels"`
	VisionModelsSet   bool     `json:"visionModelsConfigured"`
	ModelsURL         string   `json:"modelsUrl"`
	Default           string   `json:"default"`
	APIKeyEnv         string   `json:"apiKeyEnv"`
	KeySet            bool     `json:"keySet"` // the env var currently resolves to a non-empty value
	RequiresKey       bool     `json:"requiresKey"`
	Configured        bool     `json:"configured"` // selectable: either key is present or no key is required
	KeySource         string   `json:"keySource,omitempty"`
	KeySourcePath     string   `json:"keySourcePath,omitempty"`
	BalanceURL        string   `json:"balanceUrl"`
	ContextWindow     int      `json:"contextWindow"`
	ReasoningProtocol string   `json:"reasoningProtocol"`
	SupportedEfforts  []string `json:"supportedEfforts"`
	DefaultEffort     string   `json:"defaultEffort"`
}

type PermissionsView struct {
	Mode  string   `json:"mode"`
	Allow []string `json:"allow"`
	Ask   []string `json:"ask"`
	Deny  []string `json:"deny"`
}

type SandboxView struct {
	Bash          string   `json:"bash"`
	Network       bool     `json:"network"`
	WorkspaceRoot string   `json:"workspaceRoot"`
	AllowWrite    []string `json:"allowWrite"`
	Shell         string   `json:"shell"` // [tools.shell] prefer: auto|bash|powershell|pwsh
}

type NetworkProxyView struct {
	Type     string `json:"type"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type NetworkView struct {
	ProxyMode string           `json:"proxyMode"`
	ProxyURL  string           `json:"proxyUrl"`
	NoProxy   string           `json:"noProxy"`
	Proxy     NetworkProxyView `json:"proxy"`
}

type AgentView struct {
	Temperature       float64 `json:"temperature"`
	MaxSteps          int     `json:"maxSteps"`
	PlannerMaxSteps   int     `json:"plannerMaxSteps"`
	SystemPrompt      string  `json:"systemPrompt"`
	ColdResumePrune   bool    `json:"coldResumePrune"`
	ReasoningLanguage string  `json:"reasoningLanguage"`
}

type BotAllowlistView struct {
	Enabled      bool     `json:"enabled"`
	AllowAll     bool     `json:"allowAll"`
	QQUsers      []string `json:"qqUsers"`
	FeishuUsers  []string `json:"feishuUsers"`
	WeixinUsers  []string `json:"weixinUsers"`
	QQGroups     []string `json:"qqGroups"`
	FeishuGroups []string `json:"feishuGroups"`
	WeixinGroups []string `json:"weixinGroups"`
}

type QQBotView struct {
	Enabled      bool   `json:"enabled"`
	AppID        string `json:"appId"`
	AppSecretEnv string `json:"appSecretEnv"`
	SecretSet    bool   `json:"secretSet"`
	Sandbox      bool   `json:"sandbox"`
}

type FeishuBotView struct {
	Enabled           bool   `json:"enabled"`
	Domain            string `json:"domain"`
	AppID             string `json:"appId"`
	AppSecretEnv      string `json:"appSecretEnv"`
	SecretSet         bool   `json:"secretSet"`
	VerificationToken string `json:"verificationToken"`
	Mode              string `json:"mode"`
	WebhookPort       int    `json:"webhookPort"`
	RequireMention    bool   `json:"requireMention"`
}

type WeixinBotView struct {
	Enabled   bool   `json:"enabled"`
	AccountID string `json:"accountId"`
	TokenEnv  string `json:"tokenEnv"`
	TokenSet  bool   `json:"tokenSet"`
	APIBase   string `json:"apiBase"`
}

type BotSettingsView struct {
	Enabled          bool                `json:"enabled"`
	Model            string              `json:"model"`
	ToolApprovalMode string              `json:"toolApprovalMode"`
	MaxSteps         int                 `json:"maxSteps"`
	DebounceMs       int                 `json:"debounceMs"`
	Allowlist        BotAllowlistView    `json:"allowlist"`
	QQ               QQBotView           `json:"qq"`
	Feishu           FeishuBotView       `json:"feishu"`
	Weixin           WeixinBotView       `json:"weixin"`
	Connections      []BotConnectionView `json:"connections"`
}

// SettingsView is the whole Settings panel payload.
type SettingsView struct {
	DefaultModel       string          `json:"defaultModel"`
	PlannerModel       string          `json:"plannerModel"`
	SubagentModel      string          `json:"subagentModel"`
	SubagentEffort     string          `json:"subagentEffort"`
	AutoPlan           string          `json:"autoPlan"`
	Providers          []ProviderView  `json:"providers"`
	OfficialProviders  []ProviderView  `json:"officialProviders"`
	Permissions        PermissionsView `json:"permissions"`
	Sandbox            SandboxView     `json:"sandbox"`
	Network            NetworkView     `json:"network"`
	Agent              AgentView       `json:"agent"`
	Bot                BotSettingsView `json:"bot"`
	DesktopLanguage    string          `json:"desktopLanguage"`
	DesktopLayoutStyle string          `json:"desktopLayoutStyle"`
	DesktopTheme       string          `json:"desktopTheme"`
	DesktopThemeStyle  string          `json:"desktopThemeStyle"`
	CloseBehavior      string          `json:"closeBehavior"`
	DisplayMode        string          `json:"displayMode"`
	StatusBarStyle     string          `json:"statusBarStyle"`
	StatusBarItems     []string        `json:"statusBarItems"`
	CheckUpdates       bool            `json:"checkUpdates"`
	Telemetry          bool            `json:"telemetry"`
	Metrics            bool            `json:"metrics"`
	ExpandThinking     bool            `json:"expandThinking"`
	ConfigPath         string          `json:"configPath"`
	// ProviderKinds lists the provider implementations the kernel actually
	// registered (provider.Kinds()), so the editor's "kind" picker offers only
	// kinds that resolve — selecting an unregistered one would fail the rebuild.
	ProviderKinds []string `json:"providerKinds"`
	// AutoApproveTools is the live YOLO/full-access state (runtime-only, not from
	// config), so the panel's toggle reflects whether tool approvals are currently
	// being skipped this session.
	AutoApproveTools bool `json:"autoApproveTools"`
	// Bypass is the legacy JSON key for the same live state.
	Bypass bool `json:"bypass"`
}

// DesktopStartupSettingsView is the lightweight Settings subset needed during
// frontend startup. It deliberately excludes providers and credential state so
// slow keychain/env resolution stays off the first-render path.
type DesktopStartupSettingsView struct {
	Bot                BotSettingsView `json:"bot"`
	DesktopLanguage    string          `json:"desktopLanguage"`
	DesktopLayoutStyle string          `json:"desktopLayoutStyle"`
	DesktopTheme       string          `json:"desktopTheme"`
	DesktopThemeStyle  string          `json:"desktopThemeStyle"`
	DisplayMode        string          `json:"displayMode"`
	StatusBarStyle     string          `json:"statusBarStyle"`
	StatusBarItems     []string        `json:"statusBarItems"`
	CheckUpdates       bool            `json:"checkUpdates"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func providerRemovalFallbackRef(c *config.Config, name string) string {
	for i := range c.Providers {
		p := &c.Providers[i]
		if p.Name == name || !p.Configured() || len(p.ModelList()) == 0 {
			continue
		}
		return p.Name + "/" + p.DefaultModel()
	}
	return ""
}

func desktopModelRefsProvider(c *config.Config, ref, name string) bool {
	if config.ModelRefsProvider(ref, name) {
		return true
	}
	if e, ok := c.ResolveModel(ref); ok {
		return e.Name == name
	}
	return false
}

func officialProviderHost(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func officialProviderKindFromEntry(p config.ProviderEntry) string {
	host := officialProviderHost(p.BaseURL)
	switch config.CanonicalDesktopOfficialProviderName(p.Name) {
	case "deepseek":
		if host == "api.deepseek.com" {
			return "deepseek"
		}
	case "mimo-api":
		if host == "api.xiaomimimo.com" {
			return "mimo-api"
		}
	case "mimo-token-plan":
		if host == "token-plan-cn.xiaomimimo.com" {
			return "mimo-token-plan"
		}
	}
	return ""
}

func isOfficialBuiltInProvider(p config.ProviderEntry) bool {
	return officialProviderKindFromEntry(p) != ""
}

func providerAccessSet(names []string) map[string]bool {
	out := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func addProviderAccess(c *config.Config, names ...string) {
	seen := providerAccessSet(c.Desktop.ProviderAccess)
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		c.Desktop.ProviderAccess = append(c.Desktop.ProviderAccess, name)
		seen[name] = true
	}
}

func removeProviderAccess(c *config.Config, names ...string) {
	remove := providerAccessSet(names)
	if len(remove) == 0 {
		return
	}
	out := c.Desktop.ProviderAccess[:0]
	for _, name := range c.Desktop.ProviderAccess {
		if !remove[name] {
			out = append(out, name)
		}
	}
	c.Desktop.ProviderAccess = out
}

func providerViewFromEntry(p config.ProviderEntry, builtIn, added bool) ProviderView {
	return providerViewFromEntryForRoot(p, builtIn, added, ".")
}

func providerViewFromEntryForRoot(p config.ProviderEntry, builtIn, added bool, root string) ProviderView {
	return providerViewFromEntryForRootWithResolver(p, builtIn, added, root, nil)
}

func providerViewFromEntryForRootWithResolver(p config.ProviderEntry, builtIn, added bool, root string, resolver *config.CredentialResolver) ProviderView {
	models := p.ChatModelList()
	visionModels := p.VisionModels
	visionModelsSet := p.Vision || p.VisionModels != nil
	if p.Vision {
		visionModels = models
	}
	if resolver == nil {
		resolver = config.NewCredentialResolverForRoot(root)
	}
	key := resolver.ResolveGlobalFirst(p.APIKeyEnv)
	requiresKey := p.RequiresAPIKey()
	return ProviderView{
		Name: p.Name, BuiltIn: builtIn, Added: added, Kind: p.Kind, BaseURL: p.BaseURL,
		Models: nonNil(models), VisionModels: nonNil(providerVisionModels(models, visionModels)), VisionModelsSet: visionModelsSet, ModelsURL: p.ModelsURL, Default: p.DefaultModel(),
		APIKeyEnv:         p.APIKeyEnv,
		KeySet:            key.Set,
		RequiresKey:       requiresKey,
		Configured:        !requiresKey || key.Set,
		KeySource:         key.Source.Label,
		KeySourcePath:     key.Source.Path,
		BalanceURL:        p.BalanceURL,
		ContextWindow:     p.ContextWindow,
		ReasoningProtocol: p.ReasoningProtocol,
		SupportedEfforts:  nonNil(p.SupportedEfforts),
		DefaultEffort:     p.DefaultEffort,
	}
}

func officialProviderViews(added map[string]bool, pricingLanguage string) []ProviderView {
	return officialProviderViewsForRoot(added, pricingLanguage, ".")
}

func officialProviderViewsForRoot(added map[string]bool, pricingLanguage, root string) []ProviderView {
	return officialProviderViewsForRootWithResolver(added, pricingLanguage, root, nil)
}

func officialProviderViewsForRootWithResolver(added map[string]bool, pricingLanguage, root string, resolver *config.CredentialResolver) []ProviderView {
	var out []ProviderView
	if resolver == nil {
		resolver = config.NewCredentialResolverForRoot(root)
	}
	for _, kind := range []string{"deepseek", "mimo-api", "mimo-token-plan"} {
		entries, _, err := officialProviderTemplate(kind, pricingLanguage)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			out = append(out, providerViewFromEntryForRootWithResolver(entry, true, added[entry.Name], root, resolver))
		}
	}
	return out
}

func officialProviderAddedSet(cfg *config.Config) map[string]bool {
	out := map[string]bool{}
	if cfg == nil {
		return out
	}
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	for i := range cfg.Providers {
		p := cfg.Providers[i]
		if !access[p.Name] {
			continue
		}
		if kind := officialProviderKindFromEntry(p); kind != "" {
			out[kind] = true
		}
	}
	return out
}

func desktopStartupSettingsFromConfig(cfg *config.Config) DesktopStartupSettingsView {
	if cfg == nil {
		return DesktopStartupSettingsView{
			Bot:                botSettingsView(config.BotConfig{}),
			DesktopLayoutStyle: "workbench",
			DesktopTheme:       "auto",
			DesktopThemeStyle:  "graphite",
			DisplayMode:        "standard",
			StatusBarStyle:     "text",
			StatusBarItems:     config.DefaultDesktopStatusBarItems(),
			CheckUpdates:       true,
		}
	}
	return DesktopStartupSettingsView{
		Bot:                botSettingsView(cfg.Bot),
		DesktopLanguage:    cfg.DesktopLanguage(),
		DesktopLayoutStyle: cfg.DesktopLayoutStyle(),
		DesktopTheme:       cfg.DesktopTheme(),
		DesktopThemeStyle:  cfg.DesktopThemeStyle(),
		DisplayMode:        cfg.DesktopDisplayMode(),
		StatusBarStyle:     cfg.DesktopStatusBarStyle(),
		StatusBarItems:     cfg.DesktopStatusBarItems(),
		CheckUpdates:       cfg.DesktopCheckUpdates(),
	}
}

// DesktopStartupSettings returns only the desktop chrome preferences needed at
// app startup. Keep provider/key status in Settings(), where the Settings panel
// actually needs it.
func (a *App) DesktopStartupSettings() DesktopStartupSettingsView {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return desktopStartupSettingsFromConfig(nil)
	}
	return desktopStartupSettingsFromConfig(cfg)
}

// Settings returns the current configuration for the Settings panel.
func (a *App) Settings() SettingsView {
	cfg, cfgPath, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return SettingsView{
			Providers:         []ProviderView{},
			OfficialProviders: officialProviderViews(map[string]bool{}, ""),
			ProviderKinds:     nonNil(provider.Kinds()),
			Permissions: PermissionsView{
				Mode:  "ask",
				Allow: []string{},
				Ask:   []string{},
				Deny:  []string{},
			},
			Sandbox:            SandboxView{Bash: "enforce", AllowWrite: []string{}, Shell: "auto"},
			Agent:              AgentView{PlannerMaxSteps: 12, ColdResumePrune: true, ReasoningLanguage: "auto"},
			Bot:                botSettingsView(config.BotConfig{}),
			AutoPlan:           "off",
			DesktopLayoutStyle: "workbench",
			DesktopTheme:       "auto",
			DesktopThemeStyle:  "graphite",
			CloseBehavior:      "background",
			DisplayMode:        "standard",
			StatusBarStyle:     "text",
			StatusBarItems:     config.DefaultDesktopStatusBarItems(),
			CheckUpdates:       true,
			Telemetry:          true,
			Metrics:            true,
			ExpandThinking:     false,
		}
	}
	ctrl := a.activeCtrl()
	bash := cfg.Sandbox.Bash
	if bash == "" {
		bash = "enforce"
	}
	shell := cfg.Tools.Shell.Prefer
	if shell == "" {
		shell = "auto"
	}
	v := SettingsView{
		DefaultModel:      cfg.DefaultModel,
		PlannerModel:      cfg.Agent.PlannerModel,
		SubagentModel:     cfg.Agent.SubagentModel,
		SubagentEffort:    cfg.Agent.SubagentEffort,
		AutoPlan:          desktopAutoPlanMode(cfg.Agent.AutoPlan),
		Providers:         []ProviderView{},
		OfficialProviders: []ProviderView{},
		Permissions: PermissionsView{
			Mode:  orDefault(cfg.Permissions.Mode, "ask"),
			Allow: nonNil(cfg.Permissions.Allow),
			Ask:   nonNil(cfg.Permissions.Ask),
			Deny:  nonNil(cfg.Permissions.Deny),
		},
		Sandbox: SandboxView{
			Bash: bash, Network: cfg.Sandbox.Network,
			WorkspaceRoot: cfg.Sandbox.WorkspaceRoot, AllowWrite: nonNil(cfg.Sandbox.AllowWrite),
			Shell: shell,
		},
		Network: NetworkView{
			ProxyMode: cfg.NetworkProxyMode(),
			ProxyURL:  cfg.Network.ProxyURL,
			NoProxy:   cfg.Network.NoProxy,
			Proxy: NetworkProxyView{
				Type:     orDefault(cfg.Network.Proxy.Type, "socks5"),
				Server:   cfg.Network.Proxy.Server,
				Port:     cfg.Network.Proxy.Port,
				Username: cfg.Network.Proxy.Username,
				Password: cfg.Network.Proxy.Password,
			},
		},
		Agent:              AgentView{Temperature: cfg.Agent.Temperature, MaxSteps: cfg.Agent.MaxSteps, PlannerMaxSteps: cfg.Agent.PlannerMaxSteps, SystemPrompt: cfg.Agent.SystemPrompt, ColdResumePrune: cfg.ColdResumePruneEnabled(), ReasoningLanguage: cfg.ReasoningLanguage()},
		Bot:                botSettingsView(cfg.Bot),
		DesktopLanguage:    cfg.DesktopLanguage(),
		DesktopLayoutStyle: cfg.DesktopLayoutStyle(),
		DesktopTheme:       cfg.DesktopTheme(),
		DesktopThemeStyle:  cfg.DesktopThemeStyle(),
		CloseBehavior:      cfg.DesktopCloseBehavior(),
		DisplayMode:        cfg.DesktopDisplayMode(),
		StatusBarStyle:     cfg.DesktopStatusBarStyle(),
		StatusBarItems:     cfg.DesktopStatusBarItems(),
		CheckUpdates:       cfg.DesktopCheckUpdates(),
		Telemetry:          cfg.DesktopTelemetry(),
		Metrics:            cfg.DesktopMetrics(),
		ExpandThinking:     cfg.Desktop.ExpandThinking,
		ConfigPath:         cfgPath,
		ProviderKinds:      nonNil(provider.Kinds()),
		AutoApproveTools:   ctrl != nil && ctrl.AutoApproveTools(),
		Bypass:             ctrl != nil && ctrl.AutoApproveTools(),
	}
	added := providerAccessSet(cfg.Desktop.ProviderAccess)
	root := a.activeWorkspaceRoot()
	resolver := config.NewCredentialResolverForRoot(root)
	v.OfficialProviders = officialProviderViewsForRootWithResolver(officialProviderAddedSet(cfg), cfg.DeepSeekOfficialPricingLanguage(), root, resolver)
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		v.Providers = append(v.Providers, providerViewFromEntryForRootWithResolver(*p, isOfficialBuiltInProvider(*p), added[p.Name], root, resolver))
	}
	return v
}

func botSettingsView(b config.BotConfig) BotSettingsView {
	mode := strings.TrimSpace(b.Feishu.Mode)
	if mode == "" {
		mode = "webhook"
	}
	return BotSettingsView{
		Enabled:          b.Enabled,
		Model:            b.Model,
		ToolApprovalMode: normalizeBotConnectionToolApprovalMode(b.ToolApprovalMode),
		MaxSteps:         b.MaxSteps,
		DebounceMs:       b.DebounceMs,
		Allowlist: BotAllowlistView{
			Enabled:      b.Allowlist.Enabled,
			AllowAll:     b.Allowlist.AllowAll,
			QQUsers:      nonNil(b.Allowlist.QQUsers),
			FeishuUsers:  nonNil(b.Allowlist.FeishuUsers),
			WeixinUsers:  nonNil(b.Allowlist.WeixinUsers),
			QQGroups:     nonNil(b.Allowlist.QQGroups),
			FeishuGroups: nonNil(b.Allowlist.FeishuGroups),
			WeixinGroups: nonNil(b.Allowlist.WeixinGroups),
		},
		QQ: QQBotView{
			Enabled:      b.QQ.Enabled,
			AppID:        b.QQ.AppID,
			AppSecretEnv: b.QQ.AppSecretEnv,
			SecretSet:    strings.TrimSpace(b.QQ.AppSecretEnv) != "" && os.Getenv(b.QQ.AppSecretEnv) != "",
			Sandbox:      b.QQ.Sandbox,
		},
		Feishu: FeishuBotView{
			Enabled:           b.Feishu.Enabled,
			Domain:            orDefault(strings.TrimSpace(b.Feishu.Domain), "feishu"),
			AppID:             b.Feishu.AppID,
			AppSecretEnv:      b.Feishu.AppSecretEnv,
			SecretSet:         strings.TrimSpace(b.Feishu.AppSecretEnv) != "" && os.Getenv(b.Feishu.AppSecretEnv) != "",
			VerificationToken: b.Feishu.VerificationToken,
			Mode:              mode,
			WebhookPort:       b.Feishu.WebhookPort,
			RequireMention:    b.Feishu.RequireMention,
		},
		Weixin: WeixinBotView{
			Enabled:   b.Weixin.Enabled,
			AccountID: b.Weixin.AccountID,
			TokenEnv:  b.Weixin.TokenEnv,
			TokenSet:  strings.TrimSpace(b.Weixin.TokenEnv) != "" && os.Getenv(b.Weixin.TokenEnv) != "",
			APIBase:   b.Weixin.APIBase,
		},
		Connections: botConnectionViews(b.Connections),
	}
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func botDomainOrDefault(domain string) string {
	if strings.EqualFold(strings.TrimSpace(domain), "lark") {
		return "lark"
	}
	return "feishu"
}

// --- apply (write config, then rebuild the controller so it's live) ---

// applyConfigChange mutates the user-global config and rebuilds the controller so
// the change takes effect this session. Desktop settings such as providers and
// keys are account-level, not per-project: writing them to the global config
// rather than the cwd's reasonix.toml is what lets them survive a workspace switch.
func (a *App) applyConfigChange(mutate func(*config.Config) error) error {
	if err := a.ensureActiveTabRebuildAllowed("settings"); err != nil {
		return err
	}
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	return a.rebuild()
}

func (a *App) applyConfigOnly(mutate func(*config.Config) error) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if err := mutate(cfg); err != nil {
		return err
	}
	return cfg.SaveTo(path)
}

func (a *App) ensureActiveTabRebuildAllowed(setting string) error {
	if a.ctx == nil {
		return nil
	}
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active tab")
	}
	if controllerHasActiveRuntimeWork(tab.Ctrl) {
		return rebuildControllerActiveWorkError(setting)
	}
	return nil
}

func (a *App) ensureLiveControllersRuntimeMutationAllowed(setting string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, tab := range a.tabs {
		if tab == nil {
			continue
		}
		if controllerHasActiveRuntimeWork(tab.Ctrl) {
			return rebuildControllerActiveWorkError(setting)
		}
	}
	return nil
}

func (a *App) loadDesktopUserConfigForEdit() (*config.Config, string, error) {
	userPath := config.UserConfigPath()
	if userPath == "" {
		return nil, "", fmt.Errorf("cannot resolve user config directory")
	}
	if _, err := os.Stat(userPath); err == nil {
		cfg := config.LoadForEdit(userPath)
		normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath)
		if err := a.migrateLegacyBotConfigToUser(cfg, userPath); err != nil {
			return nil, "", err
		}
		return cfg, userPath, nil
	}
	cfg := config.LoadForEdit(userPath)
	legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath)
		return cfg, userPath, nil
	}
	legacyCfg := config.LoadForEdit(legacyPath)
	normalizeLegacyDesktopProviderAccessForSettings(legacyCfg, legacyPath)
	legacyCfg.ConfigVersion = config.Default().ConfigVersion
	if err := migrateLegacyBotConfigToUser(cfg, legacyCfg, userPath); err != nil {
		return nil, "", err
	}
	return legacyCfg, userPath, nil
}

func (a *App) loadDesktopUserConfigForView() (*config.Config, string, error) {
	userPath := config.UserConfigPath()
	if userPath == "" {
		return nil, "", fmt.Errorf("cannot resolve user config directory")
	}
	if _, err := os.Stat(userPath); err == nil {
		cfg := config.LoadForEditWithoutCredentials(userPath)
		normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath)
		legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
		if legacyPath != "" && !sameConfigPath(legacyPath, userPath) {
			legacyCfg := config.LoadForEditWithoutCredentials(legacyPath)
			if err := migrateLegacyBotConfigToUser(cfg, legacyCfg, userPath); err != nil {
				return nil, "", err
			}
		}
		return cfg, userPath, nil
	}
	cfg := config.LoadForEditWithoutCredentials(userPath)
	legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		normalizeLegacyDesktopProviderAccessForSettings(cfg, userPath)
		return cfg, userPath, nil
	}
	legacyCfg := config.LoadForEditWithoutCredentials(legacyPath)
	normalizeLegacyDesktopProviderAccessForSettings(legacyCfg, legacyPath)
	legacyCfg.ConfigVersion = config.Default().ConfigVersion
	if err := migrateLegacyBotConfigToUser(cfg, legacyCfg, userPath); err != nil {
		return nil, "", err
	}
	return legacyCfg, userPath, nil
}

func (a *App) migrateLegacyBotConfigToUser(userCfg *config.Config, userPath string) error {
	if userCfg == nil {
		return nil
	}
	legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		return nil
	}
	legacyCfg := config.LoadForEdit(legacyPath)
	return migrateLegacyBotConfigToUser(userCfg, legacyCfg, userPath)
}

func migrateLegacyBotConfigToUser(userCfg, legacyCfg *config.Config, userPath string) error {
	if userCfg == nil || legacyCfg == nil || desktopBotConfigConfigured(userCfg.Bot) {
		return nil
	}
	if !desktopBotConfigConfigured(legacyCfg.Bot) {
		return nil
	}
	userCfg.Bot = legacyCfg.Bot
	if err := userCfg.SaveTo(userPath); err != nil {
		return fmt.Errorf("migrate legacy bot config: %w", err)
	}
	return nil
}

func desktopBotConfigConfigured(bot config.BotConfig) bool {
	defaults := config.Default().Bot
	if bot.Enabled || strings.TrimSpace(bot.Model) != "" || len(bot.Connections) > 0 {
		return true
	}
	if (bot.MaxSteps != 0 && bot.MaxSteps != defaults.MaxSteps) || (bot.DebounceMs != 0 && bot.DebounceMs != defaults.DebounceMs) {
		return true
	}
	if bot.Allowlist.AllowAll ||
		len(bot.Allowlist.QQUsers)+len(bot.Allowlist.FeishuUsers)+len(bot.Allowlist.WeixinUsers) > 0 ||
		len(bot.Allowlist.QQGroups)+len(bot.Allowlist.FeishuGroups)+len(bot.Allowlist.WeixinGroups) > 0 {
		return true
	}
	if bot.QQ.Enabled || strings.TrimSpace(bot.QQ.AppID) != "" || bot.QQ.AppSecretEnv != defaults.QQ.AppSecretEnv || bot.QQ.Sandbox != defaults.QQ.Sandbox {
		return true
	}
	if bot.Feishu.Enabled ||
		strings.TrimSpace(bot.Feishu.AppID) != "" ||
		bot.Feishu.Domain != defaults.Feishu.Domain ||
		bot.Feishu.AppSecretEnv != defaults.Feishu.AppSecretEnv ||
		strings.TrimSpace(bot.Feishu.VerificationToken) != "" ||
		bot.Feishu.Mode != defaults.Feishu.Mode ||
		bot.Feishu.WebhookPort != defaults.Feishu.WebhookPort ||
		bot.Feishu.RequireMention != defaults.Feishu.RequireMention {
		return true
	}
	if bot.Weixin.Enabled ||
		bot.Weixin.AccountID != defaults.Weixin.AccountID ||
		bot.Weixin.TokenEnv != defaults.Weixin.TokenEnv ||
		bot.Weixin.APIBase != defaults.Weixin.APIBase {
		return true
	}
	return false
}

func normalizeLegacyDesktopProviderAccessForSettings(cfg *config.Config, path string) {
	if cfg == nil || len(cfg.Desktop.ProviderAccess) > 0 || configDeclaresProviderAccess(path) {
		return
	}
	config.NormalizeLegacyDesktopProviderAccess(cfg)
}

func configDeclaresProviderAccess(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(body), "\n") {
		if before, _, ok := strings.Cut(line, "#"); ok {
			line = before
		}
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "provider_access") {
			rest := strings.TrimSpace(strings.TrimPrefix(line, "provider_access"))
			return strings.HasPrefix(rest, "=")
		}
	}
	return false
}

func (a *App) activeWorkspaceRoot() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab := a.activeTabLocked(); tab != nil {
		return tab.WorkspaceRoot
	}
	return "."
}

func (a *App) saveProviderCredential(apiKeyEnv, value string) (string, error) {
	apiKeyEnv = strings.TrimSpace(apiKeyEnv)
	value = strings.TrimSpace(value)
	root := a.activeWorkspaceRoot()
	before := config.ResolveCredentialForRoot(root, apiKeyEnv)
	beforeEnvValue, beforeEnvSet := os.LookupEnv(apiKeyEnv)
	if err := upsertDotEnv(apiKeyEnv, value); err != nil {
		return "", err
	}
	return providerCredentialShadowWarning(apiKeyEnv, value, root, before, beforeEnvSet, beforeEnvValue), nil
}

func providerCredentialShadowWarning(apiKeyEnv, value, root string, before config.CredentialResolution, beforeEnvSet bool, beforeEnvValue string) string {
	if beforeEnvSet && beforeEnvValue != value {
		return fmt.Sprintf("saved %s to Reasonix credentials, but an existing environment variable with the same name can override it after restart; update or remove that environment variable", apiKeyEnv)
	}
	if before.Set && before.Source.Kind == config.CredentialSourceEnvironment && before.Value != value {
		return fmt.Sprintf("saved %s to Reasonix credentials, but an existing environment variable with the same name can override it after restart; update or remove that environment variable", apiKeyEnv)
	}
	current := config.ResolveCredentialForRoot(root, apiKeyEnv)
	for _, source := range current.Shadowed {
		if source.Kind == config.CredentialSourceProjectEnv {
			return fmt.Sprintf("saved %s to Reasonix credentials, but this workspace's project .env also defines %s and can override it after restart; update or remove that project .env entry", apiKeyEnv, apiKeyEnv)
		}
	}
	return ""
}

func projectConfigPathForRoot(root string) string {
	if strings.TrimSpace(root) == "" || root == "." {
		return "reasonix.toml"
	}
	return filepath.Join(root, "reasonix.toml")
}

func sameConfigPath(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	aAbs, aErr := filepath.Abs(a)
	bAbs, bErr := filepath.Abs(b)
	if aErr == nil && bErr == nil {
		return filepath.Clean(aAbs) == filepath.Clean(bAbs)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// rebuild tears down the controller and rebuilds it from the (just-changed)
// config, carrying the conversation forward. It keeps the active model if it
// still resolves; otherwise it falls back to the new default. Mirrors SetModel.
func (a *App) rebuild() error {
	if a.ctx == nil {
		return nil
	}
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active tab")
	}
	if controllerHasActiveRuntimeWork(tab.Ctrl) {
		return rebuildControllerActiveWorkError("settings")
	}
	var carried []provider.Message
	prevPath := ""
	if tab.Ctrl != nil {
		prevPath = tab.Ctrl.SessionPath()
		_ = a.snapshotTab(tab)
		carried = tab.Ctrl.History()
		tab.Ctrl.Close()
	}
	model := tab.model
	if cfg, err := config.LoadForRoot(tab.WorkspaceRoot); err == nil {
		if resolved, fallback, ok := cfg.ResolveModelWithFallback(model); ok {
			if fallback && strings.TrimSpace(model) != "" {
				a.noticeForTab(tab.ID, fmt.Sprintf("model %q is no longer available; switched to %s", model, resolved))
			}
			model = resolved
		}
	}
	sharedHost := a.lookupSharedHost(tab.SharedHostKey)
	ctrl, err := boot.Build(a.bootContext(), boot.Options{
		Model: model, RequireKey: false,
		Sink:                     tab.sink,
		WorkspaceRoot:            tab.WorkspaceRoot,
		SessionDir:               tabSessionDir(tab),
		EffortOverride:           cloneStringPtr(tab.effort),
		TokenMode:                currentTabTokenMode(tab),
		SharedHost:               sharedHost,
		CleanupPendingReconciler: reconcileDesktopCleanupPending,
	})
	if err != nil {
		a.mu.Lock()
		tab.StartupErr = err.Error()
		tab.Ready = true
		a.mu.Unlock()
		a.emitReady(a.ctx)
		return err
	}
	a.bindControllerDisplayRecorder(ctrl)
	a.mu.Lock()
	tab.Ctrl = ctrl
	tab.model = model
	tab.Label = ctrl.Label()
	tab.StartupErr = ""
	tab.Ready = true
	a.saveTabsLocked()
	a.mu.Unlock()
	a.emitReady(a.ctx)
	ctrl.EnableInteractiveApproval()
	applyTabModeToController(ctrl, tab.mode)
	path := agent.ContinueSessionPath(prevPath, ctrl.SessionDir(), ctrl.Label())
	if len(carried) > 0 {
		carried = withFreshSystemPrompt(carried, systemPromptFrom(ctrl.History()))
		ctrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		ctrl.SetSessionPath(path)
	}
	a.persistTabSessionPath(tab, path)
	return nil
}

func systemPromptFrom(messages []provider.Message) string {
	for _, m := range messages {
		if m.Role == provider.RoleSystem {
			return m.Content
		}
	}
	return ""
}

func withFreshSystemPrompt(messages []provider.Message, system string) []provider.Message {
	if strings.TrimSpace(system) == "" {
		return messages
	}
	out := append([]provider.Message(nil), messages...)
	for i := range out {
		if out[i].Role == provider.RoleSystem {
			out[i].Content = system
			out[i].ReasoningContent = ""
			out[i].ReasoningSignature = ""
			out[i].ToolCalls = nil
			out[i].ToolCallID = ""
			out[i].Name = ""
			return out
		}
	}
	return append([]provider.Message{{Role: provider.RoleSystem, Content: system}}, out...)
}

// SetDefaultModel sets the config default and switches the live model to it.
func (a *App) SetDefaultModel(ref string) error {
	tab := a.activeTab()
	if tab == nil {
		return fmt.Errorf("no active tab")
	}
	prev := tab.model
	tab.model = ref
	if err := a.applyConfigChange(func(c *config.Config) error {
		resolved, err := selectableDesktopModelRef(c, ref)
		if err != nil {
			return err
		}
		c.DefaultModel = resolved
		tab.model = resolved
		return nil
	}); err != nil {
		tab.model = prev
		return err
	}
	return nil
}

// SetPlannerModel sets (or, with "", clears) the two-model planner.
func (a *App) SetPlannerModel(ref string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		if ref != "" {
			resolved, err := selectableDesktopModelRef(c, ref)
			if err != nil {
				return err
			}
			ref = resolved
		}
		c.Agent.PlannerModel = ref
		return nil
	})
}

// SetSubagentModel sets (or clears) the default model used by subagent entry points.
func (a *App) SetSubagentModel(ref string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			resolved, err := selectableDesktopModelRef(c, ref)
			if err != nil {
				return err
			}
			ref = resolved
		}
		c.Agent.SubagentModel = ref
		return nil
	})
}

func selectableDesktopModelRef(c *config.Config, ref string) (string, error) {
	entry, ok := c.ResolveModel(ref)
	if !ok {
		return "", fmt.Errorf("unknown model %q", ref)
	}
	if !modelProviderAccessAllowed(providerAccessSet(c.Desktop.ProviderAccess), entry.Name) {
		return "", fmt.Errorf("model %q is not available because provider %q is not added", ref, entry.Name)
	}
	if !entry.Configured() {
		return "", fmt.Errorf("model %q is not available because provider %q has no key", ref, entry.Name)
	}
	return entry.Name + "/" + entry.Model, nil
}

// SetSubagentEffort sets (or clears) the default effort used by subagent entry points.
func (a *App) SetSubagentEffort(level string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		level = strings.TrimSpace(level)
		if level == "" || level == "auto" {
			c.Agent.SubagentEffort = ""
			return nil
		}
		model := strings.TrimSpace(c.Agent.SubagentModel)
		if model == "" {
			model = c.DefaultModel
		}
		entry, ok := c.ResolveModel(model)
		if !ok {
			return fmt.Errorf("unknown subagent model %q", model)
		}
		effort, err := config.NormalizeEffort(entry, level)
		if err != nil {
			return err
		}
		c.Agent.SubagentEffort = effort
		return nil
	})
}

// SetAutoPlan updates the automatic plan-mode gate (off|on).
func (a *App) SetAutoPlan(mode string) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.SetAutoPlan(mode) })
}

func desktopAutoPlanMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "on", "ask":
		return "on"
	default:
		return "off"
	}
}

func officialProviderTemplate(kind, pricingLanguage string) ([]config.ProviderEntry, string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deepseek", "deepseek-official":
		return []config.ProviderEntry{{
			Name:          "deepseek",
			Kind:          "openai",
			BaseURL:       "https://api.deepseek.com",
			Models:        []string{"deepseek-v4-flash", "deepseek-v4-pro"},
			Default:       "deepseek-v4-flash",
			APIKeyEnv:     "DEEPSEEK_API_KEY",
			BalanceURL:    "https://api.deepseek.com/user/balance",
			ContextWindow: 1_000_000,
			Prices:        config.DeepSeekV4PricesForLanguage(pricingLanguage),
		}}, "DEEPSEEK_API_KEY", nil
	case "mimo-api", "xiaomi-mimo", "xiaomi_mimo":
		models := []string{"mimo-v2.5-pro", "mimo-v2.5", "mimo-v2-omni"}
		return []config.ProviderEntry{{
			Name:          "mimo-api",
			Kind:          "openai",
			BaseURL:       "https://api.xiaomimimo.com/v1",
			Models:        models,
			VisionModels:  []string{"mimo-v2.5", "mimo-v2-omni"},
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_API_KEY",
			ContextWindow: 1_048_576,
			Prices: map[string]*provider.Pricing{
				"mimo-v2.5-pro": &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"},
				"mimo-v2.5":     &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"},
				"mimo-v2-omni":  &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"},
			},
			NoProxy: true,
		}}, "MIMO_API_KEY", nil
	case "mimo-token-plan", "xiaomi-mimo-token-plan", "xiaomi_mimo_token_plan":
		models := []string{"mimo-v2.5-pro", "mimo-v2.5"}
		return []config.ProviderEntry{{
			Name:          "mimo-token-plan",
			Kind:          "openai",
			BaseURL:       "https://token-plan-cn.xiaomimimo.com/v1",
			Models:        models,
			VisionModels:  []string{"mimo-v2.5"},
			Default:       "mimo-v2.5-pro",
			APIKeyEnv:     "MIMO_API_KEY",
			ContextWindow: 1_048_576,
			Prices: map[string]*provider.Pricing{
				"mimo-v2.5-pro": &provider.Pricing{CacheHit: 0.025, Input: 3, Output: 6, Currency: "¥"},
				"mimo-v2.5":     &provider.Pricing{CacheHit: 0.02, Input: 1, Output: 2, Currency: "¥"},
			},
			NoProxy: true,
		}}, "MIMO_API_KEY", nil
	default:
		return nil, "", fmt.Errorf("unknown official provider template %q", kind)
	}
}

func chatProviderModels(models []string) []string {
	out := make([]string, 0, len(models))
	seen := map[string]bool{}
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || seen[model] || !config.IsLikelyChatModel(model) {
			continue
		}
		seen[model] = true
		out = append(out, model)
	}
	return out
}

func providerVisionModels(models, visionModels []string) []string {
	enabled := map[string]bool{}
	for _, model := range models {
		enabled[model] = true
	}
	out := make([]string, 0, len(visionModels))
	for _, model := range chatProviderModels(visionModels) {
		if enabled[model] {
			out = append(out, model)
		}
	}
	return out
}

func providerDefaultForModels(currentDefault string, models []string) string {
	currentDefault = strings.TrimSpace(currentDefault)
	if currentDefault != "" {
		for _, model := range models {
			if model == currentDefault {
				return currentDefault
			}
		}
	}
	if len(models) > 0 {
		return models[0]
	}
	return ""
}

// SaveProvider adds or updates a provider. Enabled models are persisted through
// `models` even when only one model is selected, while `model` remains populated
// in-memory for validation/back-compat. The shared key/endpoint live on the entry.
func (a *App) SaveProvider(p ProviderView) error {
	return a.applyConfigChange(func(c *config.Config) error {
		e := config.ProviderEntry{Name: p.Name}
		for i := range c.Providers {
			if c.Providers[i].Name == p.Name {
				e = c.Providers[i]
				break
			}
		}
		e.Name = p.Name
		e.Kind = p.Kind
		e.BaseURL = p.BaseURL
		e.ModelsURL = p.ModelsURL
		e.APIKeyEnv = p.APIKeyEnv
		e.BalanceURL = strings.TrimSpace(p.BalanceURL)
		e.ContextWindow = p.ContextWindow
		e.ReasoningProtocol = p.ReasoningProtocol
		e.SupportedEfforts = p.SupportedEfforts
		e.DefaultEffort = p.DefaultEffort
		e.Model = ""
		e.Models = nil
		e.Default = ""
		e.VisionModels = nil
		models := chatProviderModels(p.Models)
		if len(models) > 0 {
			e.Model = models[0] // also satisfies validateProvider's model requirement
			e.Models = models
			if p.VisionModelsSet || len(p.VisionModels) > 0 {
				e.Vision = false
				e.VisionModels = providerVisionModels(models, p.VisionModels)
			}
			if len(models) > 1 {
				e.Default = providerDefaultForModels(p.Default, models)
			}
		} else {
			e.Vision = false
			e.VisionModels = nil
		}
		if err := c.UpsertProvider(e); err != nil {
			return err
		}
		addProviderAccess(c, p.Name)
		return nil
	})
}

// AddOfficialProviderAccess adds one curated desktop provider template to the
// Settings > Model > Access list. The runtime default providers still exist
// independently; this only records the user's explicit access setup.
func (a *App) AddOfficialProviderAccess(kind, key string) (string, error) {
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return "", err
	}
	entries, keyEnv, err := officialProviderTemplate(kind, cfg.DeepSeekOfficialPricingLanguage())
	if err != nil {
		return "", err
	}
	if err := a.ensureActiveTabRebuildAllowed("provider access"); err != nil {
		return "", err
	}
	keyWarning := ""
	if strings.TrimSpace(key) != "" && keyEnv != "" {
		var err error
		keyWarning, err = a.saveProviderCredential(keyEnv, key)
		if err != nil {
			return "", err
		}
	}
	if err := a.applyConfigChange(func(c *config.Config) error {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if err := c.UpsertProvider(e); err != nil {
				return err
			}
			names = append(names, e.Name)
		}
		addProviderAccess(c, names...)
		return nil
	}); err != nil {
		return "", err
	}
	return keyWarning, nil
}

// FetchProviderModels probes the provider's OpenAI-compatible model-list
// endpoint and returns the available model IDs. This is a settings-only helper:
// it never touches chat request serialization or provider-visible prompt data.
func (a *App) FetchProviderModels(p ProviderView) ([]string, error) {
	e := config.ProviderEntry{
		Name:      p.Name,
		BaseURL:   p.BaseURL,
		ModelsURL: p.ModelsURL,
		APIKeyEnv: p.APIKeyEnv,
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), 15*time.Second)
	defer cancel()
	models, err := e.FetchModels(ctx)
	if err != nil {
		return []string{}, err
	}
	return nonNil(chatProviderModels(models)), nil
}

// DeleteProvider removes a provider and retargets open idle tabs that used it.
func (a *App) DeleteProvider(name string) error {
	return a.deleteProviderAndRetargetTabs(name)
}

// RemoveProviderAccess hides a provider from Settings > Model > Access and from
// settings model pickers. Built-in provider entries remain in the runtime config
// for back-compat, but visible defaults and idle tabs are retargeted away from
// the removed access entry when another accessed provider is available. Custom
// providers are deleted outright.
func (a *App) RemoveProviderAccess(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("remove provider access: empty provider name")
	}
	cfg, _, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if p, ok := cfg.Provider(name); ok && isOfficialBuiltInProvider(*p) {
		return a.removeBuiltInProviderAccessAndRetargetTabs(name)
	}
	return a.deleteProviderAndRetargetTabs(name)
}

type providerRemovalTab struct {
	id       string
	ctrl     *control.Controller
	readOnly bool
}

func providerAccessFallbackRef(c *config.Config, name string) string {
	name = strings.TrimSpace(name)
	for _, candidate := range c.Desktop.ProviderAccess {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == name {
			continue
		}
		p, ok := c.Provider(candidate)
		if !ok || len(p.ModelList()) == 0 {
			continue
		}
		return p.Name + "/" + p.DefaultModel()
	}
	return ""
}

func retargetProviderReferences(c *config.Config, name, fallbackRef string) {
	if strings.TrimSpace(fallbackRef) == "" {
		return
	}
	if desktopModelRefsProvider(c, c.DefaultModel, name) {
		c.DefaultModel = fallbackRef
	}
	if desktopModelRefsProvider(c, c.Agent.PlannerModel, name) {
		c.Agent.PlannerModel = fallbackRef
	}
	if desktopModelRefsProvider(c, c.Agent.SubagentModel, name) {
		c.Agent.SubagentModel = fallbackRef
	}
	for skill, ref := range c.Agent.SubagentModels {
		if desktopModelRefsProvider(c, ref, name) {
			c.Agent.SubagentModels[skill] = fallbackRef
		}
	}
}

func (a *App) removeBuiltInProviderAccessAndRetargetTabs(name string) error {
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	fallbackRef := providerAccessFallbackRef(cfg, name)

	var affected []providerRemovalTab
	if fallbackRef != "" {
		a.mu.RLock()
		for _, id := range a.orderedTabIDsLocked() {
			tab := a.tabs[id]
			if tab == nil {
				continue
			}
			ref := tab.model
			if strings.TrimSpace(ref) == "" {
				ref = cfg.DefaultModel
			}
			if !desktopModelRefsProvider(cfg, ref, name) {
				continue
			}
			if controllerHasActiveRuntimeWork(tab.Ctrl) {
				a.mu.RUnlock()
				return fmt.Errorf("finish or cancel active work using %q before removing the provider access", name)
			}
			affected = append(affected, providerRemovalTab{id: id, ctrl: tab.Ctrl, readOnly: tab.ReadOnly})
		}
		a.mu.RUnlock()
	}

	if len(affected) == 0 {
		if err := a.ensureActiveTabRebuildAllowed("provider access"); err != nil {
			return err
		}
	}
	retargetProviderReferences(cfg, name, fallbackRef)
	removeProviderAccess(cfg, name)
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	if len(affected) == 0 {
		return a.rebuild()
	}
	for _, item := range affected {
		if item.ctrl != nil {
			if !item.readOnly {
				_ = item.ctrl.Snapshot()
			}
			item.ctrl.Close()
		}
	}

	var rebuildTabs []*WorkspaceTab
	a.mu.Lock()
	for _, item := range affected {
		tab := a.tabs[item.id]
		if tab == nil {
			continue
		}
		tab.Ctrl = nil
		tab.model = fallbackRef
		tab.Label = fallbackRef
		tab.StartupErr = ""
		tab.Ready = a.ctx == nil
		if a.ctx != nil {
			rebuildTabs = append(rebuildTabs, tab)
		}
	}
	a.saveTabsLocked()
	a.mu.Unlock()

	for _, tab := range rebuildTabs {
		go a.buildTabController(tab)
	}
	return nil
}

func (a *App) deleteProviderAndRetargetTabs(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("remove provider: empty provider name")
	}
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	fallbackRef := providerRemovalFallbackRef(cfg, name)

	var affected []providerRemovalTab
	a.mu.RLock()
	for _, id := range a.orderedTabIDsLocked() {
		tab := a.tabs[id]
		if tab == nil {
			continue
		}
		ref := tab.model
		if strings.TrimSpace(ref) == "" {
			ref = cfg.DefaultModel
		}
		if !desktopModelRefsProvider(cfg, ref, name) {
			continue
		}
		if controllerHasActiveRuntimeWork(tab.Ctrl) {
			a.mu.RUnlock()
			return fmt.Errorf("finish or cancel active work using %q before deleting the provider", name)
		}
		affected = append(affected, providerRemovalTab{id: id, ctrl: tab.Ctrl, readOnly: tab.ReadOnly})
	}
	a.mu.RUnlock()

	if len(affected) > 0 && fallbackRef == "" {
		return fmt.Errorf("remove provider: %q is used by open tabs and no other configured provider exists", name)
	}
	if len(affected) == 0 {
		if err := a.ensureActiveTabRebuildAllowed("provider"); err != nil {
			return err
		}
	}
	if err := cfg.RemoveProvider(name); err != nil {
		return err
	}
	removeProviderAccess(cfg, name)
	if err := cfg.SaveTo(path); err != nil {
		return err
	}

	if len(affected) == 0 {
		return a.rebuild()
	}
	for _, item := range affected {
		if item.ctrl != nil {
			if !item.readOnly {
				_ = item.ctrl.Snapshot()
			}
			item.ctrl.Close()
		}
	}

	var rebuildTabs []*WorkspaceTab
	a.mu.Lock()
	for _, item := range affected {
		tab := a.tabs[item.id]
		if tab == nil {
			continue
		}
		tab.Ctrl = nil
		tab.model = fallbackRef
		tab.Label = fallbackRef
		tab.StartupErr = ""
		tab.Ready = a.ctx == nil
		if a.ctx != nil {
			rebuildTabs = append(rebuildTabs, tab)
		}
	}
	a.saveTabsLocked()
	a.mu.Unlock()

	for _, tab := range rebuildTabs {
		go a.buildTabController(tab)
	}
	return nil
}

// SetProviderKey writes a secret to the global credential store under the given
// env-var name (the one a provider's api_key_env points at) and rebuilds so it
// resolves immediately.
func (a *App) SetProviderKey(apiKeyEnv, value string) (string, error) {
	if strings.TrimSpace(apiKeyEnv) == "" {
		return "", fmt.Errorf("this provider has no api_key_env set")
	}
	if err := a.ensureActiveTabRebuildAllowed("provider key"); err != nil {
		return "", err
	}
	warning, err := a.saveProviderCredential(apiKeyEnv, value)
	if err != nil {
		return "", err
	}
	if err := a.ensureProviderAccessForKey(apiKeyEnv); err != nil {
		return "", err
	}
	if err := a.rebuild(); err != nil {
		return "", err
	}
	return warning, nil
}

func (a *App) ensureProviderAccessForKey(apiKeyEnv string) error {
	apiKeyEnv = strings.TrimSpace(apiKeyEnv)
	if apiKeyEnv == "" {
		return nil
	}
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	access := providerAccessSet(cfg.Desktop.ProviderAccess)
	changed := false
	addAccess := func(name string) {
		if name == "" || access[name] {
			return
		}
		addProviderAccess(cfg, name)
		access[name] = true
		changed = true
	}
	for i := range cfg.Providers {
		p := cfg.Providers[i]
		if strings.TrimSpace(p.APIKeyEnv) != apiKeyEnv {
			continue
		}
		if len(p.ModelList()) == 0 {
			continue
		}
		if isOfficialBuiltInProvider(p) {
			addAccess(config.CanonicalDesktopOfficialProviderName(p.Name))
		} else {
			addAccess(strings.TrimSpace(p.Name))
		}
	}
	if !changed && apiKeyEnv == "DEEPSEEK_API_KEY" {
		entries, _, err := officialProviderTemplate("deepseek", cfg.DeepSeekOfficialPricingLanguage())
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := cfg.UpsertProvider(e); err != nil {
				return err
			}
			addAccess(e.Name)
		}
	}
	if !changed {
		return nil
	}
	return cfg.SaveTo(path)
}

// ClearProviderKey removes a provider secret from the global credential store
// and rebuilds so the provider immediately becomes unauthenticated.
func (a *App) ClearProviderKey(apiKeyEnv string) error {
	if strings.TrimSpace(apiKeyEnv) == "" {
		return fmt.Errorf("this provider has no api_key_env set")
	}
	if err := a.ensureActiveTabRebuildAllowed("provider key"); err != nil {
		return err
	}
	if err := removeDotEnv(apiKeyEnv); err != nil {
		return err
	}
	return a.rebuild()
}

// SetPermissionMode sets the writer-fallback mode (ask|allow|deny).
func (a *App) SetPermissionMode(mode string) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.SetPermissionMode(mode) })
}

// AddPermissionRule appends a rule to the allow/ask/deny list.
func (a *App) AddPermissionRule(list, rule string) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.AddPermissionRule(list, rule) })
}

// RemovePermissionRule drops a rule from the allow/ask/deny list.
func (a *App) RemovePermissionRule(list, rule string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		_, err := c.RemovePermissionRule(list, rule)
		return err
	})
}

// SetSandbox updates the bash sandbox mode, network egress, and write roots.
func (a *App) SetSandbox(bash string, network bool, workspaceRoot string, allowWrite []string, shell string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Sandbox.Bash = bash
		c.Sandbox.Network = network
		c.Sandbox.WorkspaceRoot = strings.TrimSpace(workspaceRoot)
		c.Sandbox.AllowWrite = trimList(allowWrite)
		c.Tools.Shell.Prefer = strings.TrimSpace(shell)
		return nil
	})
}

// SetNetwork updates ordinary outbound proxy settings.
func (a *App) SetNetwork(n NetworkView) error {
	return a.applyConfigChange(func(c *config.Config) error {
		return c.SetNetwork(config.NetworkConfig{
			ProxyMode: n.ProxyMode,
			ProxyURL:  n.ProxyURL,
			NoProxy:   n.NoProxy,
			Proxy: config.NetworkProxyConfig{
				Type:     n.Proxy.Type,
				Server:   n.Proxy.Server,
				Port:     n.Proxy.Port,
				Username: n.Proxy.Username,
				Password: n.Proxy.Password,
			},
		})
	})
}

func (a *App) SetBotSettings(b BotSettingsView) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		c.Bot.Enabled = b.Enabled
		c.Bot.Model = strings.TrimSpace(b.Model)
		c.Bot.ToolApprovalMode = normalizeBotConnectionToolApprovalMode(b.ToolApprovalMode)
		c.Bot.MaxSteps = b.MaxSteps
		c.Bot.DebounceMs = b.DebounceMs
		c.Bot.Allowlist = config.BotAllowlist{
			Enabled:      b.Allowlist.Enabled,
			AllowAll:     b.Allowlist.AllowAll,
			QQUsers:      trimList(b.Allowlist.QQUsers),
			FeishuUsers:  trimList(b.Allowlist.FeishuUsers),
			WeixinUsers:  trimList(b.Allowlist.WeixinUsers),
			QQGroups:     trimList(b.Allowlist.QQGroups),
			FeishuGroups: trimList(b.Allowlist.FeishuGroups),
			WeixinGroups: trimList(b.Allowlist.WeixinGroups),
		}
		c.Bot.QQ = config.QQBotConfig{
			Enabled:      b.QQ.Enabled,
			AppID:        strings.TrimSpace(b.QQ.AppID),
			AppSecretEnv: strings.TrimSpace(b.QQ.AppSecretEnv),
			Sandbox:      b.QQ.Sandbox,
		}
		c.Bot.Feishu = config.FeishuBotConfig{
			Enabled:           b.Feishu.Enabled,
			Domain:            botDomainOrDefault(b.Feishu.Domain),
			AppID:             strings.TrimSpace(b.Feishu.AppID),
			AppSecretEnv:      strings.TrimSpace(b.Feishu.AppSecretEnv),
			VerificationToken: strings.TrimSpace(b.Feishu.VerificationToken),
			Mode:              strings.TrimSpace(b.Feishu.Mode),
			WebhookPort:       b.Feishu.WebhookPort,
			RequireMention:    b.Feishu.RequireMention,
		}
		c.Bot.Weixin = config.WeixinBotConfig{
			Enabled:   b.Weixin.Enabled,
			AccountID: strings.TrimSpace(b.Weixin.AccountID),
			TokenEnv:  strings.TrimSpace(b.Weixin.TokenEnv),
			APIBase:   strings.TrimRight(strings.TrimSpace(b.Weixin.APIBase), "/"),
		}
		c.Bot.Connections = botConnectionConfigs(b.Connections)
		return nil
	})
	if err == nil {
		a.refreshBotRuntimeAsync()
	}
	return err
}

func (a *App) SetBotSecret(envName, value string) error {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return fmt.Errorf("bot secret env name is empty")
	}
	if err := upsertDotEnv(envName, value); err != nil {
		return err
	}
	a.refreshBotRuntimeAsync()
	return nil
}

func (a *App) ClearBotSecret(envName string) error {
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return fmt.Errorf("bot secret env name is empty")
	}
	if err := removeDotEnv(envName); err != nil {
		return err
	}
	a.refreshBotRuntimeAsync()
	return nil
}

// SetCloseBehavior updates desktop-only window close behavior without rebuilding
// the active controller. It must stay out of provider-visible prompt/request data.
func (a *App) SetCloseBehavior(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopCloseBehavior(mode) })
}

// SetDisplayMode updates the transcript display mode. UI-only, no rebuild needed.
func (a *App) SetDisplayMode(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopDisplayMode(mode) })
}

// SetStatusBarStyle updates the desktop status bar metric label style. UI-only,
// no rebuild needed.
func (a *App) SetStatusBarStyle(style string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopStatusBarStyle(style) })
}

// SetStatusBarItems updates the ordered visible desktop status bar items.
// UI-only, no rebuild needed.
func (a *App) SetStatusBarItems(items []string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopStatusBarItems(items) })
}

// SetDesktopLanguage updates only the desktop UI language. It deliberately does
// not touch config.language, which the CLI/model-facing runtime uses.
func (a *App) SetDesktopLanguage(lang string) error {
	if err := a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopLanguage(lang) }); err != nil {
		return err
	}
	a.updateTrayLocale(lang)
	return nil
}

// SetTrayLocale mirrors the resolved desktop UI language into the native tray
// menu. It is runtime-only; the persisted preference remains [desktop].language.
func (a *App) SetTrayLocale(locale string) error {
	if locale != "zh" {
		locale = "en"
	}
	a.updateTrayLocale(locale)
	return nil
}

// SetDesktopAppearance updates only desktop theme preferences. It does not
// rebuild the active controller and must stay out of provider-visible requests.
func (a *App) SetDesktopAppearance(theme, style string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopAppearance(theme, style) })
}

// SetDesktopLayoutStyle updates only the desktop layout style. It does not
// rebuild the active controller and must stay out of provider-visible requests.
func (a *App) SetDesktopLayoutStyle(style string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopLayoutStyle(style) })
}

// SetDesktopCheckUpdates updates only the desktop startup update-check
// preference. Manual checks in Settings are unaffected.
func (a *App) SetDesktopCheckUpdates(enabled bool) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopCheckUpdates(enabled) })
}

// SetDesktopTelemetry sets whether the desktop sends the anonymous launch ping.
func (a *App) SetDesktopTelemetry(enabled bool) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopTelemetry(enabled) })
}

// SetDesktopMetrics sets whether the desktop sends aggregate desktop metrics,
// starting or stopping the live aggregator so the toggle takes effect immediately.
func (a *App) SetDesktopMetrics(enabled bool) error {
	if err := a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopMetrics(enabled) }); err != nil {
		return err
	}
	switch {
	case enabled && a.metrics.Load() == nil && version != "dev":
		a.metrics.Store(newMetricsAggregator(config.MemoryUserDir()))
		if cfg, err := config.Load(); err == nil {
			a.recordSettingsMetricsSnapshot(cfg)
		}
	case !enabled:
		a.metrics.Store(nil)
	}
	return nil
}

// SetExpandThinking sets whether reasoning text is expanded by default on
// the desktop. It is desktop-only and does not rebuild the controller.
func (a *App) SetExpandThinking(on bool) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetExpandThinking(on) })
}

// MigrateDesktopPreferences imports old browser-local desktop preferences into
// the user config once. Existing [desktop] values win so stale localStorage never
// overwrites an explicit config edit.
func (a *App) MigrateDesktopPreferences(language, theme, style string) error {
	return a.applyConfigOnly(func(c *config.Config) error {
		if strings.TrimSpace(c.Desktop.Language) == "" {
			if err := c.SetDesktopLanguage(language); err != nil {
				return err
			}
		}
		if strings.TrimSpace(c.Desktop.Theme) == "" && strings.TrimSpace(c.Desktop.ThemeStyle) == "" {
			if err := c.SetDesktopAppearance(theme, style); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetAgentParams updates sampling temperature, optional step guards, and the
// base system prompt.
func (a *App) SetAgentParams(temperature float64, maxSteps int, plannerMaxSteps int, systemPrompt string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Agent.Temperature = temperature
		c.Agent.MaxSteps = maxSteps
		c.Agent.PlannerMaxSteps = plannerMaxSteps
		c.Agent.SystemPrompt = systemPrompt
		return nil
	})
}

func (a *App) SetColdResumePrune(enabled bool) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.SetColdResumePrune(enabled) })
}

func (a *App) SetReasoningLanguage(lang string) error {
	if err := a.ensureLiveControllersRuntimeMutationAllowed("reasoning language"); err != nil {
		return err
	}
	cfg, path, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return err
	}
	if err := cfg.SetReasoningLanguage(lang); err != nil {
		return err
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	a.applyReasoningLanguageToLiveControllers(cfg.ReasoningLanguage())
	return nil
}

func (a *App) applyReasoningLanguageToLiveControllers(fallback string) {
	type liveTab struct {
		root string
		ctrl *control.Controller
	}
	var tabs []liveTab
	a.mu.RLock()
	for _, tab := range a.tabs {
		if tab != nil && tab.Ctrl != nil {
			tabs = append(tabs, liveTab{root: tab.WorkspaceRoot, ctrl: tab.Ctrl})
		}
	}
	a.mu.RUnlock()
	for _, tab := range tabs {
		mode := fallback
		if cfg, err := config.LoadForRoot(tab.root); err == nil {
			mode = cfg.ReasoningLanguage()
		}
		tab.ctrl.SetReasoningLanguage(mode)
	}
}

// trimList drops blank entries from a string slice (and returns a non-nil slice).
func trimList(in []string) []string {
	out := []string{}
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	return out
}
