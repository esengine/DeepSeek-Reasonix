package control

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitpush"
)

func pushController(t *testing.T, trust config.WorkspaceTrust, repo bool) *Controller {
	t.Helper()
	home, ws := testenv.TempDir(t), testenv.TempDir(t)
	if repo {
		for _, args := range [][]string{
			{"init", "-q"},
			{"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "first"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = ws
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("git %v: %v\n%s", args, err, out)
			}
		}
	}
	opts := Options{WorkspaceRoot: ws, Posture: PostureEvidence{Home: home}}
	if repo {
		opened, err := gitcmd.Open(context.Background(), ws)
		if err != nil {
			t.Fatal(err)
		}
		opts.WorkspaceRepo = opened
	}
	c := New(opts)
	if trust != config.WorkspaceTrustUndecided {
		if err := c.SetWorkspaceTrust(trust); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestGitPushIsRefusedFromAWorkspaceThatIsNotTrusted(t *testing.T) {
	for _, trust := range []config.WorkspaceTrust{config.WorkspaceTrustUndecided, config.WorkspaceTrustDeclined} {
		c := pushController(t, trust, true)
		if _, err := c.GitPushPreview(context.Background(), ""); !errors.Is(err, ErrWorkspaceNotTrusted) {
			t.Errorf("preview under %q = %v, want ErrWorkspaceNotTrusted", trust, err)
		}
		if _, err := c.GitPush(context.Background(), PushRequest{}); !errors.Is(err, ErrWorkspaceNotTrusted) {
			t.Errorf("push under %q = %v, want ErrWorkspaceNotTrusted", trust, err)
		}
	}
}

func TestATrustedWorkspaceWithoutARepositoryFailsAsThatNotAsUntrusted(t *testing.T) {
	c := pushController(t, config.WorkspaceTrusted, false)
	if _, err := c.GitPushPreview(context.Background(), ""); err == nil || errors.Is(err, ErrWorkspaceNotTrusted) {
		t.Fatalf("preview with no repository = %v, want a repository error", err)
	}
	if _, err := c.GitPush(context.Background(), PushRequest{}); err == nil || errors.Is(err, ErrWorkspaceNotTrusted) {
		t.Fatalf("push with no repository = %v, want a repository error", err)
	}
}

func TestATrustedPreviewReachesTheRepositoryAndAFailedPushReleasesTheCommitLock(t *testing.T) {
	c := pushController(t, config.WorkspaceTrusted, true)
	if _, err := c.GitPushPreview(context.Background(), ""); !errors.Is(err, gitpush.ErrNoUpstream) {
		t.Fatalf("preview of a branch with no upstream = %v, want ErrNoUpstream", err)
	}
	if _, err := c.GitPush(context.Background(), PushRequest{}); err == nil {
		t.Fatal("a push of an empty plan was accepted")
	}
	if !c.commitMu.TryLock() {
		t.Fatal("a refused push left the commit lock held")
	}
	c.commitMu.Unlock()
}

func TestAPushNamingARepositoryThatIsNotThereFailsBeforeTouchingTheLock(t *testing.T) {
	c := pushController(t, config.WorkspaceTrusted, true)
	if _, err := c.GitPushPreview(context.Background(), "no-such-repository"); err == nil || errors.Is(err, ErrWorkspaceNotTrusted) {
		t.Fatalf("preview of an unknown repository = %v, want a lookup error", err)
	}
	if _, err := c.GitPush(context.Background(), PushRequest{Repo: "no-such-repository"}); err == nil || errors.Is(err, ErrWorkspaceNotTrusted) {
		t.Fatalf("push to an unknown repository = %v, want a lookup error", err)
	}
}
