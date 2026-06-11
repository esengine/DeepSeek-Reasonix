package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"reasonix/internal/proc"
)

type gitStatusEntry struct {
	Path           string
	OldPath        string
	Status         string
	IndexStatus    string
	WorktreeStatus string
}

type workspaceChangeAccumulator struct {
	view       WorkspaceChangeView
	hasSession bool
	hasGit     bool
}

func (a *App) WorkspaceChanges() WorkspaceChangesView {
	out := WorkspaceChangesView{GitAvailable: true}
	base, err := a.activeWorkspaceBase()
	if err != nil {
		out.GitAvailable = false
		out.GitErr = err.Error()
		return out
	}

	out.GitBranch = workspaceGitBranch(base)

	changes := map[string]*workspaceChangeAccumulator{}
	add := func(path string) *workspaceChangeAccumulator {
		path = normalizeWorkspaceRelPath(base, path)
		if path == "" {
			return nil
		}
		if changes[path] == nil {
			changes[path] = &workspaceChangeAccumulator{view: WorkspaceChangeView{Path: path}}
		}
		return changes[path]
	}

	a.mu.RLock()
	ctrl := a.activeCtrlLocked()
	a.mu.RUnlock()
	if ctrl != nil {
		for _, meta := range ctrl.Checkpoints() {
			for _, path := range meta.Paths {
				acc := add(path)
				if acc == nil {
					continue
				}
				acc.hasSession = true
				if len(acc.view.Turns) == 0 || acc.view.Turns[len(acc.view.Turns)-1] != meta.Turn {
					acc.view.Turns = append(acc.view.Turns, meta.Turn)
				}
				if meta.Time.UnixMilli() >= acc.view.LatestTime {
					acc.view.LatestPrompt = meta.Prompt
					acc.view.LatestTime = meta.Time.UnixMilli()
				}
			}
		}
	}

	gitEntries, gitErr := workspaceGitStatus(base)
	if gitErr != nil {
		out.GitAvailable = false
		out.GitErr = gitErr.Error()
	}
	for _, entry := range gitEntries {
		acc := add(entry.Path)
		if acc == nil {
			continue
		}
		acc.hasGit = true
		acc.view.GitStatus = entry.Status
		acc.view.GitIndexStatus = entry.IndexStatus
		acc.view.GitWorktreeStatus = entry.WorktreeStatus
		acc.view.OldPath = normalizeWorkspaceRelPath(base, entry.OldPath)
	}

	out.Files = make([]WorkspaceChangeView, 0, len(changes))
	for _, acc := range changes {
		if acc.hasSession {
			acc.view.Sources = append(acc.view.Sources, "session")
		}
		if acc.hasGit {
			acc.view.Sources = append(acc.view.Sources, "git")
		}
		out.Files = append(out.Files, acc.view)
	}
	sort.Slice(out.Files, func(i, j int) bool {
		a, b := out.Files[i], out.Files[j]
		if len(a.Sources) != len(b.Sources) {
			return len(a.Sources) > len(b.Sources)
		}
		return strings.ToLower(a.Path) < strings.ToLower(b.Path)
	})
	return out
}

// workspaceGit builds a console-hidden git probe: CREATE_NO_WINDOW so git's own
// children inherit the invisible console, fsmonitor/auto-maintenance off so a
// probe never spawns a background daemon that opens a console of its own (#3906).
func workspaceGit(args ...string) *exec.Cmd {
	cmd := exec.Command("git", append([]string{"-c", "core.fsmonitor=false", "-c", "maintenance.auto=false"}, args...)...)
	proc.HideWindow(cmd)
	return cmd
}

func workspaceGitStatus(base string) ([]gitStatusEntry, error) {
	cmd := workspaceGit("-C", base, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	entries := parseGitStatusPorcelainZ(raw)
	topCmd := workspaceGit("-C", base, "rev-parse", "--show-toplevel")
	topRaw, err := topCmd.Output()
	if err != nil {
		return nil, err
	}
	repoRoot := strings.TrimSpace(string(topRaw))
	if repoRoot == "" {
		return entries, nil
	}
	out := make([]gitStatusEntry, 0, len(entries))
	for _, entry := range entries {
		entry.Path = workspaceRelPathFromGitStatus(repoRoot, base, entry.Path)
		if entry.Path == "" {
			continue
		}
		entry.OldPath = workspaceRelPathFromGitStatus(repoRoot, base, entry.OldPath)
		out = append(out, entry)
	}
	return out, nil
}

func parseGitStatusPorcelainZ(raw []byte) []gitStatusEntry {
	parts := bytes.Split(raw, []byte{0})
	out := make([]gitStatusEntry, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if len(part) < 4 {
			continue
		}
		status := string(part[:2])
		path := string(part[3:])
		entry := gitStatusEntry{
			Path:           path,
			Status:         strings.TrimSpace(status),
			IndexStatus:    strings.TrimSpace(status[:1]),
			WorktreeStatus: strings.TrimSpace(status[1:2]),
		}
		if strings.ContainsAny(status, "RC") && i+1 < len(parts) {
			i++
			entry.OldPath = string(parts[i])
		}
		out = append(out, entry)
	}
	return out
}

func normalizeWorkspaceRelPath(base, path string) string {
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

func workspaceRelPathFromGitStatus(repoRoot, base, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, filepath.FromSlash(path))
	}
	return normalizeWorkspaceRelPath(base, path)
}

