package vcs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/proc"
)

// LoadJJInfo returns the current jj status for the repo rooted at cwd.
func LoadJJInfo(ctx context.Context, cwd string) (VCSInfo, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}

	root, err := jjRun(ctx, cwd, "workspace", "root")
	if err != nil {
		return VCSInfo{}, err
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return VCSInfo{}, errors.New("empty jj root")
	}

	info := VCSInfo{Type: "jj", Repo: filepath.Base(root)}

	// Current bookmark(s) attached to @ or its parent.
	// `jj bookmark list` alone returns all bookmarks, so taking the first
	// one can show an unrelated bookmark. Querying @ first covers pure jj
	// repos; falling back to @- covers git-backed repos where jj git init
	// creates a working-copy child commit that never carries bookmarks.
	branchName := func(rev string) string {
		out, err := jjRun(ctx, cwd,
			"log", "-r", rev, "--no-graph",
			"-T", `local_bookmarks.map(|b| b.name()).join("\n") ++ "\n"`,
		)
		if err != nil {
			return ""
		}
		out = strings.TrimSpace(out)
		if idx := strings.IndexByte(out, '\n'); idx != -1 {
			out = out[:idx]
		}
		return out
	}
	if b := branchName("@"); b != "" {
		info.Branch = b
	} else if b := branchName("@-"); b != "" {
		info.Branch = b
	}

	// Fallback: short commit id (equivalent to detached HEAD)
	if info.Branch == "" {
		if sha, err := jjRun(ctx, cwd,
			"log", "-r", "@",
			"-T", `commit_id.short() ++ "\n"`,
		); err == nil && strings.TrimSpace(sha) != "" {
			info.Branch = strings.TrimSpace(sha)
			info.Detached = true
		}
	}
	if info.Branch == "" {
		info.Branch = "HEAD"
		info.Detached = true
	}

	// Parse jj diff --stat for added/removed lines.
	// jj diff --stat outputs a histogram per file, ending with:
	//   "N files changed, M insertions(+), K deletions(-)"
	if out, err := jjRun(ctx, cwd, "diff", "--stat"); err == nil {
		info.Added, info.Removed = parseJJDiffStat(out)
	}
	// jj has no git-style untracked concept; Untracked stays 0.

	return info, nil
}

// JJFileStatus returns the file-level status for the jj repo at cwd.
func JJFileStatus(ctx context.Context, cwd string) ([]VCSFileStatus, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}

	raw, err := jjRun(ctx, cwd, "diff", "--summary")
	if err != nil {
		return nil, err
	}
	entries := parseJJSummary(raw)
	out := make([]VCSFileStatus, 0, len(entries))
	for _, e := range entries {
		path := normalizeRelPath(cwd, e.Path)
		if path == "" {
			continue
		}
		oldPath := normalizeRelPath(cwd, e.OldPath)
		out = append(out, VCSFileStatus{Path: path, OldPath: oldPath, Status: e.Status})
	}
	return out, nil
}

// JJListBranches returns all jj bookmarks.
func JJListBranches(ctx context.Context, cwd string) ([]string, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}

	raw, err := jjRun(ctx, cwd,
		"bookmark", "list",
		"-T", `name ++ "\n"`,
	)
	if err != nil {
		return nil, err
	}
	return splitLines(raw), nil
}

// JJCheckout edits the given bookmark (equivalent to git checkout).
func JJCheckout(ctx context.Context, cwd, bookmark string) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}

	raw, err := jjRunCombined(ctx, cwd, "edit", bookmark)
	if err != nil {
		msg := strings.TrimSpace(string(raw))
		if msg != "" {
			return errors.New("jj edit: " + msg)
		}
		return err
	}
	return nil
}

// JJHistory returns up to limit commits, optionally filtered to a file path.
func JJHistory(ctx context.Context, cwd, path string, limit int) ([]VCSCommit, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}

	template := `commit_id ++ "\t" ++ author.name() ++ "\t" ++ author.timestamp() ++ "\t" ++ description.first_line() ++ "\n"`
	args := []string{"log", "--no-graph", "-T", template, "--limit", strconv.Itoa(limit)}
	if path != "" {
		args = append(args, "--", path)
	}
	raw, err := jjRun(ctx, cwd, args...)
	if err != nil {
		return nil, err
	}
	return parseJJLog(raw), nil
}

