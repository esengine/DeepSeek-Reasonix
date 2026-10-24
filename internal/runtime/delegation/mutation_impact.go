package delegation

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
)

func (*TaskTool) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationReadOnly}, nil
}

func (*FleetTool) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationReadOnly}, nil
}
