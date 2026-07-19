package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/command"
	"reasonix/internal/config"
	"reasonix/internal/installsource"
	"reasonix/internal/pluginpkg"
)

type PluginView struct {
	Name                string                         `json:"name"`
	Version             string                         `json:"version,omitempty"`
	Description         string                         `json:"description,omitempty"`
	Source              string                         `json:"source,omitempty"`
	Root                string                         `json:"root"`
	ManifestKind        string                         `json:"manifestKind,omitempty"`
	Enabled             bool                           `json:"enabled"`
	Skills              int                            `json:"skills"`
	Commands            int                            `json:"commands"`
	Hooks               int                            `json:"hooks"`
	MCPServers          int                            `json:"mcpServers"`
	Agents              int                            `json:"agents,omitempty"`
	Compatibility       string                         `json:"compatibility,omitempty"`
	MappedCapabilities  []string                       `json:"mappedCapabilities,omitempty"`
	SkippedCapabilities []pluginpkg.CompatibilityIssue `json:"skippedCapabilities,omitempty"`
	SkillDetails        []PluginSkillView              `json:"skillDetails,omitempty"`
	AgentDetails        []PluginAgentView              `json:"agentDetails,omitempty"`
	CommandDetails      []PluginCommandView            `json:"commandDetails,omitempty"`
	HookDetails         []PluginHookView               `json:"hookDetails,omitempty"`
	MCPServerDetails    []PluginMCPServerView          `json:"mcpServerDetails,omitempty"`
	Warnings            []string                       `json:"warnings,omitempty"`
	Error               string                         `json:"error,omitempty"`
	Verification        *PluginVerificationView        `json:"verification,omitempty"`
}

type PluginVerificationView struct {
	CatalogEntryID  string `json:"catalogEntryId"`
	Commit          string `json:"commit"`
	PackageSHA256   string `json:"packageSha256"`
	VerifiedAt      string `json:"verifiedAt"`
	CatalogSequence uint64 `json:"catalogSequence"`
}

type PluginInstallOptions struct {
	DryRun  bool   `json:"dryRun,omitempty"`
	Link    bool   `json:"link,omitempty"`
	Replace bool   `json:"replace,omitempty"`
	Name    string `json:"name,omitempty"`
}

type PluginSkillView struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
	Invocation  string `json:"invocation,omitempty"`
	RunAs       string `json:"runAs,omitempty"`
}

type PluginAgentView struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Path         string   `json:"path,omitempty"`
	Invocation   string   `json:"invocation,omitempty"`
	Model        string   `json:"model,omitempty"`
	AllowedTools []string `json:"allowedTools,omitempty"`
}

type PluginCommandView struct {
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	ArgHint          string `json:"argHint,omitempty"`
	Path             string `json:"path,omitempty"`
	Invocation       string `json:"invocation,omitempty"`
	Shadowed         bool   `json:"shadowed,omitempty"`
	ShadowedByPlugin string `json:"shadowedByPlugin,omitempty"`
}

type PluginHookView struct {
	Event       string `json:"event"`
	Match       string `json:"match,omitempty"`
	Command     string `json:"command,omitempty"`
	ContextFile string `json:"contextFile,omitempty"`
	Description string `json:"description,omitempty"`
}

type PluginMCPServerView struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	Transport   string `json:"transport,omitempty"`
	Command     string `json:"command,omitempty"`
	URL         string `json:"url,omitempty"`
	AutoStart   bool   `json:"autoStart,omitempty"`
}

