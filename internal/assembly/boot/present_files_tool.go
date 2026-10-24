package boot

import (
	"slices"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/builtin"
)

// addPresentFiles omits the tool and provider schema when the user disables it.
func addPresentFiles(reg *tool.Registry, cfg *config.Config, env toolEnvironment, root string) {
	if !cfg.Tools.PresentFiles || len(cfg.Tools.Enabled) > 0 && !slices.Contains(cfg.Tools.Enabled, "present_files") {
		return
	}
	reg.Add(builtin.Workspace{
		Dir: root, WriteRoots: env.writeRoots, ReadRoots: env.readRoots, ForbidReadRoots: env.forbidReadRoots,
	}.PresentFiles())
}
