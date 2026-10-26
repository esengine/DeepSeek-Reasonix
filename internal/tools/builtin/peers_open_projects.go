package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/contract/tool"
)

// OpenProjectsTool lets the model ask for folders to be opened as projects, so
// a later dispatch can name them. It is registered only where the user's own
// file turned dispatch on, and every call waits on a card naming each folder.
func OpenProjectsTool(link *tool.PeerLink) tool.Tool { return &openProjects{link: link} }

type openProjects struct{ link *tool.PeerLink }

func (*openProjects) Name() string { return "open_projects" }
func (*openProjects) Description() string {
	return "Ask the user to add folders to their project list so tasks can be dispatched to them. Give absolute paths of folders that already exist. The user is shown every path on a card and decides; nothing is added or trusted before they approve, and a folder not on the card is never touched. It is for opening projects, not for reading or writing in them; follow it with dispatch_to_project."
}
func (*openProjects) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string"},"minItems":1,"maxItems":12,"description":"Absolute paths of existing folders to open as projects"}},"required":["paths"],"additionalProperties":false}`)
}

// ReadOnly is true because the only write waits on the user's own answer to a
// card that cannot be answered by any mode; it keeps the tool out of YOLO's reach.
func (*openProjects) ReadOnly() bool { return true }

// ReadOnlyExecutionHostMutation keeps the tool out of a read-only child, whose
// host-mutating tools are withdrawn.
func (*openProjects) ReadOnlyExecutionHostMutation() bool { return true }

// ApprovalScope says the card is about more than the call: it adds projects.
func (*openProjects) ApprovalScope() string { return tool.ApprovalScopeOpenProjects }

func (t *openProjects) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("open_projects needs {\"paths\": [...]}: %w", err)
	}
	dir, ok := t.link.Directory()
	if !ok {
		return "", errors.New("peer.disabled: messages between sessions are not available in this session")
	}
	o, ok := dir.(tool.ProjectOpener)
	if !ok {
		return "", errors.New("dispatch.unavailable: this session has no window to open projects in; open the project in Reasonix Studio or the web UI")
	}
	got, err := o.OpenProjects(ctx, in.Paths)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range got {
		switch {
		case p.Code != "":
			fmt.Fprintf(&b, "%s: refused (%s)\n", p.Path, p.Code)
		case p.Added:
			fmt.Fprintf(&b, "%s: opened as a project\n", p.Path)
		default:
			fmt.Fprintf(&b, "%s: already in the project list\n", p.Path)
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\nYou can now name these projects in dispatch_to_project.", nil
}
