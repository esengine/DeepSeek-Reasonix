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
		Model:                    firstNonEmpty(p.Model, f.model),
		RequireKey:               true,
		Sink:                     p.Sink,
		EffortOverride:           p.EffortOverride,
		Stderr:                   os.Stderr,
		WorkspaceRoot:            root,
		ExtraPlugins:             p.MCPServers,
		CleanupPendingReconciler: acp.ReconcileCleanupPending,
	})
}

func (f *acpFactory) SessionConfigState(_ context.Context, p acp.SessionConfigStateParams) (acp.SessionConfigState, error) {
	root := strings.TrimSpace(p.Cwd)
	if root == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	}
	if root != "" && !filepath.IsAbs(root) {
		return acp.SessionConfigState{}, fmt.Errorf("session cwd must be an absolute path: %s", root)
	}
	_, _ = config.MigrateLegacyIfNeeded()
	_, _ = config.MigrateMCPToUserConfigOnUpgrade([]string{root})
	cfg, err := config.LoadForRoot(root)
	if err != nil {
		return acp.SessionConfigState{}, err
	}

	ref := firstNonEmpty(p.Model, f.model, cfg.DefaultModel)
	if strings.TrimSpace(ref) == "" {
		return acp.SessionConfigState{}, fmt.Errorf("no default_model configured")
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return acp.SessionConfigState{}, fmt.Errorf("unknown model %q", ref)
	}
	if !entry.Configured() {
		return acp.SessionConfigState{}, fmt.Errorf("model %q is not configured", ref)
	}
	currentModel := entry.Name + "/" + entry.Model
	modelOptions, modelInfos := acpModelOptions(cfg)
	if !hasModelOption(modelOptions, currentModel) {
		modelOptions = append(modelOptions, acp.SessionConfigSelectOption{
			Value:       currentModel,
			Name:        currentModel,
			Description: entry.Name,
		})
		modelInfos = append(modelInfos, acp.ModelInfo{
			ModelID:     currentModel,
			Name:        currentModel,
			Description: entry.Name,
		})
	}

	effortEntry := *entry
	effortOverride := cloneStringPtr(p.EffortOverride)
	hadEffortOverride := effortOverride != nil
	if effortOverride != nil {
		if strings.TrimSpace(*effortOverride) == "" {
			effortEntry.Effort = ""
		} else {
			normalized, err := config.NormalizeEffort(&effortEntry, *effortOverride)
			if err != nil {
				effortEntry.Effort = ""
				cleared := ""
				effortOverride = &cleared
			} else {
				effortEntry.Effort = normalized
				effortOverride = &normalized
			}
		}
	}

	options := []acp.SessionConfigOption{{
		ID:           "model",
		Name:         "Model",
		Category:     "model",
		Type:         "select",
		CurrentValue: currentModel,
		Options:      modelOptions,
	}}
	if cap := config.EffortCapabilityForEntry(&effortEntry); cap.Supported {
		currentEffort := config.EffortDisplay(&effortEntry)
		if !containsString(cap.Levels, currentEffort) {
			currentEffort = "auto"
			auto := ""
			effortOverride = &auto
		}
		options = append(options, acp.SessionConfigOption{
			ID:           "effort",
			Name:         "Effort",
			Category:     "thought_level",
			Type:         "select",
			CurrentValue: currentEffort,
			Options:      acpEffortOptions(cap.Levels),
		})
	} else if hadEffortOverride {
		cleared := ""
		effortOverride = &cleared
	}

	return acp.SessionConfigState{
		Model:          currentModel,
		EffortOverride: effortOverride,
		Models: &acp.SessionModelState{
			AvailableModels: modelInfos,
			CurrentModelID:  currentModel,
		},
		ConfigOptions: options,
	}, nil
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

func acpModelOptions(cfg *config.Config) ([]acp.SessionConfigSelectOption, []acp.ModelInfo) {
	if cfg == nil {
		return nil, nil
	}
	var options []acp.SessionConfigSelectOption
	var models []acp.ModelInfo
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		for _, model := range p.ChatModelList() {
			ref := p.Name + "/" + model
			options = append(options, acp.SessionConfigSelectOption{
				Value:       ref,
				Name:        ref,
				Description: p.Name,
			})
			models = append(models, acp.ModelInfo{
				ModelID:     ref,
				Name:        ref,
				Description: p.Name,
			})
		}
	}
	return options, models
}

func hasModelOption(options []acp.SessionConfigSelectOption, ref string) bool {
	for _, opt := range options {
		if opt.Value == ref {
			return true
		}
	}
	return false
}

func acpEffortOptions(levels []string) []acp.SessionConfigSelectOption {
	out := make([]acp.SessionConfigSelectOption, 0, len(levels))
	for _, level := range levels {
		out = append(out, acp.SessionConfigSelectOption{Value: level, Name: effortOptionName(level)})
	}
	return out
}

func effortOptionName(level string) string {
	if level == "" {
		return ""
	}
	if level == "xhigh" {
		return "XHigh"
	}
	return strings.ToUpper(level[:1]) + level[1:]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cloneStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
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
