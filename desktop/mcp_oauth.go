package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/mcpdiag"
	"reasonix/internal/mcplaunch"
	"reasonix/internal/netclient"
	"reasonix/internal/plugin"
)

func (a *App) mcpLaunchSpec(root, name string) (plugin.Spec, error) {
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return plugin.Spec{}, err
	}
	for _, entry := range cfg.Plugins {
		if entry.Name == name {
			return a.mcpLaunchSpecForEntryWithConfig(root, entry, cfg)
		}
	}
	return plugin.Spec{}, fmt.Errorf("no configured MCP server named %q", name)
}

func (a *App) mcpLaunchSpecForEntry(root string, entry config.PluginEntry) (plugin.Spec, error) {
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return plugin.Spec{}, err
	}
	return a.mcpLaunchSpecForEntryWithConfig(root, entry, cfg)
}

func (a *App) mcpLaunchSpecForEntryWithConfig(root string, entry config.PluginEntry, cfg *config.Config) (plugin.Spec, error) {
	oauthHTTPClient, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{})
	if err != nil {
		return plugin.Spec{}, err
	}
	specs := boot.PluginSpecsForRootWithOptions([]config.PluginEntry{entry}, root, boot.PluginSpecOptions{
		DefaultCallTimeout: time.Duration(cfg.MCPCallTimeoutSeconds()) * time.Second,
		LaunchManager:      mcplaunch.ForWorkspace(config.ReasonixHomeDir(), root),
		ConfigSource:       "workspace_config", StateHome: config.ReasonixHomeDir(),
		WriterRoots: cfg.WriteRootsForRoot(root), ForbidReadRoots: boot.RuntimeForbidReadRoots(cfg, root),
		Network: cfg.Sandbox.Network, OAuthHTTPClient: oauthHTTPClient,
	})
	if len(specs) != 1 {
		return plugin.Spec{}, fmt.Errorf("failed to build MCP server %q", entry.Name)
	}
	return specs[0], nil
}

var (
	desktopAuthorizeHTTPMCP        = plugin.AuthorizeHTTPMCP
	desktopOpenMCPAuthorizationURL = func(a *App, rawURL string) error {
		if a == nil || a.ctx == nil {
			return fmt.Errorf("desktop runtime is not ready to open the authorization page")
		}
		runtime.BrowserOpenURL(a.ctx, rawURL)
		return nil
	}
)

// AuthenticateMCPServer authorizes a remote MCP in private Reasonix state and
// reconnects every controller sharing the active host.
func (a *App) AuthenticateMCPServer(name string) error {
	_, ctrl, root := a.activeMCPRuntime()
	if ctrl == nil {
		return fmt.Errorf("no active session")
	}
	entry, found, err := desktopEffectiveMCPServer(root, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	if !mcpdiag.CanUseHTTPMCPOAuth(entry.Type, entry.URL, mcpdiag.HasAuthConfig(entry.Headers, entry.Env, entry.URL)) {
		return fmt.Errorf("MCP OAuth is only available for Streamable HTTP MCP servers without configured authentication")
	}
	spec, err := a.mcpLaunchSpecForEntry(root, entry)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if a.ctx != nil {
		ctx = a.ctx
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := desktopAuthorizeHTTPMCP(ctx, spec, func(rawURL string) error {
		return desktopOpenMCPAuthorizationURL(a, rawURL)
	}); err != nil {
		return err
	}
	return a.ReconnectMCPServer(name)
}

// ClearMCPServerAuthentication removes Reasonix-owned auth state without
// signing out the third-party browser session or removing the server.
func (a *App) ClearMCPServerAuthentication(name string) error {
	defer a.lockMCPMutation("clear-auth")()
	tab, ctrl, root := a.activeMCPRuntime()
	if tab == nil || ctrl == nil {
		return fmt.Errorf("no active session")
	}
	host, releaseGates, err := a.lockMCPHostTurnGates("MCP server", ctrl)
	if err != nil {
		return err
	}
	defer releaseGates()
	controllers := a.mcpControllersSharingHost(host, name, ctrl)
	if err := ensureMCPServerDirectlyWritable(root, name); err != nil {
		return err
	}
	entry, found, err := desktopEffectiveMCPServer(root, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no configured MCP server named %q", name)
	}
	specs := boot.PluginSpecsForRootWithOptions([]config.PluginEntry{entry}, root, boot.PluginSpecOptions{
		DefaultCallTimeout: 30 * time.Second, ConfigSource: string(entry.Source),
		StateHome: config.ReasonixHomeDir(), Network: true,
	})
	if len(specs) == 1 {
		if _, err := plugin.ClearHTTPMCPOAuth(specs[0]); err != nil {
			return err
		}
	}
	if _, _, _, err := config.ClearPluginAuthenticationInSourceForRoot(root, name); err != nil {
		return err
	}
	disconnectMCPServerControllers(name, ctrl, controllers)
	if host != nil {
		host.ClearFailure(name)
	}
	// Auth state is authoritative configuration for in-flight controller builds.
	a.bumpExtensionGeneration()
	return nil
}
