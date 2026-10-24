package skill

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
)

func (*runSkillTool) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationReadOnly}, nil
}

func (*subagentSkillTool) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationReadOnly}, nil
}
