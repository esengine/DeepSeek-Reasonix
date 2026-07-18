package runtimeservice

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"reasonix/internal/proc"
	"reasonix/internal/runtimeapi"
)

var fullGitHash = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func (s *FileGitService) gitCommand(ctx context.Context, args ...string) *exec.Cmd {
	base := []string{"-c", "core.fsmonitor=false", "-c", "maintenance.auto=false", "-c", "color.ui=false", "-C", s.root}
	cmd := exec.CommandContext(ctx, s.gitBinary, append(base, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	proc.HideWindow(cmd)
	return cmd
}

func (s *FileGitService) gitOutput(ctx context.Context, args ...string) ([]byte, error) {
	output, err := s.gitCommand(ctx, args...).Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return nil, ErrGitUnavailable
		}
		return nil, ErrQueryFailed
	}
	return output, nil
}

func (s *FileGitService) ensureGitRepository(ctx context.Context) error {
	raw, err := s.gitOutput(ctx, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(string(raw)) != "true" {
		return ErrGitUnavailable
	}
	return nil
}

func (s *FileGitService) ensureCommit(ctx context.Context, hash string) error {
	if !fullGitHash.MatchString(hash) {
		return ErrGitObjectNotFound
	}
	if err := s.ensureGitRepository(ctx); err != nil {
		return err
	}
	if _, err := s.gitOutput(ctx, "cat-file", "-e", strings.ToLower(hash)+"^{commit}"); err != nil {
		return ErrGitObjectNotFound
	}
	return nil
}

func literalPathspec(rel string) string { return ":(literal)" + rel }

func (s *FileGitService) GitHistory(ctx context.Context, input runtimeapi.GitHistoryInput) (runtimeapi.GitHistoryResult, error) {
	result := runtimeapi.GitHistoryResult{Commits: []runtimeapi.GitCommit{}}
	if err := requireSession(input.Session); err != nil {
		return result, err
	}
	rel, err := normalizeRelative(input.Path, true)
	if err != nil {
		return result, err
	}
	if err := s.ensureGitRepository(ctx); err != nil {
		return result, err
	}
	args := []string{"log", "-z", "--format=%H%x00%an%x00%aI%x00%s", "-n", strconv.Itoa(runtimeapi.GitHistoryCommits + 1)}
	if rel != "" {
		args = append(args, "--", literalPathspec(rel))
	} else {
		args = append(args, "--", ".")
	}
	raw, err := s.gitOutput(ctx, args...)
	if err != nil {
		return result, ErrQueryFailed
	}
	parts := bytes.Split(raw, []byte{0})
	for i := 0; i+3 < len(parts); i += 4 {
		hash := strings.TrimSpace(string(parts[i]))
		author := strings.ToValidUTF8(string(parts[i+1]), "�")
		date := string(parts[i+2])
		message := strings.ToValidUTF8(string(parts[i+3]), "�")
		if !fullGitHash.MatchString(hash) || strings.TrimSpace(author) == "" {
			return runtimeapi.GitHistoryResult{Commits: []runtimeapi.GitCommit{}}, ErrQueryFailed
		}
		if _, err := time.Parse(time.RFC3339, date); err != nil {
			return runtimeapi.GitHistoryResult{Commits: []runtimeapi.GitCommit{}}, ErrQueryFailed
		}
		result.Commits = append(result.Commits, runtimeapi.GitCommit{
			Hash: strings.ToLower(hash), Author: author, Date: date, Message: message,
		})
	}
	if len(result.Commits) > runtimeapi.GitHistoryCommits {
		result.Commits = result.Commits[:runtimeapi.GitHistoryCommits]
		result.Truncated = true
		result.TruncationReason = runtimeapi.GitHistoryLimit
	}
	result.ReturnedItems = len(result.Commits)
	return result, nil
}

func (s *FileGitService) GitCommitDetail(ctx context.Context, input runtimeapi.GitCommitDetailInput) (runtimeapi.GitCommitDetail, error) {
	if err := requireSession(input.Session); err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	if err := input.Validate(); err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	hash := strings.ToLower(input.Hash)
	if err := s.ensureCommit(ctx, hash); err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	if input.Path != "" {
		rel, err := normalizeRelative(input.Path, false)
		if err != nil {
			return runtimeapi.GitCommitDetail{}, err
		}
		return s.gitPatch(ctx, hash, rel)
	}
	limit, err := normalizedPageLimit(input.Limit)
	if err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	files, err := s.gitCommitFiles(ctx, hash)
	if err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	revision := snapshotRevision(files, "git/commitDetail", hash)
	session := sessionBinding(input.Session)
	offset, err := s.pageOffset(input.Cursor, "git/commitDetail", session, hash, revision, len(files))
	if err != nil {
		return runtimeapi.GitCommitDetail{}, err
	}
	end := offset + limit
	if end > len(files) {
		end = len(files)
	}
	page := append([]runtimeapi.GitCommitFile(nil), files[offset:end]...)
	hasMore := end < len(files)
	result := runtimeapi.GitCommitDetail{Kind: runtimeapi.GitDetailFiles, Files: &page, HasMore: &hasMore}
	if hasMore {
		result.Next = s.encodeCursor(cursorPayload{
			Method: "git/commitDetail", Session: session, Filter: hash,
			Revision: revision, Offset: end,
		})
	}
	return result, result.Validate()
}

type gitNameStatus struct {
	path    string
	oldPath string
	status  string
}

type gitNumstat struct {
	path      string
	oldPath   string
	additions int
	deletions int
}

func (s *FileGitService) gitCommitFiles(ctx context.Context, hash string) ([]runtimeapi.GitCommitFile, error) {
	common := []string{"diff-tree", "--root", "--no-commit-id", "-r", "-M", "--relative"}
	nameRaw, err := s.gitOutput(ctx, append(append([]string{}, common...), "--name-status", "-z", hash, "--", ".")...)
	if err != nil {
		return nil, ErrQueryFailed
	}
	numRaw, err := s.gitOutput(ctx, append(append([]string{}, common...), "--numstat", "-z", hash, "--", ".")...)
	if err != nil {
		return nil, ErrQueryFailed
	}
	names, err := parseNameStatus(nameRaw)
	if err != nil {
		return nil, err
	}
	counts, err := parseNumstat(numRaw)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]gitNumstat, len(counts))
	for _, count := range counts {
		byPath[count.path] = count
	}
	files := make([]runtimeapi.GitCommitFile, 0, len(names))
	for _, name := range names {
		rel, err := normalizeRelative(name.path, false)
		if err != nil {
			return nil, ErrQueryFailed
		}
		oldPath := ""
		if name.oldPath != "" {
			oldPath, err = normalizeRelative(name.oldPath, false)
			if err != nil {
				return nil, ErrQueryFailed
			}
		}
		count := byPath[name.path]
		files = append(files, runtimeapi.GitCommitFile{
			Path: rel, OldPath: oldPath, Status: name.status,
			Additions: count.additions, Deletions: count.deletions,
		})
	}
	sort.Slice(files, func(i, j int) bool {
		left, right := strings.ToLower(files[i].Path), strings.ToLower(files[j].Path)
		if left != right {
			return left < right
		}
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func parseNameStatus(raw []byte) ([]gitNameStatus, error) {
	parts := bytes.Split(raw, []byte{0})
	result := make([]gitNameStatus, 0, len(parts)/2)
	for i := 0; i < len(parts); {
		if len(parts[i]) == 0 {
			i++
			continue
		}
		status := string(parts[i])
		i++
		if i >= len(parts) || len(parts[i]) == 0 {
			return nil, ErrQueryFailed
		}
		entry := gitNameStatus{status: status}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			entry.oldPath = string(parts[i])
			i++
			if i >= len(parts) || len(parts[i]) == 0 {
				return nil, ErrQueryFailed
			}
		}
		entry.path = string(parts[i])
		i++
		result = append(result, entry)
	}
	return result, nil
}

func parseNumstat(raw []byte) ([]gitNumstat, error) {
	parts := bytes.Split(raw, []byte{0})
	result := make([]gitNumstat, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		if len(parts[i]) == 0 {
			continue
		}
		fields := strings.SplitN(string(parts[i]), "\t", 3)
		if len(fields) != 3 {
			return nil, ErrQueryFailed
		}
		additions, err := gitCount(fields[0])
		if err != nil {
			return nil, err
		}
		deletions, err := gitCount(fields[1])
		if err != nil {
			return nil, err
		}
		entry := gitNumstat{additions: additions, deletions: deletions}
		if fields[2] == "" {
			if i+2 >= len(parts) || len(parts[i+1]) == 0 || len(parts[i+2]) == 0 {
				return nil, ErrQueryFailed
			}
			entry.oldPath, entry.path = string(parts[i+1]), string(parts[i+2])
			i += 2
		} else {
			entry.path = fields[2]
		}
		result = append(result, entry)
	}
	return result, nil
}

func gitCount(value string) (int, error) {
	if value == "-" {
		return 0, nil
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		return 0, ErrQueryFailed
	}
	return count, nil
}

func (s *FileGitService) gitPatch(ctx context.Context, hash, rel string) (runtimeapi.GitCommitDetail, error) {
	cmd := s.gitCommand(ctx, "show", "--format=", "--no-color", "--no-ext-diff", "--relative", "--patch", hash, "--", literalPathspec(rel))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return runtimeapi.GitCommitDetail{}, ErrQueryFailed
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return runtimeapi.GitCommitDetail{}, ErrGitUnavailable
		}
		return runtimeapi.GitCommitDetail{}, ErrQueryFailed
	}
	stored := make([]byte, 0, runtimeapi.GitPatchBytes)
	buffer := make([]byte, 32<<10)
	var total int64
	for {
		n, readErr := stdout.Read(buffer)
		if n > 0 {
			total += int64(n)
			remaining := runtimeapi.GitPatchBytes - len(stored)
			if remaining > 0 {
				if n < remaining {
					remaining = n
				}
				stored = append(stored, buffer[:remaining]...)
			}
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				_ = cmd.Wait()
				return runtimeapi.GitCommitDetail{}, ErrQueryFailed
			}
			break
		}
	}
	if err := cmd.Wait(); err != nil {
		return runtimeapi.GitCommitDetail{}, ErrQueryFailed
	}
	textBytes := stored
	if total > int64(len(stored)) {
		textBytes = trimUTF8Boundary(stored)
	}
	body := string(textBytes)
	if !utf8.ValidString(body) {
		body = strings.ToValidUTF8(body, "�")
	}
	returned := int64(len(textBytes))
	truncated := total > returned
	result := runtimeapi.GitCommitDetail{
		Kind: runtimeapi.GitDetailPatch, Path: rel, Body: &body,
		SizeBytes: &total, ReturnedBytes: &returned, Truncated: &truncated,
	}
	if truncated {
		result.TruncationReason = runtimeapi.ByteLimit
	}
	return result, result.Validate()
}

