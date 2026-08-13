package agent

import (
	"encoding/json"
	"strings"
	"sync"

	"reasonix/internal/event"
	"reasonix/internal/evidence"
)

type workspaceEffectiveCall struct {
	name     string
	args     json.RawMessage
	readOnly bool
}

var workspaceMutationSignalMu sync.Mutex

func finalizeWorkspaceMutationOutcome(out *toolOutcome, plan *toolCallPlan) {
	out.executed = plan.executed
	if plan.evidenceName != "" {
		out.effective = workspaceEffectiveCall{
			name: plan.evidenceName, args: append([]byte(nil), plan.evidenceArgs...), readOnly: plan.readOnly,
		}
	}
	if !plan.executed || isMCPLifecycleConnectTarget(plan.runTool) {
		return
	}
	if mutation, ok := workspaceMutationForCall(plan.call.ID, plan.evidenceName, plan.evidenceArgs, plan.readOnly); ok {
		out.workspaceMutation = &mutation
	}
}

// tool.before can turn nominally read-only parallel calls into writers. Keep
// the optional sink callback serial while publishing from each worker as soon
// as that concrete replacement completes.
func recordWorkspaceMutation(sink event.Sink, mutation *event.WorkspaceMutation) {
	if mutation == nil {
		return
	}
	workspaceMutationSignalMu.Lock()
	defer workspaceMutationSignalMu.Unlock()
	event.RecordWorkspaceMutation(sink, *mutation)
}

// workspaceMutationForCall classifies host resource invalidation independently
// from the delivery evidence ledger. Delivery asks whether a call invalidates a
// completed-review receipt; the desktop asks which workspace resources may have
// changed. Those contracts intentionally differ for operations such as a bare
// git commit, which changes HEAD/index/history without changing file contents.
func workspaceMutationForCall(toolID, toolName string, args json.RawMessage, readOnly bool) (event.WorkspaceMutation, bool) {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" || evidence.IsNonMutationMetaTool(toolName) || workspaceHostStateOnlyTool(toolName) {
		return event.WorkspaceMutation{}, false
	}
	effects := evidence.ClassifyToolCall(toolName, args, readOnly)
	if !effects.WorkspaceMutation {
		return event.WorkspaceMutation{}, false
	}

	paths := evidence.ToolCallPaths(args)
	mutation := event.WorkspaceMutation{
		ToolID:      toolID,
		ToolName:    toolName,
		Paths:       paths,
		AllPaths:    toolName == "bash" || len(paths) == 0,
		Content:     effects.ContentMutation,
		Tree:        effects.ContentMutation,
		WorkingTree: effects.ContentMutation,
		GitMeta:     effects.RepositoryMutation,
	}
	return mutation, true
}

func workspaceHostStateOnlyTool(toolName string) bool {
	if strings.HasPrefix(toolName, "mcp_connect__") {
		return true
	}
	switch toolName {
	case "kill_shell", "remember", "forget":
		return true
	default:
		return false
	}
}
