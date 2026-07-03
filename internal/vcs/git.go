package vcs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/proc"
)

// LoadGitInfo returns the current git status for the repo rooted at cwd.
func LoadGitInfo(ctx context.Context, cwd string) (VCSInfo, error) {
	root, err := gitRun(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return VCSInfo{}, err
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return VCSInfo{}, errors.New("empty git root")
	}

	info := VCSInfo{Type: "git", Repo: filepath.Base(root)}

	if branch, err := gitRun(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil && strings.TrimSpace(branch) != "" {
		info.Branch = strings.TrimSpace(branch)
	} else if sha, err := gitRun(ctx, root, "rev-parse", "--short", "HEAD"); err == nil && strings.TrimSpace(sha) != "" {
		info.Branch = strings.TrimSpace(sha)
		info.Detached = true
	} else if ref, err := gitRun(ctx, root, "symbolic-ref", "--short", "HEAD"); err == nil && strings.TrimSpace(ref) != "" {
		info.Branch = strings.TrimSpace(ref)
	}
	if info.Branch == "" {
		info.Branch = "HEAD"
		info.Detached = true
	}

	if out, err := gitRun(ctx, root, "diff", "--numstat", "HEAD", "--"); err == nil {
		info.Added, info.Removed = ParseGitNumstat(out)
	}
	if out, err := gitRun(ctx, root, "status", "--porcelain=v1", "--untracked-files=normal"); err == nil {
		info.Untracked = CountGitUntracked(out)
	}
	return info, nil
}

// GitFileStatus returns the file-level status for the git repo at cwd.
func GitFileStatus(ctx context.Context, cwd string) ([]VCSFileStatus, error) {
	raw, err := gitRunWithTimeout(ctx, cwd, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	topRaw, err := gitRunWithTimeout(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	repoRoot := strings.TrimSpace(string(topRaw))
	if repoRoot == "" {
		return nil, nil
	}
	entries := ParseGitPorcelainZ([]byte(raw))
	out := make([]VCSFileStatus, 0, len(entries))
	for _, e := range entries {
		path := gitRelPath(repoRoot, cwd, e.Path)
		if path == "" {
			continue
		}
		oldPath := gitRelPath(repoRoot, cwd, e.OldPath)
		out = append(out, VCSFileStatus{Path: path, OldPath: oldPath, Status: e.Status})
	}
	return out, nil
}

// GitListBranches returns all local git branches.
func GitListBranches(ctx context.Context, cwd string) ([]string, error) {
	raw, err := gitRunWithTimeout(ctx, cwd, "branch", "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	return splitLines(raw), nil
}

// GitCheckout switches to the given branch.
func GitCheckout(ctx context.Context, cwd, branch string) error {
	raw, err := gitRunWithTimeoutCombined(ctx, cwd, "checkout", branch)
	if err != nil {
		if len(raw) > 0 {
			return errors.New("git checkout: " + strings.TrimSpace(string(raw)))
		}
		return err
	}
	return nil
}

// GitHistory returns up to limit commits, optionally filtered to a file path.
func GitHistory(ctx context.Context, cwd, path string, limit int) ([]VCSCommit, error) {
	args := []string{"log", "--pretty=format:%H%x00%an%x00%ad%x00%s", "-z", "-n", strconv.Itoa(limit)}
	if path != "" {
		args = append(args, "--", path)
	}
	raw, err := gitRunWithTimeout(ctx, cwd, args...)
	if err != nil {
		return nil, err
	}
	parts := bytes.Split([]byte(raw), []byte{0})
	var out []VCSCommit
	for i := 0; i+3 < len(parts); i += 4 {
		out = append(out, VCSCommit{
			Hash:    string(parts[i]),
			Author:  string(parts[i+1]),
			Date:    string(parts[i+2]),
			Message: string(parts[i+3]),
		})
	}
	return out, nil
}

// GitCommitDetail returns diff or file list for a single commit.
func GitCommitDetail(ctx context.Context, cwd, hash, path string) (VCSCommitDetail, error) {
	if path != "" {
		raw, err := gitRunWithTimeout(ctx, cwd, "show", "--relative", "--pretty=format:", "--patch", hash, "--", path)
		if err != nil {
			return VCSCommitDetail{}, err
		}
		diffStr := strings.TrimSpace(string(raw))
		return VCSCommitDetail{Diff: &diffStr}, nil
	}
	raw, err := gitRunWithTimeout(ctx, cwd, "diff-tree", "--relative", "--no-commit-id", "--name-only", "-r", hash)
	if err != nil {
		return VCSCommitDetail{}, err
	}
	return VCSCommitDetail{Files: splitLines(raw)}, nil
}

// --- internal helpers ---

func gitRun(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-c", "maintenance.auto=false"}, args...)...)
	proc.HideWindow(cmd)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func gitRunWithTimeout(ctx context.Context, cwd string, args ...string) (string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	return gitRun(ctx, cwd, args...)
}

func gitRunWithTimeoutCombined(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.fsmonitor=false", "-c", "maintenance.auto=false"}, args...)...)
	proc.HideWindow(cmd)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	return cmd.CombinedOutput()
}

// ParseGitPorcelainZ parses git status --porcelain=v1 -z output.
func ParseGitPorcelainZ(raw []byte) []VCSFileStatus {
	parts := bytes.Split(raw, []byte{0})
	out := make([]VCSFileStatus, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) < 4 {
			continue
		}
		status := string(part[:2])
		path := string(part[3:])
		entry := VCSFileStatus{Path: path, Status: strings.TrimSpace(status)}
		if strings.ContainsAny(status, "RC") && i+1 < len(parts) {
			i++
			entry.OldPath = string(parts[i])
		}
		out = append(out, entry)
	}
	return out
}

// ParseGitNumstat parses git diff --numstat output.
func ParseGitNumstat(out string) (added int, removed int) {
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] != "-" {
			if n, err := strconv.Atoi(fields[0]); err == nil {
				added += n
			}
		}
		if fields[1] != "-" {
			if n, err := strconv.Atoi(fields[1]); err == nil {
				removed += n
			}
		}
	}
	return added, removed
}

// CountGitUntracked counts untracked files from git status --porcelain output.
func CountGitUntracked(out string) int {
	n := 0
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, "?? ") {
			n++
		}
	}
	return n
}

func gitRelPath(repoRoot, base, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, filepath.FromSlash(path))
	}
	return normalizeRelPath(base, path)
}

func normalizeRelPath(base, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		if rel, err := filepath.Rel(base, path); err == nil {
			path = rel
		}
	}
	path = filepath.Clean(path)
	if path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(path)
}

func splitLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