func (s *FileGitService) gitBranch(ctx context.Context) string {
	raw, err := s.gitOutput(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err == nil {
		if branch := strings.TrimSpace(string(raw)); branch != "" {
			return branch
		}
	}
	raw, err = s.gitOutput(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	short := strings.TrimSpace(string(raw))
	if short == "" {
		return ""
	}
	return "@" + short
}

func (s *FileGitService) gitStatus(ctx context.Context) ([]gitNameStatus, error) {
	if err := s.ensureGitRepository(ctx); err != nil {
		return nil, err
	}
	prefixRaw, err := s.gitOutput(ctx, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, ErrGitUnavailable
	}
	prefix := strings.TrimSpace(string(prefixRaw))
	raw, err := s.gitOutput(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".")
	if err != nil {
		return nil, ErrGitUnavailable
	}
	parts := bytes.Split(raw, []byte{0})
	result := make([]gitNameStatus, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) == 0 {
			continue
		}
		if len(part) < 4 || part[2] != ' ' {
			return nil, ErrGitUnavailable
		}
		status := strings.TrimSpace(string(part[:2]))
		entry := gitNameStatus{status: status, path: primaryGitPath(prefix, string(part[3:]))}
		if strings.ContainsAny(status, "RC") {
			if i+1 >= len(parts) || len(parts[i+1]) == 0 {
				return nil, ErrGitUnavailable
			}
			i++
			entry.oldPath = primaryGitPath(prefix, string(parts[i]))
		}
		if entry.path != "" {
			result = append(result, entry)
		}
	}
	return result, nil
}

func primaryGitPath(prefix, gitPath string) string {
	prefix = strings.Trim(filepathSlash(prefix), "/")
	gitPath = strings.Trim(filepathSlash(gitPath), "/")
	if gitPath == "" {
		return ""
	}
	if prefix == "" {
		return gitPath
	}
	if gitPath == prefix {
		return ""
	}
	if !strings.HasPrefix(gitPath, prefix+"/") {
		return ""
	}
	return strings.TrimPrefix(gitPath, prefix+"/")
}

func filepathSlash(value string) string { return strings.ReplaceAll(value, "\\", "/") }
