package worktree

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

type snapshotEntry struct {
	Path string
	Mode string
}

func snapshotEntries(ctx context.Context, repo gitcmd.Repo, tree string) ([]snapshotEntry, error) {
	out, stderr, err := runGit(ctx, repo, "ls-tree", "-r", "-z", tree)
	if err != nil {
		return nil, fmt.Errorf("read snapshot paths: %w%s", err, stderrSuffix(stderr))
	}
	var entries []snapshotEntry
	for record := range strings.SplitSeq(out, "\x00") {
		if record == "" {
			continue
		}
		header, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(header)
		if !ok || len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755" && fields[0] != "120000" && fields[0] != "160000") {
			return nil, fmt.Errorf("unsupported snapshot tree entry")
		}
		if _, err := ChangePaths(Snapshot{RepoRoot: repo.WorkTree}, []Change{{Path: path}}); err != nil {
			return nil, err
		}
		entries = append(entries, snapshotEntry{Path: path, Mode: fields[0]})
		if len(entries) > environmentMaxEntries {
			return nil, fmt.Errorf("snapshot metadata exceeds supported entry limit")
		}
	}
	return entries, nil
}
