package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/checkpoint"
	"reasonix/internal/diff"
	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/proc"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/workbench/files"
)

type runtimeGitStatus struct {
	path    string
	oldPath string
	status  string
}

type runtimeChange struct {
	file       protocol.ChangedFile
	hasSession bool
	hasGit     bool
}

func (s *Server) workspaceChanges(p protocol.WorkspaceChangesParams) (protocol.WorkspaceChangesResult, error) {
	sess, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.WorkspaceChangesResult{}, err
	}
	changes := map[string]*runtimeChange{}
	add := func(path string) *runtimeChange {
		path = normalizeWorkspacePath(s.opts.Workspace, path)
		if path == "" {
			return nil
		}
		if changes[path] == nil {
			changes[path] = &runtimeChange{file: protocol.ChangedFile{Path: path, Sources: []protocol.ChangeSource{}}}
		}
		return changes[path]
	}
	if controller, ok := sess.ctrl.(interface{ Checkpoints() []checkpoint.Meta }); ok {
		for _, meta := range controller.Checkpoints() {
			for _, path := range meta.Paths {
				item := add(path)
				if item == nil {
					continue
				}
				item.hasSession = true
				if len(item.file.Turns) == 0 || item.file.Turns[len(item.file.Turns)-1] != meta.Turn {
					item.file.Turns = append(item.file.Turns, meta.Turn)
				}
				at := meta.Time.UnixMilli()
				if item.file.LatestTimeMs == nil || at >= *item.file.LatestTimeMs {
					item.file.LatestPrompt = meta.Prompt
					item.file.LatestTimeMs = &at
				}
			}
		}
	}
	entries, branch, gitErr := s.gitStatus()
	for _, entry := range entries {
		item := add(entry.path)
		if item == nil {
			continue
		}
		item.hasGit = true
		item.file.GitStatus = entry.status
		item.file.OldPath = normalizeGitPath(entry.oldPath)
	}
	filesOut := make([]protocol.ChangedFile, 0, len(changes))
	for _, item := range changes {
		if item.hasSession {
			item.file.Sources = append(item.file.Sources, protocol.ChangeSession)
		}
		if item.hasGit {
			item.file.Sources = append(item.file.Sources, protocol.ChangeGit)
		}
		filesOut = append(filesOut, item.file)
	}
	sort.Slice(filesOut, func(i, j int) bool {
		if len(filesOut[i].Sources) != len(filesOut[j].Sources) {
			return len(filesOut[i].Sources) > len(filesOut[j].Sources)
		}
		return strings.ToLower(filesOut[i].Path) < strings.ToLower(filesOut[j].Path)
	})
	offset, err := pageOffset(p.Cursor, "changes")
	if err != nil {
		return protocol.WorkspaceChangesResult{}, protocol.MustRemoteError(protocol.ErrStaleCursor, protocol.ErrorOptions{})
	}
	limit := protocol.PageDefaultItems
	if p.Limit != nil {
		limit = *p.Limit
	}
	page, more, next := pageChangedFiles(filesOut, offset, limit, "changes")
	return protocol.WorkspaceChangesResult{Files: page, GitAvailable: gitErr == nil, GitBranch: branch, HasMore: more, NextCursor: next}, nil
}

func (s *Server) workspaceChangeDetail(p protocol.WorkspaceChangeDetailParams) (protocol.WorkspaceChangeDetailResult, error) {
	sess, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch)
	if err != nil {
		return protocol.WorkspaceChangeDetailResult{}, err
	}
	rel := normalizeGitPath(p.Path)
	if rel == "" {
		return protocol.WorkspaceChangeDetailResult{}, protocol.MustRemoteError(protocol.ErrPathNotFound, protocol.ErrorOptions{Target: &p.Target})
	}
	if _, err := files.ResolveRel(s.opts.Workspace, rel); err != nil {
		return protocol.WorkspaceChangeDetailResult{}, protocol.MustRemoteError(protocol.ErrPathNotFound, protocol.ErrorOptions{Target: &p.Target})
	}

	if entries, _, gitErr := s.gitStatus(); gitErr == nil {
		for i := range entries {
			if entries[i].path != rel {
				continue
			}
			if detail, found := s.gitWorkspaceChangeDetail(rel, entries[i]); found {
				return detail, nil
			}
			break
		}
	}
	if controller, ok := sess.ctrl.(interface {
		CheckpointFileState(string) (checkpoint.FileState, bool)
	}); ok {
		if state, found := controller.CheckpointFileState(rel); found {
			return s.checkpointWorkspaceChangeDetail(rel, state.Content, protocol.ChangeSession)
		}
	}
	return protocol.WorkspaceChangeDetailResult{}, nil
}

