package main

import (
	"fmt"
	"os"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

// settings_app.go is the desktop Settings panel's command surface: it reads the
// resolved config and applies edits through internal/config/edit.go (the
// purpose-built mutation API), then rebuilds the controller so the change takes
// effect live — the same snapshot→reload→resume pattern as SetModel. Secrets are
// the exception: they go to ./.env (upsertDotEnv), since config stores only the
// env-var name, not the key.

// --- read ---

type ProviderView struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	BaseURL       string   `json:"baseUrl"`
	Models        []string `json:"models"`
	Default       string   `json:"default"`
	APIKeyEnv     string   `json:"apiKeyEnv"`
	KeySet        bool     `json:"keySet"` // the env var currently resolves to a non-empty value
	BalanceURL    string   `json:"balanceUrl"`
	ContextWindow int      `json:"contextWindow"`
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

type AgentView struct {
	Temperature  float64 `json:"temperature"`
	MaxSteps     int     `json:"maxSteps"`
	SystemPrompt string  `json:"systemPrompt"`
}

// SettingsView is the whole Settings panel payload.
type SettingsView struct {
	DefaultModel string          `json:"defaultModel"`
	PlannerModel string          `json:"plannerModel"`
	Providers    []ProviderView  `json:"providers"`
	Permissions  PermissionsView `json:"permissions"`
	Sandbox      SandboxView     `json:"sandbox"`
	Agent        AgentView       `json:"agent"`
	Language     string          `json:"language"`
	ConfigPath   string          `json:"configPath"`
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
	cfg, err := config.Load()
	if err != nil {
		return SettingsView{Providers: []ProviderView{}}
	}
	bash := cfg.Sandbox.Bash
	if bash == "" {
		bash = "enforce"
	}
	v := SettingsView{
		DefaultModel: cfg.DefaultModel,
		PlannerModel: cfg.Agent.PlannerModel,
		Providers:    []ProviderView{},
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
		Agent:         AgentView{Temperature: cfg.Agent.Temperature, MaxSteps: cfg.Agent.MaxSteps, SystemPrompt: cfg.Agent.SystemPrompt},
		Language:      cfg.Language,
		ConfigPath:    config.SourcePath(),
		ProviderKinds: provider.Kinds(),
		Bypass:        a.ctrl != nil && a.ctrl.Bypass(),
	}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		v.Providers = append(v.Providers, ProviderView{
			Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL,
			Models: nonNil(p.ModelList()), Default: p.DefaultModel(),
			APIKeyEnv:     p.APIKeyEnv,
			KeySet:        p.APIKeyEnv != "" && os.Getenv(p.APIKeyEnv) != "",
			BalanceURL:    p.BalanceURL,
			ContextWindow: p.ContextWindow,
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
// the change takes effect this session. Desktop settings (providers, keys,
// language) are account-level, not per-project: writing them to the global config
// rather than the cwd's reasonix.toml is what lets them survive a workspace switch.
func (a *App) applyConfigChange(mutate func(*config.Config) error) error {
	path := config.UserConfigPath()
	if path == "" {
		return fmt.Errorf("cannot resolve user config directory")
	}
	cfg := config.LoadForEdit(path)
	if err := mutate(cfg); err != nil {
		return err
	}
	if err := cfg.SaveTo(path); err != nil {
		return err
	}
	return a.rebuild()
}

// rebuild tears down the controller and rebuilds it from the (just-changed)
// config, carrying the conversation forward. It keeps the active model if it
// still resolves; otherwise it falls back to the new default. Mirrors SetModel.
func (a *App) rebuild() error {
	if a.ctx == nil {
		return nil
	}
	var carried []provider.Message
	if a.ctrl != nil {
		_ = a.ctrl.Snapshot()
		carried = a.ctrl.History()
		a.ctrl.Close()
	}
	model := a.model
	if cfg, err := config.Load(); err == nil {
		if _, ok := cfg.ResolveModel(model); !ok {
			model = cfg.DefaultModel
			if e, ok := cfg.ResolveModel(model); ok {
				model = e.Name + "/" + e.Model
			}
		}
	}
	ctrl, err := boot.Build(a.ctx, boot.Options{Model: model, RequireKey: false, Sink: a.sink})
	if err != nil {
		a.ctrl = nil
		a.startupErr = err.Error()
		return err
	}
	a.ctrl = ctrl
	a.model = model
	a.label = ctrl.Label()
	a.startupErr = ""
	ctrl.EnableInteractiveApproval()
	path := ""
	if dir := ctrl.SessionDir(); dir != "" {
		path = agent.NewSessionPath(dir, ctrl.Label())
	}
	if len(carried) > 0 {
		ctrl.Resume(&agent.Session{Messages: carried}, path)
	} else if path != "" {
		ctrl.SetSessionPath(path)
	}
	return nil
}

// SetDefaultModel sets the config default and switches the live model to it.
func (a *App) SetDefaultModel(ref string) error {
	prev := a.model
	a.model = ref
	if err := a.applyConfigChange(func(c *config.Config) error {
		if _, ok := c.ResolveModel(ref); !ok {
			return fmt.Errorf("unknown model %q", ref)
		}
		c.DefaultModel = ref
		return nil
	}); err != nil {
		a.model = prev
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

// SaveProvider adds or updates a provider. A single model fills `model`; several
// fill `models` (with `default`). The shared key/endpoint live on the entry.
func (a *App) SaveProvider(p ProviderView) error {
	return a.applyConfigChange(func(c *config.Config) error {
		e := config.ProviderEntry{
			Name: p.Name, Kind: p.Kind, BaseURL: p.BaseURL,
			APIKeyEnv: p.APIKeyEnv, BalanceURL: strings.TrimSpace(p.BalanceURL), ContextWindow: p.ContextWindow,
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

// SetProviderKey writes a secret to ./.env under the given env-var name (the one a
// provider's api_key_env points at) and rebuilds so it resolves immediately.
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

// SetLanguage sets the UI language tag ("zh" | "en" | "" for auto). It only
// rewrites config — no controller rebuild needed.
func (a *App) SetLanguage(lang string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Language = strings.TrimSpace(lang)
	// Keep the Go-side catalogue in sync so backend-provided slash UI re-localizes
	// on the next fetch (matches the frontend's language switch).
	i18n.DetectLanguage(cfg.Language)
	return cfg.Save()
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
