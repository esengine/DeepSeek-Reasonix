package schedrun

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/filelock"
	"reasonix/internal/contract/observe"
	"reasonix/internal/state/schedule"
)

const fakeEnv = "SCHEDRUN_FAKE_CHILD"

func TestMain(m *testing.M) {
	if mode := os.Getenv(fakeEnv); mode != "" {
		os.Exit(runFakeChild(mode))
	}
	os.Exit(m.Run())
}

func env(k string) string { return os.Getenv("SCHEDRUN_" + k) }

// runFakeChild is the program a supervised child would be, with a behaviour the
// test picks. It holds the run's lease first and waits for the release line,
// exactly as the real command does, and it ignores every signal it can.
func runFakeChild(mode string) int {
	signal.Ignore(os.Interrupt)
	if mode == "grand" {
		release, err := filelock.TryAcquire(env("GRAND_LOCK"))
		if err != nil {
			return 13
		}
		defer release()
		time.Sleep(time.Hour)
		return 0
	}
	store, err := schedule.Open(env("STORE"))
	if err != nil {
		return 10
	}
	id := env("ID")
	if mode == "parent" {
		return runFakeParent(store, id)
	}
	release, err := store.HoldRun(id)
	if err != nil {
		return 11
	}
	defer release()
	w := NewWriter(os.Stdout)
	_ = w.Ready()
	_, gone, err := AwaitGo(os.Stdin, 20*time.Second)
	if err != nil {
		return 12
	}
	if mark := env("MARK"); mark != "" {
		f, _ := os.OpenFile(mark, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		fmt.Fprintf(f, "ran %d\n", os.Getpid())
		f.Close()
	}
	switch mode {
	case "ok":
		_ = w.Usage(400)
		_ = w.Usage(600)
		_ = w.Result(Done{State: schedule.RunSucceeded, Tokens: 1000, Usages: 2,
			Report: "found 3 TODOs\x00 ‮evil", Posture: observe.New(false),
			Pending: []observe.Pending{{ID: "p1", Kind: observe.KindAsk, Source: "ask", Detail: "which?\x1b[31m"}}})
		return 0
	case "lie":
		_ = w.Usage(100)
		_ = w.Result(Done{State: schedule.RunSucceeded, Tokens: 100, Usages: 5})
		return 0
	case "unmetered":
		_ = w.Result(Done{State: schedule.RunSucceeded, Tokens: 0, Usages: 0, Unmetered: true})
		return 0
	case "slow-ok":
		time.Sleep(600 * time.Millisecond)
		_ = w.Usage(10)
		_ = w.Result(Done{State: schedule.RunSucceeded, Tokens: 10, Usages: 1})
		return 0
	case "noisy":
		_, _ = os.Stderr.WriteString("boom\x1b[31m\u202e\x00 secret")
		return 3
	case "crash":
		_ = w.Usage(50)
		return 3
	case "tree":
		exe, _ := os.Executable()
		g := exec.Command(exe)
		g.Env = append(os.Environ(), fakeEnv+"=grand")
		if err := g.Start(); err != nil {
			return 14
		}
		time.Sleep(time.Hour)
	case "sleep":
		time.Sleep(time.Hour)
	case "flood":
		for range 10 {
			_ = w.Usage(int64(envInt("CHUNK")))
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(time.Hour)
	case "huge-line":
		_, _ = os.Stdout.WriteString(strings.Repeat("x", MaxLineBytes+10) + "\n")
		time.Sleep(time.Hour)
	case "hold":
		<-gone
		return 0
	}
	return 0
}

func envInt(k string) int {
	n, _ := strconv.Atoi(env(k))
	return n
}

// runFakeParent is a supervisor process a test can kill: it runs one child in
// the mode SCHEDRUN_CHILD_MODE and never returns on its own.
func runFakeParent(store *schedule.Store, id string) int {
	sup := &Supervisor{Store: store, Policy: testPolicy(), Command: childCommand(os.Getenv("SCHEDRUN_CHILD_MODE"), env("STORE"), id, env("MARK"))}
	_, _ = sup.Run(context.Background(), id)
	return 0
}

func childCommand(mode, store, id, mark string, extra ...string) func(string) (*exec.Cmd, error) {
	return func(string) (*exec.Cmd, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		c := exec.Command(exe)
		c.Env = append(os.Environ(), fakeEnv+"="+mode, "SCHEDRUN_STORE="+store, "SCHEDRUN_ID="+id, "SCHEDRUN_MARK="+mark)
		c.Env = append(c.Env, extra...)
		return c, nil
	}
}
