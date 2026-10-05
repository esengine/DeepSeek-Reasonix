package gitcmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// StageAll is `git add -A` over r's work tree into the index env names, never
// entering a nested repository: git checks a submodule by running inside it,
// under its config. A gitlink present as a directory keeps the commit the
// index records; an untracked nested repository is left out.
func (r Repo) StageAll(ctx context.Context, env ...string) error {
	top := r.Top()
	nested, err := top.nestedRepositories(ctx, env)
	if err != nil {
		return err
	}
	var specs bytes.Buffer
	specs.WriteString(".\x00")
	for _, path := range nested {
		specs.WriteString(":(exclude,literal)" + path + "\x00")
	}
	cmd := top.Command(ctx, "add", "-A", "--pathspec-from-file=-", "--pathspec-file-nul")
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = &specs
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// nestedRepositories lists, relative to the work tree root, the gitlinks
// present as directories and the untracked nested repositories git reports
// (with a trailing slash) instead of descending into them.
func (r Repo) nestedRepositories(ctx context.Context, env []string) ([]string, error) {
	var nested []string
	staged, err := r.output(ctx, env, "ls-files", "-s", "-z")
	if err != nil {
		return nil, err
	}
	for entry := range strings.SplitSeq(staged, "\x00") {
		meta, path, ok := strings.Cut(entry, "\t")
		if !ok || !strings.HasPrefix(meta, "160000 ") {
			continue
		}
		if st, err := os.Lstat(filepath.Join(r.WorkTree, filepath.FromSlash(path))); err == nil && st.IsDir() {
			nested = append(nested, path)
		}
	}
	untracked, err := r.output(ctx, env, "ls-files", "-o", "-z", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for path := range strings.SplitSeq(untracked, "\x00") {
		if dir, ok := strings.CutSuffix(path, "/"); ok {
			nested = append(nested, dir)
		}
	}
	return nested, nil
}

func (r Repo) output(ctx context.Context, env []string, args ...string) (string, error) {
	cmd := r.Command(ctx, args...)
	cmd.Env = append(cmd.Env, env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
