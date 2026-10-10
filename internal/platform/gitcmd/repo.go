package gitcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotRepository reports that a directory is not inside a git work tree, or
// that a Repo was used before one was resolved.
var ErrNotRepository = errors.New("gitcmd: not inside a git work tree")

// Repo is a repository identity resolved once, when a workspace is opened.
// Commands run through it name the git dir, common dir and work tree
// explicitly, so files the workspace gains later cannot change which
// repository, or whose configuration, git reads.
type Repo struct {
	Dir       string `json:"dir"` // where commands run; relative pathspecs start here
	GitDir    string `json:"gitDir"`
	CommonDir string `json:"commonDir"`
	WorkTree  string `json:"workTree"`
}

// Open resolves the repository whose work tree contains dir. A directory
// outside any work tree, bare repositories included, is ErrNotRepository.
func Open(ctx context.Context, dir string) (Repo, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Repo{}, err
	}
	out, err := Command(ctx, abs, "rev-parse", "--absolute-git-dir", "--git-common-dir", "--show-toplevel").Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return Repo{}, fmt.Errorf("%w: %s", ErrNotRepository, abs)
	}
	if err != nil {
		return Repo{}, err
	}
	lines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	if len(lines) != 3 {
		return Repo{}, fmt.Errorf("%w: %s: unexpected rev-parse output %q", ErrNotRepository, abs, out)
	}
	// Git before 2.31 has no --path-format, so spelling is normalised here.
	resolve := func(p string) string {
		p = filepath.FromSlash(strings.TrimRight(p, "\r"))
		if !filepath.IsAbs(p) {
			p = filepath.Join(abs, p)
		}
		if real, err := filepath.EvalSymlinks(p); err == nil {
			p = real
		}
		return filepath.Clean(p)
	}
	repo := Repo{Dir: abs, GitDir: resolve(lines[0]), CommonDir: resolve(lines[1]), WorkTree: resolve(lines[2])}
	if err := repo.selfConsistent(); err != nil {
		return Repo{}, fmt.Errorf("%w: %s: %w", ErrNotRepository, abs, err)
	}
	return repo, nil
}

// Verify checks a Repo recorded earlier against the files as they stand: the
// same test Open applies, so a stored identity names only its own checkout.
func (r Repo) Verify() error {
	if !r.Valid() {
		return ErrNotRepository
	}
	if err := r.selfConsistent(); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrNotRepository, r.Dir, err)
	}
	return nil
}

// selfConsistent accepts only a git dir that belongs to the work tree holding
// Dir: the work tree's own .git directory, or a linked worktree's admin dir
// under the common dir whose gitdir file names that work tree's .git. A .git
// file or core.worktree pointing elsewhere names another checkout's content.
func (r Repo) selfConsistent() error {
	if !within(r.WorkTree, r.Dir) {
		return errors.New("work tree does not contain the directory")
	}
	dotGit := filepath.Join(r.WorkTree, ".git")
	if sameFile(r.GitDir, dotGit) {
		if !sameFile(r.CommonDir, r.GitDir) {
			return errors.New("git dir shares another repository's common dir")
		}
		return nil
	}
	if !sameFile(filepath.Dir(r.GitDir), filepath.Join(r.CommonDir, "worktrees")) {
		return errors.New("git dir belongs to another checkout")
	}
	raw, err := os.ReadFile(filepath.Join(r.GitDir, "gitdir"))
	if err != nil {
		return err
	}
	back := filepath.FromSlash(strings.TrimSpace(string(raw)))
	if !filepath.IsAbs(back) {
		back = filepath.Join(r.GitDir, back)
	}
	if !sameFile(back, dotGit) {
		return errors.New("linked worktree admin dir names another checkout")
	}
	return nil
}

func sameFile(a, b string) bool {
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	return err == nil && os.SameFile(left, right)
}

// within reports whether dir is root or lies under it, symlinks resolved.
func within(root, dir string) bool {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realRoot, realDir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Valid reports whether r names a resolved repository.
func (r Repo) Valid() bool { return r.GitDir != "" && r.CommonDir != "" && r.WorkTree != "" }

// Top is r with commands running from the work tree's root.
func (r Repo) Top() Repo {
	r.Dir = r.WorkTree
	return r
}

// Command is gitcmd.Command against r's resolved identity. An unresolved Repo
// yields a command that fails with ErrNotRepository.
func (r Repo) Command(ctx context.Context, args ...string) *exec.Cmd {
	return r.CommandWithConfig(ctx, nil, args...)
}

// CommandWithConfig is gitcmd.CommandWithConfig against r's resolved identity.
func (r Repo) CommandWithConfig(ctx context.Context, extraConfig []string, args ...string) *exec.Cmd {
	if !r.Valid() {
		if ctx == nil {
			ctx = context.Background()
		}
		screened, refused := screen(args)
		cmd := newCommand(ctx, Args(r.Dir, extraConfig, screened...), nil)
		cmd.Err = errors.Join(ErrNotRepository, refused)
		return cmd
	}
	return build(ctx, r.Dir, r.env(), extraConfig, args)
}

// env pins the repository: GIT_COMMON_DIR outranks a commondir file, and an
// explicit GIT_DIR is never swapped for a nested or implicit-bare one.
func (r Repo) env() []string {
	return []string{"GIT_DIR=" + r.GitDir, "GIT_COMMON_DIR=" + r.CommonDir, "GIT_WORK_TREE=" + r.WorkTree}
}
