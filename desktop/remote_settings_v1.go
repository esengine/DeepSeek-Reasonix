package main

import (
	"fmt"

	"reasonix/internal/config"
	"reasonix/internal/runtimeapi"
)

// RemoteHostRuntimeSummaryView is the only Host configuration payload exposed
// by the Remote Settings page. Both members are frozen, secret-free RuntimeAPI
// projections; Desktop config, SSH arguments and Host credentials cannot enter
// this shape.
type RemoteHostRuntimeSummaryView struct {
	Capabilities runtimeapi.Capabilities      `json:"capabilities"`
	Config       runtimeapi.HostConfigSummary `json:"config"`
	Catalog      RemoteSessionCatalogView     `json:"catalog"`
}

// RemoteSessionCatalogView is the deliberately narrow, read-only projection
// shown by Remote Settings for the currently active Host Session. It excludes
// opaque IDs, commands, paths, URLs, skill bodies, plugin sources and all
// transport/mutation details even though richer Desktop-local management
// views exist for those domains.
type RemoteSessionCatalogView struct {
	Available  bool                         `json:"available"`
	MCPServers []RemoteSessionMCPServerView `json:"mcpServers"`
	Skills     []RemoteSessionSkillView     `json:"skills"`
	Plugins    []RemoteSessionPluginView    `json:"plugins"`
}

type RemoteSessionMCPServerView struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	ToolCount int    `json:"toolCount"`
}

