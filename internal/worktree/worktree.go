// Package worktree creates durable, Git-backed workspaces for parallel
// Delivery sessions. Worktrees live under Reasonix-managed state, never inside
// the source repository, and are never deleted automatically.
package worktree

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"reasonix/internal/proc"
)

const (
	gitProbeTimeout       = 15 * time.Second
	gitWorktreeAddTimeout = 5 * time.Minute
)

// Availability describes whether a project can be isolated with Git worktree.
type Availability struct {
	Available   bool   `json:"available"`
	Reason      string `json:"reason,omitempty"`
	RepoRoot    string `json:"repoRoot,omitempty"`
	Branch      string `json:"branch,omitempty"`
	SourceDirty bool   `json:"sourceDirty,omitempty"`
}

// Result identifies one newly created isolated Delivery workspace.
type Result struct {
	WorkspaceRoot string `json:"workspaceRoot"`
	WorktreeRoot  string `json:"worktreeRoot"`
	SourceRoot    string `json:"sourceRoot"`
	Branch        string `json:"branch"`
	Head          string `json:"head"`
	SourceDirty   bool   `json:"sourceDirty"`
}

type inspection struct {
	Availability
	head      string
	prefix    string
	commonDir string
}

// Inspect checks Git and repository prerequisites without changing state.
func Inspect(ctx context.Context, workspaceRoot string) Availability {
	info, err := inspect(ctx, workspaceRoot)
	if err != nil {
		return Availability{Available: false, Reason: err.Error()}
	}
	return info.Availability
}

// Create makes a new branch and linked worktree at managedRoot, based on the
// source repository's committed HEAD. Uncommitted source changes are reported
// but never copied or modified. When workspaceRoot names a repository
// subdirectory, Result.WorkspaceRoot points at the corresponding subdirectory
// in the new worktree.
func Create(ctx context.Context, workspaceRoot, managedRoot string) (Result, error) {
	info, err := inspect(ctx, workspaceRoot)
	if err != nil {
		return Result{}, err
	}
	managedRoot = strings.TrimSpace(managedRoot)
	if managedRoot == "" {
		return Result{}, errors.New("Reasonix worktree storage is unavailable")
	}
	if err := os.MkdirAll(managedRoot, 0o700); err != nil {
		return Result{}, fmt.Errorf("create Reasonix worktree storage: %w", err)
	}

	repoSum := sha256.Sum256([]byte(info.commonDir))
	repoKey := hex.EncodeToString(repoSum[:8])
	repoBase := safePathComponent(filepath.Base(info.RepoRoot))
	if repoBase == "" {
		repoBase = "repository"
	}

	for attempt := 0; attempt < 5; attempt++ {
		id, randomErr := randomID()
		if randomErr != nil {
			return Result{}, randomErr
		}
		branch := fmt.Sprintf("reasonix/delivery-%s-%s", time.Now().Format("20060102-150405"), id)
		worktreeRoot := filepath.Join(managedRoot, repoKey, id, repoBase)
		if _, statErr := os.Stat(worktreeRoot); statErr == nil {
			continue
		} else if !os.IsNotExist(statErr) {
			return Result{}, fmt.Errorf("inspect worktree destination: %w", statErr)
		}
		if err := os.MkdirAll(filepath.Dir(worktreeRoot), 0o700); err != nil {
			return Result{}, fmt.Errorf("create worktree parent: %w", err)
		}

		_, stderr, addErr := runGit(ctx, info.RepoRoot, "worktree", "add", "-b", branch, worktreeRoot, info.head)
		if addErr != nil {
			// A random branch collision is retryable. We deliberately leave any
			// non-empty partial directory untouched rather than risk deleting user
			// data after Git returned an ambiguous failure.
			if strings.Contains(strings.ToLower(stderr), "already exists") {
				continue
			}
			return Result{}, fmt.Errorf("create Git worktree: %w%s", addErr, stderrSuffix(stderr))
		}

		selectedRoot := worktreeRoot
		if prefix := filepath.FromSlash(strings.Trim(strings.TrimSpace(info.prefix), "/")); prefix != "" && prefix != "." {
			selectedRoot = filepath.Join(worktreeRoot, prefix)
			st, statErr := os.Stat(selectedRoot)
			if statErr != nil || !st.IsDir() {
				return Result{}, fmt.Errorf("created worktree is missing selected project subdirectory %q", prefix)
			}
		}
		return Result{
			WorkspaceRoot: selectedRoot,
			WorktreeRoot:  worktreeRoot,
			SourceRoot:    info.RepoRoot,
			Branch:        branch,
			Head:          info.head,
			SourceDirty:   info.SourceDirty,
		}, nil
	}
	return Result{}, errors.New("could not allocate a unique Delivery worktree")
}

