package delegation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reasonix/internal/base/fileutil"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

type budgetTranscriptBinding struct {
	Version   int    `json:"version"`
	Ref       string `json:"ref"`
	Parent    string `json:"parent"`
	Workspace string `json:"workspace"`
	Scope     string `json:"scope"`
	Schema    string `json:"schema"`
}

func (s *SubagentStore) restoreBudgetBinding(meta *sessionstore.SubagentMeta) error {
	root, err := os.OpenRoot(s.dir)
	if err != nil {
		return err
	}
	defer root.Close()
	name := filepath.Base(store.SessionBudgetBinding(s.sessionPath(meta.Ref)))
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<10 {
		return fmt.Errorf("invalid task budget binding")
	}
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil {
		return err
	}
	var r budgetTranscriptBinding
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if len(data) > 64<<10 || d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF || r.Version != 1 || r.Ref != meta.Ref || r.Parent != meta.ParentSession || r.Workspace != meta.WorkspaceRoot || r.Scope == "" || r.Schema != meta.ToolSchemaHash {
		return fmt.Errorf("unsupported or mismatched task budget binding; preserved")
	}
	if meta.BudgetScope != "" && meta.BudgetScope != r.Scope {
		return fmt.Errorf("task budget binding changed")
	}
	meta.BudgetScope = r.Scope
	return nil
}

func (s *SubagentStore) preserveBudgetBinding(meta sessionstore.SubagentMeta) error {
	if meta.BudgetScope == "" {
		return s.restoreBudgetBinding(&meta)
	}
	data, err := json.Marshal(budgetTranscriptBinding{1, meta.Ref, meta.ParentSession, meta.WorkspaceRoot, meta.BudgetScope, meta.ToolSchemaHash})
	if err != nil {
		return err
	}
	err = fileutil.AtomicCreateFile(store.SessionBudgetBinding(s.sessionPath(meta.Ref)), data, 0600)
	if errors.Is(err, os.ErrExist) {
		return s.restoreBudgetBinding(&meta)
	}
	return err
}

func (t *TaskTool) continuationScopeContext(ctx context.Context, spec ProfileExecSpec) (context.Context, error) {
	ctx, err := t.isolatedSpecContext(ctx, spec)
	if err != nil {
		return ctx, err
	}
	ref := spec.Context.ContinueFrom
	if ref == "" || t.transcripts == nil {
		return ctx, nil
	}
	if agent.ParentSession(ctx) == "" {
		return ctx, fmt.Errorf("subagent continuation requires a persisted session; none is active in this run")
	}
	meta, err := t.transcripts.LoadMeta(ref)
	if err != nil {
		return ctx, err
	}
	if meta.BudgetScope == "" || meta.BudgetScope == agent.SharedTaskBudgetScope(ctx) {
		return ctx, nil
	}
	if err := validateContinueOwner(meta, SubagentSpec{ParentSession: agent.ParentSession(ctx)}); err != nil {
		return ctx, err
	}
	if meta.WorkspaceRoot != firstNonEmpty(t.transcriptRoot, t.workspaceRoot) {
		return ctx, ErrSubagentNotOwned
	}
	return agent.RestoreSharedTaskBudget(ctx, t.executionSessionPath(ctx), meta.BudgetScope)
}
