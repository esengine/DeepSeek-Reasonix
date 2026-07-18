package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
)

func (a *LocalTargetAdapter) SessionContext(ctx context.Context, input runtimeapi.SessionContextInput) (runtimeapi.ContextView, error) {
	tab, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.ContextView{}, err
	}
	defer a.endLocalSession()
	used, window := ctrl.ContextSnapshot()
	return runtimeservice.ProjectContext(runtimeservice.ContextSource{
		UsedTokens: used, WindowTokens: window, LastUsage: ctrl.LastUsage(), Telemetry: tab.telemetrySnapshot(),
	})
}

func (a *LocalTargetAdapter) SessionBalance(ctx context.Context, input runtimeapi.SessionBalanceInput) (runtimeapi.BalanceView, error) {
	_, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.BalanceView{}, err
	}
	defer a.endLocalSession()
	balance, queryErr := ctrl.Balance(localContext(ctx))
	return runtimeservice.ProjectBalance(balance, queryErr), nil
}

func (a *LocalTargetAdapter) ListJobs(ctx context.Context, input runtimeapi.ListJobsInput) (runtimeapi.JobPage, error) {
	_, ctrl, record, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.JobPage{}, err
	}
	defer a.endLocalSession()
	page, err := runtimeservice.PageJobs(runtimeservice.RuntimeBinding{
		Session: input.Session, Incarnation: localRuntimeIncarnation(a, record),
	}, ctrl.Jobs(), input.Cursor, input.Limit)
	if err != nil {
		return runtimeapi.JobPage{}, err
	}
	a.mu.Lock()
	for index := range page.Jobs {
		raw := string(page.Jobs[index].ID)
		opaque := runtimeapi.JobID(localOpaqueID("local_job", string(input.Session.SessionID)+"\x00"+raw))
		a.v1.jobs[opaque] = localJobBinding{session: input.Session, rawID: raw}
		page.Jobs[index].ID = opaque
	}
	a.mu.Unlock()
	return page, nil
}

func (a *LocalTargetAdapter) CancelJob(ctx context.Context, input runtimeapi.CancelJobInput) (runtimeapi.CancelJobResult, error) {
	_, ctrl, _, err := a.withLocalSession(ctx, input.Session)
	if err != nil {
		return runtimeapi.CancelJobResult{}, err
	}
	defer a.endLocalSession()
	a.mu.Lock()
	binding, ok := a.v1.jobs[input.JobID]
	a.mu.Unlock()
	if !ok || binding.session != input.Session {
		return runtimeapi.CancelJobResult{}, errors.New("Local job ID is stale or unknown")
	}
	canceller, ok := ctrl.(control.JobCancellation)
	if !ok {
		return runtimeapi.CancelJobResult{}, runtimeservice.ErrCapabilityUnavailable
	}
	disposition := runtimeapi.JobNotRunning
	if canceller.CancelBackgroundJob(binding.rawID) {
		disposition = runtimeapi.JobCancelled
	}
	return runtimeapi.CancelJobResult{Disposition: disposition}, nil
}

func (a *LocalTargetAdapter) localMemoryService(ctx context.Context, ref runtimeapi.SessionRef) (*runtimeservice.MemoryResearchService, control.SessionAPI, func(), error) {
	_, ctrl, record, err := a.withLocalSession(ctx, ref)
	if err != nil {
		return nil, nil, nil, err
	}
	incarnation := localRuntimeIncarnation(a, record)
	root := record.workspaceRoot
	if record.scope == "global" {
		root = globalWorkspaceRoot()
	}
	a.mu.Lock()
	entry := a.v1.memoryResearch[ref]
	if entry.service == nil || entry.incarnation != incarnation || !sameProjectRoot(entry.root, root) {
		service, serviceErr := runtimeservice.NewMemoryResearchService(runtimeservice.RuntimeBinding{Session: ref, Incarnation: incarnation}, root)
		if serviceErr != nil {
			a.mu.Unlock()
			a.endLocalSession()
			return nil, nil, nil, serviceErr
		}
		entry = localMemoryResearchEntry{incarnation: incarnation, root: root, service: service}
		a.v1.memoryResearch[ref] = entry
	}
	a.mu.Unlock()
	return entry.service, ctrl, a.endLocalSession, nil
}