func (s *Server) gitWorkspaceChangeDetail(rel string, entry runtimeGitStatus) (protocol.WorkspaceChangeDetailResult, bool) {
	if entry.status == "??" || !s.gitHasHead() {
		detail, err := s.checkpointWorkspaceChangeDetail(rel, nil, protocol.ChangeGit)
		return detail, err == nil
	}
	args := []string{"diff", "--no-ext-diff", "--no-textconv", "--relative", "HEAD", "--", filepath.FromSlash(rel)}
	if entry.oldPath != "" && entry.oldPath != rel {
		args = append(args, filepath.FromSlash(entry.oldPath))
	}
	raw, truncated, err := s.gitOutputBounded(protocol.GitPatchBytes, args...)
	if err != nil {
		return protocol.WorkspaceChangeDetailResult{}, false
	}
	source := protocol.ChangeGit
	if truncated {
		return protocol.WorkspaceChangeDetailResult{Source: &source, Truncated: true}, true
	}
	patch := strings.TrimSpace(string(raw))
	if patch == "" {
		return protocol.WorkspaceChangeDetailResult{}, false
	}
	added, removed := tallyRuntimeUnifiedPatch(patch)
	binary := strings.Contains(patch, "Binary files ") || strings.Contains(patch, "GIT binary patch")
	return protocol.WorkspaceChangeDetailResult{
		Diff: &patch, Source: &source, Added: added, Removed: removed, Binary: binary,
	}, true
}

func (s *Server) gitHasHead() bool {
	_, err := s.gitOutput(1024, "rev-parse", "--verify", "HEAD")
	return err == nil
}

func (s *Server) checkpointWorkspaceChangeDetail(rel string, old *string, source protocol.ChangeSource) (protocol.WorkspaceChangeDetailResult, error) {
	if old != nil && len(*old) > protocol.GitPatchBytes {
		return protocol.WorkspaceChangeDetailResult{Source: &source, Truncated: true}, nil
	}
	oldText := ""
	if old != nil {
		oldText = *old
	}
	newText, exists, truncated, err := runtimeWorkspaceText(s.opts.Workspace, rel, protocol.GitPatchBytes)
	if err != nil {
		return protocol.WorkspaceChangeDetailResult{}, err
	}
	if truncated {
		return protocol.WorkspaceChangeDetailResult{Source: &source, Truncated: true}, nil
	}
	kind := diff.Modify
	if old == nil {
		kind = diff.Create
	} else if !exists {
		kind = diff.Delete
	}
	change := diff.Build(rel, oldText, newText, kind)
	if len(change.Diff) > protocol.GitPatchBytes {
		return protocol.WorkspaceChangeDetailResult{Source: &source, Truncated: true}, nil
	}
	if change.Diff == "" && !change.Binary {
		return protocol.WorkspaceChangeDetailResult{Source: &source}, nil
	}
	patch := change.Diff
	return protocol.WorkspaceChangeDetailResult{
		Diff: &patch, Source: &source, Added: change.Added, Removed: change.Removed, Binary: change.Binary,
	}, nil
}

