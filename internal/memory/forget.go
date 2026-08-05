package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

// forgetTool deletes a saved memory the model judges wrong or stale. Like
// rememberTool it is stateful (bound to one project's Store), so boot constructs
// it and adds it to the registry.
type forgetTool struct{ store Store }

// NewForgetTool returns the `forget` tool bound to store.
func NewForgetTool(store Store) tool.Tool { return forgetTool{store: store} }

func (forgetTool) Name() string { return "forget" }

func (forgetTool) Description() string {
	return "Delete a saved memory by name when it is wrong, stale, or superseded, so it stops loading into future sessions. " +
		"Use the stable project/<name>.md or global/<name>.md reference returned by memory search/read/list. " +
		"Prefer updating a memory with `remember` (reuse its name) over forget-then-recreate; reach for forget only when the fact should no longer exist at all."
}

func (forgetTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "Stable memory id, project/<name>.md or global/<name>.md reference, or legacy slug of the memory to archive."}
		},
		"required": ["name"]
	}`)
}

func (t forgetTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	memory, found := t.store.Read(in.Name)
	archive, err := t.store.Archive(in.Name)
	if err != nil {
		return "", err
	}
	if q, ok := QueueFromContext(ctx); ok {
		name := slug(strings.TrimSuffix(in.Name, ".md"))
		if found {
			name = memory.Name
		}
		q.QueueMemory("Forgot memory \"" + name + "\" — disregard its loaded guidance and background-index entry for the rest of this session.")
	}
	if archive != "" {
		if found {
			return fmt.Sprintf("Forgot memory %q (it no longer applies and will not load in future sessions; archived from %s).", in.Name, providerMemoryReference(memory)), nil
		}
		return fmt.Sprintf("Forgot memory %q (it no longer applies and will not load in future sessions; archived).", in.Name), nil
	}
	return fmt.Sprintf("Forgot memory %q (it no longer applies and will not load in future sessions).", in.Name), nil
}

func (forgetTool) ReadOnly() bool { return false }
