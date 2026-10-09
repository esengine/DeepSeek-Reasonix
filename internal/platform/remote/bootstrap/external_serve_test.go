package bootstrap

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote"
)

type published struct {
	paths StatePaths
	root  string
}

func publishExternalServe(t *testing.T, pid string, addr string, withToken bool) published {
	t.Helper()
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(paths.PortFile, addr+"\n")
	write(paths.PidFile, pid+"\n")
	if withToken {
		write(paths.TokenFile, "external-token\n")
	}
	return published{paths: paths, root: root}
}

func (p published) intact(t *testing.T) {
	t.Helper()
	for _, f := range []string{p.paths.PortFile, p.paths.PidFile} {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("a file the running serve published was removed: %v", err)
		}
	}
}

// aliveServe answers the pid probe for pid and fails the test on anything that
// would launch or stop a serve.
func aliveServe(t *testing.T, pid string, version ...string) func(string) (remote.ExecResult, error) {
	t.Helper()
	v := strings.Join(version, "")
	return func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "--version"):
			return ok(v)
		case strings.Contains(cmd, "kill -0 "+pid):
			return ok("1\n")
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "nohup"), strings.Contains(cmd, "kill -TERM"):
			t.Errorf("a live serve was replaced or stopped: %s", cmd)
		}
		return ok("")
	}
}

func TestEnsureServeAttachesToAServeStartedOutsideIt(t *testing.T) {
	skipOnWindows(t)
	pub := publishExternalServe(t, "4242", "127.0.0.1:47001", true)
	conn := newFakeConn(t, pub.root, aliveServe(t, "4242"))

	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", MinVersion: MinPaneVersion})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if !res.Reused || res.State.PID != 4242 || res.State.Addr != "127.0.0.1:47001" || res.Token != "external-token" {
		t.Fatalf("did not attach to the running serve: %+v reused=%v", res.State, res.Reused)
	}
	pub.intact(t)
	rec, err := readStateFile(pub.paths.StateJSON)
	if err != nil || rec.PID != 4242 || rec.Workspace != pub.root {
		t.Fatalf("attached serve was not recorded: %+v (%v)", rec, err)
	}
	if conn.ranContaining("command -v reasonix") {
		t.Fatal("attaching located a binary it had no use for")
	}
}

func TestEnsureServeAttachesToAnExternalServeOnAProviderlessConnect(t *testing.T) {
	skipOnWindows(t)
	pub := publishExternalServe(t, "4242", "127.0.0.1:47001", true)
	conn := newFakeConn(t, pub.root, aliveServe(t, "4242"))
	if _, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"}); err != nil {
		t.Fatal(err)
	}
	// A second connect finds the record the first wrote and reuses it.
	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"})
	if err != nil || !res.Reused || res.State.PID != 4242 {
		t.Fatalf("second connect: %+v reused=%v err=%v", res.State, res.Reused, err)
	}
}

func TestEnsureServeRefusesToReplaceAServeItCannotDrive(t *testing.T) {
	skipOnWindows(t)
	pub := publishExternalServe(t, "4242", "127.0.0.1:47001", false)
	conn := newFakeConn(t, pub.root, aliveServe(t, "4242"))

	_, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"})
	if !errors.Is(err, ErrServeNotAttachable) {
		t.Fatalf("err = %v, want ErrServeNotAttachable", err)
	}
	pub.intact(t)
}

func TestEnsureServeDoesNotStopAServeWithAnotherProviderSource(t *testing.T) {
	skipOnWindows(t)
	for name, tc := range map[string]struct {
		recorded ServeState
		want     Broker
	}{
		"started without a broker, this connect brokers": {
			recorded: ServeState{PID: 4242, Addr: "127.0.0.1:47001"},
			want:     Broker{Addr: "127.0.0.1:40002", Token: "tok"},
		},
		"started on a broker file, this connect has none": {
			recorded: ServeState{PID: 4242, Addr: "127.0.0.1:47001", Broker: "127.0.0.1:40001", BrokerFile: true},
		},
	} {
		t.Run(name, func(t *testing.T) {
			pub := publishExternalServe(t, "4242", "127.0.0.1:47001", true)
			tc.recorded.Workspace = pub.root
			tc.recorded.TokenFile = pub.paths.TokenFile
			data, _ := MarshalState(tc.recorded)
			if err := os.WriteFile(pub.paths.StateJSON, data, 0o600); err != nil {
				t.Fatal(err)
			}
			conn := newFakeConn(t, pub.root, aliveServe(t, "4242"))

			_, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", Broker: tc.want})
			if !errors.Is(err, ErrServeProviderMismatch) {
				t.Fatalf("err = %v, want ErrServeProviderMismatch", err)
			}
			pub.intact(t)
			if _, err := os.Stat(pub.paths.StateJSON); err != nil {
				t.Fatalf("the serve's record was removed: %v", err)
			}
		})
	}
}

