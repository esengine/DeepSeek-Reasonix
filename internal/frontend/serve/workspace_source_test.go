package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func worktreeServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	dir := testenv.TempDir(t)
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "base", "--allow-empty"}, {"branch", "studio"}} {
		out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	repo, err := gitcmd.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	exec := agent.New(testutil.NewMock("draft"), tool.NewRegistry(), sessionstore.NewSession("system"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{WorkspaceRoot: dir, WorkspaceRepo: repo, Runner: exec, Executor: exec})
	ctrl.SetPlanMode(true)
	ctrl.SetToolApprovalMode("auto")
	s := New(ctrl, NewBroadcaster(), config.ServeConfig{})
	s.AllowWorkspaceSwitch()
	s.buildWorkspaceController = func(ctx context.Context, root, _ string) (*control.Controller, error) {
		repo, err := gitcmd.Open(ctx, root)
		if err != nil {
			return nil, err
		}
		return control.New(control.Options{WorkspaceRoot: root, WorkspaceRepo: repo}), nil
	}
	t.Cleanup(func() { s.Controller().Close(); ctrl.Close() })
	srv := httptest.NewServer(operatorHandler(s))
	t.Cleanup(srv.Close)
	return s, srv.URL, dir
}

func prepareWorktree(t *testing.T, url, root string, attachments []string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"isolate": true, "worktree": worktreeSelection{Ref: "refs/heads/studio", SourceRoot: root, Attachments: attachments}})
	resp, err := http.Post(url+"/workspace", "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Logf("prepare: %s", data)
	}
	return resp.StatusCode
}

func TestSessionWorktreePreservesDraftAndRejectsDuplicatePreparation(t *testing.T) {
	s, url, source := worktreeServer(t)
	attachment, err := control.SaveTextAttachmentInRoot(source, "task input")
	if err != nil {
		t.Fatal(err)
	}
	var sources worktreeSourcesView
	fetchInto(t, url+"/workspace/worktree-sources", &sources)
	if !sources.Available || len(sources.Sources) != 2 {
		t.Fatalf("sources = %+v", sources)
	}
	if code := prepareWorktree(t, url, source, []string{attachment}); code != http.StatusOK {
		t.Fatalf("create = %d", code)
	}
	root := s.Controller().WorkspaceRoot()
	if root == source || !s.Controller().PlanMode() || s.Controller().ToolApprovalMode() != "auto" {
		t.Fatal("workspace/axes not preserved")
	}
	for _, dir := range []string{source, root} {
		data, err := os.ReadFile(filepath.Join(dir, attachment))
		if err != nil || string(data) != "task input" {
			t.Fatalf("attachment in %s = %q %v", dir, data, err)
		}
	}
	if code := prepareWorktree(t, url, source, nil); code != http.StatusConflict {
		t.Fatalf("duplicate = %d", code)
	}
	if s.Controller().WorkspaceRoot() != root {
		t.Fatal("duplicate moved the session")
	}
	view := (&Runtime{ID: "draft", Server: s}).view()
	if view.Root != root {
		t.Fatalf("runtime root = %s", view.Root)
	}
}

func TestSessionWorktreeBuildFailureRollsBackAndKeepsSource(t *testing.T) {
	s, url, source := worktreeServer(t)
	old := s.Controller()
	s.buildWorkspaceController = func(context.Context, string, string) (*control.Controller, error) {
		return nil, errors.New("build failed")
	}
	if code := prepareWorktree(t, url, source, nil); code != http.StatusConflict {
		t.Fatalf("failure = %d", code)
	}
	if s.Controller() != old {
		t.Fatal("failure replaced source")
	}
	out, err := exec.Command("git", "-C", source, "for-each-ref", "--format=%(refname)", "refs/heads/reasonix/").Output()
	if err != nil || len(out) != 0 {
		t.Fatalf("failed preparation left branches: %s %v", out, err)
	}
}

func TestSessionWorktreeRefusesExistingConversation(t *testing.T) {
	s, url, source := worktreeServer(t)
	ctrl := s.Controller().(*control.Controller)
	ctrl.AdoptHistory([]provider.Message{{Role: provider.RoleUser, Content: "existing task"}}, "")
	if code := prepareWorktree(t, url, source, nil); code != http.StatusConflict {
		t.Fatalf("existing = %d", code)
	}
}
