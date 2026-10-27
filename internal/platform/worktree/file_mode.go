package worktree

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

type worktreeTrust struct {
	fileMode bool
	symlinks bool
}

func readWorktreeTrust(ctx context.Context, repo gitcmd.Repo) (worktreeTrust, error) {
	fileMode, err := configBool(ctx, repo, "core.filemode")
	if err != nil {
		return worktreeTrust{}, err
	}
	symlinks, err := configBool(ctx, repo, "core.symlinks")
	if err != nil {
		return worktreeTrust{}, err
	}
	return worktreeTrust{fileMode: fileMode, symlinks: symlinks}, nil
}

func configBool(ctx context.Context, repo gitcmd.Repo, key string) (bool, error) {
	out, stderr, err := runGit(ctx, repo, "config", "--type=bool", "--default=true", "--get", key)
	if err != nil {
		return false, fmt.Errorf("read %s: %w%s", key, err, stderrSuffix(stderr))
	}
	return strings.TrimSpace(out) == "true", nil
}

func indexModes(ctx context.Context, repo gitcmd.Repo, paths []string) (map[string]string, error) {
	args := append([]string{"ls-files", "--stage", "-z", "--"}, literalPathspecs(paths)...)
	out, stderr, err := runGit(ctx, repo, args...)
	if err != nil {
		return nil, fmt.Errorf("read index modes: %w%s", err, stderrSuffix(stderr))
	}
	modes := map[string]string{}
	for record := range strings.SplitSeq(strings.TrimSuffix(out, "\x00"), "\x00") {
		if record == "" {
			continue
		}
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 || fields[2] != "0" {
			return nil, fmt.Errorf("unresolved index entry %q", record)
		}
		modes[path] = fields[0]
	}
	return modes, nil
}

func (trust worktreeTrust) canApply(base, target gitFileVersion) error {
	if target == absent {
		return nil
	}
	if !trust.fileMode && ((target.Mode == "100755" && base.Mode != "100755") ||
		(target.Mode == "100644" && base.Mode == "100755")) {
		return fmt.Errorf("executable-mode transition requires core.filemode=true; no files applied")
	}
	if !trust.symlinks && (target.Mode == "120000") != (base.Mode == "120000") {
		return fmt.Errorf("symlink-type transition requires core.symlinks=true; no files applied")
	}
	return nil
}
