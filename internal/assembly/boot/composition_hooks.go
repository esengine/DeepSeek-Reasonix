package boot

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/state/agentpreset"
)

type retainedHook struct {
	Snapshot hook.Snapshot
	Assets   agentpreset.DirectorySnapshot
	Root     string
	EnvKeys  []string
	Plugin   string
}

func (b *builder) retainHook(id string, h hook.ResolvedHook) error {
	launch, err := json.Marshal(map[string]any{"command": h.Command, "args": h.Argv})
	if err != nil {
		return err
	}
	sanitized, _, err := pluginpkg.StripCredentials(launch)
	if err != nil {
		return err
	}
	var original, redacted any
	_ = json.Unmarshal(launch, &original)
	_ = json.Unmarshal(sanitized, &redacted)
	if agentcomposition.Digest(original) != agentcomposition.Digest(redacted) {
		return agentcomposition.Unavailable(id, "Hook credentials must use environment references.")
	}
	assets, paths, err := agentpreset.CaptureHookResources(h.ResourcePaths(b.root))
	plugin := h.Env["REASONIX_PLUGIN_NAME"]
	if h.Scope == hook.ScopePlugin {
		assets, paths, err = b.compositionHookPackage(h, plugin)
	}
	if err != nil {
		return err
	}
	root, err := agentpreset.New(b.roots.Home(), b.root).Materialize(assets)
	if err != nil {
		return err
	}
	for path, rel := range paths {
		paths[path] = filepath.Join(root, rel)
	}
	h, err = h.RewriteResourcePaths(b.root, paths)
	if err != nil {
		return err
	}
	if plugin != "" {
		from := h.Env["REASONIX_PLUGIN_ROOT"]
		h.Cwd = rebaseCompositionPath(h.Cwd, from, root)
		for key, value := range h.Env {
			h.Env[key] = rebaseCompositionPath(value, from, root)
		}
	}
	saved := retainedHook{Snapshot: h.Snapshot(), Assets: assets, Root: root, Plugin: plugin}
	saved.Snapshot.Hook.Env, saved.EnvKeys = agentpreset.CaptureEnvironment(h.Env)
	return keepResource(b.opts.Composition, "hook", id, saved)
}

func (b *builder) compositionHookPackage(h hook.ResolvedHook, name string) (agentpreset.DirectorySnapshot, map[string]string, error) {
	packages, err := resource[retainedExtension](b.opts.Composition, "extension")
	if err != nil {
		return agentpreset.DirectorySnapshot{}, nil, err
	}
	for _, p := range packages {
		if p.Package.Installed.Name != name {
			continue
		}
		paths := map[string]string{}
		for _, file := range p.Assets.Files {
			paths[filepath.Join(h.Env["REASONIX_PLUGIN_ROOT"], filepath.FromSlash(file.Path))] = filepath.FromSlash(file.Path)
		}
		return p.Assets, paths, nil
	}
	return agentpreset.DirectorySnapshot{}, nil, fmt.Errorf("%w: hook package missing", agentcomposition.ErrUnavailable)
}

func compositionHookSet(opts Options, home, workspace string) ([]hook.ResolvedHook, error) {
	store := agentpreset.New(home, workspace)
	live := hook.Load(hook.LoadOptions{ProjectRoot: workspace, ReasonixHomeDir: home})
	byID := map[string]hook.ResolvedHook{}
	for i, h := range live {
		byID[fmt.Sprintf("%s:%s:%d", h.Scope, h.Event, i)] = h
	}
	out := []hook.ResolvedHook{}
	for _, r := range opts.Composition.Resources {
		if r.Kind != "hook" {
			continue
		}
		var saved retainedHook
		if err := jsonResource(r, &saved); err != nil {
			return nil, err
		}
		h, err := restoreCompositionHook(store, saved, workspace, byID[r.ID])
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}

func restoreCompositionHook(store *agentpreset.Store, saved retainedHook, workspace string, live hook.ResolvedHook) (hook.ResolvedHook, error) {
	check := func() error {
		if _, err := store.Materialize(saved.Assets); err != nil {
			return err
		}
		if p := saved.Snapshot.Approval; p != nil {
			ok, err := config.NewProjectProgramStore(store.Home).Approved(workspace, *p)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%w: hook approval revoked", agentcomposition.ErrUnavailable)
			}
		}
		if saved.Plugin != "" {
			installed, _ := pluginpkg.LoadInstalled(store.Home)
			found := false
			for _, p := range installed {
				if p.Installed.Name == saved.Plugin {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("%w: hook package disabled", agentcomposition.ErrUnavailable)
			}
		}
		return nil
	}
	if err := check(); err != nil {
		return hook.ResolvedHook{}, err
	}
	root, err := store.Materialize(saved.Assets)
	if err != nil {
		return hook.ResolvedHook{}, err
	}
	paths := map[string]string{}
	for _, f := range saved.Assets.Files {
		paths[filepath.Join(saved.Root, filepath.FromSlash(f.Path))] = filepath.Join(root, filepath.FromSlash(f.Path))
	}
	h, err := saved.Snapshot.Restore().RewriteResourcePaths(workspace, paths)
	if err != nil {
		return h, err
	}
	if len(saved.EnvKeys) > 0 && h.Source != live.Source {
		return h, agentcomposition.ErrUnavailable
	}
	h.Env, err = agentpreset.ResolveEnvironment(h.Env, saved.EnvKeys, live.Env)
	if err != nil {
		return h, err
	}
	h.Cwd = rebaseCompositionPath(h.Cwd, saved.Root, root)
	for key, value := range h.Env {
		h.Env[key] = rebaseCompositionPath(value, saved.Root, root)
	}
	return h.WithRetainedCheck(check), nil
}
