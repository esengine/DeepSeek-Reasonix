package boot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/state/agentpreset"
)

type retainedExtension struct {
	Package         pluginpkg.InstalledPackage
	Assets          agentpreset.DirectorySnapshot
	RuntimeSnapshot *agentpreset.MCPSnapshot
}

func (b *builder) retainExtension(p pluginpkg.InstalledPackage) error {
	assets, err := agentpreset.CaptureDirectory(p.Package.Root)
	if err != nil {
		return err
	}
	files := assets.Files[:0]
	for _, f := range assets.Files {
		if f.Path == pluginpkg.NativeManifest || f.Path == pluginpkg.CodexManifest || f.Path == pluginpkg.ClaudeManifest {
			continue
		}
		if f.Path == ".mcp.json" || f.Path == "hooks/hooks.json" {
			f.Data, _, err = pluginpkg.StripCredentials(f.Data)
			if err != nil {
				return err
			}
		}
		files = append(files, f)
	}
	assets.Files = files
	var runtimeSnapshot *agentpreset.MCPSnapshot
	if p.Package.Manifest.Runtime != nil {
		rt := p.Package.Manifest.Runtime
		snapshot := agentpreset.CaptureMCP(config.PluginEntry{Name: p.Installed.Name, Command: rt.Command, Args: rt.Args, Env: rt.Env})
		runtimeSnapshot = &snapshot
	}
	p = agentcomposition.Clone(p)
	p.Installed.Source, p.Warnings = "", nil
	// Environment and header values remain owned by the live credential/config layer.
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	raw, _, err = pluginpkg.StripCredentials(raw)
	if err != nil {
		return err
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return err
	}
	stripCompositionCredentials(tree)
	raw, err = json.Marshal(tree)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if runtimeSnapshot != nil {
		p.Package.Manifest.Runtime.Args = runtimeSnapshot.Entry.Args
	}
	return keepResource(b.opts.Composition, "extension", p.Installed.Name, retainedExtension{Package: p, Assets: assets, RuntimeSnapshot: runtimeSnapshot})
}

func stripCompositionCredentials(value any) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if strings.EqualFold(key, "env") || strings.EqualFold(key, "headers") {
				delete(v, key)
			} else {
				stripCompositionCredentials(child)
			}
		}
	case []any:
		for _, child := range v {
			stripCompositionCredentials(child)
		}
	}
}

func restoreExtension(home string, live pluginpkg.InstalledPackage, binding *agentcomposition.Binding) (pluginpkg.InstalledPackage, error) {
	old, err := resource[retainedExtension](binding, "extension")
	if err != nil {
		return live, err
	}
	for _, saved := range old {
		p := saved.Package
		if p.Installed.Name != live.Installed.Name {
			continue
		}
		if p.Package.Manifest.Runtime != nil {
			if live.Package.Manifest.Runtime == nil || p.Package.Manifest.Runtime.Command != live.Package.Manifest.Runtime.Command {
				return live, fmt.Errorf("%w: extension entry authorization changed", agentcomposition.ErrUnavailable)
			}
			if saved.RuntimeSnapshot == nil {
				return live, agentcomposition.ErrUnavailable
			}
			rt := live.Package.Manifest.Runtime
			resolved, err := saved.RuntimeSnapshot.Resolve(config.PluginEntry{Name: live.Installed.Name, Command: rt.Command, Args: rt.Args, Env: rt.Env})
			if err != nil {
				return live, err
			}
			p.Package.Manifest.Runtime.Args, p.Package.Manifest.Runtime.Env = resolved.Args, resolved.Env
		}
		root, err := agentpreset.New(home, binding.Workspace).Materialize(saved.Assets)
		if err != nil {
			return live, err
		}
		if rt := p.Package.Manifest.Runtime; rt != nil {
			rt.Command = rebaseCompositionPath(rt.Command, p.Package.Root, root)
			for i, arg := range rt.Args {
				rt.Args[i] = rebaseCompositionPath(arg, p.Package.Root, root)
			}
		}
		p.Package.Root = filepath.Clean(root)
		return p, nil
	}
	return live, fmt.Errorf("%w: retained extension %s missing", agentcomposition.ErrUnavailable, live.Installed.Name)
}

func compositionPackages(home string, packages []pluginpkg.InstalledPackage, binding *agentcomposition.Binding) ([]pluginpkg.InstalledPackage, error) {
	if binding == nil {
		return packages, nil
	}
	installed, warnings := pluginpkg.LoadInstalled(home)
	if err := verifySelected(binding.Resolved.Extensions, installed, func(p pluginpkg.InstalledPackage) string { return p.Installed.Name }); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.Join(warnings, "; "))
	}
	out := []pluginpkg.InstalledPackage{}
	for _, p := range installed {
		if !member(binding.Resolved.Extensions, p.Installed.Name) {
			continue
		}
		var err error
		p, err = restoreExtension(home, p, binding)
		if err != nil {
			return nil, err
		}
		for _, m := range binding.Resolved.Extensions.Members {
			if m.ID != p.Installed.Name {
				continue
			}
			params, err := parameters(m.Parameters)
			if err != nil {
				return nil, err
			}
			if params.URL != nil || params.Headers != nil || params.CallTimeoutSeconds != nil || params.StartupTimeoutSeconds != nil {
				return nil, fmt.Errorf("%w: unsupported extension parameter", agentcomposition.ErrInvalid)
			}
			if rt := p.Package.Manifest.Runtime; rt != nil {
				copy := *rt
				copy.Required = true
				if params.Args != nil {
					copy.Args = *params.Args
				}
				if params.Env != nil {
					copy.Env = map[string]string{}
					for key, value := range params.Env {
						copy.Env[key] = os.ExpandEnv(value)
					}
				}
				if params.TimeoutMillis != nil {
					copy.TimeoutMillis = *params.TimeoutMillis
				}
				p.Package.Manifest.Runtime = &copy
			} else if len(m.Parameters) > 0 && string(m.Parameters) != "{}" {
				return nil, fmt.Errorf("%w: extension has no configurable runtime", agentcomposition.ErrInvalid)
			}
		}
		if p.Package.Manifest.Runtime != nil {
			out = append(out, p)
		}
	}
	return out, nil
}
