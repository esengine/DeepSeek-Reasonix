package daemon

import (
	"context"
	"errors"
	"os"

	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/host"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/runtimeapi"
	"reasonix/internal/runtimeservice"
)

type fileGitServiceEntry struct {
	epoch   protocol.RuntimeEpoch
	root    string
	service *runtimeservice.FileGitService
}

type runtimeCheckpointProvider struct{ runtime *host.SessionRuntime }

func (p runtimeCheckpointProvider) CheckpointChanges(ctx context.Context) ([]runtimeservice.CheckpointChange, error) {
	changes, err := p.runtime.CheckpointChanges(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]runtimeservice.CheckpointChange, len(changes))
	for index, change := range changes {
		result[index] = runtimeservice.CheckpointChange{
			Path: change.Path, Turn: change.Turn, Prompt: change.Prompt, TimeMillis: change.TimeMillis,
		}
	}
	return result, nil
}

func (s *Server) fileGitService(target protocol.RuntimeTarget, runtime *host.SessionRuntime, primaryRoot string) (*runtimeservice.FileGitService, error) {
	s.fileGitMu.Lock()
	defer s.fileGitMu.Unlock()
	if s.fileGitServices == nil {
		s.fileGitServices = make(map[protocol.RuntimeTarget]fileGitServiceEntry)
	}
	if cached, ok := s.fileGitServices[target]; ok && cached.epoch == runtime.Epoch() && cached.root == primaryRoot && cached.service != nil {
		return cached.service, nil
	}
	service, err := runtimeservice.NewFileGitService(runtimeservice.Options{
		Root: primaryRoot, Checkpoints: runtimeCheckpointProvider{runtime: runtime},
	})
	if err != nil {
		return nil, err
	}
	s.fileGitServices[target] = fileGitServiceEntry{epoch: runtime.Epoch(), root: primaryRoot, service: service}
	return service, nil
}

func (t *transport) fileGitQueryService(ctx context.Context, method protocol.Method, query protocol.RuntimeQuery) (*runtimeservice.FileGitService, error) {
	if err := t.validateLease(true); err != nil {
		return nil, err
	}
	if err := t.validateHostEpoch(query.ExpectedHostEpoch); err != nil {
		return nil, err
	}
	runtime, err := t.currentRuntime(query.Target, query.ExpectedRuntimeEpoch)
	if err != nil {
		return nil, err
	}
	resolved, err := t.server.catalog.ResolveRuntimeTarget(ctx, query.Target)
	if err != nil {
		return nil, t.mapFileGitCatalogError(method, query.Target, err)
	}
	service, err := t.server.fileGitService(query.Target, runtime, resolved.WorkspaceRoot)
	if err != nil {
		return nil, t.mapFileGitError(method, query.Target, query.ExpectedRuntimeEpoch, err)
	}
	return service, nil
}

func runtimeSession(target protocol.RuntimeTarget) runtimeapi.SessionRef {
	return runtimeapi.SessionRef{
		WorkspaceID: runtimeapi.WorkspaceID(target.WorkspaceID),
		SessionID:   runtimeapi.SessionID(target.SessionID),
	}
}

func runtimeLimit(limit *int) int {
	if limit == nil {
		return 0
	}
	return *limit
}

