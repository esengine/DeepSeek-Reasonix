package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/acp"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/i18n"
	"reasonix/internal/netclient"
	"reasonix/internal/provider"
	"reasonix/internal/sandbox"
	"reasonix/internal/tool"
	"reasonix/internal/tool/builtin"
)

// acpCommand runs Reasonix as an Agent Client Protocol agent: a stdio JSON-RPC
// server that editors and other host clients drive (initialize, session/new,
// session/prompt, session/cancel). It keeps v2 wire-compatible with the many
// tools that integrated with v1 over ACP.
//
// stdin/stdout are the JSON-RPC channel — nothing else may write to stdout, so
// all diagnostics go to stderr. Each session is assembled by acpFactory, rooted
// at the cwd the client opens.
func acpCommand(args []string, version string) int {
	fs := flag.NewFlagSet("acp", flag.ContinueOnError)
	model := fs.String("model", "", "provider name (default: config default_model)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	factory := &acpFactory{model: *model}
	info := acp.AgentInfo{Name: "reasonix", Version: version}
	if err := acp.Serve(ctx, os.Stdin, os.Stdout, factory, info); err != nil {
		fmt.Fprintln(os.Stderr, i18n.M.ErrorPrefix, err)
		return 1
	}
	return 0
}

// acpFactory builds one control.Controller per ACP session by reusing boot.Build
// with the session cwd as WorkspaceRoot. That keeps ACP aligned with chat,
// desktop, and serve assembly while still adding the host-supplied MCP servers
// for this session only.
type acpFactory struct {
	model string
}

func (f *acpFactory) SessionDir() string {
	return config.SessionDir()
}

// NewSession assembles the per-session controller. Resources (MCP subprocesses)
// are released via the controller's Cleanup, run on ctrl.Close().
func (f *acpFactory) NewSession(ctx context.Context, p acp.SessionParams) (*control.Controller, error) {
	root := strings.TrimSpace(p.Cwd)
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	if root != "" && !filepath.IsAbs(root) {
		return nil, fmt.Errorf("session cwd must be an absolute path: %s", root)
	}
	return boot.Build(ctx, boot.Options{
		Model:         f.model,
		RequireKey:    true,
		Sink:          p.Sink,
		Stderr:        os.Stderr,
		WorkspaceRoot: root,
		ExtraPlugins:  p.MCPServers,
	})
}

func acpBuiltinTools(cfg *config.Config, cwd string, writeRoots []string) []tool.Tool {
	bashSpec := sandbox.Spec{Mode: cfg.BashMode(), WriteRoots: writeRoots, Network: cfg.Sandbox.Network}
	ws := builtin.Workspace{
		Dir:         cwd,
		WriteRoots:  writeRoots,
		Bash:        bashSpec,
		BashTimeout: time.Duration(cfg.BashTimeoutSeconds()) * time.Second,
		Search:      builtin.ResolveSearch(cfg.Tools.Search.Engine, cfg.Tools.Search.RgPath, nil),
		ProxySpec:   cfg.NetworkProxySpec(),
	}
	return ws.Tools(cfg.Tools.Enabled...)
}

func acpTaskProfileDefaults(cfg *config.Config) (string, string) {
	if cfg == nil {
		return "", ""
	}
	model := strings.TrimSpace(cfg.Agent.SubagentModels["task"])
	if model == "" {
		model = strings.TrimSpace(cfg.Agent.SubagentModel)
	}
	effort := strings.TrimSpace(cfg.Agent.SubagentEfforts["task"])
	if effort == "" {
		effort = strings.TrimSpace(cfg.Agent.SubagentEffort)
	}
	return model, effort
}

func newACPSubagentProviderResolver(cfg *config.Config, parent *config.ProviderEntry, proxySpec netclient.ProxySpec) func(string, string) (provider.Provider, *provider.Pricing, int, error) {
	return func(modelRef, effort string) (provider.Provider, *provider.Pricing, int, error) {
		modelRef = strings.TrimSpace(modelRef)
		effort = strings.TrimSpace(effort)

		var entry *config.ProviderEntry
		if modelRef != "" {
			var ok bool
			entry, ok = cfg.ResolveModel(modelRef)
			if !ok {
				return nil, nil, 0, fmt.Errorf("subagent_model %q is not a configured provider", modelRef)
			}
		} else {
			cp := *parent
			entry = &cp
		}

		if effort != "" {
			normalized, err := config.NormalizeEffort(entry, effort)
			if err != nil {
				return nil, nil, 0, err
			}
			entry.Effort = normalized
			if entry.Kind == "anthropic" && strings.TrimSpace(entry.Effort) != "" && strings.TrimSpace(entry.Thinking) == "" {
				entry.Thinking = "adaptive"
			}
		}

		prov, err := boot.NewProviderWithProxy(entry, proxySpec)
		if err != nil {
			return nil, nil, 0, err
		}
		return prov, entry.Price, entry.ContextWindow, nil
	}
}
