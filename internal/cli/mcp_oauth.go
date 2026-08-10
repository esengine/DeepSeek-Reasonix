package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/mcpdiag"
	"reasonix/internal/netclient"
	"reasonix/internal/plugin"
)

var mcpAuthorizeForCLI = plugin.AuthorizeHTTPMCP

func mcpAuthCLI(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: reasonix mcp auth <name>")
		return 2
	}
	name, workspace := strings.TrimSpace(args[0]), mcpCLIWorkspaceRoot()
	cfg, err := config.LoadForRoot(workspace)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var entry config.PluginEntry
	found := false
	for _, configured := range cfg.Plugins {
		if configured.Name == name {
			entry, found = configured, true
			break
		}
	}
	if !found {
		fmt.Fprintf(os.Stderr, "no MCP server named %q in config\n", name)
		return 1
	}
	if !mcpdiag.CanUseHTTPMCPOAuth(entry.Type, entry.URL, mcpdiag.HasAuthConfig(entry.Headers, entry.Env, entry.URL)) {
		fmt.Fprintln(os.Stderr, "MCP OAuth is only available for Streamable HTTP MCP servers without configured authentication")
		return 1
	}
	client, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	specs := boot.PluginSpecsForRootWithOptions([]config.PluginEntry{entry}, workspace, boot.PluginSpecOptions{
		DefaultCallTimeout: 30 * time.Second, ConfigSource: string(entry.Source),
		StateHome: config.ReasonixHomeDir(), Network: true, OAuthHTTPClient: client,
	})
	if len(specs) != 1 {
		fmt.Fprintf(os.Stderr, "could not build MCP authorization specification for %q\n", name)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := mcpAuthorizeForCLI(ctx, specs[0], openMCPAuthorizationURL); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("authorized MCP server %q — run `reasonix mcp retry %s` or reconnect it in the current session\n", name, name)
	return 0
}

func openMCPAuthorizationURL(rawURL string) error {
	cmd, err := mcpOpenCommand(rawURL)
	if err != nil {
		return err
	}
	return cmd.Start()
}

var mcpProbeForInstall = probeMCPReadiness

func probeMCPReadiness(entry config.PluginEntry) (plugin.MCPInstallResult, error) {
	entry.Source = config.MCPSourceUserConfig
	workspace, err := os.Getwd()
	if err != nil {
		workspace = ""
	}
	cfg, err := config.LoadForRoot(workspace)
	if err != nil {
		return plugin.InstallResultForError(entry.Name, err), err
	}
	client, err := netclient.NewHTTPClient(cfg.NetworkProxySpec(), netclient.TransportOptions{})
	if err != nil {
		return plugin.InstallResultForError(entry.Name, err), err
	}
	specs := boot.PluginSpecsForRootWithOptions([]config.PluginEntry{entry}, workspace, boot.PluginSpecOptions{
		DefaultCallTimeout: 30 * time.Second, ConfigSource: string(config.MCPSourceUserConfig),
		StateHome: config.ReasonixHomeDir(), Network: true, OAuthHTTPClient: client,
	})
	if len(specs) != 1 {
		err := fmt.Errorf("could not build MCP launch specification")
		return plugin.InstallResultForError(entry.Name, err), err
	}
	host := plugin.NewHost()
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return host.InstallAndConnect(ctx, specs[0])
}
