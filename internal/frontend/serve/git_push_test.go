package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/session/control"
)

// pushFixture is a work repository whose upstream is an ssh:// remote answered by
// a local bare repository: GIT_SSH_COMMAND runs the command git asks the far end
// to run, so the push crosses the one allowed transport without a network.
func pushFixture(t *testing.T, trusted bool) (srv *httptest.Server, work, remote string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in ssh is a shell script")
	}
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	isolateGitGlobalConfig(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "t")
		t.Setenv(k+"_EMAIL", "t@t")
	}
	root := testenv.TempDir(t)
	fakeSSH := filepath.Join(root, "ssh")
	if err := os.WriteFile(fakeSSH, []byte("#!/bin/sh\nshift\nexec sh -c \"$*\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSH_COMMAND", fakeSSH)
	remote = filepath.Join(root, "remote.git")
	work = filepath.Join(root, "work")
	gitIn(t, root, "init", "-q", "--bare", "-b", "main", remote)
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(work, "a.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", ".")
	gitIn(t, work, "commit", "-qm", "base")
	gitIn(t, work, "push", "-q", remote, "main")
	gitIn(t, work, "remote", "add", "origin", "ssh://localhost"+remote)
	gitIn(t, work, "config", "branch.main.remote", "origin")
	gitIn(t, work, "config", "branch.main.merge", "refs/heads/main")
	gitIn(t, work, "update-ref", "refs/remotes/origin/main", "HEAD")
	if err := os.WriteFile(filepath.Join(work, "b.go"), []byte("package b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", ".")
	gitIn(t, work, "commit", "-qm", "second")
	repo, err := gitcmd.Open(context.Background(), work)
	if err != nil {
		t.Fatal(err)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: work, WorkspaceRepo: repo, Posture: control.PostureEvidence{WritesConfined: true, Home: testenv.TempDir(t)}})
	t.Cleanup(func() { ctrl.Close() })
	if trusted {
		if err := ctrl.DecideWorkspaceTrust(config.WorkspaceTrusted); err != nil {
			t.Fatal(err)
		}
	}
	srv = httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv, work, remote
}

func postPlain(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestAPushIsPreviewedThenConfirmedAndReachesTheRemoteOverSSH(t *testing.T) {
	srv, work, remote := pushFixture(t, true)
	plan := hubGet[map[string]any](t, srv, "/git/push")
	if plan["scheme"] != "ssh" || plan["host"] != "localhost" || plan["branch"] != "main" || plan["ahead"] != float64(1) {
		t.Fatalf("plan = %v", plan)
	}
	body, _ := json.Marshal(map[string]any{"plan": plan})
	status, out := postPlain(t, srv.URL+"/git/push", string(body))
	if status != http.StatusOK {
		t.Fatalf("push = %d %v", status, out)
	}
	if got, want := gitIn(t, remote, "rev-parse", "main"), gitIn(t, work, "rev-parse", "HEAD"); got != want {
		t.Fatalf("remote at %s, work at %s", got, want)
	}
}

func TestNothingIsPushedFromAWorkspaceThatIsNotTrusted(t *testing.T) {
	srv, work, remote := pushFixture(t, false)
	resp, err := http.Get(srv.URL + "/git/push")
	if err != nil {
		t.Fatal(err)
	}
	var refusal struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&refusal)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || refusal.Code != "push.workspace_untrusted" {
		t.Fatalf("preview = %d %q, want 403 push.workspace_untrusted", resp.StatusCode, refusal.Code)
	}
	status, out := postPlain(t, srv.URL+"/git/push", `{"plan":{"remote":"origin","branch":"main","upstreamBranch":"main"}}`)
	if status != http.StatusForbidden || out["code"] != "push.workspace_untrusted" {
		t.Fatalf("push = %d %v, want 403 push.workspace_untrusted", status, out)
	}
	if gitIn(t, remote, "rev-parse", "main") == gitIn(t, work, "rev-parse", "HEAD") {
		t.Fatal("an untrusted workspace pushed")
	}
}

func TestEachPushRefusalHasItsOwnCode(t *testing.T) {
	srv, work, _ := pushFixture(t, true)
	plan := hubGet[map[string]any](t, srv, "/git/push")
	gitIn(t, work, "remote", "set-url", "origin", "file:///tmp/not-allowed.git")
	resp, err := http.Get(srv.URL + "/git/push")
	if err != nil {
		t.Fatal(err)
	}
	var refusal struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&refusal)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden || refusal.Code != "push.protocol_not_allowed" {
		t.Fatalf("a file:// remote = %d %q, want 403 push.protocol_not_allowed", resp.StatusCode, refusal.Code)
	}
	gitIn(t, work, "remote", "set-url", "origin", "ssh://localhost/elsewhere.git")
	body, _ := json.Marshal(map[string]any{"plan": plan})
	if status, out := postPlain(t, srv.URL+"/git/push", string(body)); status != http.StatusConflict || out["code"] != "push.plan_changed" {
		t.Fatalf("a plan that no longer matches = %d %v, want 409 push.plan_changed", status, out)
	}
	if status, out := postPlain(t, srv.URL+"/git/push", `{"repo":"..","plan":{}}`); status != http.StatusNotFound || out["code"] != "git.repo_unknown" {
		t.Fatalf("a repository the workspace does not hold = %d %v", status, out)
	}
	if status, out := postPlain(t, srv.URL+"/git/push", `nope`); status != http.StatusBadRequest || out["code"] != "push.bad_request" {
		t.Fatalf("a malformed request = %d %v", status, out)
	}
	gitIn(t, work, "checkout", "-q", "-b", "topic")
	resp, err = http.Get(srv.URL + "/git/push")
	if err != nil {
		t.Fatal(err)
	}
	_ = json.NewDecoder(resp.Body).Decode(&refusal)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict || refusal.Code != "push.no_upstream" {
		t.Fatalf("a branch with no upstream = %d %q, want 409 push.no_upstream", resp.StatusCode, refusal.Code)
	}
}
