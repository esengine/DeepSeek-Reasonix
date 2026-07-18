package runtimeservice

import (
	"context"
	"errors"
	"sort"
	"strings"

	"reasonix/internal/runtimeapi"
)

type changeAccumulator struct {
	file       runtimeapi.ChangedFile
	hasSession bool
	hasGit     bool
}

func (s *FileGitService) WorkspaceChanges(ctx context.Context, input runtimeapi.WorkspaceChangesInput) (runtimeapi.WorkspaceChangesPage, error) {
	result := runtimeapi.WorkspaceChangesPage{Files: []runtimeapi.ChangedFile{}}
	if err := requireSession(input.Session); err != nil {
		return result, err
	}
	limit, err := normalizedPageLimit(input.Limit)
	if err != nil {
		return result, err
	}
	changes := map[string]*changeAccumulator{}
	accumulator := func(rel string) *changeAccumulator {
		if changes[rel] == nil {
			changes[rel] = &changeAccumulator{file: runtimeapi.ChangedFile{Path: rel, Sources: []runtimeapi.ChangeSource{}}}
		}
		return changes[rel]
	}

	if s.checkpoints != nil {
		checkpointChanges, err := s.checkpoints.CheckpointChanges(ctx)
		if err != nil {
			// Preserve the target runtime's typed lifecycle error so an adapter
			// can map replacement/closure to its structured stale-target error.
			// Transport layers still suppress arbitrary provider detail.
			return result, err
		}
		for _, change := range checkpointChanges {
			if change.Turn < 0 || change.TimeMillis < 0 {
				return result, ErrQueryFailed
			}
			rel, err := normalizeRelative(change.Path, false)
			if err != nil {
				return result, err
			}
			if _, _, resolveErr := s.resolveExisting(rel); errors.Is(resolveErr, ErrPathEscapesRoot) {
				return result, ErrPathEscapesRoot
			}
			acc := accumulator(rel)
			acc.hasSession = true
			if !containsInt(acc.file.Turns, change.Turn) {
				acc.file.Turns = append(acc.file.Turns, change.Turn)
			}
			if acc.file.LatestTimeMillis == nil || change.TimeMillis >= *acc.file.LatestTimeMillis {
				timeMillis := change.TimeMillis
				acc.file.LatestTimeMillis = &timeMillis
				acc.file.LatestPrompt = change.Prompt
			}
		}
	}

	gitChanges, gitErr := s.gitStatus(ctx)
	result.GitAvailable = gitErr == nil
	if result.GitAvailable {
		result.GitBranch = s.gitBranch(ctx)
		for _, change := range gitChanges {
			rel, err := normalizeRelative(change.path, false)
			if err != nil {
				// Git output outside the primary workspace is ignored rather
				// than converted into a path-shaped protocol value.
				continue
			}
			acc := accumulator(rel)
			acc.hasGit = true
			acc.file.GitStatus = change.status
			if change.oldPath != "" {
				if oldPath, err := normalizeRelative(change.oldPath, false); err == nil {
					acc.file.OldPath = oldPath
				}
			}
		}
	}

	full := make([]runtimeapi.ChangedFile, 0, len(changes))
	for _, acc := range changes {
		if acc.hasSession {
			acc.file.Sources = append(acc.file.Sources, runtimeapi.ChangeSession)
			sort.Ints(acc.file.Turns)
		}
		if acc.hasGit {
			acc.file.Sources = append(acc.file.Sources, runtimeapi.ChangeGit)
		}
		full = append(full, acc.file)
	}
	sort.Slice(full, func(i, j int) bool {
		if len(full[i].Sources) != len(full[j].Sources) {
			return len(full[i].Sources) > len(full[j].Sources)
		}
		left, right := strings.ToLower(full[i].Path), strings.ToLower(full[j].Path)
		if left != right {
			return left < right
		}
		return full[i].Path < full[j].Path
	})

	revision := snapshotRevision(full, "workspace/changes", result.GitBranch, boolText(result.GitAvailable))
	session := sessionBinding(input.Session)
	offset, err := s.pageOffset(input.Cursor, "workspace/changes", session, "", revision, len(full))
	if err != nil {
		return runtimeapi.WorkspaceChangesPage{Files: []runtimeapi.ChangedFile{}}, err
	}
	end := offset + limit
	if end > len(full) {
		end = len(full)
	}
	result.Files = append(result.Files, full[offset:end]...)
	result.HasMore = end < len(full)
	if result.HasMore {
		result.Next = s.encodeCursor(cursorPayload{
			Method: "workspace/changes", Session: session, Revision: revision, Offset: end,
		})
	}
	return result, nil
}

func containsInt(values []int, candidate int) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