func (a *App) Plugins() []PluginView {
	st, err := pluginpkg.LoadState(config.ReasonixHomeDir())
	if err != nil {
		return []PluginView{{Error: err.Error()}}
	}
	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	var activeCommands []command.Command
	if ctrl != nil {
		activeCommands = ctrl.Commands()
	}
	out := make([]PluginView, 0, len(st.Plugins))
	for _, p := range st.Plugins {
		view := PluginView{
			Name:         p.Name,
			Version:      p.Version,
			Description:  p.Description,
			Source:       p.Source,
			Root:         pluginpkg.ResolveRoot(config.ReasonixHomeDir(), p.Root),
			ManifestKind: p.ManifestKind,
			Enabled:      p.Enabled,
		}
		if p.Verification != nil && pluginpkg.VerificationValid(config.ReasonixHomeDir(), p) {
			view.Verification = &PluginVerificationView{
				CatalogEntryID: p.Verification.CatalogEntryID, Commit: p.Verification.Commit,
				PackageSHA256: p.Verification.PackageSHA256, VerifiedAt: p.Verification.VerifiedAt.Format(time.RFC3339),
				CatalogSequence: p.Verification.CatalogSequence,
			}
		}
		if pkg, warnings, err := pluginpkg.ParseDir(view.Root); err == nil {
			applyPluginPackageDetails(&view, pkg, warnings)
			decoratePluginCommandConflicts(&view, activeCommands)
		} else {
			view.Error = err.Error()
		}
		out = append(out, view)
	}
	return out
}

func decoratePluginCommandConflicts(view *PluginView, commands []command.Command) {
	if view == nil || !view.Enabled || len(view.CommandDetails) == 0 || len(commands) == 0 {
		return
	}
	byName := make(map[string]command.Command, len(commands))
	for _, cmd := range commands {
		byName[cmd.Name] = cmd
	}
	for i := range view.CommandDetails {
		detail := &view.CommandDetails[i]
		qualified := view.Name + ":" + detail.Name
		winner, ok := byName[qualified]
		if !ok || winner.Plugin == view.Name && winner.ShortName == detail.Name && !winner.Hidden {
			continue
		}
		detail.Shadowed = true
		detail.ShadowedByPlugin = winner.Plugin
	}
}

func applyPluginPackageDetails(view *PluginView, pkg pluginpkg.Package, warnings []string) {
	view.Skills, view.Commands, view.Hooks, view.MCPServers = pkg.CapabilityCounts()
	view.Agents = pkg.AgentCount()
	view.Compatibility = pkg.Compatibility.Status
	view.MappedCapabilities = append([]string(nil), pkg.Compatibility.Mapped...)
	view.SkippedCapabilities = append([]pluginpkg.CompatibilityIssue(nil), pkg.Compatibility.Skipped...)
	view.Warnings = warnings
	inv := pkg.Inventory()
	view.CommandDetails = make([]PluginCommandView, 0, len(inv.Commands))
	for _, cmd := range inv.Commands {
		view.CommandDetails = append(view.CommandDetails, PluginCommandView{
			Name:        cmd.Name,
			Description: cmd.Description,
			ArgHint:     cmd.ArgHint,
			Path:        cmd.Path,
			Invocation:  "/" + view.Name + ":" + cmd.Name,
		})
	}
	view.SkillDetails = make([]PluginSkillView, 0, len(inv.Skills))
	for _, sk := range inv.Skills {
		view.SkillDetails = append(view.SkillDetails, PluginSkillView{
			Name:        sk.Name,
			Description: sk.Description,
			Path:        sk.Path,
			Invocation:  "/" + view.Name + ":" + sk.Name,
			RunAs:       sk.RunAs,
		})
	}
	view.AgentDetails = make([]PluginAgentView, 0, len(inv.Agents))
	for _, agent := range inv.Agents {
		view.AgentDetails = append(view.AgentDetails, PluginAgentView{
			Name: agent.Name, Description: agent.Description, Path: agent.Path,
			Invocation: "/" + view.Name + ":agent:" + agent.Name, Model: agent.Model,
			AllowedTools: append([]string(nil), agent.AllowedTools...),
		})
	}
	view.HookDetails = make([]PluginHookView, 0, len(inv.Hooks))
	for _, hook := range inv.Hooks {
		view.HookDetails = append(view.HookDetails, PluginHookView{
			Event:       hook.Event,
			Match:       hook.Match,
			Command:     hook.Command,
			ContextFile: hook.ContextFile,
			Description: hook.Description,
		})
	}
	view.MCPServerDetails = make([]PluginMCPServerView, 0, len(inv.MCPServers))
	for _, server := range inv.MCPServers {
		view.MCPServerDetails = append(view.MCPServerDetails, PluginMCPServerView{
			Name: server.Name, DisplayName: server.DisplayName, Description: server.Description,
			Transport: server.Transport, Command: server.Command, URL: server.URL, AutoStart: server.AutoStart,
		})
	}
}

