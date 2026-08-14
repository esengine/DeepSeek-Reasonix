// Package sourceupdate reports upstream source movement and provides an
// explicit, conservative fetch for the local tracking baseline.
package sourceupdate

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	DefaultRemoteURL = "https://github.com/esengine/DeepSeek-Reasonix.git"
	DefaultRemoteRef = "refs/heads/main-v2"
)

type Status string

const (
	StatusUpToDate        Status = "up-to-date"
	StatusUpstreamUpdate  Status = "upstream-update"
	StatusBaselineMissing Status = "baseline-missing"
	StatusCheckFailed     Status = "check-failed"
)

type Result struct {
	SourceRoot      string `json:"sourceRoot"`
	Branch          string `json:"branch"`
	Head            string `json:"head"`
	LocalBase       string `json:"localBase"`
	RemoteURL       string `json:"remoteUrl"`
	RemoteRef       string `json:"remoteRef"`
	RemoteHead      string `json:"remoteHead"`
	Status          Status `json:"status"`
	HasLocalPatches bool   `json:"hasLocalPatches"`
	Message         string `json:"message,omitempty"`
}

type GitRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execGitRunner struct{}

func (execGitRunner) Run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		detail := compactError(err)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			if stderr := compactError(errors.New(string(exitErr.Stderr))); stderr != "" {
				detail += ": " + stderr
			}
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), detail)
	}
	return out, nil
}

func Check(ctx context.Context, root string) (Result, error) {
	return CheckWithRunner(ctx, root, execGitRunner{})
}

// Fetch updates only refs/remotes/origin/main-v2 from the configured upstream.
// It requires a clean worktree and never checks out, merges, rebases, resets,
// or replaces a build artifact. The caller must review and integrate the
// fetched commits separately.
func Fetch(ctx context.Context, root string) (Result, error) {
	return FetchWithRunner(ctx, root, execGitRunner{})
}

func FetchWithRunner(ctx context.Context, root string, git GitRunner) (Result, error) {
	result := Result{
		RemoteURL: DefaultRemoteURL,
		RemoteRef: DefaultRemoteRef,
		Status:    StatusCheckFailed,
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return fail(result, "source root is empty", errors.New("source root is required"))
	}
	if git == nil {
		return fail(result, "git runner is nil", errors.New("git runner is required"))
	}

	top, err := runText(ctx, git, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return fail(result, "resolve source root", err)
	}
	result.SourceRoot = top
	if absolute, absErr := filepath.Abs(top); absErr == nil {
		result.SourceRoot = absolute
	}
	branch, err := runText(ctx, git, root, "branch", "--show-current")
	if err != nil {
		return fail(result, "read current branch", err)
	}
	if branch == "" {
		branch = "(detached)"
	}
	result.Branch = branch
	result.Head, err = runHash(ctx, git, root, "HEAD")
	if err != nil {
		return fail(result, "read HEAD", err)
	}

	origin, err := runText(ctx, git, root, "remote", "get-url", "origin")
	if err != nil {
		return fail(result, "read origin URL", err)
	}
	if !sameRemoteURL(origin, DefaultRemoteURL) {
		return fail(result, "refuse unexpected origin URL", errors.New("origin does not match the configured upstream"))
	}

	status, err := runAllowEmpty(ctx, git, root, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return fail(result, "inspect worktree", err)
	}
	if strings.TrimSpace(status) != "" {
		return fail(result, "refuse fetch from dirty worktree", errors.New("worktree is not clean; commit or stash changes first"))
	}

	if _, err := git.Run(ctx, root, "fetch", "--no-tags", "origin", "refs/heads/main-v2:refs/remotes/origin/main-v2"); err != nil {
		return fail(result, "fetch origin main-v2", err)
	}
	result.LocalBase, err = runHash(ctx, git, root, "refs/remotes/origin/main-v2")
	if err != nil {
		return fail(result, "read fetched main-v2 baseline", err)
	}
	result.HasLocalPatches = result.Head != result.LocalBase
	result.Status = StatusUpToDate
	result.Message = "fetched upstream main-v2 into the local tracking baseline; current branch was not changed"
	return result, nil
}

