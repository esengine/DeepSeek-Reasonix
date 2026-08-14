// Package sourceupdate reports whether the configured upstream source moved
// beyond the local tracking baseline. It never fetches, merges, or installs.
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
	out, err := git.Run(ctx, root, args...)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "", errors.New("git returned empty output")
	}
	return value, nil
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