func TestEnsureServeMismatchOfADeadServeLaunchesInstead(t *testing.T) {
	skipOnWindows(t)
	pub := publishExternalServe(t, "4242", "127.0.0.1:47001", true)
	data, _ := MarshalState(ServeState{PID: 4242, Addr: "127.0.0.1:47001", Workspace: pub.root, TokenFile: pub.paths.TokenFile})
	_ = os.WriteFile(pub.paths.StateJSON, data, 0o600)
	conn := newFakeConn(t, pub.root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -0 4242"):
			return ok("0\n")
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.0\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			for _, f := range []string{pub.paths.PortFile, pub.paths.PidFile} {
				if _, err := os.Stat(f); err == nil {
					t.Errorf("stale %s still present at launch", f)
				}
			}
			_ = os.WriteFile(pub.paths.PortFile, []byte("127.0.0.1:47555\n"), 0o600)
			return ok("999\n")
		case strings.Contains(cmd, "ps -p 999"):
			return ok("1\n")
		}
		return ok("")
	})
	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", Broker: Broker{Addr: "127.0.0.1:40002", Token: "tok"}})
	if err != nil || res.Reused || res.State.PID != 999 {
		t.Fatalf("dead serve was not replaced: %+v reused=%v err=%v", res.State, res.Reused, err)
	}
}

// Two windows connecting at once to a workspace nothing runs on: one launches,
// the other attaches to what it launched, and neither stops the other's.
func TestEnsureServeTwoClientsShareOneLaunch(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	var launches, stops atomic.Int32
	handler := func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.0\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			launches.Add(1)
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:45123\n"), 0o600)
			_ = os.WriteFile(paths.PidFile, []byte("321\n"), 0o600)
			return ok("321\n")
		case strings.Contains(cmd, "kill -TERM"):
			stops.Add(1)
		case strings.Contains(cmd, "kill -0 321"), strings.Contains(cmd, "ps -p 321"):
			return ok("1\n")
		}
		return ok("")
	}
	a := newFakeConn(t, root, handler)
	b := newFakeConn(t, root, handler)
	broker := Broker{Addr: "127.0.0.1:40002", Token: "tok"}
	type outcome struct {
		res Result
		err error
	}
	out := make(chan outcome, 2)
	start := make(chan struct{})
	for _, c := range []*fakeConn{a, b} {
		go func() {
			<-start
			res, err := EnsureServe(context.Background(), c, Options{Workspace: "~", Broker: broker})
			out <- outcome{res, err}
		}()
	}
	close(start)
	reused := 0
	for range 2 {
		got := <-out
		if got.err != nil {
			t.Fatal(got.err)
		}
		if got.res.Reused {
			reused++
		}
	}
	if launches.Load() != 1 || reused != 1 || stops.Load() != 0 {
		t.Fatalf("launches=%d reused=%d stops=%d, want 1/1/0", launches.Load(), reused, stops.Load())
	}
}

// A desktop that brokers and a `remote serve start` that does not, racing for
// one workspace: one serve wins, the other is told why, nothing is stopped.
func TestEnsureServeRacingProviderSourcesDoNotKickEachOther(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	var launches, stops atomic.Int32
	handler := func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.0\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			launches.Add(1)
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:45123\n"), 0o600)
			_ = os.WriteFile(paths.PidFile, []byte("321\n"), 0o600)
			return ok("321\n")
		case strings.Contains(cmd, "kill -TERM"):
			stops.Add(1)
		case strings.Contains(cmd, "kill -0 321"), strings.Contains(cmd, "ps -p 321"):
			return ok("1\n")
		}
		return ok("")
	}
	desktop := newFakeConn(t, root, handler)
	cli := newFakeConn(t, root, handler)
	errs := make(chan error, 2)
	start := make(chan struct{})
	go func() {
		<-start
		_, err := EnsureServe(context.Background(), desktop, Options{Workspace: "~", Broker: Broker{Addr: "127.0.0.1:40002", Token: "tok"}})
		errs <- err
	}()
	go func() {
		<-start
		_, err := EnsureServe(context.Background(), cli, Options{Workspace: "~"})
		errs <- err
	}()
	close(start)
	var failed, mismatched int
	for range 2 {
		if err := <-errs; err != nil {
			failed++
			if errors.Is(err, ErrServeProviderMismatch) {
				mismatched++
			}
		}
	}
	if launches.Load() != 1 || stops.Load() != 0 || failed != 1 || mismatched != 1 {
		t.Fatalf("launches=%d stops=%d failed=%d mismatched=%d, want 1/0/1/1", launches.Load(), stops.Load(), failed, mismatched)
	}
}

