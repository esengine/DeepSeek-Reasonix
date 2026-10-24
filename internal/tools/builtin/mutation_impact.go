package builtin

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
)

func (bash) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationWorkspace}, nil
}

func (browserOpen) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationExternal}, nil
}

func (browserAct) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationExternal}, nil
}

func (computerRead) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationReadOnly}, nil
}

func (*dispatchToProject) DescribeMutation(context.Context, json.RawMessage) (tool.MutationImpact, error) {
	return tool.MutationImpact{Kind: tool.MutationExternal}, nil
}
