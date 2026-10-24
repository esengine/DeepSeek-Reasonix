package serve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/runtime/commitmsg"
	"reasonix/internal/session/control"
)

// projectsFolder is a workspace that is not a repository and holds two.
func projectsFolder(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	isolateGitGlobalConfig(t)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "t")
		t.Setenv(k+"_EMAIL", "t@t")
	}
	root := testenv.TempDir(t)
	for _, name := range []string{"api", "web"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "init", "-q")
		gitIn(t, dir, "config", "core.autocrlf", "false")
		if err := os.WriteFile(filepath.Join(dir, "base.go"), []byte("package a\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitIn(t, dir, "add", "base.go")
		gitIn(t, dir, "commit", "-qm", "base "+name)
	}
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: root, WorkspaceRepo: gitcmd.Repo{Dir: root}, CommitMessenger: commitmsg.New(refineProvider{answer: "add things"}, nil, "fake/m", nil)})
	t.Cleanup(func() { ctrl.Close() })
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv, root
}

func TestAFolderOfProjectsListsItsRepositories(t *testing.T) {
	srv, _ := projectsFolder(t)
	got := hubGet[struct {
		Repos []gitRepoRow `json:"repos"`
	}](t, srv, "/git/repos")
	if len(got.Repos) != 2 || got.Repos[0].Dir != "api" || got.Repos[1].Dir != "web" {
		t.Fatalf("repos = %+v, want api and web", got.Repos)
	}
	if got.Repos[0].Name != "api" || got.Repos[0].Branch == "" {
		t.Fatalf("a row carries its own identity: %+v", got.Repos[0])
	}
}

func TestEachRepositoryAnswersForItself(t *testing.T) {
	srv, root := projectsFolder(t)
	if err := os.WriteFile(filepath.Join(root, "api", "new.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	type changes struct {
		Repo    bool `json:"repo"`
		Changes []struct {
			Path string `json:"path"`
		} `json:"changes"`
	}
	api := hubGet[changes](t, srv, "/changes?repo=api")
	web := hubGet[changes](t, srv, "/changes?repo=web")
	if !api.Repo || len(api.Changes) != 1 || api.Changes[0].Path != "new.go" {
		t.Fatalf("api changes = %+v, want new.go", api)
	}
	if !web.Repo || len(web.Changes) != 0 {
		t.Fatalf("web changes = %+v, want none: a change in one repository is not another's", web)
	}
	post := func(path, body string) *http.Response {
		resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { resp.Body.Close() })
		return resp
	}
	if resp := post("/changes/stage", `{"repo":"api","paths":["new.go"]}`); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("stage in api = %d", resp.StatusCode)
	}
	if resp := post("/commit", fmt.Sprintf(`{"repo":"api","message":"add new","fingerprint":%q}`, stagedFingerprint(t, srv, "api"))); resp.StatusCode != http.StatusOK {
		t.Fatalf("commit in api = %d", resp.StatusCode)
	}
	if out := gitIn(t, filepath.Join(root, "api"), "log", "--format=%s", "-1"); strings.TrimSpace(out) != "add new" {
		t.Fatalf("api head = %q, want the new commit", out)
	}
	if out := gitIn(t, filepath.Join(root, "web"), "log", "--format=%s", "-1"); strings.TrimSpace(out) != "base web" {
		t.Fatalf("web head = %q: a commit in api moved another repository", out)
	}
}

