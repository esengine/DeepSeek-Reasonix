package isolation

import (
	"context"
	"encoding/json"

	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/worktree"
)

type RecoverTool struct {
	repo  gitcmd.Repo
	store *Store
}

func init() { tool.RegisterConditional(&RecoverTool{}) }

func NewRecoverTool(repo gitcmd.Repo) *RecoverTool { return &RecoverTool{repo: repo} }

func (t *RecoverTool) WithStore(store *Store) *RecoverTool { t.store = store; return t }

func (*RecoverTool) Name() string { return "recover_integration" }
func (*RecoverTool) Description() string {
	return "Recover an interrupted workspace integration before retrying writes. Restores only files still matching that integration's postimage; conflicting external edits and candidate results are retained."
}
func (*RecoverTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (*RecoverTool) ReadOnly() bool            { return false }
func (*RecoverTool) RecoversIntegration() bool { return true }

func (t *RecoverTool) DeclaredWritePaths(ctx context.Context, _ json.RawMessage) ([]string, error) {
	if t.store != nil {
		preview, err := t.store.PreviewRecovery(ctx)
		if err != nil || preview == nil {
			return nil, err
		}
		return preview.Paths, nil
	}
	return worktree.ApplyRecoveryPaths(worktree.Snapshot{Repo: t.repo})
}

func (t *RecoverTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if t.store != nil {
		preview, err := t.store.PreviewRecovery(ctx)
		if err != nil {
			return "", err
		}
		if preview != nil {
			err = t.store.RecoverExpected(ctx, preview.Revision, func(ctx context.Context, paths []string) error { return worktree.AuthorizeRecoveryPaths(ctx, paths) })
		}
		if err != nil {
			return "", err
		}
		return "Workspace recovery is complete. Interrupted results are retained as conflicts with unknown verification; prepare and verify a new integration candidate before automatic publication.", nil
	}
	if err := worktree.RecoverIntegration(ctx, t.repo); err != nil {
		return "", tool.Refusal{Code: CodeRecoveryRequired, Message: "integration recovery could not finish; files and journal retained: " + err.Error()}
	}
	return "Workspace integration recovery is complete. Candidate results are retained; review and retry their application separately.", nil
}