// IsManagedPath reports whether path belongs to Reasonix's durable worktree
// storage. It is a lexical UI identity check, not an authorization boundary.
func IsManagedPath(path, managedRoot string) bool {
	path = strings.TrimSpace(path)
	managedRoot = strings.TrimSpace(managedRoot)
	if path == "" || managedRoot == "" {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absManaged, err := filepath.Abs(managedRoot)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(absManaged), filepath.Clean(absPath))
	if err != nil || rel == "." || rel == "" {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func inspect(ctx context.Context, workspaceRoot string) (inspection, error) {
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		return inspection{}, errors.New("project folder is required")
	}
	st, err := os.Stat(workspaceRoot)
	if err != nil {
		return inspection{}, fmt.Errorf("project folder is unavailable: %w", err)
	}
	if !st.IsDir() {
		return inspection{}, errors.New("project path is not a folder")
	}
	if _, err := exec.LookPath("git"); err != nil {
		return inspection{}, errors.New("Git is not installed; Delivery remains safe and will serialize writes in this folder")
	}

	repoRoot, stderr, err := runGit(ctx, workspaceRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return inspection{}, fmt.Errorf("project folder is not inside a Git repository%s", stderrSuffix(stderr))
	}
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	if repoRoot == "" {
		return inspection{}, errors.New("Git did not report a repository root")
	}
	bare, _, err := runGit(ctx, workspaceRoot, "rev-parse", "--is-bare-repository")
	if err != nil || strings.EqualFold(strings.TrimSpace(bare), "true") {
		return inspection{}, errors.New("bare Git repositories cannot be opened as Delivery workspaces")
	}
	head, _, err := runGit(ctx, repoRoot, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(head) == "" {
		return inspection{}, errors.New("the Git repository needs an initial commit before a worktree can be created")
	}
	head = strings.TrimSpace(head)
	prefix, _, err := runGit(ctx, workspaceRoot, "rev-parse", "--show-prefix")
	if err != nil {
		return inspection{}, fmt.Errorf("resolve selected project path inside repository: %w", err)
	}
	prefix = strings.TrimSpace(prefix)
	if prefix != "" {
		objectType, _, objectErr := runGit(ctx, repoRoot, "cat-file", "-t", head+":"+strings.TrimSuffix(prefix, "/"))
		if objectErr != nil || strings.TrimSpace(objectType) != "tree" {
			return inspection{}, errors.New("the selected project folder is not present in the committed HEAD; commit it before creating a worktree")
		}
	}
	commonDir, _, err := runGit(ctx, repoRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return inspection{}, fmt.Errorf("resolve Git common directory: %w", err)
	}
	commonDir = strings.TrimSpace(commonDir)
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoRoot, commonDir)
	}
	commonDir = filepath.Clean(commonDir)
	branch, _, _ := runGit(ctx, repoRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	status, _, statusErr := runGit(ctx, repoRoot, "status", "--porcelain=v1", "--untracked-files=normal")
	if statusErr != nil {
		return inspection{}, fmt.Errorf("inspect Git working tree: %w", statusErr)
	}
	return inspection{
		Availability: Availability{
			Available:   true,
			RepoRoot:    repoRoot,
			Branch:      strings.TrimSpace(branch),
			SourceDirty: strings.TrimSpace(status) != "",
		},
		head:      head,
		prefix:    prefix,
		commonDir: commonDir,
	}, nil
}

func runGit(parent context.Context, dir string, args ...string) (stdout, stderr string, err error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, gitTimeout(args))
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", gitCommandArgs(runtime.GOOS, dir, args...)...)
	proc.HideWindow(cmd)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return outBuf.String(), strings.TrimSpace(errBuf.String()), err
}

func gitTimeout(args []string) time.Duration {
	if len(args) >= 2 && args[0] == "worktree" && args[1] == "add" {
		return gitWorktreeAddTimeout
	}
	return gitProbeTimeout
}

func gitCommandArgs(goos, dir string, args ...string) []string {
	commandArgs := []string{"-c", "core.fsmonitor=false", "-c", "maintenance.auto=false"}
	if goos == "windows" {
		commandArgs = append(commandArgs, "-c", "core.longpaths=true")
	}
	commandArgs = append(commandArgs, "-C", dir)
	return append(commandArgs, args...)
}

func randomID() (string, error) {
	var b [5]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate worktree id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func safePathComponent(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r < 32:
			return '-'
		case strings.ContainsRune(`/\\:<>"|?*`, r):
			return '-'
		default:
			return r
		}
	}, name)
	name = strings.Trim(name, ". ")
	reserved := strings.ToUpper(strings.SplitN(name, ".", 2)[0])
	if reserved == "CON" || reserved == "PRN" || reserved == "AUX" || reserved == "NUL" ||
		(len(reserved) == 4 && (strings.HasPrefix(reserved, "COM") || strings.HasPrefix(reserved, "LPT")) && reserved[3] >= '1' && reserved[3] <= '9') {
		name = "_" + name
	}
	return name
}

func stderrSuffix(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	const max = 500
	if len(stderr) > max {
		stderr = stderr[:max] + "…"
	}
	return ": " + stderr
}
