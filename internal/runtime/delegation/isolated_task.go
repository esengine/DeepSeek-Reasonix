package delegation

import (
	"context"
	"encoding/json"
	"reasonix/internal/runtime/isolation"
)

// isolationMode is the one value task's isolation argument takes.
const isolationMode = "worktree"

// codeIsolationUnsupported refuses a mode or continuation that cannot preserve isolation.
const codeIsolationUnsupported = "isolation.unsupported_arg"

// taskIsolation is what a session offering worktree isolation hands task.
type taskIsolation struct {
	store   *isolation.Store
	root    string
	profile func(context.Context, isolation.Run, ProfileExecSpec) (isolation.Outcome, error)
}

// withIsolation adds the isolation argument to task's schema when the session
// offers it. The session decides once, at boot, so the schema stays byte-stable.
func (t *TaskTool) withIsolation(schema json.RawMessage) json.RawMessage {
	if t.isolated == nil {
		return schema
	}
	var s struct {
		Type       string                     `json:"type"`
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return schema
	}
	s.Properties["isolation"] = json.RawMessage(`{"type":"string","enum":["worktree"],"description":"Run in a git worktree with the same delegation controls, selected context and reporting. Changes stay under an iso_ id for apply_isolated or discard_isolated. A compatible continue_from resumes its original retained worktree, even when isolation is omitted; repeat the original tools, model and write_paths. Applied/discarded worktrees and fork_from cannot be resumed."}`)
	out, err := json.Marshal(s)
	if err != nil {
		return schema
	}
	return out
}
