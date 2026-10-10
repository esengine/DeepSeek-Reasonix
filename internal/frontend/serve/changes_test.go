package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitstatus"
	"reasonix/internal/session/control"
)

// The panel behind this endpoint used to infer pending changes from tool events,
// which cannot see a file a shell command removed. /changes answers from the
// tree, and says repo=false rather than "nothing changed" when there is no git.
// The repository is the one the session resolved when it opened.
func TestChangesReportsTheTreeNotTheTranscript(t *testing.T) {
	dir := testenv.TempDir(t)
	srv := changesServer(t, dir, gitcmd.Repo{Dir: dir})

	var body struct {
		Repo    bool `json:"repo"`
		Changes []struct {
			Path   string `json:"path"`
			Status string `json:"status"`
		} `json:"changes"`
	}
	read := func() {
		t.Helper()
		resp, err := http.Get(srv + "/changes")
		if err != nil {
			t.Fatalf("GET /changes: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /changes = %d", resp.StatusCode)
		}
		body.Changes = nil
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}

	read()
	if body.Repo {
		t.Fatalf("a workspace with no git should report repo=false, got %+v", body)
	}

	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base", "--allow-empty"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v\n%s", err, out)
		}
	}
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	srv = changesServer(t, dir, repo)
	if err := os.WriteFile(filepath.Join(dir, "scratch.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	read()
	if !body.Repo || len(body.Changes) != 1 || body.Changes[0].Path != "scratch.go" {
		t.Fatalf("after create: %+v, want one untracked scratch.go", body)
	}
	if err := os.Remove(filepath.Join(dir, "scratch.go")); err != nil {
		t.Fatal(err)
	}
	read()
	if len(body.Changes) != 0 {
		t.Fatalf("after delete: %+v, want no pending change", body)
	}
}

func changesServer(t *testing.T, dir string, repo gitcmd.Repo) string {
	t.Helper()
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Runner: fakeRunner{}, Sink: bc, WorkspaceRoot: dir, WorkspaceRepo: repo})
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestWorkspaceGitReportsBranchAndDirtOfTheSessionRepo(t *testing.T) {
	dir := testenv.TempDir(t)
	read := func(srv string) (body struct {
		Repo      bool   `json:"repo"`
		Name      string `json:"name"`
		Branch    string `json:"branch"`
		Untracked int    `json:"untracked"`
	}) {
		t.Helper()
		resp, err := http.Get(srv + "/workspace/git")
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /workspace/git: %v %v", err, resp)
		}
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if got := read(changesServer(t, dir, gitcmd.Repo{Dir: dir})); got.Repo {
		t.Fatalf("no git must say repo=false, got %+v", got)
	}
	for _, args := range [][]string{{"init", "-q", "-b", "trunk"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base", "--allow-empty"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v\n%s", err, out)
		}
	}
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := read(changesServer(t, dir, repo))
	if !got.Repo || got.Name != filepath.Base(dir) || got.Branch != "trunk" || got.Untracked != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestWorkspaceGitWirePreservesSummaryFields(t *testing.T) {
	dir := testenv.TempDir(t)
	for _, args := range [][]string{{"init", "-q", "-b", "trunk"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base", "--allow-empty"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v\n%s", err, out)
		}
	}
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scratch.go"), []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, ok := gitstatus.Summary(context.Background(), repo)
	if !ok {
		t.Fatal("test repository has no summary")
	}
	encoded, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]any
	if err := json.Unmarshal(encoded, &want); err != nil {
		t.Fatal(err)
	}
	want["repo"] = true
	resp, err := http.Get(changesServer(t, dir, repo) + "/workspace/git")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workspace Git wire = %#v; want %#v", got, want)
	}
}