func (t *transport) handleFileList(ctx context.Context, value any) (any, error) {
	params := value.(protocol.FileListParams)
	service, err := t.fileGitQueryService(ctx, protocol.MethodFileList, params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := service.ListFiles(ctx, runtimeapi.FileListInput{
		Session: runtimeSession(params.Target), Path: params.Path,
		Cursor: runtimeapi.Cursor(params.Cursor), Limit: runtimeLimit(params.Limit),
	})
	if err != nil {
		return nil, t.mapFileGitError(protocol.MethodFileList, params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return protocol.FileListResult{
		Entries: fileEntriesToProtocol(result.Entries), HasMore: result.HasMore,
		NextCursor: protocol.Cursor(result.Next),
	}, nil
}

func (t *transport) handleFileSearch(ctx context.Context, value any) (any, error) {
	params := value.(protocol.FileSearchParams)
	service, err := t.fileGitQueryService(ctx, protocol.MethodFileSearch, params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := service.SearchFiles(ctx, runtimeapi.FileSearchInput{
		Session: runtimeSession(params.Target), Query: params.Query, Limit: runtimeLimit(params.Limit),
	})
	if err != nil {
		return nil, t.mapFileGitError(protocol.MethodFileSearch, params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return protocol.FileSearchResult{
		Entries: fileEntriesToProtocol(result.Entries), Truncated: result.Truncated,
		TruncationReason: protocol.SearchTruncationReason(result.TruncationReason),
		ReturnedItems:    result.ReturnedItems, TotalItems: result.TotalItems,
	}, nil
}

func (t *transport) handleFilePreview(ctx context.Context, value any) (any, error) {
	params := value.(protocol.FilePreviewParams)
	service, err := t.fileGitQueryService(ctx, protocol.MethodFilePreview, params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := service.PreviewFile(ctx, runtimeapi.FilePreviewInput{
		Session: runtimeSession(params.Target), Path: params.Path,
	})
	if err != nil {
		return nil, t.mapFileGitError(protocol.MethodFilePreview, params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return protocol.FilePreviewResult{
		Name: result.Name, Path: result.Path, Kind: protocol.FileKind(result.Kind),
		SizeBytes: result.SizeBytes, ReturnedBytes: result.ReturnedBytes,
		Binary: result.Binary, Truncated: result.Truncated,
		TruncationReason: protocol.ByteTruncationReason(result.TruncationReason), Body: result.Body,
	}, nil
}

func (t *transport) handleWorkspaceChanges(ctx context.Context, value any) (any, error) {
	params := value.(protocol.WorkspaceChangesParams)
	service, err := t.fileGitQueryService(ctx, protocol.MethodWorkspaceChanges, params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := service.WorkspaceChanges(ctx, runtimeapi.WorkspaceChangesInput{
		Session: runtimeSession(params.Target), Cursor: runtimeapi.Cursor(params.Cursor), Limit: runtimeLimit(params.Limit),
	})
	if err != nil {
		return nil, t.mapFileGitError(protocol.MethodWorkspaceChanges, params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return protocol.WorkspaceChangesResult{
		Files: changedFilesToProtocol(result.Files), GitAvailable: result.GitAvailable,
		GitBranch: result.GitBranch, HasMore: result.HasMore, NextCursor: protocol.Cursor(result.Next),
	}, nil
}

func (t *transport) handleGitHistory(ctx context.Context, value any) (any, error) {
	params := value.(protocol.GitHistoryParams)
	service, err := t.fileGitQueryService(ctx, protocol.MethodGitHistory, params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := service.GitHistory(ctx, runtimeapi.GitHistoryInput{
		Session: runtimeSession(params.Target), Path: params.Path,
	})
	if err != nil {
		return nil, t.mapFileGitError(protocol.MethodGitHistory, params.Target, params.ExpectedRuntimeEpoch, err)
	}
	commits := make([]protocol.GitCommit, len(result.Commits))
	for index, commit := range result.Commits {
		commits[index] = protocol.GitCommit{
			Hash: commit.Hash, Author: commit.Author, Date: commit.Date, Message: commit.Message,
		}
	}
	return protocol.GitHistoryResult{
		Commits: commits, Truncated: result.Truncated,
		TruncationReason: protocol.GitHistoryTruncationReason(result.TruncationReason),
		ReturnedItems:    result.ReturnedItems,
	}, nil
}

func (t *transport) handleGitCommitDetail(ctx context.Context, value any) (any, error) {
	params := value.(protocol.GitCommitDetailParams)
	service, err := t.fileGitQueryService(ctx, protocol.MethodGitCommitDetail, params.RuntimeQuery)
	if err != nil {
		return nil, err
	}
	result, err := service.GitCommitDetail(ctx, runtimeapi.GitCommitDetailInput{
		Session: runtimeSession(params.Target), Hash: params.Hash, Path: params.Path,
		Cursor: runtimeapi.Cursor(params.Cursor), Limit: runtimeLimit(params.Limit),
	})
	if err != nil {
		return nil, t.mapFileGitError(protocol.MethodGitCommitDetail, params.Target, params.ExpectedRuntimeEpoch, err)
	}
	return gitCommitDetailToProtocol(result), nil
}

func fileEntriesToProtocol(entries []runtimeapi.FileEntry) []protocol.FileEntry {
	result := make([]protocol.FileEntry, len(entries))
	for index, entry := range entries {
		result[index] = protocol.FileEntry{Name: entry.Name, Path: entry.Path, IsDir: entry.IsDir}
	}
	return result
}

func changedFilesToProtocol(files []runtimeapi.ChangedFile) []protocol.ChangedFile {
	result := make([]protocol.ChangedFile, len(files))
	for index, file := range files {
		sources := make([]protocol.ChangeSource, len(file.Sources))
		for sourceIndex, source := range file.Sources {
			sources[sourceIndex] = protocol.ChangeSource(source)
		}
		result[index] = protocol.ChangedFile{
			Path: file.Path, OldPath: file.OldPath, Sources: sources, GitStatus: file.GitStatus,
			Turns: append([]int(nil), file.Turns...), LatestPrompt: file.LatestPrompt,
			LatestTimeMs: cloneInt64(file.LatestTimeMillis),
		}
	}
	return result
}

func gitCommitDetailToProtocol(detail runtimeapi.GitCommitDetail) protocol.GitCommitDetailResult {
	result := protocol.GitCommitDetailResult{
		Kind: protocol.GitCommitDetailKind(detail.Kind), NextCursor: protocol.Cursor(detail.Next),
		Path: detail.Path, Body: cloneString(detail.Body), SizeBytes: cloneInt64(detail.SizeBytes),
		ReturnedBytes: cloneInt64(detail.ReturnedBytes), Truncated: cloneBool(detail.Truncated),
		TruncationReason: protocol.ByteTruncationReason(detail.TruncationReason),
	}
	if detail.Files != nil {
		files := make([]protocol.GitCommitFile, len(*detail.Files))
		for index, file := range *detail.Files {
			files[index] = protocol.GitCommitFile{
				Path: file.Path, OldPath: file.OldPath, Status: file.Status,
				Additions: file.Additions, Deletions: file.Deletions,
			}
		}
		result.Files = &files
	}
	result.HasMore = cloneBool(detail.HasMore)
	return result
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func (t *transport) mapFileGitCatalogError(method protocol.Method, target protocol.RuntimeTarget, err error) error {
	if _, ok := catalog.ErrorCode(err); ok {
		return t.server.mapCatalogError(method, &target, err)
	}
	t.server.reportInternal(method, err)
	targetCopy := target
	return protocol.MustRemoteError(protocol.ErrQueryFailed, protocol.ErrorOptions{Target: &targetCopy})
}

func (t *transport) mapFileGitError(method protocol.Method, target protocol.RuntimeTarget, expected protocol.RuntimeEpoch, err error) error {
	if err == nil {
		return nil
	}
	var remote *protocol.RemoteError
	if errors.As(err, &remote) {
		return remote
	}
	targetCopy := target
	options := protocol.ErrorOptions{Target: &targetCopy}
	switch {
	case errors.Is(err, host.ErrRuntimeClosed):
		return t.mapRuntimeError(target, expected, err)
	case errors.Is(err, runtimeservice.ErrInvalidCursor), errors.Is(err, runtimeservice.ErrStaleCursor):
		return protocol.MustRemoteError(protocol.ErrStaleCursor, options)
	case errors.Is(err, runtimeservice.ErrPathEscapesRoot), errors.Is(err, os.ErrPermission):
		return protocol.MustRemoteError(protocol.ErrPermissionDenied, options)
	case errors.Is(err, runtimeservice.ErrPathNotFound):
		return protocol.MustRemoteError(protocol.ErrPathNotFound, options)
	case errors.Is(err, runtimeservice.ErrNotDirectory):
		return protocol.MustRemoteError(protocol.ErrNotDirectory, options)
	case errors.Is(err, runtimeservice.ErrNotFile):
		return protocol.MustRemoteError(protocol.ErrNotFile, options)
	case errors.Is(err, runtimeservice.ErrGitUnavailable):
		return protocol.MustRemoteError(protocol.ErrGitUnavailable, options)
	case errors.Is(err, runtimeservice.ErrGitObjectNotFound):
		return protocol.MustRemoteError(protocol.ErrGitObjectNotFound, options)
	case errors.Is(err, runtimeservice.ErrQueryFailed), errors.Is(err, runtimeservice.ErrInvalidPath),
		errors.Is(err, runtimeservice.ErrInvalidSession), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return protocol.MustRemoteError(protocol.ErrQueryFailed, options)
	default:
		t.server.reportInternal(method, err)
		return protocol.MustRemoteError(protocol.ErrQueryFailed, options)
	}
}