func (a *App) PlanPluginInstall(source string, opts PluginInstallOptions) (string, error) {
	opts.DryRun = true
	return a.runPluginInstallSource(source, opts, false)
}

func (a *App) InstallPlugin(source string, opts PluginInstallOptions) (string, error) {
	if err := a.ensureActiveTabRebuildAllowed("plugins"); err != nil {
		return "", err
	}
	out, err := a.runPluginInstallSource(source, opts, true)
	if err != nil {
		return "", err
	}
	a.invalidateSkillRootsCache()
	if rebuildErr := a.rebuild(); rebuildErr != nil {
		if _, ok := a.deferredRebuildWarning("plugins", rebuildErr); ok {
			return out, nil
		}
		return out, rebuildErr
	}
	return out, nil
}

func (a *App) RemovePlugin(name string) error {
	if err := a.ensureActiveTabRebuildAllowed("plugins"); err != nil {
		return err
	}
	raw, _ := json.Marshal(map[string]any{"op": "uninstall", "kind": "plugin", "name": strings.TrimSpace(name), "scope": "global"})
	tl := installsource.NewTool(installsource.Options{
		ProjectRoot: a.activeWorkspaceRoot(),
		OnDisconnect: func(serverName string) bool {
			tab := a.activeTab()
			if tab == nil || tab.Ctrl == nil {
				return false
			}
			return tab.Ctrl.DisconnectMCPServer(serverName)
		},
	})
	if _, err := tl.Execute(context.Background(), raw); err != nil {
		return err
	}
	a.invalidateSkillRootsCache()
	if err := a.rebuild(); err != nil {
		if _, ok := a.deferredRebuildWarning("plugins", err); ok {
			return nil
		}
		return err
	}
	return nil
}

func (a *App) SetPluginEnabled(name string, enabled bool) error {
	if err := a.ensureActiveTabRebuildAllowed("plugins"); err != nil {
		return err
	}
	if err := pluginpkg.SetEnabled(config.ReasonixHomeDir(), strings.TrimSpace(name), enabled); err != nil {
		return err
	}
	a.invalidateSkillRootsCache()
	if err := a.rebuild(); err != nil {
		if _, ok := a.deferredRebuildWarning("plugins", err); ok {
			return nil
		}
		return err
	}
	return nil
}

func (a *App) UpdatePlugin(name string) (string, error) {
	name = strings.TrimSpace(name)
	for _, p := range a.Plugins() {
		if p.Name == name {
			if strings.TrimSpace(p.Source) == "" {
				return "", fmt.Errorf("plugin %q has no recorded source", name)
			}
			return a.InstallPlugin(p.Source, PluginInstallOptions{Name: name, Replace: true})
		}
	}
	return "", fmt.Errorf("plugin %q is not installed", name)
}

func (a *App) PluginDoctor(name string) PluginView {
	name = strings.TrimSpace(name)
	for _, p := range a.Plugins() {
		if p.Name != name {
			continue
		}
		if p.Error != "" {
			return p
		}
		if p.Root == "" {
			p.Error = "missing plugin root"
			return p
		}
		if _, err := os.Stat(p.Root); err != nil {
			p.Error = err.Error()
			return p
		}
		return p
	}
	return PluginView{Name: name, Error: "plugin is not installed"}
}

func (a *App) runPluginInstallSource(source string, opts PluginInstallOptions, apply bool) (string, error) {
	mode := "copy"
	if opts.Link {
		mode = "link"
	}
	body := map[string]any{
		"source":  strings.TrimSpace(source),
		"kind":    "plugin",
		"mode":    mode,
		"replace": opts.Replace,
		"apply":   apply && !opts.DryRun,
	}
	if strings.TrimSpace(opts.Name) != "" {
		body["name"] = strings.TrimSpace(opts.Name)
	}
	raw, _ := json.Marshal(body)
	tl := installsource.NewTool(installsource.Options{ProjectRoot: a.activeWorkspaceRoot()})
	return tl.Execute(context.Background(), raw)
}