func runtimeWorkspaceText(workspace, rel string, limit int64) (string, bool, bool, error) {
	path, err := files.ResolveRel(workspace, rel)
	if err != nil {
		return "", false, false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	if !info.Mode().IsRegular() {
		return "", true, false, fmt.Errorf("workspace change path %q is not a regular file", rel)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", true, false, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", true, false, err
	}
	if int64(len(raw)) > limit {
		return "", true, true, nil
	}
	decoded := fileencoding.DecodeToUTF8(raw)
	if int64(len(decoded)) > limit {
		return "", true, true, nil
	}
	return string(decoded), true, false, nil
}

func tallyRuntimeUnifiedPatch(patch string) (added, removed int) {
	inHunk := false
	for _, line := range strings.Split(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
		case inHunk && strings.HasPrefix(line, "+"):
			added++
		case inHunk && strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}

func (s *Server) gitStatus() ([]runtimeGitStatus, string, error) {
	prefixRaw, err := s.gitOutput(protocol.FrameBytes, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, "", err
	}
	prefix := filepath.ToSlash(strings.TrimSpace(string(prefixRaw)))
	raw, err := s.gitOutput(protocol.FrameBytes, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".")
	if err != nil {
		return nil, "", err
	}
	parts := bytes.Split(raw, []byte{0})
	entries := make([]runtimeGitStatus, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) < 4 {
			continue
		}
		status := string(part[:2])
		entry := runtimeGitStatus{status: strings.TrimSpace(status), path: trimGitPrefix(prefix, string(part[3:]))}
		if strings.ContainsAny(status, "RC") && i+1 < len(parts) {
			i++
			entry.oldPath = trimGitPrefix(prefix, string(parts[i]))
		}
		if entry.path != "" {
			entries = append(entries, entry)
		}
	}
	branchRaw, branchErr := s.gitOutput(1024, "branch", "--show-current")
	branch := strings.TrimSpace(string(branchRaw))
	if branchErr == nil && branch == "" {
		if short, shortErr := s.gitOutput(1024, "rev-parse", "--short", "HEAD"); shortErr == nil {
			branch = "@" + strings.TrimSpace(string(short))
		}
	}
	return entries, branch, nil
}

func (s *Server) gitHistory(p protocol.GitHistoryParams) (protocol.GitHistoryResult, error) {
	if _, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch); err != nil {
		return protocol.GitHistoryResult{}, err
	}
	args := []string{"log", "--pretty=format:%H%x00%an%x00%aI%x00%s", "-z", "-n", "101"}
	if p.Path != "" {
		args = append(args, "--", filepath.FromSlash(p.Path))
	}
	raw, err := s.gitOutput(protocol.FrameBytes, args...)
	if err != nil {
		return protocol.GitHistoryResult{}, protocol.MustRemoteError(protocol.ErrGitUnavailable, protocol.ErrorOptions{Target: &p.Target})
	}
	parts := bytes.Split(raw, []byte{0})
	commits := make([]protocol.GitCommit, 0, protocol.GitHistoryCommits+1)
	for i := 0; i+3 < len(parts); i += 4 {
		if len(parts[i]) == 0 {
			continue
		}
		commits = append(commits, protocol.GitCommit{Hash: string(parts[i]), Author: string(parts[i+1]), Date: string(parts[i+2]), Message: string(parts[i+3])})
	}
	truncated := len(commits) > protocol.GitHistoryCommits
	if truncated {
		commits = commits[:protocol.GitHistoryCommits]
	}
	result := protocol.GitHistoryResult{Commits: commits, Truncated: truncated, ReturnedItems: len(commits)}
	if truncated {
		result.TruncationReason = protocol.GitHistoryLimit
	}
	return result, nil
}

func (s *Server) gitCommitDetail(p protocol.GitCommitDetailParams) (protocol.GitCommitDetailResult, error) {
	if _, err := s.sessionForQuery(p.ExpectedHostEpoch, p.Target, p.ExpectedRuntimeEpoch); err != nil {
		return protocol.GitCommitDetailResult{}, err
	}
	if p.Path != "" {
		raw, err := s.gitOutput(protocol.FrameBytes, "show", "--relative", "--pretty=format:", "--patch", p.Hash, "--", filepath.FromSlash(p.Path))
		if err != nil {
			return protocol.GitCommitDetailResult{}, protocol.MustRemoteError(protocol.ErrGitObjectNotFound, protocol.ErrorOptions{Target: &p.Target})
		}
		size := int64(len(raw))
		returned := size
		truncated := false
		if len(raw) > protocol.GitPatchBytes {
			raw = raw[:protocol.GitPatchBytes]
			returned = int64(len(raw))
			truncated = true
		}
		body := string(raw)
		result := protocol.GitCommitDetailResult{Kind: protocol.GitDetailPatch, Path: p.Path, Body: &body, SizeBytes: &size, ReturnedBytes: &returned, Truncated: &truncated}
		if truncated {
			result.TruncationReason = protocol.ByteLimit
		}
		return result, nil
	}
	raw, err := s.gitOutput(protocol.FrameBytes, "diff-tree", "--root", "--relative", "--no-commit-id", "--name-status", "-r", "-z", p.Hash, "--", ".")
	if err != nil {
		return protocol.GitCommitDetailResult{}, protocol.MustRemoteError(protocol.ErrGitObjectNotFound, protocol.ErrorOptions{Target: &p.Target})
	}
	filesOut := parseGitCommitFiles(raw)
	offset, cursorErr := pageOffset(p.Cursor, "commit")
	if cursorErr != nil {
		return protocol.GitCommitDetailResult{}, protocol.MustRemoteError(protocol.ErrStaleCursor, protocol.ErrorOptions{})
	}
	limit := protocol.PageDefaultItems
	if p.Limit != nil {
		limit = *p.Limit
	}
	if offset > len(filesOut) {
		offset = len(filesOut)
	}
	end := min(offset+limit, len(filesOut))
	page := append([]protocol.GitCommitFile{}, filesOut[offset:end]...)
	more := end < len(filesOut)
	next := protocol.Cursor("")
	if more {
		next = protocol.Cursor("commit:" + fmt.Sprint(end))
	}
	return protocol.GitCommitDetailResult{Kind: protocol.GitDetailFiles, Files: &page, HasMore: &more, NextCursor: next}, nil
}

