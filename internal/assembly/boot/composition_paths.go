package boot

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/state/agentpreset"
)

func rebaseCompositionPath(value, from, to string) string {
	prefix := ""
	if key, path, ok := strings.Cut(value, "="); ok && strings.HasPrefix(key, "--") {
		prefix, value = key+"=", path
	}
	if !filepath.IsAbs(value) {
		return prefix + value
	}
	rel, err := filepath.Rel(from, value)
	if err != nil || !filepath.IsLocal(rel) {
		return prefix + value
	}
	return prefix + filepath.Join(to, rel)
}

func (b *builder) rebaseCompositionMCP(entry config.PluginEntry) (config.PluginEntry, error) {
	owner, owned := b.cfg.PluginPackageOwner(entry.Name)
	if !owned {
		return entry, nil
	}
	if !member(b.opts.Composition.Resolved.Extensions, owner) {
		return entry, fmt.Errorf("%w: MCP %s requires extension %s", agentcomposition.ErrUnavailable, entry.Name, owner)
	}
	installed, _ := pluginpkg.LoadInstalled(b.roots.Home())
	retained, err := resource[retainedExtension](b.opts.Composition, "extension")
	if err != nil {
		return entry, err
	}
	for _, saved := range retained {
		if saved.Package.Installed.Name != owner {
			continue
		}
		for _, live := range installed {
			if live.Installed.Name != owner {
				continue
			}
			root, err := agentpreset.New(b.roots.Home(), b.root).Materialize(saved.Assets)
			if err != nil {
				return entry, err
			}
			entry.Args = slices.Clone(entry.Args)
			entry.Env = maps.Clone(entry.Env)
			entry.Source = config.MCPSourcePluginPackage
			entry.Command = rebaseCompositionPath(entry.Command, live.Package.Root, root)
			for i, arg := range entry.Args {
				entry.Args[i] = rebaseCompositionPath(arg, live.Package.Root, root)
			}
			for key, value := range entry.Env {
				entry.Env[key] = rebaseCompositionPath(value, live.Package.Root, root)
			}
			return entry, nil
		}
	}
	return entry, fmt.Errorf("%w: MCP package resource missing", agentcomposition.ErrUnavailable)
}