func CheckWithRunner(ctx context.Context, root string, git GitRunner) (Result, error) {
	result := Result{
		RemoteURL: DefaultRemoteURL,
		RemoteRef: DefaultRemoteRef,
		Status:    StatusCheckFailed,
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return fail(result, "source root is empty", errors.New("source root is required"))
	}
	if git == nil {
		return fail(result, "git runner is nil", errors.New("git runner is required"))
	}

	top, err := runText(ctx, git, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return fail(result, "resolve source root", err)
	}
	result.SourceRoot = top
	if absolute, absErr := filepath.Abs(top); absErr == nil {
		result.SourceRoot = absolute
	}

	branch, err := runText(ctx, git, root, "branch", "--show-current")
	if err != nil {
		return fail(result, "read current branch", err)
	}
	if branch == "" {
		branch = "(detached)"
	}
	result.Branch = branch

	result.Head, err = runHash(ctx, git, root, "HEAD")
	if err != nil {
		return fail(result, "read HEAD", err)
	}
	result.LocalBase, err = runHash(ctx, git, root, "refs/remotes/origin/main-v2")
	if err != nil {
		result.Status = StatusBaselineMissing
		result.Message = failureMessage("local origin/main-v2 baseline is unavailable; source check did not fetch it", err)
		return result, fmt.Errorf("source update: resolve local baseline: %w", err)
	}
	result.HasLocalPatches = result.Head != result.LocalBase

	remote, err := runText(ctx, git, root, "ls-remote", "--heads", DefaultRemoteURL, "main-v2")
	if err != nil {
		return fail(result, "read remote main-v2", err)
	}
	result.RemoteHead, err = parseRemoteHead(remote)
	if err != nil {
		return fail(result, "parse remote main-v2", err)
	}
	if result.RemoteHead == result.LocalBase {
		result.Status = StatusUpToDate
		result.Message = "upstream main-v2 matches the local tracking baseline"
		return result, nil
	}
	result.Status = StatusUpstreamUpdate
	result.Message = "upstream main-v2 is newer than the local tracking baseline; no source was fetched"
	return result, nil
}

func runText(ctx context.Context, git GitRunner, root string, args ...string) (string, error) {
	value, err := runAllowEmpty(ctx, git, root, args...)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", errors.New("git returned empty output")
	}
	return value, nil
}

func runAllowEmpty(ctx context.Context, git GitRunner, root string, args ...string) (string, error) {
	out, err := git.Run(ctx, root, args...)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func sameRemoteURL(left, right string) bool {
	left = strings.TrimRight(strings.TrimSpace(left), "/")
	right = strings.TrimRight(strings.TrimSpace(right), "/")
	return strings.EqualFold(left, right)
}

func runHash(ctx context.Context, git GitRunner, root string, ref string) (string, error) {
	value, err := runText(ctx, git, root, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	if len(value) != 40 && len(value) != 64 {
		return "", fmt.Errorf("git object id has unexpected length %d", len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("git object id is not hexadecimal: %w", err)
	}
	return strings.ToLower(value), nil
}

func parseRemoteHead(output string) (string, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var head string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[1] != DefaultRemoteRef {
			continue
		}
		if head != "" {
			return "", errors.New("remote main-v2 returned duplicate refs")
		}
		if len(fields[0]) != 40 && len(fields[0]) != 64 {
			return "", fmt.Errorf("remote object id has unexpected length %d", len(fields[0]))
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", fmt.Errorf("remote object id is not hexadecimal: %w", err)
		}
		head = strings.ToLower(fields[0])
	}
	if head == "" {
		return "", errors.New("remote main-v2 ref was not found")
	}
	return head, nil
}

func fail(result Result, message string, err error) (Result, error) {
	result.Status = StatusCheckFailed
	result.Message = failureMessage(message, err)
	return result, fmt.Errorf("source update: %s: %w", message, err)
}

func failureMessage(message string, err error) string {
	detail := compactError(err)
	if detail == "" {
		return message
	}
	return message + ": " + detail
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	return strings.Join(strings.Fields(err.Error()), " ")
}
