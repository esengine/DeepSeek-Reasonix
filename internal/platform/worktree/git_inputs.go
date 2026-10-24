package worktree

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/platform/gitcmd"
)

// GitInputs binds the logical HEAD, refs, index and effective configuration of
// one checkout. It does not claim to capture network state or every Git object.
// Values, including credential-bearing configuration, are hashed in memory.
type GitInputs struct {
	Scope    string `json:"scope"`
	Digest   string `json:"digest"`
	Complete bool   `json:"complete"`
	Reason   string `json:"reason"`
}

func CaptureGitInputs(ctx context.Context, repo gitcmd.Repo) GitInputs {
	result := GitInputs{Scope: "git-head-refs-index-config-v1"}
	if err := repo.VerifyDiscovery(ctx); err != nil {
		result.Reason = "repository_identity_changed"
		return result
	}
	h := sha256.New()
	encode := json.NewEncoder(h)
	for _, args := range [][]string{
		{"rev-parse", "--verify", "HEAD"},
		{"symbolic-ref", "-q", "HEAD"},
		{"for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)"},
		{"ls-files", "--stage", "-z"},
		{"config", "--null", "--show-origin", "--includes", "--list"},
	} {
		out, err := repo.Command(ctx, args...).Output()
		var exit *exec.ExitError
		detached := args[0] == "symbolic-ref" && errors.As(err, &exit) && exit.ExitCode() == 1
		if err != nil && !detached {
			result.Reason = "git_input_unreadable"
			return result
		}
		_ = encode.Encode(struct {
			Arguments []string
			Output    []byte
		}{args, out})
	}
	index, err := fileutil.ReadContentVersion(ctx, filepath.Join(repo.GitDir, "index"))
	if err != nil {
		result.Reason = "index_unreadable"
		return result
	}
	_ = encode.Encode(index)
	if ctx.Err() != nil {
		result.Reason = "cancelled"
		return result
	}
	result.Complete, result.Digest = true, hex.EncodeToString(h.Sum(nil))
	return result
}
