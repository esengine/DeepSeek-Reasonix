package cli

import (
	"context"
	"io"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/ablation"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surface"
	"reasonix/internal/platform/browser"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessiontemp"
)

type cliBuildOverrides struct {
	// Version marks a top-level session of a real installation, which is what
	// lets the assembly record a last-known-good config snapshot. Nested and
	// rebuild paths leave it empty; the snapshot is recorded once per process.
	Version              string
	Effort               *string
	PermissionAllow      []string
	AdditionalDirs       []string
	WorkspaceRoot        string
	HeadlessApprovalMode string
	GoalTurnsUnreachable bool
	Stderr               io.Writer
	OnSessionRecovered   func(control.SessionRecoveryInfo) error
	Ablation             ablation.Set
	// SessionTemp carries the previous Controller's private temporary directory
	// manager across model/profile rebuilds so temporary files survive.
	SessionTemp *sessiontemp.Manager
	// BrowserSession carries the previous Controller's browser across the same
	// rebuilds, so its tabs stay open.
	BrowserSession *browser.Session
	// ProviderResolver routes model roles through a caller-owned catalog. A
	// bootstrapped serve sets the broker's; nil keeps the local config path.
	ProviderResolver provider.Resolver
}

func setupProfileWithOverrides(ctx context.Context, modelName string, maxStepsOverride int, requireKey bool, sink event.Sink, profile string, overrides cliBuildOverrides) (*control.Controller, error) {
	migrateMCPConfigForCLIWorkspace()
	return boot.Build(ctx, cliProfileBuildOptions(modelName, maxStepsOverride, requireKey, sink, profile, overrides))
}

func cliProfileBuildOptions(modelName string, maxStepsOverride int, requireKey bool, sink event.Sink, profile string, overrides cliBuildOverrides) boot.Options {
	// profile is dual-write TokenMode; also set AgentPreset for the new path.
	return boot.Options{
		Version:              overrides.Version,
		Model:                modelName,
		MaxSteps:             maxStepsOverride,
		MaxStepsKey:          "--max-steps",
		RequireKey:           requireKey,
		Sink:                 sink,
		AgentPreset:          boot.NormalizeAgentPreset(profile),
		TokenMode:            boot.NormalizeTokenMode(profile),
		SessionDir:           resolveCLISessionDirFor(overrides.WorkspaceRoot),
		WorkspaceRoot:        overrides.WorkspaceRoot,
		EffortOverride:       overrides.Effort,
		PermissionAllow:      overrides.PermissionAllow,
		AdditionalDirs:       overrides.AdditionalDirs,
		HeadlessApprovalMode: overrides.HeadlessApprovalMode,
		GoalTurnsUnreachable: overrides.GoalTurnsUnreachable,
		StatsSource:          surface.CLI,
		Stderr:               overrides.Stderr,
		OnSessionRecovered:   overrides.OnSessionRecovered,
		Ablation:             overrides.Ablation,
		SessionTemp:          overrides.SessionTemp,
		BrowserSession:       overrides.BrowserSession,
		ProviderResolver:     overrides.ProviderResolver,
	}
}

// runBuildOverrides assembles the headless `run` build. It executes one task and
// exits, so nothing in this path can reach SetGoal and goal-only tools are left
// out of the provider schema.
func runBuildOverrides(effort *string, allow, dirs []string, workspaceRoot, approval string,
	onRecovered func(control.SessionRecoveryInfo) error, ablated ablation.Set) cliBuildOverrides {
	return cliBuildOverrides{
		Effort:               effort,
		PermissionAllow:      allow,
		AdditionalDirs:       dirs,
		WorkspaceRoot:        workspaceRoot,
		HeadlessApprovalMode: approval,
		GoalTurnsUnreachable: true,
		OnSessionRecovered:   onRecovered,
		Ablation:             ablated,
	}
}
