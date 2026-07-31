package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

// rememberTool lets the model persist a durable fact to the auto-memory store.
// It is stateful (bound to one project's Store), so boot constructs it and adds
// it to the registry — the same pattern as the task tool — rather than
// self-registering as a stateless built-in.
type rememberTool struct{ store Store }

type rememberRequest struct {
	ID               string `json:"id"`
	ExpectedRevision int    `json:"expected_revision"`
	Name             string `json:"name"`
	Title            string `json:"title"`
	Description      string `json:"description"`
	Type             string `json:"type"`
	Scope            string `json:"scope"`
	Body             string `json:"body"`
}

// NewRememberTool returns the `remember` tool bound to store. A zero/disabled
// store yields a tool that reports the store is unavailable rather than silently
// dropping saves.
func NewRememberTool(store Store) tool.Tool { return rememberTool{store: store} }

func (rememberTool) Name() string { return "remember" }

func (rememberTool) Description() string {
	return "Save a durable background fact so it survives across sessions. " +
		"Use for things worth remembering long-term: who the user is and their preferences (type \"user\"); " +
		"guidance on how to work, including the why (type \"feedback\"); ongoing goals or constraints not " +
		"derivable from the code (type \"project\"); or pointers to external resources (type \"reference\"). " +
		"For feedback/project, structure the body with a \"**Why:**\" line and a \"**How to apply:**\" line so the fact is actionable later; " +
		"link related memories inline with [[their-name]]. " +
		"Do NOT save what the repo already records (code structure, git history) or facts that only matter to the current conversation; " +
		"if asked to remember one of those, save instead the non-obvious point behind it. " +
		"Choose scope \"project\" for the current workspace (the safe default) or \"global\" only when the fact should affect every project. " +
		"Standing rules that must always be followed belong in project or global REASONIX.md/AGENTS.md instructions, not background memory. " +
		"Before saving, check the loaded memory index for an entry that already covers this — reuse that name to update it rather than create a near-duplicate, and use `forget` to drop one that is now wrong. " +
		"The saved index loads into context at the start of each session."
}

func (rememberTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"id": {"type": "string", "description": "Stable memory id for an update. Prefer this over name when supplied by memory search/read."},
			"expected_revision": {"type": "integer", "minimum": 1, "description": "Revision returned by memory search/read. When set with id, the update fails instead of overwriting a newer change."},
			"name": {"type": "string", "description": "Stable project/<name>.md or global/<name>.md reference returned by memory search/read/list, or a short kebab-case slug for a new fact. Reusing a reference updates that exact memory. Omit to derive a new slug from the description."},
			"title": {"type": "string", "description": "Short human-readable label shown in the memory index, e.g. \"Prefers tabs\". Omit to derive one from the name."},
			"description": {"type": "string", "description": "One-line hook shown in the index — the phrase a future session reads to decide whether to open this memory. Make it specific."},
			"type": {"type": "string", "enum": ["user", "feedback", "project", "reference"], "description": "Category of the fact."},
			"scope": {"type": "string", "enum": ["project", "global"], "description": "Where the fact applies. For a new fact, omit for the safe default, project. When updating an existing name, omit to preserve its current scope. Use global only when it should affect every workspace."},
			"body": {"type": "string", "description": "The fact itself (Markdown). For feedback/project, include a \"**Why:**\" line and a \"**How to apply:**\" line; link related memories with [[their-name]]."}
		},
		"required": ["description", "body"]
	}`)
}

func (t rememberTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := parseRememberRequest(args)
	if err != nil {
		return "", err
	}
	if in.Description == "" || in.Body == "" {
		return "", fmt.Errorf("description and body are required")
	}
	scope := strings.ToLower(strings.TrimSpace(in.Scope))
	if scope != "" && scope != string(FactScopeProject) && scope != string(FactScopeGlobal) {
		return "", fmt.Errorf("scope must be one of project, global")
	}
	factScope := FactScope(scope)
	name := rememberRequestName(in)
	autoCreate := ClaimAutoMemoryWriteFromContext(ctx, args)
	result, err := t.store.SaveWithOptions(Memory{
		ID:          in.ID,
		Name:        name,
		Title:       in.Title,
		Description: in.Description,
		Type:        NormalizeType(in.Type),
		Scope:       factScope,
		Body:        in.Body,
	}, SaveOptions{
		ExpectedRevision:        in.ExpectedRevision,
		RequireExpectedRevision: in.ExpectedRevision > 0,
		RequireCreate:           autoCreate,
	})
	if err != nil {
		return "", err
	}
	path := result.Path
	if saved, ok := loadMemory(path); ok && saved.Scope != "" {
		factScope = NormalizeFactScope(string(saved.Scope))
	} else {
		factScope = t.store.scopeForPath(path)
	}
	if q, ok := QueueFromContext(ctx); ok {
		q.QueueMemory("Saved memory \"" + result.Memory.Name + "\" (" + string(factScope) + "): " + oneLine(result.Memory.Description) + "\n" + strings.TrimSpace(result.Memory.Body))
	}
	return fmt.Sprintf("Saved memory id=%s revision=%d (%s background) as %s (it applies now and its derived index loads automatically in future sessions).", result.Memory.ID, result.Memory.Revision, factScope, providerMemoryReference(result.Memory)), nil
}

func (rememberTool) ReadOnly() bool { return false }

func parseRememberRequest(args json.RawMessage) (rememberRequest, error) {
	var in rememberRequest
	if err := json.Unmarshal(args, &in); err != nil {
		return rememberRequest{}, fmt.Errorf("invalid arguments: %w", err)
	}
	return in, nil
}

func rememberRequestName(in rememberRequest) string {
	if name := strings.TrimSpace(in.Name); name != "" {
		return name
	}
	name := ""
	if in.ID == "" {
		name = in.Title
	}
	if name == "" && in.ID == "" {
		name = in.Description
	}
	return slug(name)
}
