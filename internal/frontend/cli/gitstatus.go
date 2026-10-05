package cli

import (
	"context"
	"strconv"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

func runGit(ctx context.Context, repo gitcmd.Repo, args ...string) (string, error) {
	cmd := repo.Command(ctx, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseGitNumstat(out string) (added int, removed int) {
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
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
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if strings.HasPrefix(line, "?? ") {
			n++
		}
	}
	return n
}

var ()
