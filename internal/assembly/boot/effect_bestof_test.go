package boot

import (
	"context"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
)

func firstUser(req provider.Request) string {
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			return m.Content
		}
	}
	return ""
}

func hasToolResult(req provider.Request) bool {
	for _, m := range req.Messages {
		if m.Role == provider.RoleTool {
			return true
		}
	}
	return false
}

func gitInDir(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := gitcmd.Command(context.Background(), dir, args...)
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