func (a *LocalTargetAdapter) Memory(ctx context.Context, input runtimeapi.MemoryInput) (runtimeapi.MemoryView, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.MemoryView{}, err
	}
	defer release()
	return service.MemoryView(input.Session, ctrl)
}

func (a *LocalTargetAdapter) MemorySuggestions(ctx context.Context, input runtimeapi.MemoryInput) (runtimeapi.MemorySuggestionsView, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.MemorySuggestionsView{}, err
	}
	defer release()
	return service.Suggestions(input.Session, ctrl)
}

func (a *LocalTargetAdapter) RememberMemory(ctx context.Context, input runtimeapi.RememberMemoryInput) (runtimeapi.RememberMemoryResult, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.RememberMemoryResult{}, err
	}
	defer release()
	result, err := service.Remember(input.Session, ctrl, input.Scope, input.Note)
	if err == nil {
		a.notifyLocalCatalog(result.InvalidationScope, affectedLocalWorkspace(result.InvalidationScope, input.Session.WorkspaceID), runtimeapi.CatalogMemory)
	}
	return result, err
}

func (a *LocalTargetAdapter) ForgetMemory(ctx context.Context, input runtimeapi.ForgetMemoryInput) (runtimeapi.ForgetMemoryResult, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.ForgetMemoryResult{}, err
	}
	defer release()
	result, err := service.Forget(input.Session, ctrl, input.MemoryID)
	if err == nil {
		a.notifyLocalCatalog(result.InvalidationScope, affectedLocalWorkspace(result.InvalidationScope, input.Session.WorkspaceID), runtimeapi.CatalogMemory)
	}
	return result, err
}

func (a *LocalTargetAdapter) SaveMemoryDocument(ctx context.Context, input runtimeapi.SaveMemoryDocumentInput) (runtimeapi.SaveMemoryDocumentResult, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.SaveMemoryDocumentResult{}, err
	}
	defer release()
	result, err := service.SaveDocument(input.Session, ctrl, input.DocumentID, input.Body)
	if err == nil {
		a.notifyLocalCatalog(result.InvalidationScope, affectedLocalWorkspace(result.InvalidationScope, input.Session.WorkspaceID), runtimeapi.CatalogMemory)
	}
	return result, err
}

func (a *LocalTargetAdapter) AcceptMemorySuggestion(ctx context.Context, input runtimeapi.AcceptMemorySuggestionInput) (runtimeapi.AcceptMemorySuggestionResult, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.AcceptMemorySuggestionResult{}, err
	}
	defer release()
	result, err := service.AcceptMemorySuggestion(input.Session, ctrl, input.SuggestionID, input.ExpectedRevision)
	if err == nil {
		a.notifyLocalCatalog(result.InvalidationScope, affectedLocalWorkspace(result.InvalidationScope, input.Session.WorkspaceID), runtimeapi.CatalogMemory)
	}
	return result, err
}

func (a *LocalTargetAdapter) AcceptSkillSuggestion(ctx context.Context, input runtimeapi.AcceptSkillSuggestionInput) (runtimeapi.AcceptSkillSuggestionResult, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.AcceptSkillSuggestionResult{}, err
	}
	defer release()
	result, err := service.AcceptSkillSuggestion(input.Session, ctrl, input.SuggestionID, input.ExpectedRevision)
	if err == nil {
		a.notifyLocalCatalog(result.InvalidationScope, affectedLocalWorkspace(result.InvalidationScope, input.Session.WorkspaceID), runtimeapi.CatalogMemory, runtimeapi.CatalogSessionCatalog)
	}
	return result, err
}

func affectedLocalWorkspace(scope runtimeapi.CatalogScope, workspaceID runtimeapi.WorkspaceID) []runtimeapi.WorkspaceID {
	if scope == runtimeapi.CatalogWorkspace {
		return []runtimeapi.WorkspaceID{workspaceID}
	}
	return nil
}

func localResearchAccess(ctrl control.SessionAPI) (control.AutoResearchTaskAccess, error) {
	access, ok := ctrl.(control.AutoResearchTaskAccess)
	if !ok {
		return nil, runtimeservice.ErrCapabilityUnavailable
	}
	return access, nil
}

