// Package pluginspec maps configured plugin entries onto the plugin.Spec the
// runtime starts servers from. It is the one place that translation lives, so
// a caller that only needs to describe configured MCP servers — a diagnostic,
// an OAuth probe — does not have to reach up into the boot assembly for it.
package pluginspec

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/mcplaunch"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/safety/sandbox"
)

// Options carries host runtime policy into plugin specifications.
type Options struct {
	DefaultStartupTimeout time.Duration
	DefaultCallTimeout    time.Duration
	LaunchManager         *mcplaunch.Manager
	ConfigSource          string
	StateHome             string
	WriterRoots           []string
	ForbidReadRoots       []string
	Network               bool
	PackageOwners         map[string]string
	OAuthHTTPClient       *http.Client
}

// ForRoot maps entries against a workspace root without runtime policy.
func ForRoot(entries []config.PluginEntry, workspaceRoot string) []plugin.Spec {
	return ForRootWithOptions(entries, workspaceRoot, Options{})
}

// ForRootWithOptions maps configured plugin entries to plugin.Spec and injects
// runtime policy such as the global MCP call timeout.
func ForRootWithOptions(entries []config.PluginEntry, workspaceRoot string, opts Options) []plugin.Spec {
	specs := make([]plugin.Spec, len(entries))
	for i, e := range entries {
		specs[i] = FromEntry(e, workspaceRoot, opts)
	}
	return specs
}

// FromEntry maps one configured entry, including MCP isolation.
func FromEntry(e config.PluginEntry, workspaceRoot string, opts Options) plugin.Spec {
	e = e.ExpandedPluginForRoot(workspaceRoot)
	configSource := strings.TrimSpace(string(e.Source))
	if configSource == "" {
		configSource = opts.ConfigSource
	}
	spec := plugin.ApplyKnownOverrides(plugin.Spec{
		Name:                          e.Name,
		Package:                       strings.TrimSpace(opts.PackageOwners[e.Name]),
		Type:                          e.Type,
		Command:                       e.Command,
		Args:                          e.Args,
		Env:                           e.Env,
		URL:                           e.URL,
		Headers:                       e.Headers,
		DefaultStartupTimeout:         opts.DefaultStartupTimeout,
		StartupTimeout:                secondsDuration(e.StartupTimeoutSeconds),
		DefaultCallTimeout:            opts.DefaultCallTimeout,
		CallTimeout:                   secondsDuration(e.CallTimeoutSeconds),
		ToolTimeouts:                  toolTimeoutDurations(e.ToolTimeoutSeconds),
		WorkspaceRoot:                 strings.TrimSpace(workspaceRoot),
		LaunchManager:                 opts.LaunchManager,
		ConfigSource:                  configSource,
		Authorized:                    e.Source.UserAuthorized(),
		OAuthHTTPClient:               opts.OAuthHTTPClient,
		OAuthAllowMissingPKCEMetadata: e.OAuthAllowMissingPKCEMetadata && userOwnedSource(e.Source),
	}, workspaceRoot)
	if e.Source.ProjectScoped() && strings.TrimSpace(spec.Dir) == "" {
		spec.Dir = workspaceRoot
	}
	ApplyIsolation(&spec, workspaceRoot, opts)
	return spec
}