func (s *Server) gitOutput(limit int64, args ...string) ([]byte, error) {
	raw, truncated, err := s.gitOutputBounded(limit, args...)
	if err != nil {
		return nil, err
	}
	if truncated {
		return nil, errors.New("git output exceeded protocol limit")
	}
	return raw, nil
}

func (s *Server) gitOutputBounded(limit int64, args ...string) ([]byte, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fullArgs := append([]string{"-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-c", "core.quotepath=false", "-C", s.opts.Workspace}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	proc.HideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	if err := cmd.Start(); err != nil {
		return nil, false, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(stdout, limit+1))
	if int64(len(raw)) > limit {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, true, nil
	}
	waitErr := cmd.Wait()
	if readErr != nil {
		return nil, false, readErr
	}
	if waitErr != nil {
		return nil, false, waitErr
	}
	return raw, false, nil
}

func normalizeGitPath(path string) string {
	path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if path == "." || path == ".." || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
		return ""
	}
	return path
}

func normalizeWorkspacePath(root, path string) string {
	path = strings.TrimSpace(path)
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return ""
		}
		path = rel
	}
	return normalizeGitPath(path)
}

func trimGitPrefix(prefix, path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if prefix != "" {
		if !strings.HasPrefix(path, prefix) {
			return ""
		}
		path = strings.TrimPrefix(path, prefix)
	}
	return normalizeGitPath(path)
}

func pageOffset(cursor protocol.Cursor, kind string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	prefix := kind + ":"
	if !strings.HasPrefix(string(cursor), prefix) {
		return 0, errors.New("invalid cursor")
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(string(cursor), prefix))
	if err != nil || offset < 0 {
		return 0, errors.New("invalid cursor")
	}
	return offset, nil
}

func pageChangedFiles(all []protocol.ChangedFile, offset, limit int, kind string) ([]protocol.ChangedFile, bool, protocol.Cursor) {
	if offset > len(all) {
		offset = len(all)
	}
	end := min(offset+limit, len(all))
	page := append([]protocol.ChangedFile{}, all[offset:end]...)
	if end == len(all) {
		return page, false, ""
	}
	return page, true, protocol.Cursor(kind + ":" + fmt.Sprint(end))
}

func parseGitCommitFiles(raw []byte) []protocol.GitCommitFile {
	parts := bytes.Split(raw, []byte{0})
	out := make([]protocol.GitCommitFile, 0, len(parts)/2)
	for i := 0; i+1 < len(parts); {
		status := string(parts[i])
		i++
		if status == "" || i >= len(parts) {
			continue
		}
		oldPath := ""
		path := string(parts[i])
		i++
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			if i >= len(parts) {
				break
			}
			oldPath, path = path, string(parts[i])
			i++
		}
		path = normalizeGitPath(path)
		if path == "" {
			continue
		}
		out = append(out, protocol.GitCommitFile{Path: path, OldPath: normalizeGitPath(oldPath), Status: status})
	}
	return out
}
