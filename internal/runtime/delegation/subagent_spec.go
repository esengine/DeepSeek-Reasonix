package delegation

import (
	"context"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

// SubagentSpec describes the current invocation identity.
type SubagentSpec struct {
	BudgetScope      string
	IsolationRef     string
	IsolationGrant   string
	Kind             string
	Name             string
	WorkspaceRoot    string
	ParentSession    string
	ParentToolCallID string
	SystemPrompt     string
	ExecutionID      string
	Registry         *tool.Registry
	ToolContext      context.Context
	Model            string
	Effort           string
	ResumedFrom      string
	UpstreamFrom     []string
	ResultRefs       []sessionstore.ContextResultRef
}