// ApplyIsolation sets the process mode, private state dir, and — in confined
// mode — the OS sandbox for a spec assembled outside FromEntry.
func ApplyIsolation(spec *plugin.Spec, workspaceRoot string, opts Options) {
	if spec == nil {
		return
	}
	// Authorized user MCP defaults to trusted host process mode. Confined mode
	// is opt-in for internal managed deployments/tests and is never selected by
	// ordinary install paths.
	if spec.ProcessMode == "" {
		spec.ProcessMode = plugin.MCPProcessHost
	}
	if strings.TrimSpace(opts.StateHome) == "" {
		return
	}
	stateDir := plugin.MCPStateDir(opts.StateHome, workspaceRoot, spec.Name)
	spec.StateDir = stateDir
	if spec.ResolvedProcessMode() != plugin.MCPProcessConfined {
		// Host mode still gets a private state/cache/temp tree; only the OS
		// command sandbox is omitted so local app integrations keep working.
		return
	}
	writerRoots := appendUniquePaths([]string{stateDir}, opts.WriterRoots...)
	readerRoots := []string{workspaceRoot}
	if home, err := os.UserHomeDir(); err == nil {
		readerRoots = appendUniquePaths(readerRoots, home)
	}
	spec.Sandbox = sandbox.Spec{
		Mode: "enforce", WriteRoots: writerRoots,
		ReadRoots:              readerRoots,
		AppContainerWriteRoots: append([]string(nil), writerRoots...),
		ForbidReadRoots:        append([]string(nil), opts.ForbidReadRoots...),
		Network:                opts.Network, MinimalWrites: true,
	}
}

// ApplyKnownOverrides re-applies the known-server overrides to every spec.
func ApplyKnownOverrides(specs []plugin.Spec, workspaceRoot string) []plugin.Spec {
	out := make([]plugin.Spec, len(specs))
	for i, spec := range specs {
		out[i] = plugin.ApplyKnownOverrides(spec, workspaceRoot)
	}
	return out
}

// ApplyDefaultCallTimeout fills the per-call timeout only where none was set.
func ApplyDefaultCallTimeout(specs []plugin.Spec, timeout time.Duration) []plugin.Spec {
	if len(specs) == 0 || timeout <= 0 {
		return specs
	}
	out := make([]plugin.Spec, len(specs))
	for i, spec := range specs {
		out[i] = spec
		if out[i].DefaultCallTimeout <= 0 {
			out[i].DefaultCallTimeout = timeout
		}
	}
	return out
}

// ApplyDefaultStartupTimeout fills the startup timeout only where none was set.
func ApplyDefaultStartupTimeout(specs []plugin.Spec, timeout time.Duration) []plugin.Spec {
	if len(specs) == 0 || timeout <= 0 {
		return specs
	}
	out := make([]plugin.Spec, len(specs))
	for i, spec := range specs {
		out[i] = spec
		if out[i].DefaultStartupTimeout <= 0 {
			out[i].DefaultStartupTimeout = timeout
		}
	}
	return out
}

// PackageOwners maps configured server names to the package that installed them.
func PackageOwners(cfg *config.Config) map[string]string {
	out := map[string]string{}
	if cfg == nil {
		return out
	}
	for _, configured := range cfg.Plugins {
		if owner, ok := cfg.PluginPackageOwner(configured.Name); ok {
			out[configured.Name] = owner
		}
	}
	return out
}

func secondsDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func toolTimeoutDurations(seconds map[string]int) map[string]time.Duration {
	if len(seconds) == 0 {
		return nil
	}
	out := make(map[string]time.Duration, len(seconds))
	for name, sec := range seconds {
		name = strings.TrimSpace(name)
		if name == "" || sec <= 0 {
			continue
		}
		out[name] = time.Duration(sec) * time.Second
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func appendUniquePaths(base []string, extra ...string) []string {
	out := append([]string(nil), base...)
	seen := make(map[string]struct{}, len(out)+len(extra))
	for _, path := range out {
		seen[pathComparisonKey(path)] = struct{}{}
	}
	for _, path := range extra {
		path = filepath.Clean(path)
		key := pathComparisonKey(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, path)
	}
	return out
}

func pathComparisonKey(path string) string {
	path = filepath.Clean(path)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// userOwnedSource is a config the user keeps in their own home. A project's
// config, its .mcp.json and an installed package are someone else's, and none
// of them may relax an OAuth check for whoever opens or installs them.
func userOwnedSource(s config.MCPConfigSource) bool {
	switch s {
	case config.MCPSourceUserConfig, config.MCPSourceLegacyUser, config.MCPSourceClaudeUser, config.MCPSourceClaudeLocal:
		return true
	default:
		return false
	}
}
