// execution_identity.go — which execution a stored child belongs to, and what a
// durable run must name before it may have one.
package delegation

import (
	"fmt"
	"reasonix/internal/state/sessionstore"
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
func ResolveExecutionIdentity(meta sessionstore.SubagentMeta, opened func(execution string) bool) ExecutionIdentity {
	if id := strings.TrimSpace(meta.ExecutionID); id != "" {
		return ExecutionIdentity{Execution: id, Source: ExecutionRecorded}
	}
	parent := strings.TrimSpace(meta.ParentToolCallID)
	if parent == "" || opened == nil || !opened(parent) {
		return ExecutionIdentity{Source: ExecutionUnknown}
	}
	return ExecutionIdentity{Execution: parent, Source: ExecutionLegacyConfirmed}
}
