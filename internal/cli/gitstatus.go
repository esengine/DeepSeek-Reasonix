package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"reasonix/internal/secrets"
	"reasonix/internal/vcs"
)

const gitStatusTimeout = 700 * time.Millisecond

// gitStatus keeps the TUI rendering methods while sharing VCSInfo fields.
type gitStatus vcs.VCSInfo

func fetchGitStatus() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitStatusTimeout)
		defer cancel()
		status, err := loadVCSStatus(ctx, "")
		if err != nil {
			return gitStatusMsg{}
		}
		return gitStatusMsg{status: status}
	}
}

func loadVCSStatus(ctx context.Context, cwd string) (gitStatus, error) {
	t := vcs.DetectVCS(cwd)
	switch t {
	case "jj":
		info, err := vcs.LoadJJInfo(ctx, cwd)
		return gitStatus(info), err
	case "git":
		info, err := vcs.LoadGitInfo(ctx, cwd)
		return gitStatus(info), err
	default:
		return gitStatus{}, nil
	}
}

// runGit is a convenience wrapper for git commands used outside VCS abstraction.
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

func (m chatTUI) gitTag() string {
	if strings.TrimSpace(m.gitStatus.Repo) == "" || strings.TrimSpace(m.gitStatus.Branch) == "" {
		return ""
	}
	return vcsRender(m.gitStatus, themeFg(m.statusModeColor(), m.gitStatus.Repo), m.gitStatus.Branch)
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

func vcsRender(s gitStatus, repo, branch string) string {
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

func vcsRenderFull(s gitStatus) string {
	return vcsRenderRepo(s, accent(s.Repo))
}

func (s gitStatus) Render() string {
	return vcsRenderFull(s)
}

func (s gitStatus) RenderRepo(repo string) string {
	return vcsRenderRepo(s, repo)
}

func (s gitStatus) RenderWithin(maxWidth int, repoColor cliColor) string {
	return vcsRenderWithin(s, maxWidth, repoColor)
}

func vcsRenderRepo(s gitStatus, repo string) string {
	if strings.TrimSpace(s.Repo) == "" || strings.TrimSpace(s.Branch) == "" {
		return ""
	}
	return vcsRender(s, repo, s.Branch)
}

func vcsRenderWithin(s gitStatus, maxWidth int, repoColor cliColor) string {
	if strings.TrimSpace(s.Repo) == "" || strings.TrimSpace(s.Branch) == "" {
		return ""
	}
	repo, branch := vcsCompactIdentity(s, maxWidth)
	out := vcsRender(s, themeFg(repoColor, repo), branch)
	if maxWidth > 0 && visibleWidth(out) > maxWidth {
		return ansi.Truncate(out, maxWidth, "…")
	}
	return out
}

func vcsCompactIdentity(s gitStatus, maxWidth int) (repo, branch string) {
	repo = strings.TrimSpace(s.Repo)
	branch = strings.TrimSpace(s.Branch)
	if maxWidth <= 0 {
		return repo, branch
	}
	dirtyWidth := visibleWidth(vcsDirtyPlain(s))
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

func vcsDirtyPlain(s gitStatus) string {
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
