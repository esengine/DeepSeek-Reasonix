package delegation

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/agent"
	"slices"

	"reasonix/internal/contract/tool"
)

func toolIdentity(reg *tool.Registry, ctx context.Context) ([]string, string) {
	if reg == nil {
		return nil, bytesHash(nil)
	}
	schemas := agent.NormalizeToolSchemas(reg.SchemasForContext(ctx))
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		names = append(names, schema.Name)
	}
	slices.Sort(names)
	data, _ := json.Marshal(schemas)
	return names, bytesHash(data)
}
