package boot

import (
	"context"
	"io"
	"net/http"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/pluginspec"
)

// addInstallSourceTool registers install_source, which installs an MCP server
// from a source the model names. Connecting one is a durable grant: the launch
// approval is recorded on apply so neither this connection nor the next session
// asks again, and a failed install revokes it rather than retaining consent for
// a server that never connected.
func addInstallSourceTool(ctx context.Context, reg *tool.Registry, host *plugin.Host, root string,
	httpClient *http.Client, specOptions pluginspec.Options, stderr io.Writer) {
	reg.Add(installsource.NewTool(installsource.Options{
		ProjectRoot: root,
		HTTPClient:  httpClient,
		// The model's apply must present back a plan someone could read.
		// Hosts skip this: the click that reached them already was the user.
		RequireApprovedPlan: true,
		ConnectMCP: func(e config.PluginEntry) (installsource.MCPConnectResult, error) {
			spec := pluginspec.FromEntry(e, root, specOptions)
			if stderr != nil {
				spec.Stderr = stderr
			}
			// Project-scoped installs retain project provenance, but the exact
			// launch grant is recorded now: applying a plan is already the
			// explicit user decision that grant stands for.
			launchAuthorized := false
			if spec.RequireLaunchApproval {
				if err := plugin.AuthorizeSpecLaunch(ctx, spec); err != nil {
					return installsource.MCPConnectResult{}, err
				}
				launchAuthorized = true
			}
			tools, err := host.Add(ctx, spec)
			if err != nil {
				// The install did not complete, so do not retain consent for a
				// server that never connected. Replacement rollback reauthorizes
				// the previous project entry before reconnecting it.
				if launchAuthorized && spec.LaunchManager != nil {
					_ = spec.LaunchManager.Revoke(spec.Name)
				}
				return installsource.MCPConnectResult{}, err
			}
			reg.RemovePrefix(plugin.ToolPrefix(spec.Name))
			for _, t := range tools {
				reg.Add(t)
			}
			// Disconnect closes the server and drops its namespaced tools.
			// Used by the install_source rollback path when SaveTo fails.
			disconnect := func() {
				if prefix, ok := host.Remove(spec.Name); ok {
					reg.RemovePrefix(prefix)
				}
				if spec.LaunchManager != nil {
					_ = spec.LaunchManager.Revoke(spec.Name)
				}
			}
			return installsource.MCPConnectResult{
				ToolCount:  len(tools),
				Disconnect: disconnect,
			}, nil
		},
		OnDisconnect: func(serverName string) bool {
			if prefix, ok := host.Remove(serverName); ok {
				reg.RemovePrefix(prefix)
				return true
			}
			return false
		},
	}))
}
