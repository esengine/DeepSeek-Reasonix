package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/commitmsg"
	"reasonix/internal/session/control"
)

func commitServer(t *testing.T, repoDir string, gen *commitmsg.Generator) *httptest.Server {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "t")
		t.Setenv(k+"_EMAIL", "t@t")
	}
	var repo gitcmd.Repo
	if repoDir != "" {
		r, err := gitcmd.Open(context.Background(), repoDir)
		if err != nil {
			t.Fatal(err)
		}
		repo = r
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, CommitMessenger: gen, WorkspaceRepo: repo})
	t.Cleanup(func() { ctrl.Close() })
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func repoWith(t *testing.T, files map[string]string) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	dir := testenv.TempDir(t)
	gitIn(t, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "base.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "base.go")
	gitIn(t, dir, "commit", "-qm", "base")
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", name)
	}
	return dir
}

func commitPost(t *testing.T, srv *httptest.Server, path, body string) (int, map[string]any) {
	t.Helper()
	res, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func messenger(answer string) *commitmsg.Generator {
	return commitmsg.New(refineProvider{answer: answer}, nil, "fake/m", nil)
}

func TestProposedMessageIsCommittedOnlyAfterConfirmation(t *testing.T) {
	dir := repoWith(t, map[string]string{"x.go": "package x\n"})
	srv := commitServer(t, dir, messenger("feat: add x"))

	status, prop := commitPost(t, srv, "/commit/propose", `{}`)
	if status != http.StatusOK || prop["message"] != "feat: add x" || prop["fingerprint"] == "" {
		t.Fatalf("propose = %d %v", status, prop)
	}
	if got := strings.TrimSpace(gitIn(t, dir, "rev-list", "--count", "HEAD")); got != "1" {
		t.Fatalf("proposing committed something: %s commits", got)
	}
	body, _ := json.Marshal(map[string]any{"message": "feat: add x (edited)", "fingerprint": prop["fingerprint"]})
	status, res := commitPost(t, srv, "/commit", string(body))
	if status != http.StatusOK || res["subject"] != "feat: add x (edited)" {
		t.Fatalf("commit = %d %v", status, res)
	}
	if got := strings.TrimSpace(gitIn(t, dir, "log", "-1", "--format=%s")); got != "feat: add x (edited)" {
		t.Fatalf("subject = %q", got)
	}
}

func TestEachCommitRefusalHasItsOwnCode(t *testing.T) {
	secret := repoWith(t, map[string]string{".env": "A=1\n"})
	secretSrv := commitServer(t, secret, messenger("chore: env"))
	_, prop := commitPost(t, secretSrv, "/commit/propose", `{}`)
	fp, _ := prop["fingerprint"].(string)

	clean := repoWith(t, nil)
	cleanSrv := commitServer(t, clean, messenger("x"))
	noModel := commitServer(t, repoWith(t, map[string]string{"x.go": "package x\n"}), nil)
	noRepo := commitServer(t, "", messenger("x"))

	cases := []struct {
		name   string
		srv    *httptest.Server
		path   string
		body   string
		status int
		code   string
	}{
		{"secrets", secretSrv, "/commit", `{"message":"m","fingerprint":"` + fp + `"}`, http.StatusConflict, "commit.secrets_staged"},
		{"stale", secretSrv, "/commit", `{"message":"m","fingerprint":"zz","acknowledgeSecrets":true}`, http.StatusConflict, "commit.staged_changed"},
		{"empty message", secretSrv, "/commit", `{"message":" ","fingerprint":"` + fp + `"}`, http.StatusBadRequest, "commit.empty_message"},
		{"bad json", secretSrv, "/commit", `not json`, http.StatusBadRequest, "commit.bad_request"},
		{"nothing staged", cleanSrv, "/commit/propose", `{}`, http.StatusConflict, "commit.nothing_staged"},
		{"no model", noModel, "/commit/propose", `{}`, http.StatusConflict, "commit.no_model"},
		{"no repository", noRepo, "/commit/propose", `{}`, http.StatusConflict, "commit.no_repository"},
	}
	for _, c := range cases {
		status, out := commitPost(t, c.srv, c.path, c.body)
		if status != c.status || out["code"] != c.code {
			t.Errorf("%s = %d %v, want %d %s", c.name, status, out["code"], c.status, c.code)
		}
	}
	status, out := commitPost(t, secretSrv, "/commit", `{"message":"m","fingerprint":"`+fp+`"}`)
	params, _ := out["params"].(map[string]any)
	files, _ := params["files"].([]any)
	if status != http.StatusConflict || len(files) != 1 || files[0] != ".env" {
		t.Errorf("secrets refusal params = %v", out["params"])
	}
}
