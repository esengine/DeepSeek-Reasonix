package boot

import (
	"slices"

	"reasonix/internal/base/netclient"
	"reasonix/internal/contract/tool"
	"reasonix/internal/tools/builtin"
)

func mediaWorkspace(root string, env toolEnvironment, proxy netclient.ProxySpec, receipt func(string, bool, []byte)) builtin.Workspace {
	return builtin.Workspace{Dir: root, WriteRoots: env.writeRoots, ForbidReadRoots: env.forbidReadRoots, ReadRoots: env.readRoots,
		ReadPaths: env.readPaths, Bash: env.bash, SessionGuard: env.sessionGuard, ProxySpec: proxy, SessionTemp: env.sessionTemp, FileWriteReceipt: receipt}
}

// addMediaTools attaches the media-production tools whose inputs and programs
// this session has: download_artifact only reads retained MCP results, and
// check_video only runs where ffmpeg and ffprobe are installed.
func addMediaTools(reg *tool.Registry, enabled []string, ws builtin.Workspace, plan mcpSpecPlan, look func(string) (string, error)) {
	if len(plan.configured)+len(plan.extra) > 0 && toolWanted(enabled, builtin.ArtifactDownloadName) {
		reg.Add(ws.ArtifactDownload())
	}
	if toolWanted(enabled, builtin.CheckVideoName) && installed(look, "ffmpeg", "ffprobe") {
		reg.Add(ws.CheckVideo())
	}
}

func installed(look func(string) (string, error), programs ...string) bool {
	for _, p := range programs {
		if _, err := look(p); err != nil {
			return false
		}
	}
	return true
}

func toolWanted(enabled []string, name string) bool {
	return len(enabled) == 0 || slices.Contains(enabled, name)
}
