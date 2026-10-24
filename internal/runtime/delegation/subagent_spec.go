package delegation

import (
	"context"

	"reasonix/internal/contract/tool"
)

// SubagentSpec describes the current invocation identity.
type SubagentSpec struct {
	Kind             string
	Name             string
	WorkspaceRoot    string
	ParentSession    string
	ParentToolCallID string
	SystemPrompt     string
	// ExecutionID is the execution this run belongs to, allocated before the
	// work was opened. A store ref cannot stand in for it: work the scheduler
	// is holding back has an execution and no transcript yet.
	ExecutionID           string
	EnvironmentID         string
	EnvironmentGeneration string
	OriginWorkspace       string
	Registry              *tool.Registry
	ToolContext           context.Context
	Model                 string
	Effort                string
	// ResumedFrom and UpstreamFrom feed the context capsule only.
	ResumedFrom        string
	UpstreamFrom       []string
	continuationSource *EnvironmentSource
}
