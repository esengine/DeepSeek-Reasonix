package delegation

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/isolation"
)

// isolationMode is the one value task's isolation argument takes.
const isolationMode = "worktree"

// codeIsolationUnsupported refuses an argument an isolated run cannot honour:
// the child is a whole kernel of its own, not a sub-agent of this one.
const codeIsolationUnsupported = "isolation.unsupported_arg"

// taskIsolation is what a session offering worktree isolation hands task.
type taskIsolation struct {
	store  *isolation.Store
	root   string
	runner isolation.Runner
}

// SetIsolation offers isolation "worktree" on task: the run happens in a git
// worktree of root and its changes wait in store for apply_isolated.
func (t *TaskTool) SetIsolation(store *isolation.Store, root string, runner isolation.Runner) {
	if t != nil && store != nil && runner != nil {
		t.isolated = &taskIsolation{store: store, root: root, runner: runner}
	}
}

// isolatedCall is the part of a task call an isolated run reads, and the
// arguments it has to refuse rather than silently drop.
type isolatedCall struct {
	mode, prompt, model string
	unsupported         []string
}

func (t *TaskTool) executeIsolated(ctx context.Context, call isolatedCall) (string, error) {
	if strings.TrimSpace(call.mode) != isolationMode {
		return "", tool.Refusal{Code: codeIsolationUnsupported, Message: `isolation takes only "worktree"`}
	}
	if t.isolated == nil {
		return "", tool.Refusal{Code: isolation.CodeUnavailable, Message: "worktree isolation is not enabled in this session ([agent] worktree_isolation)"}
	}
	if len(call.unsupported) > 0 {
		return "", tool.Refusal{Code: codeIsolationUnsupported, Message: "an isolated task runs as its own agent and cannot take " +
			strings.Join(call.unsupported, ", ") + "; drop them or run without isolation"}
	}
	return t.isolated.store.Execute(ctx, t.isolated.runner, t.isolated.root, call.prompt, strings.TrimSpace(call.model))
}

// isolatedUnsupported names the task arguments that were set and that an
// isolated run cannot honour.
func isolatedUnsupported(profile string, writePaths, tools []string, continueFrom, forkFrom string, background bool) []string {
	var out []string
	for name, set := range map[string]bool{
		"profile": strings.TrimSpace(profile) != "", "write_paths": len(writePaths) > 0, "tools": len(tools) > 0,
		"continue_from": strings.TrimSpace(continueFrom) != "", "fork_from": strings.TrimSpace(forkFrom) != "",
		"run_in_background": background,
	} {
		if set {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
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
	s.Properties["isolation"] = json.RawMessage(`{"type":"string","enum":["worktree"],"description":"Run this writer in its own git worktree of the workspace instead of the workspace itself. Its changes are held, not written: the result lists them under an iso_ id for apply_isolated or discard_isolated. The sub-agent runs as a separate agent and cannot take profile, tools, write_paths, continue_from or run_in_background."}`)
	out, err := json.Marshal(s)
	if err != nil {
		return schema
	}
	return out
}
