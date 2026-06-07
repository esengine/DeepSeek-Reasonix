package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/provider"
)

// settings_app.go is the desktop Settings panel's command surface: it reads the
// resolved config and applies edits through internal/config/edit.go (the
// purpose-built mutation API), then rebuilds the controller so the change takes
// effect live — the same snapshot→reload→resume pattern as SetModel. Secrets are
// the exception: they go to the global credentials file (upsertDotEnv), since
// config stores only the env-var name, not the key.

// --- read ---

type ProviderView struct {
	Name             string   `json:"name"`
	Kind             string   `json:"kind"`
	BaseURL          string   `json:"baseUrl"`
	Models           []string `json:"models"`
	Default          string   `json:"default"`
	APIKeyEnv        string   `json:"apiKeyEnv"`
	KeySet           bool     `json:"keySet"` // the env var currently resolves to a non-empty value
	BalanceURL       string   `json:"balanceUrl"`
	ContextWindow    int      `json:"contextWindow"`
	SupportedEfforts []string `json:"supportedEfforts"`
	DefaultEffort    string   `json:"defaultEffort"`
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
	Temperature  float64 `json:"temperature"`
	MaxSteps     int     `json:"maxSteps"`
	SystemPrompt string  `json:"systemPrompt"`
}

// SettingsView is the whole Settings panel payload.
type SettingsView struct {
	DefaultModel      string          `json:"defaultModel"`
	PlannerModel      string          `json:"plannerModel"`
	SubagentModel     string          `json:"subagentModel"`
	SubagentEffort    string          `json:"subagentEffort"`
	AutoPlan          string          `json:"autoPlan"`
	Providers         []ProviderView  `json:"providers"`
	Permissions       PermissionsView `json:"permissions"`
	Sandbox           SandboxView     `json:"sandbox"`
	Network           NetworkView     `json:"network"`
	Agent             AgentView       `json:"agent"`
	DesktopLanguage   string          `json:"desktopLanguage"`
	DesktopTheme      string          `json:"desktopTheme"`
	DesktopThemeStyle string          `json:"desktopThemeStyle"`
	CloseBehavior     string          `json:"closeBehavior"`
	ConfigPath        string          `json:"configPath"`
	// ProviderKinds lists the provider implementations the kernel actually
	// registered (provider.Kinds()), so the editor's "kind" picker offers only
	// kinds that resolve — selecting an unregistered one would fail the rebuild.
	ProviderKinds []string `json:"providerKinds"`
	// Bypass is the live YOLO state (runtime-only, not from config), so the panel's
	// toggle reflects whether approvals are currently being skipped this session.
	Bypass bool `json:"bypass"`
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// Settings returns the current configuration for the Settings panel.
func (a *App) Settings() SettingsView {
	cfg, cfgPath, err := a.loadDesktopUserConfigForEdit()
	if err != nil {
		return SettingsView{
			Providers:     []ProviderView{},
			ProviderKinds: nonNil(provider.Kinds()),
			Permissions: PermissionsView{
				Mode:  "ask",
				Allow: []string{},
				Ask:   []string{},
				Deny:  []string{},
			},
			Sandbox:           SandboxView{Bash: "enforce", AllowWrite: []string{}},
			AutoPlan:          "off",
			DesktopTheme:      "dark",
			DesktopThemeStyle: "graphite",
			CloseBehavior:     "background",
		}
	}
	ctrl := a.activeCtrl()
	bash := cfg.Sandbox.Bash
	if bash == "" {
		bash = "enforce"
	}
	v := SettingsView{
		DefaultModel:   cfg.DefaultModel,
		PlannerModel:   cfg.Agent.PlannerModel,
		SubagentModel:  cfg.Agent.SubagentModel,
		SubagentEffort: cfg.Agent.SubagentEffort,
		AutoPlan:       desktopAutoPlanMode(cfg.Agent.AutoPlan),
		Providers:      []ProviderView{},
		Permissions: PermissionsView{
			Mode:  orDefault(cfg.Permissions.Mode, "ask"),
			Allow: nonNil(cfg.Permissions.Allow),
			Ask:   nonNil(cfg.Permissions.Ask),
			Deny:  nonNil(cfg.Permissions.Deny),
		},
		Sandbox: SandboxView{
			Bash: bash, Network: cfg.Sandbox.Network,
			WorkspaceRoot: cfg.Sandbox.WorkspaceRoot, AllowWrite: nonNil(cfg.Sandbox.AllowWrite),
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
		Agent:             AgentView{Temperature: cfg.Agent.Temperature, MaxSteps: cfg.Agent.MaxSteps, SystemPrompt: cfg.Agent.SystemPrompt},
		DesktopLanguage:   cfg.DesktopLanguage(),
		DesktopTheme:      cfg.DesktopTheme(),
		DesktopThemeStyle: cfg.DesktopThemeStyle(),
		CloseBehavior:     cfg.DesktopCloseBehavior(),
		ConfigPath:        cfgPath,
		ProviderKinds:     nonNil(provider.Kinds()),
		Bypass:            ctrl != nil && ctrl.Bypass(),
	}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		v.Providers = append(v.Providers, ProviderView{
			Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL,
			Models: nonNil(p.ModelList()), Default: p.DefaultModel(),
			APIKeyEnv:        p.APIKeyEnv,
			KeySet:           p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) != "",
			BalanceURL:       p.BalanceURL,
			ContextWindow:    p.ContextWindow,
			SupportedEfforts: nonNil(p.SupportedEfforts),
			DefaultEffort:    p.DefaultEffort,
		})
	}
	return v
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// --- apply (write config, then rebuild the controller so it's live) ---

// applyConfigChange mutates the user-global config and rebuilds the controller so
// the change takes effect this session. Desktop settings such as providers and
// keys are account-level, not per-project: writing them to the global config
// rather than the cwd's reasonix.toml is what lets them survive a workspace switch.
func (a *App) applyConfigChange(mutate func(*config.Config) error) error {
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

func (a *App) loadDesktopUserConfigForEdit() (*config.Config, string, error) {
	userPath := config.UserConfigPath()
	if userPath == "" {
		return nil, "", fmt.Errorf("cannot resolve user config directory")
	}
	if _, err := os.Stat(userPath); err == nil {
		return config.LoadForEdit(userPath), userPath, nil
	}
	cfg := config.LoadForEdit(userPath)
	legacyPath := config.SourcePathForRoot(a.activeWorkspaceRoot())
	if legacyPath == "" || sameConfigPath(legacyPath, userPath) {
		return cfg, userPath, nil
	}
	legacyCfg := config.LoadForEdit(legacyPath)
	legacyCfg.ConfigVersion = config.Default().ConfigVersion
	return legacyCfg, userPath, nil
}

func (a *App) activeWorkspaceRoot() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab := a.activeTabLocked(); tab != nil {
		return tab.WorkspaceRoot
	}
	return "."
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
	var carried []provider.Message
	prevPath := ""
	if tab.Ctrl != nil {
		prevPath = tab.Ctrl.SessionPath()
		_ = tab.Ctrl.Snapshot()
		carried = tab.Ctrl.History()
		tab.Ctrl.Close()
	}
	model := tab.model
	if cfg, err := config.LoadForRoot(tab.WorkspaceRoot); err == nil {
		if _, ok := cfg.ResolveModel(model); !ok {
			model = cfg.DefaultModel
			if e, ok := cfg.ResolveModel(model); ok {
				model = e.Name + "/" + e.Model
			}
		}
	}
	ctrl, err := boot.Build(a.bootContext(), boot.Options{
		Model: model, RequireKey: false,
		Sink:           tab.sink,
		WorkspaceRoot:  tab.WorkspaceRoot,
		EffortOverride: cloneStringPtr(tab.effort),
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
		if _, ok := c.ResolveModel(ref); !ok {
			return fmt.Errorf("unknown model %q", ref)
		}
		c.DefaultModel = ref
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
			if _, ok := c.ResolveModel(ref); !ok {
				return fmt.Errorf("unknown planner model %q", ref)
			}
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
			if _, ok := c.ResolveModel(ref); !ok {
				return fmt.Errorf("unknown subagent model %q", ref)
			}
		}
		c.Agent.SubagentModel = ref
		return nil
	})
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

// SaveProvider adds or updates a provider. A single model fills `model`; several
// fill `models` (with `default`). The shared key/endpoint live on the entry.
func (a *App) SaveProvider(p ProviderView) error {
	return a.applyConfigChange(func(c *config.Config) error {
		e := config.ProviderEntry{
			Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL,
			APIKeyEnv: p.APIKeyEnv, BalanceURL: strings.TrimSpace(p.BalanceURL), ContextWindow: p.ContextWindow,
			SupportedEfforts: p.SupportedEfforts,
			DefaultEffort:    p.DefaultEffort,
		}
		if len(p.Models) > 0 {
			e.Model = p.Models[0] // also satisfies validateProvider's model requirement
			if len(p.Models) > 1 {
				e.Models = p.Models
				e.Default = p.Default
			}
		}
		return c.UpsertProvider(e)
	})
}

// DeleteProvider removes a provider (refused for the current default_model).
func (a *App) DeleteProvider(name string) error {
	return a.applyConfigChange(func(c *config.Config) error { return c.RemoveProvider(name) })
}

// SetProviderKey writes a secret to the global credentials file under the given
// env-var name (the one a provider's api_key_env points at) and rebuilds so it
// resolves immediately.
func (a *App) SetProviderKey(apiKeyEnv, value string) error {
	if strings.TrimSpace(apiKeyEnv) == "" {
		return fmt.Errorf("this provider has no api_key_env set")
	}
	if err := upsertDotEnv(apiKeyEnv, value); err != nil {
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
func (a *App) SetSandbox(bash string, network bool, workspaceRoot string, allowWrite []string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Sandbox.Bash = bash
		c.Sandbox.Network = network
		c.Sandbox.WorkspaceRoot = strings.TrimSpace(workspaceRoot)
		c.Sandbox.AllowWrite = trimList(allowWrite)
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

// SetCloseBehavior updates desktop-only window close behavior without rebuilding
// the active controller. It must stay out of provider-visible prompt/request data.
func (a *App) SetCloseBehavior(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopCloseBehavior(mode) })
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

// SetAgentParams updates sampling temperature, the optional max-steps guard, and
// the base system prompt.
func (a *App) SetAgentParams(temperature float64, maxSteps int, systemPrompt string) error {
	return a.applyConfigChange(func(c *config.Config) error {
		c.Agent.Temperature = temperature
		c.Agent.MaxSteps = maxSteps
		c.Agent.SystemPrompt = systemPrompt
		return nil
	})
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
