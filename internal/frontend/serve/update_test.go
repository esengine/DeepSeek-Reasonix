package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/appupdate"
	"reasonix/internal/platform/update"
)

type stubUpdateHost struct {
	acknowledged int
	started      []string
	install      update.Install
	startErr     error
	committed    []string
	commitErr    error
	progress     update.Progress
}

func (s *stubUpdateHost) AcknowledgeLaunchHealth() error { s.acknowledged++; return nil }

func (s *stubUpdateHost) StartInstall(install update.Install, target string) error {
	s.install = install
	s.started = append(s.started, target)
	return s.startErr
}

func (s *stubUpdateHost) CommitInstall(target string) error {
	s.committed = append(s.committed, target)
	return s.commitErr
}

func (s *stubUpdateHost) InstallProgress() update.Progress { return s.progress }

// Sharing an update engine with a desktop application must not hand a
// networked kernel the power to replace one. Owning the application is what
// makes the routes exist, so a hub without an owner has none to refuse.
func TestUpdateRoutesExistOnlyWhereSomethingOwnsTheApplication(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	bare := httptest.NewServer(operatorHandler(NewHub(HubOptions{})))
	defer bare.Close()

	resp, err := http.Post(bare.URL+"/update/health", "application/json", strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		t.Fatalf("POST /update/health answered %d with no application behind it", resp.StatusCode)
	}
}

// And the other direction, because a gate nothing passes is not a gate: the
// acknowledgement reaches the owner, carrying nothing of its own.
func TestAnOwnedApplicationAcknowledgesItsOwnLaunch(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	host := &stubUpdateHost{}
	owned := httptest.NewServer(operatorHandler(NewHub(HubOptions{Update: host})))
	defer owned.Close()

	resp, err := http.Post(owned.URL+"/update/health", "application/json", strings.NewReader(`{"transaction":"somebody else's"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /update/health = %d, want 204", resp.StatusCode)
	}
	if host.acknowledged != 1 {
		t.Fatalf("acknowledged %d times, want 1", host.acknowledged)
	}
}

// The install route needs both gates and they answer different questions:
// owning the application is what makes replacing it this process's business,
// and the declared install is which build is being replaced. A hub with an
// owner and no install must refuse by name rather than start a move over a
// layout nobody stated.
func TestInstallRefusesWhereNoInstallWasDeclared(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	host := &stubUpdateHost{}
	owned := httptest.NewServer(operatorHandler(NewHub(HubOptions{Update: host})))
	defer owned.Close()

	resp, err := http.Post(owned.URL+"/update/install", "application/json", strings.NewReader(`{"version":"v2.0.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("POST /update/install = %d, want 404", resp.StatusCode)
	}
	if len(host.started) != 0 {
		t.Fatalf("a move started with no install declared: %v", host.started)
	}
}

// The hub hands over its own declared install rather than letting the host keep
// a second copy: two answers to "which build is running" is how a swap lands on
// the wrong layout while both sides believe they agree.
func TestInstallCarriesTheHubsDeclaredInstall(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	host := &stubUpdateHost{}
	declared := update.Install{Version: "v1.0.0", Layout: update.Layout{Root: "/opt/studio", Executable: "/opt/studio/studio"}}
	owned := httptest.NewServer(operatorHandler(NewHub(HubOptions{Update: host, Install: &declared})))
	defer owned.Close()

	resp, err := http.Post(owned.URL+"/update/install", "application/json", strings.NewReader(`{"version":"v2.0.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Accepted, not OK: the move is under way, and an install that works ends
	// by ending this process, so there is no later answer to wait for.
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("POST /update/install = %d, want 202", resp.StatusCode)
	}
	if len(host.started) != 1 || host.started[0] != "v2.0.0" {
		t.Fatalf("started %v, want one move to v2.0.0", host.started)
	}
	if host.install != declared {
		t.Fatalf("the move was handed %+v, want the hub's declared %+v", host.install, declared)
	}
}

// A second move over the first would give one set of files two writers. It is
// told apart from every other refusal because the thing to do about it is
// different: wait, rather than fix what was asked for.
func TestASecondInstallIsRefusedAsAlreadyRunning(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	declared := update.Install{Version: "v1.0.0", Layout: update.Layout{Root: "/opt/studio"}}
	host := &stubUpdateHost{startErr: appupdate.ErrInstallInFlight}
	owned := httptest.NewServer(operatorHandler(NewHub(HubOptions{Update: host, Install: &declared})))
	defer owned.Close()

	resp, err := http.Post(owned.URL+"/update/install", "application/json", strings.NewReader(`{"version":"v2.0.0"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("POST /update/install = %d, want 409", resp.StatusCode)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Code != codeInstallRunning {
		t.Fatalf("refusal code %q, want %q", body.Code, codeInstallRunning)
	}
}

// Progress is pulled. The read answers from what the host holds, so a client
// that missed a frame is restored by asking again rather than by a replay
// nothing is keeping.
func TestInstallProgressIsRead(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	host := &stubUpdateHost{progress: update.Progress{Version: "v2.0.0", Phase: update.PhaseVerifying, Received: 7, Total: 7}}
	owned := httptest.NewServer(operatorHandler(NewHub(HubOptions{Update: host})))
	defer owned.Close()

	resp, err := http.Get(owned.URL + "/update/install")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got update.Progress
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got != host.progress {
		t.Fatalf("GET /update/install = %+v, want %+v", got, host.progress)
	}
}

func postRestart(t *testing.T, url, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url+"/update/restart", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Code string `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	return resp.StatusCode, got.Code
}

// Downloading and restarting are two acts: the restart reaches the host only
// when asked for, and names the release it restarts into.
func TestRestartCommitsTheReadyRelease(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	host := &stubUpdateHost{}
	owned := httptest.NewServer(operatorHandler(NewHub(HubOptions{Update: host})))
	defer owned.Close()

	if status, _ := postRestart(t, owned.URL, `{"version":"v2.0.0"}`); status != http.StatusAccepted {
		t.Fatalf("POST /update/restart = %d, want 202", status)
	}
	if len(host.committed) != 1 || host.committed[0] != "v2.0.0" {
		t.Fatalf("committed %v, want one restart into v2.0.0", host.committed)
	}
}

// Nothing verified to restart into is its own answer: the panel offers to
// download again rather than reporting a failed install.
func TestRestartWithNothingReadyIsRefusedByName(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	host := &stubUpdateHost{commitErr: appupdate.ErrNothingReady}
	owned := httptest.NewServer(operatorHandler(NewHub(HubOptions{Update: host})))
	defer owned.Close()

	status, code := postRestart(t, owned.URL, `{"version":"v2.0.0","force":true}`)
	if status != http.StatusConflict || code != codeNothingToApply {
		t.Fatalf("POST /update/restart = %d %q, want 409 %q", status, code, codeNothingToApply)
	}
}
