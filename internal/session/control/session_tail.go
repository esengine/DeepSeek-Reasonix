package control

import (
	"fmt"

	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
)

// ForkSessionTail copies the newest part of the history at sourcePath into a
// new session and answers with its path and how many messages were left out.
// The source is only read, so a history too large to open stays whole.
func (c *Controller) ForkSessionTail(sourcePath string) (string, int, error) {
	if c.sessionDir == "" {
		return "", 0, fmt.Errorf("%w: sessions are not persisted here", ErrBranchUnavailable)
	}
	tail, err := sessionstore.LoadSessionTail(sourcePath)
	if err != nil {
		return "", 0, err
	}
	child := sessionstore.NewSession("")
	child.Messages = tail.Messages
	if tail.Dropped > 0 {
		noteOmittedHistory(child.Messages, tail.Dropped)
	}
	path := sessionstore.NewSessionPath(c.sessionDir, c.label)
	preview, turns := sessionstore.SessionPreviewFromMessages(child.Messages)
	meta := sessionstore.BranchMeta{
		ParentID: sessionstore.BranchID(sourcePath), WorkspaceRoot: c.workspaceRoot,
		Preview: preview, Turns: turns, SchemaVersion: sessionstore.BranchMetaCountsVersion,
		Model: c.ModelRef(), AgentPreset: c.AgentPreset(), Mode: "agent",
	}
	if src, ok, err := sessionstore.LoadBranchMeta(sourcePath); err == nil && ok {
		meta.Name, meta.WorkspaceRoot = src.Name, src.WorkspaceRoot
		if src.Model != "" {
			meta.Model, meta.AgentPreset, meta.Mode = src.Model, src.AgentPreset, src.Mode
		}
	}
	if err := saveConversationBranch(path, child, meta, func() error { return nil }); err != nil {
		return "", 0, err
	}
	return path, tail.Dropped, nil
}

// noteOmittedHistory tells the model, in the first user message it will see,
// that earlier turns were not carried over.
func noteOmittedHistory(msgs []provider.Message, dropped int) {
	for i := range msgs {
		if msgs[i].Role == provider.RoleUser {
			msgs[i].Content = fmt.Sprintf("[Host note: this conversation continues from its most recent part; %d earlier messages were too large to carry over and are not shown to you.]\n\n%s", dropped, msgs[i].Content)
			return
		}
	}
}
