package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/secrets"
)

const gitStatusTimeout = 700 * time.Millisecond

type gitStatus struct {
	Repo      string
	Branch    string
	Detached  bool
	Added     int
	Removed   int
	Untracked int
}

func fetchGitStatus() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
		defer cancel()
		status, err := loadGitStatus(ctx, "")
		if err != nil {
			return gitStatusMsg{}
		}
		return gitStatusMsg{status: status}
	}
}

func loadGitStatus(ctx context.Context, cwd string) (gitStatus, error) {
	root, err := runGit(ctx, cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return gitStatus{}, err
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return gitStatus{}, errors.New("empty git root")
	}

	status := gitStatus{Repo: filepath.Base(root)}
	if branch, err := runGit(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil && strings.TrimSpace(branch) != "" {
		status.Branch = strings.TrimSpace(branch)
	} else if sha, err := runGit(ctx, root, "rev-parse", "--short", "HEAD"); err == nil && strings.TrimSpace(sha) != "" {
		status.Branch = strings.TrimSpace(sha)
		status.Detached = true
	} else if ref, err := runGit(ctx, root, "symbolic-ref", "--short", "HEAD"); err == nil && strings.TrimSpace(ref) != "" {
		status.Branch = strings.TrimSpace(ref)
	}
	if status.Branch == "" {
		status.Branch = "HEAD"
		status.Detached = true
	}

	if out, err := runGit(ctx, root, "diff", "--numstat", "HEAD", "--"); err == nil {
		status.Added, status.Removed = parseGitNumstat(out)
	}
	if out, err := runGit(ctx, root, "status", "--porcelain=v1", "--untracked-files=normal"); err == nil {
		status.Untracked = countUntracked(out)
	}
	return status, nil
}

func runGit(ctx context.Context, cwd string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = append(secrets.ProcessEnv(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseGitNumstat(out string) (added int, removed int) {
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

func countUntracked(out string) int {
	n := 0
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, "?? ") {
			n++
		}
	}
	return n
}

func (m chatTUI) gitTag() string {
	if strings.TrimSpace(m.gitStatus.Repo) == "" || strings.TrimSpace(m.gitStatus.Branch) == "" {
		return ""
	}
	return m.gitStatus.render(themeFg(m.statusModeColor(), m.gitStatus.Repo), m.gitStatus.Branch)
}

var (
	statusAutoColor  = cliColor{"#f59e0b", 214}
	statusPlanColor  = cliColor{"#2563eb", 27}
	statusYoloColor  = cliColor{"#e5484d", 167}
	statusShellColor = cliColor{"#16a34a", 71} // green — shell mode indicator
)

func (m chatTUI) statusModeColor() cliColor {
	switch {
	case m.ctrl != nil && m.ctrl.AutoApproveTools():
		return statusYoloColor
	case m.planMode:
		return statusPlanColor
	default:
		return statusAutoColor
	}
}

func (s gitStatus) Render() string {
	return s.RenderRepo(accent(s.Repo))
}

func (s gitStatus) RenderRepo(repo string) string {
	if strings.TrimSpace(s.Repo) == "" || strings.TrimSpace(s.Branch) == "" {
		return ""
	}
	return s.render(repo, s.Branch)
}

func (s gitStatus) RenderWithin(maxWidth int, repoColor cliColor) string {
	if strings.TrimSpace(s.Repo) == "" || strings.TrimSpace(s.Branch) == "" {
		return ""
	}
	repo, branch := s.compactIdentity(maxWidth)
	out := s.render(themeFg(repoColor, repo), branch)
	if maxWidth > 0 && visibleWidth(out) > maxWidth {
		return ansi.Truncate(out, maxWidth, "…")
	}
	return out
}

func (s gitStatus) compactIdentity(maxWidth int) (repo, branch string) {
	repo = strings.TrimSpace(s.Repo)
	branch = strings.TrimSpace(s.Branch)
	if maxWidth <= 0 {
		return repo, branch
	}
	dirtyWidth := visibleWidth(s.dirtyPlain())
	nameBudget := maxWidth - dirtyWidth - visibleWidth("@")
	if nameBudget <= 2 {
		return compactEnd(repo, max(1, nameBudget)), ""
	}
	repoWidth := visibleWidth(repo)
	branchWidth := visibleWidth(branch)
	if repoWidth+branchWidth <= nameBudget {
		return repo, branch
	}

	minRepo := min(repoWidth, 8)
	if repoBudget := nameBudget - branchWidth; repoBudget >= minRepo {
		return compactMiddle(repo, repoBudget), branch
	}

	repoBudget := min(repoWidth, max(4, min(10, nameBudget/3)))
	if nameBudget-repoBudget < 8 {
		repoBudget = max(1, nameBudget-8)
	}
	branchBudget := max(1, nameBudget-repoBudget)
	return compactMiddle(repo, repoBudget), compactMiddle(branch, branchBudget)
}

func (s gitStatus) dirtyPlain() string {
	var parts []string
	if s.Added > 0 || s.Removed > 0 {
		parts = append(parts, fmt.Sprintf("+%d", s.Added), fmt.Sprintf("-%d", s.Removed))
	}
	if s.Untracked > 0 {
		parts = append(parts, fmt.Sprintf("?%d", s.Untracked))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, " ") + ")"
}

func (s gitStatus) render(repo, branch string) string {
	var b strings.Builder
	b.WriteString(repo)
	b.WriteString(dim("@"))
	if s.Detached {
		b.WriteString(yellow(branch))
	} else {
		b.WriteString(green(branch))
	}

	var parts []string
	if s.Added > 0 || s.Removed > 0 {
		parts = append(parts, green(fmt.Sprintf("+%d", s.Added)), red(fmt.Sprintf("-%d", s.Removed)))
	}
	if s.Untracked > 0 {
		parts = append(parts, yellow(fmt.Sprintf("?%d", s.Untracked)))
	}
	if len(parts) > 0 {
		b.WriteString(dim(" ("))
		b.WriteString(strings.Join(parts, " "))
		b.WriteString(dim(")"))
	}
	return b.String()
}