// JJCommitDetail returns diff or file list for a single commit.
func JJCommitDetail(ctx context.Context, cwd, changeID, path string) (VCSCommitDetail, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
	}

	if path != "" {
		raw, err := jjRun(ctx, cwd, "diff", "--from", changeID+"-", "--to", changeID, "--", path)
		if err != nil {
			return VCSCommitDetail{}, err
		}
		diffStr := strings.TrimSpace(string(raw))
		return VCSCommitDetail{Diff: &diffStr}, nil
	}

	raw, err := jjRun(ctx, cwd, "diff", "--from", changeID+"-", "--to", changeID, "--summary")
	if err != nil {
		return VCSCommitDetail{}, err
	}
	entries := parseJJSummary(raw)
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Path != "" {
			files = append(files, e.Path)
		}
	}
	return VCSCommitDetail{Files: files}, nil
}

// --- internal helpers ---

func jjRun(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "jj", args...)
	proc.HideWindow(cmd)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(cmd.Environ(), fmt.Sprintf("JJ_CONFIG=%s", os.DevNull))
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func jjRunCombined(ctx context.Context, cwd string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "jj", args...)
	proc.HideWindow(cmd)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(cmd.Environ(), fmt.Sprintf("JJ_CONFIG=%s", os.DevNull))
	return cmd.CombinedOutput()
}

// parseJJDiffStat parses the summary line of jj diff --stat.
// Looks for: "N files changed, M insertions(+), K deletions(-)"
func parseJJDiffStat(out string) (added int, removed int) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, " file") || !strings.Contains(line, "changed") {
			continue
		}
		// Parse insertions
		if idx := strings.Index(line, " insertion"); idx != -1 {
			start := idx - 1
			for start >= 0 && line[start] >= '0' && line[start] <= '9' {
				start--
			}
			start++
			if start < idx {
				if n, err := strconv.Atoi(line[start:idx]); err == nil {
					added = n
				}
			}
		}
		// Parse deletions
		if idx := strings.Index(line, " deletion"); idx != -1 {
			start := idx - 1
			for start >= 0 && line[start] >= '0' && line[start] <= '9' {
				start--
			}
			start++
			if start < idx {
				if n, err := strconv.Atoi(line[start:idx]); err == nil {
					removed = n
				}
			}
		}
		break
	}
	return added, removed
}

// parseJJSummary parses the output of jj diff --summary.
// Lines look like: "M file.txt" or "A new.txt" or "D old.txt" or "R {old => new}"
func parseJJSummary(raw string) []VCSFileStatus {
	var out []VCSFileStatus
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if len(line) < 2 || line[1] != ' ' {
			continue
		}
		code := line[0]
		path := strings.TrimSpace(line[2:])
		switch code {
		case 'M':
			out = append(out, VCSFileStatus{Path: path, Status: "Modified"})
		case 'A':
			out = append(out, VCSFileStatus{Path: path, Status: "Added"})
		case 'D':
			out = append(out, VCSFileStatus{Path: path, Status: "Deleted"})
		case 'R':
			// "R {old => new}" or "R old => new"
			oldPath, newPath := parseJJRename(path)
			out = append(out, VCSFileStatus{Path: newPath, OldPath: oldPath, Status: "Renamed"})
		case '?':
			out = append(out, VCSFileStatus{Path: path, Status: "Untracked"})
		}
	}
	return out
}

func parseJJRename(s string) (oldPath, newPath string) {
	// Handle "{old => new}" format
	if strings.HasPrefix(s, "{") {
		s = strings.TrimPrefix(s, "{")
		s = strings.TrimSuffix(s, "}")
		if idx := strings.Index(s, " => "); idx != -1 {
			return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+4:])
		}
		return s, s
	}
	// Handle "old => new" format
	if idx := strings.Index(s, " => "); idx != -1 {
		return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+4:])
	}
	return s, s
}

// parseJJLog parses the tab-delimited output of jj log with our template.
func parseJJLog(raw string) []VCSCommit {
	var out []VCSCommit
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		out = append(out, VCSCommit{
			Hash:    parts[0],
			Author:  parts[1],
			Date:    parts[2],
			Message: parts[3],
		})
	}
	return out
}
