package script

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
)

func (runScript) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationReadOnly}, nil
}
