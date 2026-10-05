package control

import (
	"context"
	"path/filepath"

	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/promptrefine"
)

// RefinePrompt rewrites a draft into a clearer request with the session's own
// model, reading the last few turns so a reference in it can resolve. Nothing
// is sent and the history is untouched: the person decides what to do with it.
func (c *Controller) RefinePrompt(ctx context.Context, draft string) (string, error) {
	root := c.WorkspaceRoot()
	workspace := ""
	if root != "" {
		workspace = filepath.Base(root)
	}
	return c.refiner.Refine(ctx, promptrefine.Input{Draft: draft, Recent: conversationTurns(c.History()), Workspace: workspace})
}

// conversationTurns keeps what the person and the model said to each other:
// tool traffic and host-authored messages say nothing a draft refers to.
func conversationTurns(history []provider.Message) []promptrefine.Turn {
	var turns []promptrefine.Turn
	for _, m := range history {
		if m.Role != provider.RoleUser && m.Role != provider.RoleAssistant {
			continue
		}
		if m.Content == "" {
			continue
		}
		turns = append(turns, promptrefine.Turn{Role: string(m.Role), Text: m.Content})
	}
	return turns
}
