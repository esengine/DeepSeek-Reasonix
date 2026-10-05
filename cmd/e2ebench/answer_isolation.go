package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"reasonix/internal/safety/sandbox"
)

// MemoryBench keeps its seeded facts in the suite checkout. The graded child
// receives only workdir/, but without a read boundary it can still search the
// host for that checkout or read the same facts from Git objects.
func validateAnswerIsolation(tasks []task) error {
	for _, t := range tasks {
		if t.answerRoot != "" && fileExists(filepath.Join(t.dir, "workdir", "reasonix.toml")) {
			return fmt.Errorf("%s already has a project config; answer isolation cannot replace it", t.dir)
		}
	}
	for _, t := range tasks {
		if t.answerRoot == "" {
			continue
		}
		if !sandbox.Available() {
			return fmt.Errorf("%s contains answer material, but this host has no OS read sandbox", t.answerRoot)
		}
		roots, err := answerReadRoots(t.answerRoot)
		if err != nil {
			return err
		}
		stateBase, err := benchStateBase()
		if err != nil {
			return err
		}
		for _, root := range roots {
			for _, path := range []string{os.TempDir(), stateBase} {
				if within(root, resolvedPath(path)) {
					return fmt.Errorf("benchmark runtime path %s lies inside forbidden answer root %s", path, root)
				}
			}
		}
		return nil
	}
	return nil
}

func stageAnswerIsolation(answerRoot, work string) error {
	if answerRoot == "" {
		return nil
	}
	roots, err := answerReadRoots(answerRoot)
	if err != nil {
		return err
	}
	for _, root := range roots {
		if within(root, resolvedPath(work)) {
			return fmt.Errorf("workdir %s lies inside forbidden answer root %s", work, root)
		}
	}
	paths, err := json.Marshal(roots) // JSON strings are valid TOML basic strings.
	if err != nil {
		return err
	}
	path := filepath.Join(work, "reasonix.toml")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create isolated project config: %w", err)
	}
	_, writeErr := fmt.Fprintf(f, "[sandbox]\nbash = \"enforce\"\nforbid_read = %s\n", paths)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// The temporary policy is harness state, not a task artifact. Remove it before
// grading and mutation timing so it cannot affect either measurement.
func removeAnswerIsolation(answerRoot, work string) error {
	if answerRoot == "" {
		return nil
	}
	return os.Remove(filepath.Join(work, "reasonix.toml"))
}

func answerReadRoots(answerRoot string) ([]string, error) {
	tasks, err := filepath.Abs(answerRoot)
	if err != nil {
		return nil, err
	}
	tasks = resolvedPath(tasks)
	roots := []string{tasks}
	// A worktree's .git is a pointer into the common object store. Hide that
	// store and every registered checkout: the same answer files may be present
	// in the primary checkout as well as in the one running this harness.
	cmd := exec.Command("git", "-C", tasks, "worktree", "list", "--porcelain", "-z")
	listing, err := cmd.Output()
	if err != nil {
		if gitMarkerAbove(tasks) {
			return nil, fmt.Errorf("list checkout worktrees for answer isolation: %w", err)
		}
		return roots, nil // Source archive without a Git object store.
	}
	for line := range strings.SplitSeq(string(listing), "\x00") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			roots = append(roots, resolvedPath(path))
		}
	}
	common, err := exec.Command("git", "-C", tasks, "rev-parse", "--git-common-dir").Output()
	if err != nil {
		return nil, fmt.Errorf("locate Git object store for answer isolation: %w", err)
	}
	gitDir := strings.TrimSuffix(string(common), "\n")
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(tasks, gitDir)
	}
	roots = append(roots, resolvedPath(gitDir))
	return uniqueAnswerRoots(roots), nil
}

func gitMarkerAbove(path string) bool {
	for {
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(path)
		if parent == path {
			return false
		}
		path = parent
	}
}

func uniqueAnswerRoots(paths []string) []string {
	seen := make(map[string]bool, len(paths))
	var out []string
	for _, path := range paths {
		if path != "" && !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	return out
}