func (a *LocalTargetAdapter) ResearchStatus(ctx context.Context, input runtimeapi.ResearchInput) (runtimeapi.ResearchStatusView, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.ResearchStatusView{}, err
	}
	defer release()
	access, err := localResearchAccess(ctrl)
	if err != nil {
		return runtimeapi.ResearchStatusView{}, err
	}
	summary, available, err := access.CurrentAutoResearchTask()
	if err != nil {
		return runtimeapi.ResearchStatusView{}, err
	}
	if !available {
		return runtimeapi.ResearchStatusView{}, runtimeservice.ErrCapabilityUnavailable
	}
	return service.ResearchStatus(input.Session, summary)
}

func (a *LocalTargetAdapter) ListResearch(ctx context.Context, input runtimeapi.ListResearchInput) (runtimeapi.ResearchPage, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.ResearchPage{}, err
	}
	defer release()
	access, err := localResearchAccess(ctrl)
	if err != nil {
		return runtimeapi.ResearchPage{}, err
	}
	summaries, available, err := access.ListAutoResearchTasks()
	if err != nil {
		return runtimeapi.ResearchPage{}, err
	}
	if !available {
		return runtimeapi.ResearchPage{}, runtimeservice.ErrCapabilityUnavailable
	}
	return service.ResearchList(input.Session, summaries, input.Cursor, input.Limit)
}

func (a *LocalTargetAdapter) ResearchFindings(ctx context.Context, input runtimeapi.ResearchFindingsInput) (runtimeapi.ResearchFindingsPage, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	defer release()
	access, err := localResearchAccess(ctrl)
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	rawTask, err := service.ResolveResearchTask(input.Session, input.TaskID)
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	findings, available, err := access.AutoResearchTaskFindings(rawTask)
	if err != nil {
		return runtimeapi.ResearchFindingsPage{}, err
	}
	if !available {
		return runtimeapi.ResearchFindingsPage{}, runtimeservice.ErrCapabilityUnavailable
	}
	return service.ResearchFindings(input.Session, input.TaskID, findings, input.Cursor, input.Limit)
}

func (a *LocalTargetAdapter) RecordResearchEvidence(ctx context.Context, input runtimeapi.RecordResearchEvidenceInput) (runtimeapi.RecordResearchEvidenceResult, error) {
	service, ctrl, release, err := a.localMemoryService(ctx, input.Session)
	if err != nil {
		return runtimeapi.RecordResearchEvidenceResult{}, err
	}
	defer release()
	access, err := localResearchAccess(ctrl)
	if err != nil {
		return runtimeapi.RecordResearchEvidenceResult{}, err
	}
	rawTask, rawCriterion, err := service.ResolveResearchEvidenceTarget(input.Session, input.TaskID, input.CriterionID)
	if err != nil {
		return runtimeapi.RecordResearchEvidenceResult{}, err
	}
	finding, err := service.PrepareResearchEvidence(input.Session, input.TaskID, input.CriterionID, input.Evidence, time.Now().UTC())
	if err != nil {
		return runtimeapi.RecordResearchEvidenceResult{}, err
	}
	if err := access.RecordAutoResearchTaskEvidence(rawTask, rawCriterion, finding); err != nil {
		return runtimeapi.RecordResearchEvidenceResult{}, fmt.Errorf("%w: %v", runtimeservice.ErrResearchMutationFailed, err)
	}
	a.notifyLocalCatalog(runtimeapi.CatalogWorkspace, []runtimeapi.WorkspaceID{input.Session.WorkspaceID}, runtimeapi.CatalogResearch)
	return runtimeapi.RecordResearchEvidenceResult{Recorded: true}, nil
}

type localCheckpointProvider struct {
	root string
	ctrl control.SessionAPI
}