func TestEnsureServeReplacesNothingWhenTheRecordIsStale(t *testing.T) {
	skipOnWindows(t)
	pub := publishExternalServe(t, "4242", "127.0.0.1:47001", true)
	data, _ := MarshalState(ServeState{PID: 111, Addr: "127.0.0.1:41111", Workspace: pub.root, TokenFile: pub.paths.TokenFile})
	if err := os.WriteFile(pub.paths.StateJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}
	handler := aliveServe(t, "4242")
	conn := newFakeConn(t, pub.root, func(cmd string) (remote.ExecResult, error) {
		if strings.Contains(cmd, "kill -0 111") {
			return ok("0\n")
		}
		return handler(cmd)
	})
	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"})
	if err != nil || !res.Reused || res.State.PID != 4242 {
		t.Fatalf("stale record hid a live serve: %+v reused=%v err=%v", res.State, res.Reused, err)
	}
	pub.intact(t)
	if rec, err := readStateFile(pub.paths.StateJSON); err != nil || rec.PID != 4242 {
		t.Fatalf("record not repointed at the live serve: %+v (%v)", rec, err)
	}
}

func TestEnsureServeChecksAnAdoptedServesVersionBeforeTrustingIt(t *testing.T) {
	skipOnWindows(t)
	for name, tc := range map[string]struct {
		version string
		tooOld  bool
		want    string
	}{
		"too old":    {version: "reasonix v1.31.4\n", tooOld: true},
		"current":    {version: "reasonix v2.30.0\n", want: "2.30.0"},
		"unreadable": {version: ""},
	} {
		t.Run(name, func(t *testing.T) {
			pub := publishExternalServe(t, "4242", "127.0.0.1:47001", true)
			conn := newFakeConn(t, pub.root, aliveServe(t, "4242", tc.version))
			res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", MinVersion: MinPaneVersion})
			if tc.tooOld {
				var old *KernelTooOldError
				if !errors.As(err, &old) {
					t.Fatalf("err = %v, want KernelTooOldError", err)
				}
				pub.intact(t)
				if _, err := os.Stat(pub.paths.StateJSON); err == nil {
					t.Fatal("a serve that cannot drive a pane was recorded")
				}
				return
			}
			if err != nil || !res.Reused || res.State.Version != tc.want {
				t.Fatalf("res=%+v err=%v", res.State, err)
			}
		})
	}
}

// A serve started by hand names its flags in any order.
func TestServeMatchersIgnoreArgumentOrder(t *testing.T) {
	skipOnWindows(t)
	dir := testenv.TempDir(t)
	script := filepath.Join(dir, "reasonix")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	paths := StatePaths{TokenFile: filepath.Join(dir, "tok"), PortFile: filepath.Join(dir, "port")}
	for name, args := range map[string][]string{
		"token first": {"serve", "--token-file", paths.TokenFile, "--port-file", paths.PortFile},
		"port first":  {"serve", "--port-file", paths.PortFile, "--token-file", paths.TokenFile},
	} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(script, args...)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
			out, err := exec.Command("sh", "-c", ServeAliveCommand(cmd.Process.Pid, paths)).Output()
			if err != nil || strings.TrimSpace(string(out)) != "1" {
				t.Fatalf("a live serve was judged not ours: %q %v", out, err)
			}
			if err := exec.Command("sh", "-c", StopCommand(cmd.Process.Pid, paths)).Run(); err != nil {
				t.Fatal(err)
			}
			done := make(chan struct{})
			go func() { _, _ = cmd.Process.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(8 * time.Second):
				t.Fatal("StopCommand did not stop a serve it should recognise")
			}
		})
	}
}
