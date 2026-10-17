package boot

import (
	"fmt"
	"path/filepath"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/ext/command"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/pluginpkg"
)

func (b *builder) prepareCompositionModules() error {
	binding := b.opts.Composition
	installed, _ := pluginpkg.LoadInstalled(b.roots.Home())
	selected := []pluginpkg.InstalledPackage{}
	for _, pkg := range installed {
		if member(binding.Resolved.Extensions, pkg.Installed.Name) {
			selected = append(selected, pkg)
		}
	}
	if err := verifySelected(binding.Resolved.Extensions, selected, func(p pluginpkg.InstalledPackage) string { return p.Installed.Name }); err != nil {
		return err
	}
	if binding.Digest == "" {
		for _, p := range selected {
			if err := b.retainExtension(p); err != nil {
				return err
			}
		}
		if binding.Resolved.Extensions.Mode == agentcomposition.Inherit {
			binding.Resolved.Extensions = selectionOf(selected, func(p pluginpkg.InstalledPackage) string { return p.Installed.Name })
		}
	}
	if err := b.prepareCompositionCommands(); err != nil {
		return err
	}
	return b.prepareCompositionHooks(selected)
}

func (b *builder) prepareCompositionCommands() error {
	binding := b.opts.Composition
	if binding.Digest != "" {
		return nil
	}
	cmds, err := command.LoadRoots(b.cfg.Roots().CommandRootsForRoot(b.root)...)
	if err != nil {
		return err
	}
	kept := []command.Command{}
	for _, c := range cmds {
		if member(binding.Resolved.Commands, c.Name) && (c.Plugin == "" || member(binding.Resolved.Extensions, c.Plugin)) {
			kept = append(kept, c)
		}
	}
	if binding.Digest == "" {
		if err := verifySelected(binding.Resolved.Commands, kept, func(c command.Command) string { return c.Name }); err != nil {
			return err
		}
		for _, c := range kept {
			if err := keepResource(binding, "command", c.Name, c); err != nil {
				return err
			}
		}
		binding.Resolved.Commands = selectionOf(kept, func(c command.Command) string { return c.Name })
	}
	return nil
}

func (b *builder) prepareCompositionHooks(selected []pluginpkg.InstalledPackage) error {
	binding := b.opts.Composition
	if binding.Digest != "" {
		return nil
	}
	hooks := hook.Load(hook.LoadOptions{ProjectRoot: b.root, ReasonixHomeDir: b.roots.Home()})
	for i, h := range hooks {
		id := fmt.Sprintf("%s:%s:%d", h.Scope, h.Event, i)
		if !member(binding.Resolved.Hooks, id) {
			continue
		}
		if h.Scope == hook.ScopePlugin {
			allowed := false
			for _, p := range selected {
				if rel, err := filepath.Rel(p.Package.Root, h.Source); err == nil && filepath.IsLocal(rel) {
					allowed = true
				}
			}
			if !allowed {
				continue
			}
		}
		if binding.Digest == "" {
			if err := b.retainHook(id, h); err != nil {
				return err
			}
		}
	}
	if binding.Digest == "" {
		ids := []string{}
		for _, r := range binding.Resources {
			if r.Kind == "hook" {
				ids = append(ids, r.ID)
			}
		}
		if err := verifySelected(binding.Resolved.Hooks, ids, func(id string) string { return id }); err != nil {
			return err
		}
		binding.Resolved.Hooks = selectionOf(ids, func(id string) string { return id })
	}
	return nil
}
