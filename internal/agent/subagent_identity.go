package agent

import (
	"context"
	"encoding/json"
	"sort"

	"reasonix/internal/tool"
)

func toolIdentity(reg *tool.Registry, ctx context.Context) ([]string, string) {
	if reg == nil {
		return nil, bytesHash(nil)
	}
	schemas := normalizeToolSchemas(reg.SchemasForContext(ctx))
	names := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		names = append(names, schema.Name)
	}
	sort.Strings(names)
	data, _ := json.Marshal(schemas)
	return names, bytesHash(data)
}
