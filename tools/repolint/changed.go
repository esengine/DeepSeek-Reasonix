// changed.go — the universe a narrowed check covers. `git diff` answers what
// the tree changed against HEAD and nothing about a file git has not started
// tracking, so a new file reached `make check` as clean and the standards gate
// reported green over a file it had never opened.
package main

import (
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strings"
)

// trackedChangedPaths is what `git diff --name-only HEAD` answers: modified and
// staged-new files, never an untracked one. It is kept as its own function
// because it is the incomplete universe, and a test that cannot express the
// incomplete one cannot prove the complete one fixed anything.
func trackedChangedPaths(root string) ([]string, error) {
	// Deletions are excluded: a path that no longer exists cannot be scanned,
	// and handing one to the scanner asks it to fail on a file the change
	// removed on purpose.
	return gitPaths(root, "diff", "--name-only", "--diff-filter=ACMRTUXB", "HEAD")
}

// untrackedPaths is the half that was missing. --exclude-standard keeps
// .gitignore honoured, so build output does not enter the universe.
func untrackedPaths(root string) ([]string, error) {
	return gitPaths(root, "ls-files", "--others", "--exclude-standard")
}

// changedPaths is the -only list `-changed` builds: everything the working tree
// added or altered, whether or not git has been told about it yet.
func changedPaths(root string) (string, error) {
	tracked, err := trackedChangedPaths(root)
	if err != nil {
		return "", err
	}
	untracked, err := untrackedPaths(root)
	if err != nil {
		return "", err
	}
	return joinChangedPaths(tracked, untracked), nil
}

// joinChangedPaths is the union in the comma-separated form -only parses, kept
// apart from git so its shape can be checked without a repository.
func joinChangedPaths(sets ...[]string) string {
	seen := map[string]bool{}
	for _, set := range sets {
		for _, p := range set {
			if p = strings.TrimSpace(p); p != "" {
				seen[p] = true
			}
		}
	}
	return strings.Join(slices.Sorted(maps.Keys(seen)), ",")
}

func gitPaths(root string, args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	text := strings.ReplaceAll(strings.TrimSpace(string(out)), "\r\n", "\n")
	if text == "" {
		return nil, nil
	}
	return strings.Split(text, "\n"), nil
}
