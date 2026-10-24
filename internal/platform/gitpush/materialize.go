package gitpush

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// materialize copies the commits the tracking refs say the remote lacks. The
// commits they rest on become shallow roots with trees but no blobs, enough to
// prove a fast-forward. A whole copy ignores what the tracking refs claim.
func materialize(ctx context.Context, source gitcmd.Repo, sc *scratch, plan Plan, old string, whole bool) (bool, error) {
	revs := []string{plan.Head}
	switch {
	case whole:
	case strings.Trim(old, "0") != "":
		revs = append(revs, "^"+old)
	default:
		known, err := output(ctx, source.Top(), "for-each-ref", "--format=%(objectname)", "--", "refs/remotes/"+plan.Remote+"/")
		if err != nil {
			return false, err
		}
		for id := range strings.FieldsSeq(known) {
			revs = append(revs, "^"+id)
		}
	}
	input := strings.Join(revs, "\n") + "\n"
	listed, err := scratchInput(ctx, sc, input, "rev-list", "--boundary", "--stdin")
	if err != nil {
		return false, err
	}
	var boundary []string
	for line := range strings.FieldsSeq(listed) {
		if id, ok := strings.CutPrefix(line, "-"); ok {
			boundary = append(boundary, id)
		}
	}
	pack := filepath.Join(sc.repo.CommonDir, "objects", "pack", "pack")
	if _, err := scratchInput(ctx, sc, input, "pack-objects", "-q", "--revs", pack); err != nil {
		return false, err
	}
	if len(boundary) == 0 {
		return false, nil
	}
	slices.Sort(boundary)
	boundary = slices.Compact(boundary)
	roots := strings.Join(boundary, "\n") + "\n"
	trees, err := scratchInput(ctx, sc, roots, "rev-list", "--objects", "--no-walk", "--filter=blob:none", "--stdin")
	if err != nil {
		return false, err
	}
	var ids []string
	for line := range strings.Lines(trees) {
		if id, _, _ := strings.Cut(strings.TrimSpace(line), " "); id != "" {
			ids = append(ids, id)
		}
	}
	if _, err := scratchInput(ctx, sc, strings.Join(ids, "\n")+"\n", "pack-objects", "-q", pack); err != nil {
		return false, err
	}
	return true, os.WriteFile(filepath.Join(sc.repo.CommonDir, "shallow"), []byte(roots), 0o600)
}

func scratchInput(ctx context.Context, sc *scratch, input string, args ...string) (string, error) {
	cmd := sc.repo.Command(ctx, args...)
	cmd.Stdin = strings.NewReader(input)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%w: materialize push objects: %s", errors.Join(ErrPushFailed, ctx.Err(), err), strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}