// workspaceGitBranch returns the current git branch name for the repo rooted
// at base, or an empty string when base is not inside a git repository or when
// git is unavailable.
func workspaceGitBranch(base string) string {
	if branch := workspaceGitCurrentBranch(base); branch != "" {
		return branch
	}

	headCmd := exec.Command("git", "-C", base, "rev-parse", "--short", "HEAD")
	proc.HideWindowDetached(headCmd)
	raw, err := headCmd.Output()
	if err != nil {
		return ""
	}
	short := strings.TrimSpace(string(raw))
	if short == "" {
		return ""
	}
	return "@" + short
}

func workspaceGitCurrentBranch(base string) string {
	cmd := exec.Command("git", "-C", base, "branch", "--show-current")
	proc.HideWindowDetached(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// GitBranches returns all local git branches for the active workspace's repo.
func (a *App) GitBranches() ([]string, error) {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil, err
	}
	cmd := workspaceGit("-C", base, "branch", "--format=%(refname:short)")
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	branches := strings.FieldsFunc(strings.TrimSpace(string(raw)), func(r rune) bool { return r == '\n' })
	return branches, nil
}

// GitCheckout switches the active workspace's git branch and returns the
// current branch name, or an error when git is unavailable.
func (a *App) GitCheckout(branch string) error {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return err
	}
	cmd := workspaceGit("-C", base, "checkout", branch)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return fmt.Errorf("git checkout: %s", strings.TrimSpace(string(out)))
		}
		return err
	}
	return nil
}

