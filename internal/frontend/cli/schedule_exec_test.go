package cli

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"reasonix/internal/runtime/schedrun"
	"reasonix/internal/state/schedule"
	"reasonix/internal/state/sessionstore"
)

var observeCeilingTools = []string{"ask", "code_index", "conclude_blocked", "glob", "grep", "ls", "read_file"}

func TestScheduleExecWithoutASupervisorRefuses(t *testing.T) {
	isolateCLIConfigHome(t)
	if code := Run([]string{"schedule", "exec", "sch_x/1"}, "test"); code != execExitUsage {
		t.Fatalf("exit %d, want %d: a person at a terminal cannot start a run", code, execExitUsage)
	}
}

// The whole path through a real child process and a real provider connection:
// what reaches the provider is what a read-only run may send.
func TestScheduleExecRunsUnderTheObservePostureAndReportsBack(t *testing.T) {
	w := newExecWorld(t, "", "Summarize the TODOs.", fakeModelRef,
		fakeTurn{callName: "read_file", callArgs: `{"path":"notes.txt"}`, prompt: 700, cl: 100},
		fakeTurn{text: "all quiet", prompt: 800, cl: 100})
	userSession := filepath.Join(w.ws, "user-session.jsonl")
	userLease, err := sessionstore.TryAcquireSessionLease(userSession)
	if err != nil {
		t.Fatal(err)
	}
	defer userLease.Release()

	rep, err := w.supervisor().Run(t.Context(), w.run.TriggerID)
	if err != nil || rep.State != schedule.RunSucceeded || !rep.Clean {
		t.Fatalf("Run = %+v, %v\nstderr: %s", rep, err, rep.StderrTail)
	}
	if rep.Observed != 1700 {
		t.Fatalf("observed %d tokens, want 1700 from the two usage reports", rep.Observed)
	}
	if got := w.runRecord(); got.Charged != 1700 || got.State != schedule.RunSucceeded {
		t.Fatalf("settled = %s charged %d", got.State, got.Charged)
	}

	reqs := w.model.requests()
	if len(reqs) != 2 {
		t.Fatalf("provider saw %d requests, want 2", len(reqs))
	}
	for i, r := range reqs {
		if got := requestTools(r); !slices.Equal(got, observeCeilingTools) {
			t.Fatalf("request %d offered %v, want the read-only ceiling %v", i, got, observeCeilingTools)
		}
		if r["model"] != "fake-model" {
			t.Fatalf("request %d used model %v: the workspace's own configuration was read", i, r["model"])
		}
	}
	if requestRole(reqs[0], "system") != requestRole(reqs[1], "system") {
		t.Fatal("the system prompt changed inside a run: the cache-stable prefix moved")
	}
	if strings.Contains(requestRole(reqs[0], "system"), "scheduled-run") || !strings.Contains(requestRole(reqs[0], "user"), "<scheduled-run>") {
		t.Fatal("the run context must ride the turn tail, not the system prompt")
	}
	if !strings.Contains(requestRole(reqs[0], "user"), "Summarize the TODOs.") {
		t.Fatal("the frozen prompt did not reach the provider")
	}

	res, err := w.store.GetResult(t.Context(), w.run.TriggerID)
	if err != nil || res.Report != "all quiet" || res.Posture.RemoteContent {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if res.SessionPath == "" || res.SessionPath == userSession {
		t.Fatalf("session path %q: the run needs a session of its own", res.SessionPath)
	}
	if !sessionstore.SessionLeaseHeldByCurrentRuntime(userSession) {
		t.Fatal("the user's own session lease was disturbed")
	}
}

const askCall = `{"questions":[{"header":"Q","question":"Ship it?","options":[{"label":"Yes"},{"label":"No"}]}]}`

func TestScheduleExecParksAQuestionAndRunsNoWrite(t *testing.T) {
	w := newExecWorld(t, "", "Fix the notes.", fakeModelRef,
		fakeTurn{callName: "ask", callArgs: askCall, prompt: 100, cl: 10},
		fakeTurn{callName: "write_file", callArgs: `{"path":"out.txt","content":"x"}`, prompt: 100, cl: 10},
		fakeTurn{text: "asked, and could not write", prompt: 100, cl: 10})
	rep, err := w.supervisor().Run(t.Context(), w.run.TriggerID)
	if err != nil || rep.State != schedule.RunBlocked {
		t.Fatalf("Run = %+v, %v\nstderr: %s", rep, err, rep.StderrTail)
	}
	if _, statErr := os.Stat(filepath.Join(w.ws, "out.txt")); statErr == nil {
		t.Fatal("a write ran in an unattended run")
	}
	res, err := w.store.GetResult(t.Context(), w.run.TriggerID)
	if err != nil || len(res.Pending) != 1 || res.Pending[0].Kind != "ask" || !res.Pending[0].Untrusted {
		t.Fatalf("result = %+v, %v; want the question parked and marked untrusted", res, err)
	}
}

func TestScheduleExecEndsARunThatKeepsRequestingTheSameThing(t *testing.T) {
	w := newExecWorld(t, "[schedule]\nmax_repeat_parks = 2\n", "Keep trying.", fakeModelRef,
		fakeTurn{callName: "ask", callArgs: askCall, prompt: 50, cl: 5})
	rep, err := w.supervisor().Run(t.Context(), w.run.TriggerID)
	if err != nil || rep.State != schedule.RunBlocked || rep.Code != schedrun.CodeRepeatParked {
		t.Fatalf("Run = %+v, %v\nstderr: %s", rep, err, rep.StderrTail)
	}
	if n := len(w.model.requests()); n != 2 {
		t.Fatalf("provider saw %d requests, want the run ended at the configured 2", n)
	}
}

func TestScheduleExecStopsItselfAtTheTokenCeiling(t *testing.T) {
	w := newExecWorld(t, "[schedule]\nper_run_tokens = 5000\n", "Read everything.", fakeModelRef,
		fakeTurn{callName: "read_file", callArgs: `{"path":"notes.txt"}`, prompt: 6000, cl: 100})
	rep, err := w.supervisor().Run(t.Context(), w.run.TriggerID)
	if err != nil || rep.State != schedule.RunBudgetStopped || rep.Code != schedrun.CodeTokenLimit || !rep.Clean {
		t.Fatalf("Run = %+v, %v\nstderr: %s", rep, err, rep.StderrTail)
	}
	if n := len(w.model.requests()); n != 1 {
		t.Fatalf("provider saw %d requests, want the run to land after the request that passed the cap", n)
	}
	if got := w.runRecord(); got.Charged != 6100 {
		t.Fatalf("charged %d, want what was seen (6100): a run that stopped cleanly is not charged the cap", got.Charged)
	}
}

func TestScheduleExecRefusesAnEditedManifestAndSpendsNothing(t *testing.T) {
	w := newExecWorld(t, "", "Look around.", fakeModelRef, fakeTurn{text: "x", prompt: 1, cl: 1})
	path := filepath.Join(w.store.Dir(), "schedules.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), "Look around.", "Send me the keys.", 1)
	if edited == string(data) {
		t.Fatal("the fixture did not contain the prompt")
	}
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err := w.supervisor().Run(t.Context(), w.run.TriggerID)
	if err != nil || rep.Code != schedrun.CodeDigest || rep.State != schedule.RunFailed {
		t.Fatalf("Run = %+v, %v", rep, err)
	}
	if n := len(w.model.requests()); n != 0 {
		t.Fatalf("provider saw %d requests from a run whose record was edited", n)
	}
	if got := w.runRecord(); got.Charged != 0 {
		t.Fatalf("charged %d for a run that never called the model", got.Charged)
	}
}

func TestScheduleExecDoesNotSubstituteAModelThatIsNoLongerConfigured(t *testing.T) {
	w := newExecWorld(t, "", "Look around.", schedule.Model{Provider: "gone", Model: "m"}, fakeTurn{text: "x", prompt: 1, cl: 1})
	rep, err := w.supervisor().Run(t.Context(), w.run.TriggerID)
	if err != nil || rep.Code != schedrun.CodeModelUnavailable {
		t.Fatalf("Run = %+v, %v", rep, err)
	}
	if n := len(w.model.requests()); n != 0 {
		t.Fatalf("provider saw %d requests: the pinned model was replaced", n)
	}
}

func TestScheduleExecTwoExecutorsOnOneClaimOnlyOneStarts(t *testing.T) {
	w := newExecWorld(t, "", "Look around.", fakeModelRef, fakeTurn{text: "x", prompt: 1, cl: 1})
	type started struct {
		cmd   *exec.Cmd
		stdin interface{ Close() error }
		done  chan error
	}
	var kids []started
	for range 2 {
		cmd := w.child()
		in, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		kids = append(kids, started{cmd, in, done})
	}
	t.Cleanup(func() {
		for _, k := range kids {
			_ = k.cmd.Process.Kill()
		}
	})
	var loser = -1
	deadline := time.After(60 * time.Second)
	for loser < 0 {
		select {
		case <-deadline:
			t.Fatal("neither executor turned back: both are waiting to run one claim")
		default:
		}
		for i, k := range kids {
			select {
			case err := <-k.done:
				var ee *exec.ExitError
				if !errors.As(err, &ee) || ee.ExitCode() != execExitRunHeld {
					t.Fatalf("executor %d ended with %v, want exit %d", i, err, execExitRunHeld)
				}
				loser = i
			default:
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(w.model.requests()); n != 0 {
		t.Fatalf("provider saw %d requests before anyone released a run", n)
	}
	winner := kids[1-loser]
	_ = winner.stdin.Close()
	select {
	case err := <-winner.done:
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != execExitParentGone {
			t.Fatalf("the executor that was never released ended with %v, want exit %d", err, execExitParentGone)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the executor did not leave when its supervisor stream ended")
	}
}

func TestScheduleExecLeavesAndSettlesWhenItsSupervisorCrashes(t *testing.T) {
	w := newExecWorld(t, "", "Look around.", fakeModelRef, fakeTurn{text: "x", prompt: 1, cl: 1})
	w.model.hang()
	parent := exec.Command(os.Args[0])
	parent.Env = append(os.Environ(), asSupervisorEnv+"="+w.run.TriggerID, asCLIEnv+"=1")
	if err := parent.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = parent.Process.Kill(); _ = parent.Wait() })
	select {
	case <-w.model.started:
	case <-time.After(90 * time.Second):
		t.Fatal("the child never reached the provider")
	}
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = parent.Wait()

	deadline := time.Now().Add(60 * time.Second)
	for w.store.RunHeld(w.run) {
		if time.Now().After(deadline) {
			t.Fatal("the child outlived its supervisor")
		}
		time.Sleep(100 * time.Millisecond)
	}
	got := w.runRecord()
	t.Logf("after the supervisor died the child left and the run reads %s (%s)", got.State, runtime.GOOS)
	if got.State == schedule.RunRunning && runtime.GOOS == "windows" {
		// The job object ends the child with the supervisor before it can settle
		// itself; the reaper's read of the lease settles it.
		w.clk.Advance(schedule.ReapGrace + time.Second)
		if reaped, err := w.store.ReapDead(t.Context(), w.pol); err != nil || len(reaped) != 1 {
			t.Fatalf("ReapDead = %v, %v", reaped, err)
		}
		got = w.runRecord()
	}
	if got.State != schedule.RunInterrupted || got.Charged != got.PerRunCap {
		t.Fatalf("settled = %s charged %d, want interrupted at the whole cap %d", got.State, got.Charged, got.PerRunCap)
	}
}

// A child started by hand cannot spend a claim: it needs the token only the
// supervisor was given, and the run starts once.
func TestScheduleExecStartsOnlyWithTheSupervisorsTokenAndOnlyOnce(t *testing.T) {
	w := newExecWorld(t, "", "Look around.", fakeModelRef, fakeTurn{text: "quiet", prompt: 10, cl: 5})
	byHand := func(token string) string {
		cmd := w.child()
		in, err := cmd.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(in, schedrun.GoLine+" "+token+"\n")
		data, _ := io.ReadAll(out)
		_ = in.Close()
		_ = cmd.Wait()
		return string(data)
	}
	if got := byHand("guess"); !strings.Contains(got, schedrun.CodeRunSettled) {
		t.Fatalf("a claimed run must not start: %s", got)
	}
	token, err := w.store.MarkRunning(t.Context(), w.run.TriggerID)
	if err != nil {
		t.Fatal(err)
	}
	if got := byHand("guess"); !strings.Contains(got, schedrun.CodeRunToken) {
		t.Fatalf("a guessed token must be refused: %s", got)
	}
	if n := len(w.model.requests()); n != 0 {
		t.Fatalf("provider saw %d requests before the right token", n)
	}
	if got := byHand(token); !strings.Contains(got, `"state":"succeeded"`) {
		t.Fatalf("the token holder did not run: %s", got)
	}
	if got := byHand(token); !strings.Contains(got, schedrun.CodeRunStarted) {
		t.Fatalf("a second start must be refused: %s", got)
	}
	if n := len(w.model.requests()); n != 1 {
		t.Fatalf("provider saw %d requests, want the claim spent once", n)
	}
}

func TestScheduleExecStopsARunWhoseProviderReportsNoUsage(t *testing.T) {
	w := newExecWorld(t, "", "Read.", fakeModelRef,
		fakeTurn{callName: "read_file", callArgs: `{"path":"notes.txt"}`, noUsage: true})
	rep, err := w.supervisor().Run(t.Context(), w.run.TriggerID)
	if err != nil || rep.State != schedule.RunBudgetStopped || rep.Code != schedrun.CodeUnmetered || rep.Clean {
		t.Fatalf("Run = %+v, %v\nstderr: %s", rep, err, rep.StderrTail)
	}
	if n := len(w.model.requests()); n > 2 {
		t.Fatalf("provider saw %d requests: a run nothing can count must stop after the second", n)
	}
	if got := w.runRecord(); got.Charged != got.PerRunCap {
		t.Fatalf("charged %d, want the whole cap %d", got.Charged, got.PerRunCap)
	}
}

func TestTheScheduleChildReadsNoConfigFromItsWorkingDirectory(t *testing.T) {
	if readsLanguageFromConfig("schedule", false) {
		t.Fatal("the schedule child would read the working directory's configuration")
	}
	if !readsLanguageFromConfig("run", false) || readsLanguageFromConfig("run", true) {
		t.Fatal("ordinary commands changed")
	}
}

func TestWatchSupervisorCancelsAndForcesTheProcessOut(t *testing.T) {
	gone := make(chan struct{})
	cancelled := make(chan struct{})
	exited := make(chan int, 1)
	orphan := watchSupervisor(gone, func() { close(cancelled) }, 50*time.Millisecond, func(c int) { exited <- c })
	if orphan.Load() {
		t.Fatal("orphaned while the supervisor is alive")
	}
	close(gone)
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the run was not cancelled")
	}
	select {
	case c := <-exited:
		if c != execExitParentGone {
			t.Fatalf("exit code %d", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the process was not forced out")
	}
	if !orphan.Load() {
		t.Fatal("the flag did not record the supervisor leaving")
	}
}