type RemoteSessionSkillView struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`
}

type RemoteSessionPluginView struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func unavailableRemoteSessionCatalogView() RemoteSessionCatalogView {
	return RemoteSessionCatalogView{
		MCPServers: []RemoteSessionMCPServerView{},
		Skills:     []RemoteSessionSkillView{},
		Plugins:    []RemoteSessionPluginView{},
	}
}

func projectRemoteSessionCatalog(catalog runtimeapi.SessionCatalog) RemoteSessionCatalogView {
	view := unavailableRemoteSessionCatalogView()
	view.Available = true
	view.MCPServers = make([]RemoteSessionMCPServerView, len(catalog.MCPServers))
	for index, server := range catalog.MCPServers {
		status := "unavailable"
		if server.Available {
			status = "available"
		}
		toolCount := server.ToolCount
		if toolCount < 0 {
			toolCount = 0
		}
		view.MCPServers[index] = RemoteSessionMCPServerView{
			Name: server.Name, Status: status, ToolCount: toolCount,
		}
	}
	view.Skills = make([]RemoteSessionSkillView, len(catalog.Skills))
	for index, skill := range catalog.Skills {
		view.Skills[index] = RemoteSessionSkillView{
			Name: skill.Name, Description: skill.Description, Scope: skill.Scope,
		}
	}
	view.Plugins = make([]RemoteSessionPluginView, len(catalog.Plugins))
	for index, plugin := range catalog.Plugins {
		view.Plugins[index] = RemoteSessionPluginView{Name: plugin.Name, Enabled: plugin.Enabled}
	}
	return view
}

// desktopHistoryPageTurns is the single Desktop policy point used by Local and
// Remote Runtime adapters. It intentionally reads only the Desktop user setting
// and never depends on a workspace path or on Host configuration.
func (a *App) desktopHistoryPageTurns() int {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil || cfg == nil {
		return config.DefaultDesktopHistoryPageTurns
	}
	return cfg.DesktopHistoryPageTurns()
}

// SetDesktopHistoryPageTurns persists the common Local/Remote transcript page
// and Remote attach window without rebuilding a controller.
func (a *App) SetDesktopHistoryPageTurns(turns int) error {
	return a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopHistoryPageTurns(turns)
	})
}

func (a *App) RemoteHostRuntimeSummary() (RemoteHostRuntimeSummaryView, error) {
	if !a.remoteTargetSelected() {
		return RemoteHostRuntimeSummaryView{}, fmt.Errorf("Remote Host is not selected")
	}
	api, _, err := a.remoteConnectedV1Runtime()
	if err != nil {
		return RemoteHostRuntimeSummaryView{}, err
	}
	ctx, cancel := a.remoteActionContext()
	defer cancel()
	capabilities, err := api.HostCapabilities(ctx)
	if err != nil {
		return RemoteHostRuntimeSummaryView{}, err
	}
	summary, err := api.HostConfigSummary(ctx)
	if err != nil {
		return RemoteHostRuntimeSummaryView{}, err
	}
	summary.Models = nonNil(summary.Models)
	summary.CollaborationModes = nonNil(summary.CollaborationModes)
	summary.TokenModes = nonNil(summary.TokenModes)
	summary.ToolApprovalModes = nonNil(summary.ToolApprovalModes)
	if summary.EffectiveScopes == nil {
		summary.EffectiveScopes = []runtimeapi.EffectiveScope{}
	}
	if summary.DisplayPaths == nil {
		summary.DisplayPaths = []runtimeapi.ConfigDisplayPath{}
	}
	if summary.FeatureStates == nil {
		summary.FeatureStates = []runtimeapi.FeatureState{}
	}
	if summary.CLIHints == nil {
		summary.CLIHints = []runtimeapi.CLIHint{}
	}

	// SessionCatalog is session-scoped, so a connected Host without an active,
	// generation-current Session has no catalog to display. Catalog failures are
	// intentionally collapsed to the same generic unavailable projection: raw
	// Host/transport errors do not belong in this secret-free settings payload.
	catalogView := unavailableRemoteSessionCatalogView()
	if session, _, target, ok := a.remoteSessionView(""); ok &&
		target.State == TargetRemoteConnected &&
		session.AttachedGeneration == target.Generation {
		catalog, catalogErr := api.SessionCatalog(ctx, runtimeapi.SessionCatalogInput{Session: session.Created.Session})
		if catalogErr == nil {
			catalogView = projectRemoteSessionCatalog(catalog)
		}
	}
	return RemoteHostRuntimeSummaryView{Capabilities: capabilities, Config: summary, Catalog: catalogView}, nil
}

// remoteSafeSettingsView keeps the legacy broad Settings() bridge structurally
// compatible while a Remote target is selected, without projecting any Local
// runtime, provider, credential, bot, network, sandbox or filesystem state as
// Host state. Remote Host information is available only through the frozen
// RemoteHostRuntimeSummary projection above.
func remoteSafeSettingsView(cfg *config.Config) SettingsView {
	if cfg == nil {
		cfg = config.Default()
	}
	return SettingsView{
		Providers:         []ProviderView{},
		OfficialProviders: []ProviderView{},
		ProviderPresets:   []ProviderPresetView{},
		ProviderKinds:     []string{},
		Permissions:       PermissionsView{Mode: "ask", Allow: []string{}, Ask: []string{}, Deny: []string{}},
		Sandbox: SandboxView{
			Bash: "off", AllowWrite: []string{}, EffectiveWriteRoots: []string{}, Shell: "auto",
		},
		Network: NetworkView{ProxyMode: "off", Proxy: NetworkProxyView{Type: "socks5"}},
		Agent: AgentView{
			MaxSubagentDepth: 1, ColdResumePrune: true, ReasoningLanguage: "auto",
		},
		Bot:                     botSettingsView(config.BotConfig{}),
		AutoPlan:                "off",
		DesktopLanguage:         cfg.DesktopLanguage(),
		DesktopLayoutStyle:      cfg.DesktopLayoutStyle(),
		DesktopTheme:            cfg.DesktopTheme(),
		DesktopThemeStyle:       cfg.DesktopThemeStyle(),
		CloseBehavior:           cfg.DesktopCloseBehavior(),
		DisplayMode:             cfg.DesktopDisplayMode(),
		HistoryPageTurns:        cfg.DesktopHistoryPageTurns(),
		StatusBarStyle:          cfg.DesktopStatusBarStyle(),
		StatusBarItems:          cfg.DesktopStatusBarItems(),
		DefaultToolApprovalMode: "ask",
		CheckUpdates:            cfg.DesktopCheckUpdates(),
		Telemetry:               cfg.DesktopTelemetry(),
		Metrics:                 cfg.DesktopMetrics(),
		MemoryCompiler:          false,
		ExpandThinking:          cfg.Desktop.ExpandThinking,
	}
}
