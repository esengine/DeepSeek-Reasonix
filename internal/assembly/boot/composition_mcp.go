package boot

import (
	"encoding/json"
	"fmt"
	"time"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/state/agentpreset"
)

func parameters(raw json.RawMessage) (agentcomposition.Parameters, error) {
	return agentcomposition.ParseParameters(raw)
}

func (b *builder) prepareCompositionMCP() error {
	binding := b.opts.Composition
	if err := b.prepareCompositionModules(); err != nil {
		return err
	}
	available := b.cfg.EnabledPlugins(b.root, config.DefaultActivationStore())
	for i := range available {
		var err error
		available[i], err = b.rebaseCompositionMCP(available[i])
		if err != nil && member(binding.Resolved.MCP, available[i].Name) {
			return err
		}
	}
	for _, spec := range b.opts.ExtraPlugins {
		for _, entry := range available {
			if entry.Name == spec.Name {
				return fmt.Errorf("%w: duplicate MCP identity %s", agentcomposition.ErrInvalid, spec.Name)
			}
		}
		available = append(available, compositionMCPEntry(spec))
	}
	var selected []config.PluginEntry
	if binding.Digest != "" {
		var err error
		var snapshots []agentpreset.MCPSnapshot
		snapshots, err = resource[agentpreset.MCPSnapshot](binding, "mcp")
		if err != nil {
			return err
		}
		for _, saved := range snapshots {
			found := false
			for _, live := range available {
				if live.Name != saved.Entry.Name {
					continue
				}
				entry, err := saved.Resolve(live)
				if err != nil {
					return err
				}
				selected = append(selected, entry)
				found = true
			}
			if !found {
				return fmt.Errorf("%w: MCP %s", agentcomposition.ErrUnavailable, saved.Entry.Name)
			}
		}
	} else {
		selected = []config.PluginEntry{}
		for _, entry := range available {
			if !member(binding.Resolved.MCP, entry.Name) {
				continue
			}
			selected = append(selected, entry)
		}
		if err := verifySelected(binding.Resolved.MCP, selected, func(p config.PluginEntry) string { return p.Name }); err != nil {
			return err
		}
	}
	for i := range selected {
		for _, m := range binding.Resolved.MCP.Members {
			if m.ID != selected[i].Name {
				continue
			}
			var err error
			selected[i], err = applyCompositionMCPParameters(selected[i], m.Parameters)
			if err != nil {
				return err
			}
		}
		if binding.Digest == "" {
			copy := selected[i]
			snapshot := agentpreset.CaptureMCP(copy)
			for _, original := range b.cfg.EnabledPlugins(b.root, config.DefaultActivationStore()) {
				if original.Name == copy.Name {
					snapshot.AuthorizedCommand = original.Command
				}
			}
			if err := keepResource(binding, "mcp", copy.Name, snapshot); err != nil {
				return err
			}
		}
	}
	if binding.Resolved.MCP.Mode == agentcomposition.Inherit {
		binding.Resolved.MCP = selectionOf(selected, func(p config.PluginEntry) string { return p.Name })
	}
	b.cfg.Plugins, b.opts.ExtraPlugins = splitCompositionMCP(selected, b.opts.ExtraPlugins, b.root)
	if _, err := buildRuntimeGraph(b.roots.Home(), nil, binding); err != nil {
		return agentcomposition.Unavailable("extensions", err.Error())
	}
	return nil
}

func applyCompositionMCPParameters(entry config.PluginEntry, raw json.RawMessage) (config.PluginEntry, error) {
	p, err := parameters(raw)
	if err != nil {
		return entry, err
	}
	if p.Args != nil {
		entry.Args = *p.Args
	}
	if p.URL != nil {
		entry.URL = *p.URL
	}
	if p.Env != nil {
		entry.Env = p.Env
	}
	if p.Headers != nil {
		entry.Headers = p.Headers
	}
	if p.CallTimeoutSeconds != nil {
		entry.CallTimeoutSeconds = *p.CallTimeoutSeconds
	}
	if p.StartupTimeoutSeconds != nil {
		entry.StartupTimeoutSeconds = *p.StartupTimeoutSeconds
	}
	return entry, nil
}

func compositionMCPEntry(spec plugin.Spec) config.PluginEntry {
	return config.PluginEntry{Name: spec.Name, Type: spec.Type, Command: spec.Command, Args: spec.Args, URL: spec.URL, Env: spec.Env, Headers: spec.Headers, DisabledTools: spec.DisabledTools, CallTimeoutSeconds: int(spec.CallTimeout / time.Second), StartupTimeoutSeconds: int(spec.StartupTimeout / time.Second)}
}

func splitCompositionMCP(selected []config.PluginEntry, available []plugin.Spec, root string) ([]config.PluginEntry, []plugin.Spec) {
	configured := []config.PluginEntry{}
	extra := []plugin.Spec{}
	for _, entry := range selected {
		found := false
		for _, spec := range available {
			if spec.Name != entry.Name {
				continue
			}
			found = true
			entry = entry.ExpandedPluginForRoot(root)
			spec.Args, spec.URL, spec.Env, spec.Headers = entry.Args, entry.URL, entry.Env, entry.Headers
			spec.CallTimeout = time.Duration(entry.CallTimeoutSeconds) * time.Second
			spec.StartupTimeout = time.Duration(entry.StartupTimeoutSeconds) * time.Second
			extra = append(extra, spec)
		}
		if !found {
			configured = append(configured, entry)
		}
	}
	return configured, extra
}
