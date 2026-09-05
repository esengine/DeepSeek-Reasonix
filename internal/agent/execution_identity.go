// execution_identity.go — which execution a stored child belongs to, and what a
// durable run must name before it may have one.
package agent

import (
	"fmt"
	"strings"
)

// How a stored child's execution was established. The three answers are kept
// apart because only the first two may be joined on, and a reader that folded
// them would present a guess as a record.
const (
	// ExecutionRecorded is an execution the record names outright.
	ExecutionRecorded = "recorded"
	// ExecutionLegacyConfirmed is a record written before executions were named
	// separately, whose parent call the journal also opened as an execution. The
	// journal is what confirms it: a parent id alone proves only what an older
	// writer meant by lineage, not that the two were one entity.
	ExecutionLegacyConfirmed = "legacy-confirmed"
	// ExecutionUnknown is a record nothing places. It is not a defect and not a
	// loss to repair — for work the host started there was never an execution to
	// name — and inventing one would be writing history.
	ExecutionUnknown = "unknown"
)

// requireExecution refuses a durable transcript for an execution nothing named.
// Reads stay lenient about records written before this existed; a write does
// not, because such a record is one no later reader can place.
func requireExecution(spec SubagentSpec) error {
	if strings.TrimSpace(spec.ExecutionID) == "" {
		return fmt.Errorf("a persisted sub-agent run must name the execution it belongs to")
	}
	return nil
}

// requireParentSession refuses a transcript with no owning conversation.
func requireParentSession(spec SubagentSpec) error {
	if strings.TrimSpace(spec.ParentSession) == "" {
		return fmt.Errorf("subagent transcript parent session is required")
	}
	return nil
}

// ExecutionIdentity is a stored child's execution and how that was established.
// Execution is empty whenever Source is ExecutionUnknown, so a caller that joins
// on it cannot accidentally join on a guess.
type ExecutionIdentity struct {
	Execution string
	Source    string
}

// ResolveExecutionIdentity answers which execution a stored child belongs to.
// opened reports whether the journal holds an execution by that id; it is what
// confirms the one legacy shape that may be aliased, where the older writer
// demonstrably used one id for both. A parent naming no execution stays unknown:
// a single task once stored the call while the journal opened the node.
func ResolveExecutionIdentity(meta SubagentMeta, opened func(execution string) bool) ExecutionIdentity {
	if id := strings.TrimSpace(meta.ExecutionID); id != "" {
		return ExecutionIdentity{Execution: id, Source: ExecutionRecorded}
	}
	parent := strings.TrimSpace(meta.ParentToolCallID)
	if parent == "" || opened == nil || !opened(parent) {
		return ExecutionIdentity{Source: ExecutionUnknown}
	}
	return ExecutionIdentity{Execution: parent, Source: ExecutionLegacyConfirmed}
}

func validateMeta(meta SubagentMeta, spec SubagentSpec) error {
	if meta.Status == SubagentRunning {
		return fmt.Errorf("subagent reference %q is still in progress", meta.Ref)
	}
	if meta.Status == SubagentFailed {
		return fmt.Errorf("subagent reference %q failed and cannot be continued", meta.Ref)
	}
	if meta.Status == SubagentInterrupted {
		return fmt.Errorf("subagent reference %q was interrupted by a previous shutdown or crash and cannot be continued or forked; run a fresh subagent instead", meta.Ref)
	}
	want := metaFromSpec(meta.Ref, meta.Status, meta.CreatedAt, meta.UpdatedAt, spec)
	switch {
	case meta.Kind != want.Kind:
		return fmt.Errorf("subagent reference %q has kind %q, want %q", meta.Ref, meta.Kind, want.Kind)
	case meta.Name != want.Name:
		return fmt.Errorf("subagent reference %q has name %q, want %q", meta.Ref, meta.Name, want.Name)
	case meta.WorkspaceRoot != want.WorkspaceRoot:
		return fmt.Errorf("subagent reference %q belongs to workspace %q, current workspace is %q", meta.Ref, meta.WorkspaceRoot, want.WorkspaceRoot)
	case meta.SystemPromptHash != want.SystemPromptHash:
		return fmt.Errorf("subagent reference %q uses a different subagent persona; run a fresh subagent to use the current persona", meta.Ref)
	case !sameStrings(meta.ToolScope, want.ToolScope):
		return fmt.Errorf("subagent reference %q uses a different tool scope", meta.Ref)
	case meta.ToolSchemaHash != want.ToolSchemaHash:
		return fmt.Errorf("subagent reference %q uses different tool schemas", meta.Ref)
	case meta.Model != want.Model || meta.Effort != want.Effort:
		return fmt.Errorf("subagent reference %q uses model/effort %q/%q, current run would use %q/%q", meta.Ref, meta.Model, meta.Effort, want.Model, want.Effort)
	}
	return nil
}