func (p localCheckpointProvider) CheckpointChanges(ctx context.Context) ([]runtimeservice.CheckpointChange, error) {
	if err := localCheckContext(ctx); err != nil {
		return nil, err
	}
	snapshot := p.ctrl.CheckpointSnapshot()
	result := make([]runtimeservice.CheckpointChange, 0)
	for _, checkpoint := range snapshot.Metas {
		for _, raw := range checkpoint.Paths {
			path := filepath.Clean(strings.TrimSpace(raw))
			if filepath.IsAbs(path) {
				relative, err := filepath.Rel(p.root, path)
				if err != nil {
					return nil, err
				}
				path = relative
			}
			if path == "." || path == ".." || filepath.IsAbs(path) || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
				return nil, errors.New("checkpoint path escapes the primary workspace")
			}
			at := int64(0)
			if !checkpoint.Time.IsZero() {
				at = checkpoint.Time.UnixMilli()
			}
			result = append(result, runtimeservice.CheckpointChange{Path: filepath.ToSlash(path), Turn: checkpoint.Turn, Prompt: checkpoint.Prompt, TimeMillis: at})
		}
	}
	return result, nil
}

func (a *LocalTargetAdapter) localFileGitService(ctx context.Context, ref runtimeapi.SessionRef) (*runtimeservice.FileGitService, func(), error) {
	_, ctrl, record, err := a.withLocalSession(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	incarnation := localRuntimeIncarnation(a, record)
	root := record.workspaceRoot
	if record.scope == "global" {
		root = globalWorkspaceRoot()
	}
	a.mu.Lock()
	entry := a.v1.fileGit[ref]
	if entry.service == nil || entry.incarnation != incarnation || !sameProjectRoot(entry.root, root) {
		service, serviceErr := runtimeservice.NewFileGitService(runtimeservice.Options{Root: root, Checkpoints: localCheckpointProvider{root: root, ctrl: ctrl}})
		if serviceErr != nil {
			a.mu.Unlock()
			a.endLocalSession()
			return nil, nil, serviceErr
		}
		entry = localFileGitEntry{incarnation: incarnation, root: root, service: service}
		a.v1.fileGit[ref] = entry
	}
	a.mu.Unlock()
	return entry.service, a.endLocalSession, nil
}

func (a *LocalTargetAdapter) ListFiles(ctx context.Context, input runtimeapi.FileListInput) (runtimeapi.FileListResult, error) {
	service, release, err := a.localFileGitService(ctx, input.Session)
	if err != nil {
		return runtimeapi.FileListResult{}, err
	}
	defer release()
	return service.ListFiles(localContext(ctx), input)
}

func (a *LocalTargetAdapter) SearchFiles(ctx context.Context, input runtimeapi.FileSearchInput) (runtimeapi.FileSearchResult, error) {
	service, release, err := a.localFileGitService(ctx, input.Session)
	if err != nil {
		return runtimeapi.FileSearchResult{}, err
	}
	defer release()
	return service.SearchFiles(localContext(ctx), input)
}

func (a *LocalTargetAdapter) PreviewFile(ctx context.Context, input runtimeapi.FilePreviewInput) (runtimeapi.FilePreview, error) {
	service, release, err := a.localFileGitService(ctx, input.Session)
	if err != nil {
		return runtimeapi.FilePreview{}, err
	}
	defer release()
	return service.PreviewFile(localContext(ctx), input)
}

func (a *LocalTargetAdapter) WorkspaceChanges(ctx context.Context, input runtimeapi.WorkspaceChangesInput) (runtimeapi.WorkspaceChangesPage, error) {
	service, release, err := a.localFileGitService(ctx, input.Session)
	if err != nil {
		return runtimeapi.WorkspaceChangesPage{}, err
	}
	defer release()
	return service.WorkspaceChanges(localContext(ctx), input)
}

func (a *LocalTargetAdapter) GitHistory(ctx context.Context, input runtimeapi.GitHistoryInput) (runtimeapi.GitHistoryResult, error) {
	service, release, err := a.localFileGitService(ctx, input.Session)
	if err != nil {
		return runtimeapi.GitHistoryResult{}, err
	}
	defer release()
	return service.GitHistory(localContext(ctx), input)
}

func (a *LocalTargetAdapter) GitCommitDetail(ctx context.Context, input runtimeapi.GitCommitDetailInput) (runtimeapi.GitCommitDetail, error) {
	service, release, err := a.localFileGitService(ctx, input.Session)
	if err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	defer release()
	return service.GitCommitDetail(localContext(ctx), input)
}
