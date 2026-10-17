package sidecar

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/ext/extension/protocol"
	"reasonix/internal/ext/pluginpkg"
)

// StartResolvedPackages starts the caller's authorized, session-scoped selection.
func StartResolvedPackages(ctx context.Context, packages []pluginpkg.InstalledPackage, session protocol.SessionContext, ui UIHandler) (*Manager, []string, error) {
	startup, cancel := context.WithTimeout(ctx, packageStartupBudget)
	defer cancel()
	root, err := os.MkdirTemp("", "reasonix-composition-extensions-")
	if err != nil {
		return nil, nil, err
	}
	private, err := privateCompositionPackages(root, packages)
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, nil, err
	}
	mgr, warnings, err := startLoadedPackages(startup, private, session, ui, StartClient)
	if err != nil {
		_ = os.RemoveAll(root)
		return mgr, warnings, err
	}
	mgr.mu.Lock()
	mgr.privateRoots = append(mgr.privateRoots, root)
	mgr.mu.Unlock()
	return mgr, warnings, nil
}

func privateCompositionPackages(root string, packages []pluginpkg.InstalledPackage) ([]pluginpkg.InstalledPackage, error) {
	out := make([]pluginpkg.InstalledPackage, 0, len(packages))
	for i, p := range packages {
		private := filepath.Join(root, fmt.Sprintf("%d", i))
		if err := os.CopyFS(private, os.DirFS(p.Package.Root)); err != nil {
			return nil, err
		}
		if rt := p.Package.Manifest.Runtime; rt != nil {
			copy := *rt
			copy.Args = slices.Clone(rt.Args)
			copy.Env = maps.Clone(rt.Env)
			copy.Command = privatePackagePath(copy.Command, p.Package.Root, private)
			for i, arg := range copy.Args {
				copy.Args[i] = privatePackagePath(arg, p.Package.Root, private)
			}
			for key, value := range copy.Env {
				copy.Env[key] = privatePackagePath(value, p.Package.Root, private)
			}
			p.Package.Manifest.Runtime = &copy
		}
		p.Package.Root = private
		out = append(out, p)
	}
	return out, nil
}

func privatePackagePath(value, from, to string) string {
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
