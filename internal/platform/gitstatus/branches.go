package gitstatus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// Branch is one local branch of the work tree's repository, as a branch menu
// lists it.
type Branch struct {
	Name string `json:"name"`
	// Current is the branch HEAD names. A detached HEAD marks none — there is
	// no branch to mark, and the summary's short SHA is the fact to show.
	Current bool `json:"current,omitempty"`
	// Worktree names a linked worktree of the same repository that holds this
	// branch checked out. Git refuses to check the branch out elsewhere while
	// that tree lives, so a menu can close the row before the switch fails.
	Worktree string `json:"worktree,omitempty"`
}

// InvalidNameError is a request naming nothing git could take for a branch.
// It carries the name, because the answer that helps says which part was wrong.
type InvalidNameError struct{ Name string }

func (e *InvalidNameError) Error() string {
	return fmt.Sprintf("gitstatus: %q is not a usable branch name", e.Name)
}

var (
	// ErrBranchUnknown is a name that is not one of the local branches.
	ErrBranchUnknown = errors.New("gitstatus: no local branch with that name")
	// ErrLocalChanges is uncommitted work the switch would overwrite.
	ErrLocalChanges = errors.New("gitstatus: uncommitted changes would be overwritten by the switch")
	// ErrBranchBusy is a branch another linked worktree holds checked out.
	ErrBranchBusy = errors.New("gitstatus: that branch is checked out in another worktree")
	// ErrSwitchFailed is git refusing for a reason only its own words describe.
	ErrSwitchFailed = errors.New("gitstatus: git refused to switch branches")
)

// Branches lists the repository's local branches, alphabetical, the current one
// marked. ok is false for a workspace that is not version controlled, as for
// Summary — a fallback signal and not a failure.
func Branches(ctx context.Context, repo gitcmd.Repo) (list []Branch, ok bool, err error) {
	if !repo.Valid() {
		return nil, false, nil
	}
	raw, err := repo.Top().Command(ctx, "for-each-ref",
		"--format=%(refname:lstrip=2)%00%(HEAD)%00%(worktreepath)",
		"--sort=refname", "refs/heads").Output()
	if err != nil {
		return nil, false, err
	}
	list = []Branch{}
	for line := range bytes.SplitSeq(raw, []byte{'\n'}) {
		fields := bytes.SplitN(line, []byte{0}, 3)
		if len(fields) < 2 || len(fields[0]) == 0 {
			continue
		}
		b := Branch{Name: string(fields[0]), Current: bytes.Equal(bytes.TrimSpace(fields[1]), []byte("*"))}
		if len(fields) == 3 {
			// The work tree this repo was resolved for is not a blocker for
			// itself, and Windows spells one path in more than one case.
			if at := strings.TrimSpace(string(fields[2])); at != "" && !sameTree(at, repo.WorkTree) {
				b.Worktree = at
			}
		}
		list = append(list, b)
	}
	return list, true, nil
}

// SwitchBranch checks out the named local branch. The refusals are decided
// structurally, from facts the tree states outright: git localizes its own
// messages, so what the switch prints is only ever a generic refusal's detail,
// never the classifier. Hooks and signing never run — the same contract
// gitcommit.Commit keeps for writes.
func SwitchBranch(ctx context.Context, repo gitcmd.Repo, name string) error {
	if !repo.Valid() {
		return fmt.Errorf("%w: %s", gitcmd.ErrNotRepository, repo.Dir)
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, " \t\r\n\x00") {
		return &InvalidNameError{Name: name}
	}
	locals, _, err := Branches(ctx, repo)
	if err != nil {
		return err
	}
	var target Branch
	found := false
	for _, b := range locals {
		if b.Name == name {
			target, found = b, true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrBranchUnknown, name)
	}
	// Another worktree holds it: git would refuse, and the fact is already on
	// the list — no need to hear it from stderr.
	if target.Worktree != "" {
		return fmt.Errorf("%w: %s holds %s", ErrBranchBusy, target.Worktree, name)
	}
	// The branch HEAD is on: nothing to move, and git answers this as success.
	if target.Current {
		return nil
	}
	if err := preFlight(ctx, repo, name); err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd := repo.Top().Command(ctx, "switch", "--no-guess", "--no-overwrite-ignore", "--", name)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %s", ErrSwitchFailed, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// preFlight protects tracked work and untracked or ignored paths the target
// would overwrite, including file/directory replacements. Unrelated local
// paths ride along with the switch.
func preFlight(ctx context.Context, repo gitcmd.Repo, name string) error {
	raw, err := repo.Top().Command(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored=matching").Output()
	if err != nil {
		return err
	}
	var untracked []Change
	for _, c := range ParsePorcelainZ(raw) {
		if c.Status != "??" && c.Status != "!!" {
			return fmt.Errorf("%w: %s has uncommitted changes; commit or stash first", ErrLocalChanges, c.Path)
		}
		untracked = append(untracked, c)
	}
	if len(untracked) == 0 {
		return nil
	}
	tree, err := repo.Top().Command(ctx, "ls-tree", "-r", "--name-only", "-z", "refs/heads/"+name, "--").Output()
	if err != nil {
		return err
	}
	held := map[string]bool{}
	directories := map[string]bool{}
	for p := range bytes.SplitSeq(tree, []byte{0}) {
		if len(p) > 0 {
			path := string(p)
			held[path] = true
			for i := strings.LastIndexByte(path, '/'); i >= 0; i = strings.LastIndexByte(path[:i], '/') {
				directories[path[:i]] = true
			}
		}
	}
	for _, change := range untracked {
		p := change.Path
		if change.Status == "!!" && strings.HasSuffix(p, "/") && !held[strings.TrimSuffix(p, "/")] {
			collision, err := ignoredTreeCollision(repo.WorkTree, p, held)
			if err != nil {
				return err
			}
			if collision == "" {
				continue
			}
			return fmt.Errorf("%w: %s is ignored and %s would overwrite it", ErrLocalChanges, collision, name)
		}
		path := strings.TrimSuffix(p, "/")
		collision := directories[path]
		for {
			if collision || held[path] {
				return fmt.Errorf("%w: %s is untracked or ignored and %s would overwrite it", ErrLocalChanges, p, name)
			}
			i := strings.LastIndexByte(path, '/')
			if i < 0 {
				break
			}
			path = path[:i]
		}
	}
	return nil
}

// Matching ignores collapse whole directories; inspect only target paths
// beneath them so an unrelated ignored sibling never blocks a switch.
func ignoredTreeCollision(root, directory string, held map[string]bool) (string, error) {
	for target := range held {
		if !strings.HasPrefix(target, directory) {
			continue
		}
		path := strings.TrimSuffix(directory, "/")
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return path, nil
		}
		for part := range strings.SplitSeq(strings.TrimPrefix(target, directory), "/") {
			path += "/" + part
			info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return "", err
			}
			if path == target || !info.IsDir() {
				return path, nil
			}
		}
	}
	return "", nil
}

// sameTree compares two spellings of one directory: Clean for the separators a
// client may have trimmed differently, symlink-resolved because macOS spells
// one directory /var and /private/var, and on Windows the case git and Go do
// not agree on.
func sameTree(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	if real, err := filepath.EvalSymlinks(a); err == nil {
		a = real
	}
	if real, err := filepath.EvalSymlinks(b); err == nil {
		b = real
	}
	if a == b {
		return true
	}
	return runtime.GOOS == "windows" && strings.EqualFold(filepath.ToSlash(a), filepath.ToSlash(b))
}
