package boot

import (
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/ext/pluginspec"
	"reasonix/internal/state/agentpreset"
)

func (b *builder) isolateCompositionMCPPackages() error {
	if b.opts.Composition == nil {
		return nil
	}
	packages, err := resource[retainedExtension](b.opts.Composition, "extension")
	if err != nil {
		return err
	}
	b.tools.specOptions.SessionPackageRoots = map[string]pluginspec.SessionPackageRoot{}
	for _, specs := range [][]plugin.Spec{b.tools.mcp.configured, b.tools.mcp.extra} {
		for i := range specs {
			spec := &specs[i]
			if spec.Package == "" || spec.ProcessStateDir == "" || spec.Command == "" {
				continue
			}
			found := false
			for _, p := range packages {
				if p.Package.Installed.Name != spec.Package {
					continue
				}
				retained, err := agentpreset.New(b.roots.Home(), b.root).Materialize(p.Assets)
				if err != nil {
					return err
				}
				processRoot, err := plugin.CompositionProcessRoot(*spec)
				if err != nil {
					return err
				}
				previous := b.cleanup
				b.cleanup = func() {
					previous()
					_ = os.RemoveAll(processRoot)
				}
				private := filepath.Join(processRoot, "package")
				if err := os.CopyFS(private, os.DirFS(retained)); err != nil {
					return err
				}
				b.tools.specOptions.SessionPackageRoots[spec.Name] = pluginspec.SessionPackageRoot{Retained: retained, Private: private}
				pluginspec.ApplySessionPackageRoot(spec, b.tools.specOptions.SessionPackageRoots)
				found = true
				break
			}
			if !found {
				return fmt.Errorf("%w: MCP package resource missing", agentcomposition.ErrUnavailable)
			}
		}
	}
	return nil
}