func stagedFingerprint(t *testing.T, srv *httptest.Server, repo string) string {
	t.Helper()
	resp, err := http.Post(srv.URL+"/commit/propose?repo="+repo, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var p struct {
		Fingerprint string `json:"fingerprint"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil || p.Fingerprint == "" {
		t.Fatalf("propose in %s: status %d, fingerprint %q, %v", repo, resp.StatusCode, p.Fingerprint, err)
	}
	return p.Fingerprint
}

func TestARepositoryTheWorkspaceDoesNotHoldIsRefusedWithItsOwnCode(t *testing.T) {
	srv, _ := projectsFolder(t)
	for _, repo := range []string{"..", "../elsewhere", "/etc", "missing", "api/../web", "api/sub/deep"} {
		resp, err := http.Get(srv.URL + "/changes?repo=" + repo)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound || body.Code != "git.repo_unknown" {
			t.Errorf("repo=%q: %d %q, want 404 git.repo_unknown", repo, resp.StatusCode, body.Code)
		}
	}
	for _, path := range []string{"/changes/diff?path=x&repo=..", "/workspace/git?repo=..", "/workspace/git/log?repo=..", "/workspace/branches?repo=.."} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestACommitOpensIntoItsFilesAndEachDiff(t *testing.T) {
	srv, root := projectsFolder(t)
	id := strings.TrimSpace(gitIn(t, filepath.Join(root, "api"), "rev-parse", "HEAD"))
	files := hubGet[struct {
		Repo  bool `json:"repo"`
		Files []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"files"`
	}](t, srv, "/git/commit?repo=api&id="+id)
	if !files.Repo || len(files.Files) != 1 || files.Files[0].Path != "base.go" || files.Files[0].Status != "A" {
		t.Fatalf("files = %+v, want base.go added", files)
	}
	diff := hubGet[struct {
		Diff string `json:"diff"`
	}](t, srv, "/git/commit/diff?repo=api&id="+id+"&path=base.go")
	if !strings.Contains(diff.Diff, "+package a") {
		t.Fatalf("diff = %q", diff.Diff)
	}
	for url, want := range map[string]struct {
		status int
		code   string
	}{
		"/git/commit?repo=api&id=HEAD":                                     {400, "git.commit_bad_id"},
		"/git/commit?repo=api&id=--output=x":                               {400, "git.commit_bad_id"},
		"/git/commit?repo=api&id=0000000000000000000000000000000000000000": {404, "git.commit_unknown"},
		"/git/commit?repo=missing&id=" + id:                                {404, "git.repo_unknown"},
		"/git/commit/diff?repo=api&id=" + id + "&path=../x":                {400, "changes.path_outside_tree"},
	} {
		resp, err := http.Get(srv.URL + url)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode != want.status || body.Code != want.code {
			t.Errorf("%s = %d %q, want %d %q", url, resp.StatusCode, body.Code, want.status, want.code)
		}
	}
}

func TestDiscardingOneChangeThrowsItAwayAndRefusesWhatCannotBe(t *testing.T) {
	srv, root := projectsFolder(t)
	api := filepath.Join(root, "api")
	if err := os.WriteFile(filepath.Join(api, "base.go"), []byte("package a\nvar X = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(api, "fresh.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	discard := func(body string) (int, string) {
		resp, err := http.Post(srv.URL+"/changes/discard", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out.Code
	}
	for body, want := range map[string][2]any{
		`{"repo":"api","path":"fresh.go"}`:   {409, "discard.not_discardable"},
		`{"repo":"api","path":"nothing.go"}`: {400, "discard.unknown_path"},
		`{"repo":"..","path":"base.go"}`:     {404, "git.repo_unknown"},
		`{"repo":"web","path":"base.go"}`:    {400, "discard.unknown_path"},
		`not json`:                           {400, "discard.bad_request"},
	} {
		if status, code := discard(body); status != want[0].(int) || code != want[1].(string) {
			t.Errorf("%s = %d %q, want %d %q", body, status, code, want[0], want[1])
		}
	}
	if got, _ := os.ReadFile(filepath.Join(api, "base.go")); !strings.Contains(string(got), "var X = 1") {
		t.Fatal("a refused discard changed the file")
	}
	if status, _ := discard(`{"repo":"api","path":"base.go"}`); status != http.StatusNoContent {
		t.Fatalf("discard base.go = %d, want 204", status)
	}
	if got, _ := os.ReadFile(filepath.Join(api, "base.go")); string(got) != "package a\n" {
		t.Fatalf("base.go = %q, want the committed content", got)
	}
	if _, err := os.Stat(filepath.Join(api, "fresh.go")); err != nil {
		t.Fatal("discarding one file removed an untracked one")
	}
}
