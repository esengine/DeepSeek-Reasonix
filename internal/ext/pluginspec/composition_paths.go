package pluginspec

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/ext/plugin"
)

type SessionPackageRoot struct{ Retained, Private string }

func ApplySessionPackageRoot(spec *plugin.Spec, roots map[string]SessionPackageRoot) {
	root, ok := roots[spec.Name]
	if !ok {
		return
	}
	rebase := func(value string) string {
		prefix := ""
		path := value
		if key, arg, ok := strings.Cut(value, "="); ok && strings.HasPrefix(key, "--") {
			prefix, path = key+"=", arg
		}
		if !filepath.IsAbs(path) {
			return value
		}
		rel, err := filepath.Rel(root.Retained, path)
		if err != nil || !filepath.IsLocal(rel) {
			return value
		}
		return prefix + filepath.Join(root.Private, rel)
	}
	spec.Command = rebase(spec.Command)
	spec.Dir = rebase(spec.Dir)
	spec.Args = slices.Clone(spec.Args)
	spec.Env = maps.Clone(spec.Env)
	for i, arg := range spec.Args {
		spec.Args[i] = rebase(arg)
	}
	for key, value := range spec.Env {
		spec.Env[key] = rebase(value)
	}
}
