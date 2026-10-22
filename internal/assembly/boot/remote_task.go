package boot

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/safety/permission"
)

func remoteTaskResumer(host *plugin.Host, specs []plugin.Spec, policy permission.Policy) tool.RemoteTaskResumer {
	resume := host.RemoteTaskResumer(specs)
	return func(ctx context.Context, task tool.RemoteTask, out io.Writer) (tool.RemoteTaskResult, error) {
		name := task.Tool
		for _, spec := range specs {
			if spec.Name == task.Server && spec.StripRawPrefix != "" {
				name = strings.TrimPrefix(name, spec.StripRawPrefix)
			}
		}
		if policy.ExplicitlyDenies(plugin.ModelToolName(task.Server, name), json.RawMessage(`{}`)) {
			return tool.RemoteTaskResult{}, plugin.ErrMCPTaskRecoveryBinding
		}
		return resume(ctx, task, out)
	}
}
