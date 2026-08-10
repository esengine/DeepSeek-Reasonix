package main

import (
	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
)

func desktopStartupSettingsFromConfig(cfg *config.Config) DesktopStartupSettingsView {
	if cfg == nil {
		return DesktopStartupSettingsView{
			Bot: botSettingsView(config.BotConfig{}), DesktopLayoutStyle: "workbench",
			DesktopTheme: "auto", DesktopThemeStyle: "graphite", DesktopTerminalTheme: "auto",
			DisplayMode: "standard", ReasoningDisplayMode: "auto", StatusBarStyle: "text",
			StatusBarItems: config.DefaultDesktopStatusBarItems(), CheckUpdates: true,
			UpdateChannel: "stable", ConversationWidth: "standard",
		}
	}
	return DesktopStartupSettingsView{
		Bot: botSettingsView(cfg.Bot), DesktopLanguage: cfg.DesktopLanguage(),
		DesktopLayoutStyle: cfg.DesktopLayoutStyle(), DesktopTheme: cfg.DesktopTheme(),
		DesktopThemeStyle: cfg.DesktopThemeStyle(), DesktopTerminalTheme: cfg.DesktopTerminalTheme(),
		DisplayMode: cfg.DesktopDisplayMode(), ReasoningDisplayMode: cfg.DesktopReasoningDisplayMode(),
		ReasoningDisplayModeExplicit: cfg.DesktopReasoningDisplayModeExplicit(), StatusBarStyle: cfg.DesktopStatusBarStyle(),
		StatusBarItems: cfg.DesktopStatusBarItems(), CheckUpdates: cfg.DesktopCheckUpdates(),
		UpdateChannel: cfg.DesktopUpdateChannel(), ConversationWidth: cfg.DesktopConversationWidth(),
		ConfigWarnings: cfg.LoadWarnings(), ConfigPath: config.UserConfigPath(),
	}
}

func (a *App) defaultSettingsView() SettingsView {
	defaults := config.Default()
	return SettingsView{
		Providers: []ProviderView{}, OfficialProviders: officialProviderViews(map[string]bool{}, ""),
		ProviderPresets: providerPresetViewsForRootWithResolver(nil, a.activeWorkspaceRoot(), nil),
		ProviderKinds:   nonNil(provider.Kinds()),
		Permissions:     PermissionsView{Mode: "ask", Allow: []string{}, Ask: []string{}, Deny: []string{}},
		Sandbox:         SandboxView{Bash: defaults.BashMode(), AllowWrite: []string{}, EffectiveWriteRoots: []string{}, Shell: "auto", EffectiveShell: sandboxEffectiveShellView(sandbox.ResolveShell("", "", nil))},
		Agent: AgentView{
			PlannerMaxSteps: 0, MaxSubagentDepth: agent.DefaultMaxSubagentDepth,
			MaxSubagentConcurrency: agent.DefaultMaxSubagentConcurrency, MaxParallelWriters: agent.DefaultMaxParallelWriters,
			ColdResumePrune: true, ReasoningLanguage: "auto",
			CompactRatio: defaults.Agent.CompactRatio, EffectiveCompactRatio: defaults.Agent.CompactRatio,
		},
		Bot: botSettingsView(config.BotConfig{}), AutoPlan: "off", DesktopLayoutStyle: "workbench",
		DesktopTheme: "auto", DesktopThemeStyle: "graphite", DesktopTerminalTheme: "auto",
		CloseBehavior: "background", DisplayMode: "standard", ReasoningDisplayMode: "auto",
		StatusBarStyle: "text", StatusBarItems: config.DefaultDesktopStatusBarItems(),
		DefaultToolApprovalMode: "auto", CheckUpdates: true, UpdateChannel: "stable",
		Telemetry: true, Metrics: true, ExpandThinking: false, ConversationWidth: "standard",
	}
}

// SetReasoningDisplayMode persists presentation only; no controller rebuild is needed.
func (a *App) SetReasoningDisplayMode(mode string) error {
	return a.applyConfigOnly(func(c *config.Config) error { return c.SetDesktopReasoningDisplayMode(mode) })
}
