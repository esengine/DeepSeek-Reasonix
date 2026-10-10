package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitstatus"
	"reasonix/internal/session/control"
	"reasonix/internal/state/workspacelease"
	"reasonix/internal/tools/jobs"
)

// branchServer is a controller over a workspace with one commit on "trunk" and
// a second local "topic", the way a session that opened the folder holds it.
func branchServer(t *testing.T) (srv, dir string) {
	t.Helper()
	dir = testenv.TempDir(t)
	for _, args := range [][]string{
		{"init", "-q", "-b", "trunk"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-qm", "base", "--allow-empty"},
		{"branch", "topic"},
	} {
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
	return changesServer(t, dir, repo), dir
}

func fetchInto(t *testing.T, url string, into any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %v %v", url, err, resp)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
		t.Fatal(err)
	}
}

func switchTo(t *testing.T, srv, name string) (int, string) {
	t.Helper()
	resp, err := http.Post(srv+"/workspace/branch/switch", "application/json",
		strings.NewReader(`{"name":`+strconv.Quote(name)+`}`))
	if err != nil {
		t.Fatalf("POST /workspace/branch/switch: %v", err)
	}
	defer resp.Body.Close()
	raw := new(strings.Builder)
	_, _ = io.Copy(raw, resp.Body)
	return resp.StatusCode, raw.String()
}

// The composer's branch menu reads this: the locals, the current one marked.
func TestWorkspaceBranchesListAndMarkCurrent(t *testing.T) {
	srv, _ := branchServer(t)
	var body struct {
		Repo     bool `json:"repo"`
		Branches []struct {
			Name    string `json:"name"`
			Current bool   `json:"current"`
		} `json:"branches"`
	}
	fetchInto(t, srv+"/workspace/branches", &body)
	if !body.Repo || len(body.Branches) != 2 {
		t.Fatalf("got %+v, want two locals", body)
	}
	for _, b := range body.Branches {
		switch b.Name {
		case "trunk":
			if !b.Current {
				t.Fatalf("trunk is the held branch, got %+v", body.Branches)
			}
		case "topic":
			if b.Current {
				t.Fatalf("topic should be an unmarked local, got %+v", body.Branches)
			}
		default:
			t.Fatalf("unexpected branch %q in %+v", b.Name, body.Branches)
		}
	}
}

func TestWorkspaceSwitchBranchMovesHeadAndAnswersTheNewState(t *testing.T) {
	srv, dir := branchServer(t)
	code, raw := switchTo(t, srv, "topic")
	if code != http.StatusOK {
		t.Fatalf("switch to topic = %d %s", code, raw)
	}
	var body struct {
		Repo   bool   `json:"repo"`
		Branch string `json:"branch"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Repo || body.Branch != "topic" {
		t.Fatalf("the answer should carry the new identity, got %+v", body)
	}
	// And the answer really is the tree's state, not the switch's word.
	repo, err := gitcmd.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if info, ok := gitstatus.Summary(context.Background(), repo); !ok || info.Branch != "topic" {
		t.Fatalf("HEAD after switch: %+v ok=%v", info, ok)
	}
}

func TestWorkspaceSwitchBranchRefusesWithACode(t *testing.T) {
	srv, _ := branchServer(t)
	// A name that is not a branch, and one that reads as an option.
	for _, name := range []string{"no-such-branch", "-C", ""} {
		code, raw := switchTo(t, srv, name)
		if code == http.StatusOK {
			t.Fatalf("switch to %q = %d %s, want a refusal", name, code, raw)
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal([]byte(raw), &body); err != nil || body.Code == "" {
			t.Fatalf("switch to %q answered without a code: %d %s (%v)", name, code, raw, err)
		}
	}
	// A body that is not the request at all.
	resp, err := http.Post(srv+"/workspace/branch/switch", "application/json", strings.NewReader("{oops"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("a body that is not JSON = %d, want 400", resp.StatusCode)
	}
}

func TestWorkspaceSwitchBranchOutsideARepositoryRefuses(t *testing.T) {
	srv := changesServer(t, testenv.TempDir(t), gitcmd.Repo{Dir: "nowhere"})
	code, raw := switchTo(t, srv, "main")
	if code != http.StatusConflict {
		t.Fatalf("switch with no repository = %d %s, want 409", code, raw)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil || body.Code != "branch.no_repository" {
		t.Fatalf("want branch.no_repository, got %d %s (%v)", code, raw, err)
	}
}

func TestWorkspaceBranchesWithoutRepositoryIsEmpty(t *testing.T) {
	dir := testenv.TempDir(t)
	srv := changesServer(t, dir, gitcmd.Repo{Dir: dir})
	var body workspaceBranchesView
	fetchInto(t, srv+"/workspace/branches", &body)
	if body.Repo || body.Branches == nil || len(body.Branches) != 0 {
		t.Fatalf("non-repository branch list = %+v, want repo=false and an empty array", body)
	}
}

func TestWorkspaceSwitchRefusalClassesAreDistinct(t *testing.T) {
	for _, tc := range []struct {
		cause error
		code  string
	}{
		{control.ErrTurnRunning, "branch.turn_running"},
		{control.ErrJobsRunning, "branch.jobs_running"},
		{control.ErrWorkspaceBusy, "branch.workspace_busy"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			reply := httptest.NewRecorder()
			refuseBranch(reply, httptest.NewRequest(http.MethodPost, "/workspace/branch/switch", nil), fmt.Errorf("wrapped: %w", tc.cause))
			var reason Reason
			if err := json.Unmarshal(reply.Body.Bytes(), &reason); err != nil {
				t.Fatal(err)
			}
			if reply.Code != http.StatusConflict || reason.Code != tc.code || reason.Message == "" {
				t.Fatalf("refusal = %d %+v", reply.Code, reason)
			}
		})
	}
}

func TestWorkspaceSwitchLeaseContentionReturnsWorkspaceBusy(t *testing.T) {
	srv, dir := branchServer(t)
	lease, err := workspacelease.New(dir, config.WorkspaceLeaseDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	lease.BeginRun()
	defer lease.EndRun()
	if err := lease.AcquireWrite(t.Context()); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	code, raw := switchTo(t, srv, "topic")
	var reason Reason
	if err := json.Unmarshal([]byte(raw), &reason); err != nil {
		t.Fatal(err)
	}
	if code != http.StatusConflict || reason.Code != "branch.workspace_busy" {
		t.Fatalf("lease refusal = %d %s", code, raw)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("lease refusal was not bounded: %s", time.Since(started))
	}
	repo, err := gitcmd.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	info, ok := gitstatus.Summary(t.Context(), repo)
	if !ok || info.Branch != "trunk" {
		t.Fatalf("busy checkout changed HEAD: %+v", info)
	}
}

type branchBackgroundRunner struct {
	manager *jobs.Manager
	started chan struct{}
	release chan struct{}
}

func (r *branchBackgroundRunner) Run(context.Context, string) error {
	r.manager.Start("bash", "development server", func(context.Context, io.Writer) (string, error) { close(r.started); <-r.release; return "", nil })
	return nil
}

func TestWorkspaceSwitchReportsAnotherPanesBackgroundJob(t *testing.T) {
	srv, dir := branchServer(t)
	repo, err := gitcmd.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	manager := jobs.NewManager(event.Discard)
	runner := &branchBackgroundRunner{manager: manager, started: make(chan struct{}), release: make(chan struct{})}
	peer := control.New(control.Options{Runner: runner, Jobs: manager, Sink: event.Discard, WorkspaceRoot: dir, WorkspaceRepo: repo})
	t.Cleanup(func() { close(runner.release); peer.Close() })
	if err := peer.RunTurn(t.Context(), "start development server"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("background job did not start")
	}
	code, raw := switchTo(t, srv, "topic")
	var reason Reason
	if err := json.Unmarshal([]byte(raw), &reason); err != nil {
		t.Fatal(err)
	}
	if code != http.StatusConflict || reason.Code != "branch.jobs_running" {
		t.Fatalf("background refusal = %d %s", code, raw)
	}
	info, ok := gitstatus.Summary(t.Context(), repo)
	if !ok || info.Branch != "trunk" {
		t.Fatalf("job refusal changed HEAD: %+v", info)
	}
}