type GitCommitView struct {
	Hash    string `json:"hash"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
}

type GitCommitDetailView struct {
	Diff  *string  `json:"diff,omitempty"`
	Files []string `json:"files,omitempty"`
}

type WorkspaceGitDiffView struct {
	Path string `json:"path"`
	Diff string `json:"diff"`
	Err  string `json:"err,omitempty"`
}

func (a *App) WorkspaceGitDiff(path string, staged bool) WorkspaceGitDiffView {
	out := WorkspaceGitDiffView{Path: path}
	base, err := a.activeWorkspaceBase()
	if err != nil {
		out.Err = err.Error()
		return out
	}
	rel := normalizeWorkspaceRelPath(base, path)
	if rel == "" {
		out.Err = "invalid path"
		return out
	}
	out.Path = rel

	if !staged && workspaceGitPathIsUntracked(base, rel) {
		abs, ok, err := a.workspacePath(rel)
		if err != nil || !ok {
			out.Err = "invalid path"
			return out
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			out.Err = err.Error()
			return out
		}
		out.Diff = workspaceNewFileDiff(rel, data)
		return out
	}

	args := []string{"-C", base, "diff", "--no-ext-diff", "--no-color", "--relative"}
	if staged {
		args = append(args, "--cached")
	}
	args = append(args, "--", rel)
	cmd := exec.Command("git", args...)
	proc.HideWindowDetached(cmd)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		out.Err = strings.TrimSpace(string(raw))
		if out.Err == "" {
			out.Err = err.Error()
		}
		return out
	}
	out.Diff = strings.TrimSpace(string(raw))
	return out
}

func workspaceGitPathIsUntracked(base string, path string) bool {
	cmd := exec.Command("git", "-C", base, "ls-files", "--others", "--exclude-standard", "--", path)
	proc.HideWindowDetached(cmd)
	raw, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if filepath.ToSlash(strings.TrimSpace(line)) == path {
			return true
		}
	}
	return false
}

func workspaceNewFileDiff(path string, data []byte) string {
	if bytes.Contains(data, []byte{0}) {
		return fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\nBinary files /dev/null and b/%s differ", path, path, path, path)
	}
	body := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	hasTrailingNewline := strings.HasSuffix(body, "\n")
	lines := strings.Split(body, "\n")
	if body == "" {
		lines = nil
	}
	if hasTrailingNewline && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("diff --git a/%s b/%s\nnew file mode 100644\n--- /dev/null\n+++ b/%s\n", path, path, path))
	b.WriteString(fmt.Sprintf("@@ -0,0 +1,%d @@\n", len(lines)))
	for _, line := range lines {
		b.WriteString("+")
		b.WriteString(line)
		b.WriteString("\n")
	}
	if !hasTrailingNewline && len(lines) > 0 {
		b.WriteString("\\ No newline at end of file\n")
	}
	return strings.TrimSpace(b.String())
}

func (a *App) WorkspaceGitStage(path string) error {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return err
	}
	rel := normalizeWorkspaceRelPath(base, path)
	if rel == "" {
		return fmt.Errorf("invalid path")
	}
	return workspaceRunGit(base, "add", "--", rel)
}

func (a *App) WorkspaceGitUnstage(path string) error {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return err
	}
	rel := normalizeWorkspaceRelPath(base, path)
	if rel == "" {
		return fmt.Errorf("invalid path")
	}
	return workspaceRunGit(base, "reset", "HEAD", "--", rel)
}

func (a *App) WorkspaceGitStageAll() error {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return err
	}
	return workspaceRunGit(base, "add", "-A")
}

func (a *App) WorkspaceGitUnstageAll() error {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return err
	}
	return workspaceRunGit(base, "reset", "HEAD", "--")
}

func (a *App) WorkspaceGitCommit(message string, push bool, branch string) error {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return fmt.Errorf("commit message is required")
	}
	branch = strings.TrimSpace(branch)
	currentBranch := workspaceGitCurrentBranch(base)
	if branch != "" && branch != currentBranch {
		if err := workspaceRunGit(base, "check-ref-format", "refs/heads/"+branch); err != nil {
			return fmt.Errorf("invalid branch name: %q", branch)
		}
		exists, err := workspaceGitLocalBranchExists(base, branch)
		if err != nil {
			return err
		}
		if exists {
			if err := workspaceRunGit(base, "checkout", branch); err != nil {
				return err
			}
		} else if err := workspaceRunGit(base, "checkout", "-b", branch); err != nil {
			return err
		}
	}
	if err := workspaceRunGit(base, "commit", "-m", message); err != nil {
		return err
	}
	if push {
		pushBranch := branch
		if pushBranch == "" {
			pushBranch = workspaceGitCurrentBranch(base)
		}
		if pushBranch != "" {
			return workspaceRunGit(base, "push", "-u", "origin", pushBranch)
		}
		return workspaceRunGit(base, "push")
	}
	return nil
}

func workspaceGitLocalBranchExists(base string, branch string) (bool, error) {
	cmd := exec.Command("git", "-C", base, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	proc.HideWindowDetached(cmd)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

func workspaceRunGit(base string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", base}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
	)
	proc.HideWindowDetached(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return fmt.Errorf("%s", msg)
	}
	return err
}

func (a *App) WorkspaceGitHistory(path string) ([]GitCommitView, error) {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return nil, err
	}

	args := []string{"-C", base, "log", "--pretty=format:%H%x00%an%x00%ad%x00%s", "-z", "-n", "100"}
	if path != "" {
		args = append(args, "--", path)
	}

	cmd := workspaceGit(args...)
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	parts := bytes.Split(raw, []byte{0})
	var out []GitCommitView
	// 4 parts per commit: hash, author, date, message
	for i := 0; i+3 < len(parts); i += 4 {
		out = append(out, GitCommitView{
			Hash:    string(parts[i]),
			Author:  string(parts[i+1]),
			Date:    string(parts[i+2]),
			Message: string(parts[i+3]),
		})
	}
	return out, nil
}

func (a *App) WorkspaceGitCommitDetail(hash string, path string) (GitCommitDetailView, error) {
	base, err := a.activeWorkspaceBase()
	if err != nil {
		return GitCommitDetailView{}, err
	}

	if path != "" {
		// Single file diff
		cmd := workspaceGit("-C", base, "show", "--relative", "--pretty=format:", "--patch", hash, "--", path)
		raw, err := cmd.Output()
		if err != nil {
			return GitCommitDetailView{}, err
		}
		diffStr := strings.TrimSpace(string(raw))
		return GitCommitDetailView{Diff: &diffStr}, nil
	}

	// Project level: list of files changed
	cmd := workspaceGit("-C", base, "diff-tree", "--relative", "--no-commit-id", "--name-only", "-r", hash)
	raw, err := cmd.Output()
	if err != nil {
		return GitCommitDetailView{}, err
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var files []string
	for _, line := range lines {
		if line != "" {
			files = append(files, line)
		}
	}
	return GitCommitDetailView{Files: files}, nil
}
