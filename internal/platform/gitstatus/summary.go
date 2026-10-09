package gitstatus

import (
	"context"
	"path/filepath"
	"strings"

	"reasonix/internal/platform/gitcmd"
)

// Info is the one-line identity of a work tree: where it is, which branch it
// is on, and how far it has drifted from HEAD.
type Info struct {
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	Detached  bool   `json:"detached"`
	Added     int    `json:"added"`
	Removed   int    `json:"removed"`
	Untracked int    `json:"untracked"`
}

// Summary reads Info from the work tree's root, whatever subdirectory the
// session opened in. ok is false for a workspace that is not version
// controlled; counts git declines to give stay zero.
func Summary(ctx context.Context, repo gitcmd.Repo) (Info, bool) {
	if !repo.Valid() {
		return Info{}, false
	}
	top := repo.Top()
	info := Info{Name: filepath.Base(top.WorkTree)}
	info.Branch, info.Detached = branchOf(ctx, top)
	if raw, err := top.Command(ctx, "diff", "--numstat", "-z", "HEAD", "--").Output(); err == nil {
		for _, n := range ParseNumstatZ(raw) {
			if n.added != nil {
				info.Added += *n.added
			}
			if n.removed != nil {
				info.Removed += *n.removed
			}
		}
	}
	if raw, err := top.Command(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=normal").Output(); err == nil {
		for _, c := range ParsePorcelainZ(raw) {
			if c.Status == "??" {
				info.Untracked++
			}
		}
	}
	if ctx.Err() != nil {
		return Info{}, false
	}
	return info, true
}

func branchOf(ctx context.Context, top gitcmd.Repo) (name string, detached bool) {
	if out, err := top.Command(ctx, "symbolic-ref", "--quiet", "--short", "HEAD").Output(); err == nil {
		if name = strings.TrimSpace(string(out)); name != "" {
			return name, false
		}
	}
	if out, err := top.Command(ctx, "rev-parse", "--short", "HEAD").Output(); err == nil {
		if name = strings.TrimSpace(string(out)); name != "" {
			return name, true
		}
	}
	return "HEAD", true
}
