package isolation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
)

type idArgs struct {
	ID string `json:"id"`
}

func parseID(raw json.RawMessage) (string, error) {
	var a idArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if strings.TrimSpace(a.ID) == "" {
		return "", fmt.Errorf("invalid args: id is required")
	}
	return strings.TrimSpace(a.ID), nil
}

const idSchema = `{"type":"object","properties":{"id":{"type":"string","description":"The iso_ id an isolated task returned."}},"required":["id"],"additionalProperties":false}`

// ApplyTool writes an isolated run's changes into the workspace. It declares
// every path it writes, so the host reserves, checkpoints and records them.
type ApplyTool struct{ store *Store }

// NewApplyTool applies results held by store.
func NewApplyTool(store *Store) *ApplyTool { return &ApplyTool{store: store} }

// Tool names, for a host that decides which of them a session shows.
const (
	ApplyName   = "apply_isolated"
	DiscardName = "discard_isolated"
)

func (*ApplyTool) Name() string { return ApplyName }

func (*ApplyTool) Description() string {
	return "Write the changes of a finished isolated task (task with isolation \"worktree\") into the workspace. " +
		"Each changed path is checked on its own: if the workspace changed a path since that task began, nothing is written and the conflicting paths are named. " +
		"Read the task's reported changes first; apply only what you mean to keep."
}

func (*ApplyTool) Schema() json.RawMessage { return json.RawMessage(idSchema) }

func (*ApplyTool) ReadOnly() bool { return false }

func (t *ApplyTool) DeclaredWritePaths(_ context.Context, raw json.RawMessage) ([]string, error) {
	id, err := parseID(raw)
	if err != nil {
		return nil, err
	}
	return t.store.Paths(id)
}

func (t *ApplyTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	id, err := parseID(raw)
	if err != nil {
		return "", err
	}
	e, err := t.store.Apply(ctx, id)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("applied %s to the workspace:\n%s", e.ID, statTable(e)), nil
}

// DiscardTool drops an isolated run's changes and its worktree. The workspace
// is untouched, which is why it needs no approval.
type DiscardTool struct{ store *Store }

// NewDiscardTool discards results held by store.
func NewDiscardTool(store *Store) *DiscardTool { return &DiscardTool{store: store} }

func (*DiscardTool) Name() string { return DiscardName }

func (*DiscardTool) Description() string {
	return "Throw away the changes of a finished isolated task without writing them to the workspace, and remove its worktree."
}

func (*DiscardTool) Schema() json.RawMessage { return json.RawMessage(idSchema) }

func (*DiscardTool) ReadOnly() bool { return true }

// Sequential keeps a discard out of a batch that may be applying the same id.
func (*DiscardTool) Sequential(context.Context, json.RawMessage) bool { return true }

func (t *DiscardTool) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	id, err := parseID(raw)
	if err != nil {
		return "", err
	}
	e, err := t.store.Discard(ctx, id)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("discarded %s (%d changed paths); the workspace is unchanged", e.ID, len(e.Changes)), nil
}

var (
	_ tool.WritePathDeclarer = (*ApplyTool)(nil)
	_ tool.SequentialTool    = (*DiscardTool)(nil)
)

// statTable is one line per changed path: status, path, and line counts.
func statTable(e *Entry) string {
	var b strings.Builder
	for _, st := range e.Stats {
		if st.Binary {
			fmt.Fprintf(&b, "  %s %s (binary)\n", st.Status, st.Path)
			continue
		}
		fmt.Fprintf(&b, "  %s %s +%d -%d\n", st.Status, st.Path, st.Added, st.Removed)
	}
	return strings.TrimRight(b.String(), "\n")
}
