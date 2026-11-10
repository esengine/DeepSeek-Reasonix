package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/runtime/schedrun"
	"reasonix/internal/runtime/scheduler"
	"reasonix/internal/state/schedule"
)

type schedWorld struct {
	t     *testing.T
	ws    string
	now   time.Time
	out   bytes.Buffer
	err   bytes.Buffer
	input string
	human bool
	// agentChild simulates a command started by an agent tool.
	agentChild bool
}

func newSchedWorld(t *testing.T) *schedWorld {
	t.Helper()
	isolateCLIConfigHome(t)
	home := os.Getenv("HOME")
	cfg := `default_model = "local/fake-model"

[[providers]]
name = "local"
kind = "acp-test-provider"
base_url = "http://example.invalid"
model = "fake-model"
api_key_env = "REASONIX_TEST_KEY"
`
	t.Setenv("REASONIX_TEST_KEY", "k")
	if err := os.MkdirAll(config.RootsForHome("").Home(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.RootsForHome("").UserConfigLoadPath(), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = home
	w := &schedWorld{t: t, ws: testenv.TempDir(t), now: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC), human: true}
	w.trust(w.ws)
	t.Chdir(w.ws)
	return w
}

// trust marks dir a trusted project, which is the only kind a schedule may read.
func (w *schedWorld) trust(dir string) {
	w.t.Helper()
	if err := config.NewProjectGrantStore(config.RootsForHome("").Home()).SetTrust(dir, config.WorkspaceTrusted); err != nil {
		w.t.Fatal(err)
	}
}

func (w *schedWorld) run(args ...string) int {
	w.t.Helper()
	w.out.Reset()
	w.err.Reset()
	return scheduleManage(args, scheduleIO{in: w.stdin(), out: &w.out, err: &w.err, human: w.human, agentChild: w.agentChild, now: func() time.Time { return w.now }})
}

// stdin answers "CONFIRM" with whatever plan code the command printed, as a
// person reading the prompt would; any other input is passed through.
func (w *schedWorld) stdin() io.Reader {
	if w.input != "CONFIRM" {
		return strings.NewReader(w.input)
	}
	return readerFunc(func(p []byte) (int, error) {
		m := regexp.MustCompile(`Type ([0-9a-f]{6}) to create`).FindStringSubmatch(w.out.String())
		if m == nil {
			return 0, io.EOF
		}
		return copy(p, m[1]+"\n"), io.EOF
	})
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func (w *schedWorld) store() *schedule.Store {
	w.t.Helper()
	st, _, err := openScheduleStore(func() time.Time { return w.now })
	if err != nil {
		w.t.Fatal(err)
	}
	return st
}

func (w *schedWorld) schedules() []schedule.Schedule {
	m, _, err := w.store().Snapshot(context.Background())
	if err != nil {
		w.t.Fatal(err)
	}
	return m.Schedules
}

func (w *schedWorld) add() schedule.Schedule {
	w.t.Helper()
	w.input = "CONFIRM"
	if code := w.run("add", "--every", "2h", "--prompt", "summarize TODOs", "--workspace", w.ws); code != 0 {
		w.t.Fatalf("add = %d: %s %s", code, w.out.String(), w.err.String())
	}
	got := w.schedules()
	if len(got) != 1 {
		w.t.Fatalf("schedules = %d", len(got))
	}
	return got[0]
}

func TestScheduleAddShowsWhatIsConfirmedAndCreatesOnlyOnYes(t *testing.T) {
	w := newSchedWorld(t)
	w.input = "n\n"
	if code := w.run("add", "--every", "2h", "--prompt", "summarize TODOs", "--workspace", w.ws, "--label", "morning"); code != 0 {
		t.Fatalf("exit %d: %s", code, w.err.String())
	}
	realWS, err := filepath.EvalSymlinks(w.ws)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"every 2h0m0s", "first run:   2026-", realWS, "local/fake-model", "summarize TODOs", "up to 250000 tokens", "at most:     1000000 tokens per day", "parked for you"} {
		if !strings.Contains(w.out.String(), want) {
			t.Fatalf("preview lacks %q:\n%s", want, w.out.String())
		}
	}
	if len(w.schedules()) != 0 {
		t.Fatal("a declined confirmation still created a schedule")
	}
	w.input = "y\n"
	if code := w.run("add", "--every", "2h", "--prompt", "summarize TODOs", "--workspace", w.ws); code != 0 || len(w.schedules()) != 0 {
		t.Fatalf("typing y instead of the plan id created something: %d %d", code, len(w.schedules()))
	}
	sc := w.add()
	if sc.Confirmed.Via != schedule.ConfirmedViaTerminal {
		t.Fatalf("via = %q", sc.Confirmed.Via)
	}
	w.run("list")
	if !strings.Contains(w.out.String(), "created by terminal") {
		t.Fatalf("list does not say who created it:\n%s", w.out.String())
	}
	if sc.Confirmed.By != schedule.ConfirmedByHuman || sc.Model.Provider != "local" || sc.Model.Model != "fake-model" || sc.Trigger.EverySec != 7200 {
		t.Fatalf("schedule = %+v", sc)
	}
}

// A command run by a tool, a pipe or a scheduler has no terminal: it cannot
// create, resume, renew or delete anything, whatever it types.
func TestSchedulesNeedAPersonAtATerminalToCreateResumeOrDelete(t *testing.T) {
	w := newSchedWorld(t)
	sc := w.add()
	w.human = false
	w.input = "y\n"
	for _, args := range [][]string{
		{"add", "--every", "2h", "--prompt", "x", "--workspace", w.ws},
		{"resume", sc.ID}, {"renew", sc.ID}, {"rm", sc.ID}, {"resume-all"},
	} {
		if code := w.run(args...); code != scheduleExitNotHuman {
			t.Fatalf("%v = %d, want %d", args, code, scheduleExitNotHuman)
		}
	}
	if got := w.schedules(); len(got) != 1 || got[0].Status != schedule.StatusActive {
		t.Fatalf("a refused command changed the store: %+v", got)
	}
	if code := w.run("pause", sc.ID); code != 0 {
		t.Fatalf("pausing is protective and needs no terminal: %d %s", code, w.err.String())
	}
	if code := w.run("pause-all"); code != 0 {
		t.Fatalf("pause-all = %d", code)
	}
}

// A terminal an agent tool opened for itself passes the terminal check; the
// marker the host puts on its subprocesses is what refuses it.
func TestSchedulesRefuseACommandStartedByAnAgentTool(t *testing.T) {
	w := newSchedWorld(t)
	sc := w.add()
	w.agentChild = true
	w.input = "CONFIRM"
	for _, args := range [][]string{
		{"add", "--every", "2h", "--prompt", "x", "--workspace", w.ws},
		{"resume", sc.ID}, {"renew", sc.ID}, {"rm", sc.ID}, {"resume-all"},
	} {
		if code := w.run(args...); code != scheduleExitNotHuman {
			t.Fatalf("%v = %d, want %d", args, code, scheduleExitNotHuman)
		}
		if !strings.Contains(w.err.String(), "started by an agent tool") {
			t.Fatalf("%v gave no reason: %q", args, w.err.String())
		}
	}
	if got := w.schedules(); len(got) != 1 {
		t.Fatalf("a refused command changed the store: %+v", got)
	}
}

func TestScheduleAddRefusesWhatCannotBeConfirmed(t *testing.T) {
	w := newSchedWorld(t)
	w.input = "CONFIRM"
	for name, args := range map[string][]string{
		"both triggers":  {"add", "--every", "2h", "--at", "2030-01-01 09:00", "--prompt", "x", "--workspace", w.ws},
		"no trigger":     {"add", "--prompt", "x", "--workspace", w.ws},
		"a bad interval": {"add", "--every", "soon", "--prompt", "x", "--workspace", w.ws},
		"no folder":      {"add", "--every", "2h", "--prompt", "x", "--workspace", filepath.Join(w.ws, "nope")},
		"unknown model":  {"add", "--every", "2h", "--prompt", "x", "--workspace", w.ws, "--model", "nobody/none"},
	} {
		if code := w.run(args...); code == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	if code := w.run("add", "--every", "1m", "--prompt", "x", "--workspace", w.ws); code != scheduleExitFailed || !strings.Contains(w.err.String(), "below the minimum") {
		t.Fatalf("an interval under the floor = %d %s", code, w.err.String())
	}
	if len(w.schedules()) != 0 {
		t.Fatal("a refused add left a schedule")
	}
}

func TestScheduleListShowsSchedulesRunsAndTheGlobalPause(t *testing.T) {
	w := newSchedWorld(t)
	sc := w.add()
	w.run("pause-all")
	if code := w.run("list"); code != 0 {
		t.Fatalf("list = %d", code)
	}
	for _, want := range []string{"ALL SCHEDULES ARE PAUSED", sc.ID, "active", "every 2h0m0s", "spent 0 of 3000000 tokens"} {
		if !strings.Contains(w.out.String(), want) {
			t.Fatalf("list lacks %q:\n%s", want, w.out.String())
		}
	}
}

type settlingRunner struct{ st *schedule.Store }

func (r settlingRunner) Run(ctx context.Context, id string) (schedrun.Report, error) {
	return schedrun.Report{TriggerID: id, State: schedule.RunSucceeded}, r.st.Finish(ctx, schedule.DefaultPolicy(), id, schedule.Outcome{State: schedule.RunSucceeded, Observed: 10, Clean: true})
}

func TestScheduleTickClaimsWhatIsDueRunsItAndSaysSo(t *testing.T) {
	w := newSchedWorld(t)
	sc := w.add()
	prev := scheduleSupervisorForCLI
	t.Cleanup(func() { scheduleSupervisorForCLI = prev })
	scheduleSupervisorForCLI = func(st *schedule.Store, _ schedule.Policy) scheduler.Runner { return settlingRunner{st} }

	if code := w.run("tick"); code != 0 || strings.Contains(w.out.String(), "ran ") {
		t.Fatalf("tick before the slot = %d %s", code, w.out.String())
	}
	w.now = w.now.Add(2*time.Hour + time.Minute)
	if code := w.run("tick"); code != 0 || !strings.Contains(w.out.String(), "ran "+sc.ID+"/") || !strings.Contains(w.out.String(), "succeeded") {
		t.Fatalf("tick at the slot = %d %s %s", code, w.out.String(), w.err.String())
	}
	if code := w.run("tick"); code != 0 || strings.Contains(w.out.String(), "ran ") {
		t.Fatalf("the same slot ran twice: %s", w.out.String())
	}
	w.run("pause-all")
	w.now = w.now.Add(2 * time.Hour)
	if code := w.run("tick"); code != 0 || !strings.Contains(w.out.String(), "paused") {
		t.Fatalf("a paused tick said %q", w.out.String())
	}
}

func TestScheduleUsageErrors(t *testing.T) {
	w := newSchedWorld(t)
	for _, args := range [][]string{{}, {"nonsense"}, {"pause"}, {"rm", "a", "b"}} {
		if code := w.run(args...); code != scheduleExitUsage {
			t.Errorf("%v = %d", args, code)
		}
	}
}

// A schedule reads one of the trusted projects and nothing else; every other
// spelling of a folder is refused before anything about it is looked at.
func TestScheduleAddOnlyAcceptsATrustedProject(t *testing.T) {
	w := newSchedWorld(t)
	parent := filepath.Dir(w.ws)
	untrusted := testenv.TempDir(t)
	missing := filepath.Join(w.ws, "no-such-folder")
	for name, folder := range map[string]string{
		"its parent directory":       parent,
		"a path through ..":          filepath.Join(w.ws, "sub", ".."+string(filepath.Separator)+".."),
		"a folder that is not there": missing,
		"a relative path":            ".",
		"a project nobody trusted":   untrusted,
		"the filesystem root":        string(filepath.Separator),
	} {
		w.input = "CONFIRM"
		w.run("add", "--every", "2h", "--prompt", "x", "--workspace", folder)
		if got := w.schedules(); len(got) != 0 {
			t.Errorf("%s: a schedule was created: %+v", name, got)
		}
		if !strings.Contains(w.err.String(), "not a trusted project") {
			t.Errorf("%s: the refusal does not say why: %q", name, w.err.String())
		}
	}
	w.trust(untrusted)
	t.Chdir(untrusted)
	w.input = "CONFIRM"
	if code := w.run("add", "--every", "2h", "--prompt", "x", "--workspace", untrusted); code != 0 || len(w.schedules()) != 1 {
		t.Fatalf("a project trusted after the fact was refused: %d %s", code, w.err.String())
	}
}
