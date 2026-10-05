package bootstrap

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/sftpfs"
	"reasonix/internal/platform/remote/sshtest"
)

// fakeConn scripts exec responses and shares a real sftpfs.FS backed by an
// sshtest SFTP server rooted at a temp dir. The temp dir stands in for the
// remote home, so ~ resolves to it.
type fakeConn struct {
	fs      *sftpfs.FS
	sftpErr error
	mu      sync.Mutex
	execs   []string
	handler func(cmd string) (remote.ExecResult, error)
}

func (f *fakeConn) Exec(_ context.Context, cmd string) (remote.ExecResult, error) {
	f.mu.Lock()
	f.execs = append(f.execs, cmd)
	f.mu.Unlock()
	return f.handler(cmd)
}

func (f *fakeConn) SFTP() (*sftpfs.FS, error) {
	if f.sftpErr != nil {
		return nil, f.sftpErr
	}
	return f.fs, nil
}

func (f *fakeConn) ranContaining(sub string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.execs {
		if strings.Contains(c, sub) {
			return true
		}
	}
	return false
}

// skipOnWindows guards the EnsureServe integration tests. They model a POSIX
// remote — pathsFor uses path.Join and the slug maps a POSIX home, while the
// SFTP harness serves the local FS. On Windows the temp-dir "remote home" is a
// drive path, so both the test's own pathsFor pre-writes and the harness break.
// This is a harness limitation, not a product one (V1 remotes are Linux/macOS);
// Linux/macOS CI covers these flows. Call it first thing in each such test,
// before any pathsFor/os setup.
func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("EnsureServe harness models a POSIX remote; exercised on Linux/macOS")
	}
}

func newFakeConn(t *testing.T, root string, handler func(cmd string) (remote.ExecResult, error)) *fakeConn {
	t.Helper()
	skipOnWindows(t)
	srv := sshtest.Start(t, sshtest.Options{SFTPRoot: root})
	cfg := &ssh.ClientConfig{User: "t", HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second}
	cl, err := ssh.Dial("tcp", srv.Addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cl.Close() })
	fs, err := sftpfs.New(cl)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fs.Close() })
	return &fakeConn{fs: fs, handler: handler}
}

func ok(stdout string) (remote.ExecResult, error) {
	return remote.ExecResult{Stdout: []byte(stdout)}, nil
}

// TestEnsureServeLaunchesWhenAbsent drives a full cold start: no prior state,
// reasonix already on PATH, serve writes its port file.
func TestEnsureServeLaunchesWhenAbsent(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	var portFile string
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			// LocateCommand: report a path and a fresh version.
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.0\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			// Simulate serve writing the port file, then echo the pid.
			if portFile != "" {
				_ = os.WriteFile(portFile, []byte("127.0.0.1:44321\n"), 0o600)
			}
			return ok("54321\n")
		case strings.Contains(cmd, "ps -p 54321"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	// Discover the port-file path the bootstrap will use so the fake serve can
	// write it.
	paths := pathsFor(root, root)
	portFile = paths.PortFile

	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace:  "~",
		MinVersion: "1.0.0",
		Clock:      time.Now,
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if res.Reused {
		t.Fatal("cold start should not report reuse")
	}
	if res.State.Addr != "127.0.0.1:44321" || res.State.PID != 54321 {
		t.Fatalf("state wrong: %+v", res.State)
	}
	if res.Token == "" {
		t.Fatal("no token generated")
	}
	// Token file written 0600.
	fi, err := os.Stat(paths.TokenFile)
	if err != nil {
		t.Fatalf("token file missing: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("token perm = %v, want 0600", fi.Mode().Perm())
	}
	// State file persisted and reloadable.
	data, err := os.ReadFile(paths.StateJSON)
	if err != nil {
		t.Fatal(err)
	}
	st, err := UnmarshalState(data)
	if err != nil || st.Addr != "127.0.0.1:44321" {
		t.Fatalf("persisted state wrong: %+v (%v)", st, err)
	}
}

// TestEnsureServeReusesLiveProcess: a recorded, alive pid short-circuits to
// reuse without detecting/launching.
func TestEnsureServeReusesLiveProcess(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	// Pre-write state + token as if a serve is already running.
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, TokenFile: paths.TokenFile}
	data, _ := MarshalState(st)
	if err := os.WriteFile(paths.StateJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("existing-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		if strings.Contains(cmd, "kill -0 777") {
			return ok("1\n") // alive
		}
		if strings.Contains(cmd, "uname") {
			return ok("Linux x86_64\n")
		}
		if strings.Contains(cmd, "nohup") || strings.Contains(cmd, "command -v reasonix") {
			t.Errorf("reuse path should not locate or launch; ran: %s", cmd)
		}
		return ok("")
	})

	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if !res.Reused {
		t.Fatal("expected reuse of live process")
	}
	if res.Token != "existing-token" {
		t.Fatalf("token = %q, want existing-token", res.Token)
	}
	if conn.ranContaining("nohup") {
		t.Fatal("reuse path launched a new serve")
	}
}

// TestEnsureServeRelaunchesDeadProcess: a recorded but dead pid triggers a
// fresh launch.
func TestEnsureServeRelaunchesDeadProcess(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := ServeState{PID: 888, Addr: "127.0.0.1:5000", Workspace: root, TokenFile: paths.TokenFile}
	data, _ := MarshalState(st)
	_ = os.WriteFile(paths.StateJSON, data, 0o600)
	_ = os.WriteFile(paths.TokenFile, []byte("stale\n"), 0o600)

	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -0 888"):
			return ok("0\n") // dead
		case strings.Contains(cmd, "uname"):
			return ok("Linux aarch64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.0\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:6001\n"), 0o600)
			return ok("999\n")
		case strings.Contains(cmd, "ps -p 999"):
			return ok("1\n")
		default:
			return ok("")
		}
	})

	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", MinVersion: "1.0.0"})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if res.Reused {
		t.Fatal("dead process should be relaunched, not reused")
	}
	if res.State.PID != 999 || res.State.Addr != "127.0.0.1:6001" {
		t.Fatalf("relaunched state wrong: %+v", res.State)
	}
}

// TestEnsureServeInstallNeverErrorsWhenAbsent.
func TestEnsureServeInstallNeverErrorsWhenAbsent(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("\n") // not found anywhere
		default:
			return ok("")
		}
	})
	_, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", Install: InstallNever})
	if !errors.Is(err, ErrInstallDisabled) {
		t.Fatalf("expected install-never error, got %v", err)
	}
}

func TestStopRemovesStateFiles(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	_ = os.MkdirAll(paths.Dir, 0o755)
	st := ServeState{PID: 555, Addr: "127.0.0.1:5000", Workspace: root, TokenFile: paths.TokenFile}
	data, _ := MarshalState(st)
	_ = os.WriteFile(paths.StateJSON, data, 0o600)
	_ = os.WriteFile(paths.TokenFile, []byte("tok\n"), 0o600)

	stopped := false
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		// Order matters: StopCommand also contains "kill -0 555" in its wait
		// loop, so match the TERM (the stop signal) before the serve-alive probe.
		if strings.Contains(cmd, "kill -TERM 555") {
			stopped = true
			return ok("")
		}
		// Stop verifies the pid is our serve (ServeAliveCommand) before signalling.
		if strings.Contains(cmd, "ps -p 555") {
			return ok("1\n")
		}
		// Which machine this is decides which of those commands to send.
		if strings.Contains(cmd, "uname") {
			return ok("Linux x86_64\n")
		}
		return ok("")
	})
	if err := Stop(context.Background(), conn, "~"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !stopped {
		t.Error("Stop did not TERM the pid")
	}
	if _, err := os.Stat(paths.StateJSON); !os.IsNotExist(err) {
		t.Error("state file not removed")
	}
	if _, err := os.Stat(paths.TokenFile); !os.IsNotExist(err) {
		t.Error("token file not removed")
	}
}

var _ = filepath.Join

// The failure this floor exists for. A machine still on the 1.x line answers
// every call a pane makes with 405 — that line routes /runtimes to its page —
// and the window showed "Method Not Allowed", which names no next move.
// Alive is not the same question as usable.
func TestEnsureServeWillNotReuseAKernelBelowTheFloor(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, Version: "1.31.4", TokenFile: paths.TokenFile}
	data, _ := MarshalState(st)
	if err := os.WriteFile(paths.StateJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("existing-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -0 777"):
			return ok("1\n") // running, and from the wrong line
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix 1.31.4\n" + allFlagsYes())
		default:
			return ok("")
		}
	})

	_, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", Install: InstallNever, MinVersion: MinPaneVersion,
	})
	var tooOld *KernelTooOldError
	if !errors.As(err, &tooOld) {
		t.Fatalf("err = %v, want a KernelTooOldError: upgrading over there and installing are different moves", err)
	}
	if tooOld.Found != "1.31.4" || tooOld.Need != MinPaneVersion {
		t.Fatalf("too-old error = %+v, want the version it found and the floor it missed", tooOld)
	}
	if conn.ranContaining("nohup") {
		t.Fatal("a kernel below the floor must not be launched either")
	}
}

// Declining to reuse one replaces the record that points at it, and a pid with
// no record left is a pid nothing will ever stop. The replacement stops it.
func TestEnsureServeStopsTheOutdatedKernelItReplaces(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, Version: "1.31.4", TokenFile: paths.TokenFile}
	data, _ := MarshalState(st)
	if err := os.WriteFile(paths.StateJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -0 777"):
			return ok("1\n")
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v2.7.0\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:6002\n"), 0o600)
			return ok("999\n")
		case strings.Contains(cmd, "ps -p 999"):
			return ok("1\n")
		default:
			return ok("")
		}
	})

	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", MinVersion: MinPaneVersion})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if res.Reused || res.State.PID != 999 {
		t.Fatalf("state = %+v reused=%v, want the freshly launched 999", res.State, res.Reused)
	}
	if res.State.Version != "2.7.0" {
		t.Fatalf("recorded version = %q, want the one that was launched", res.State.Version)
	}
	if !conn.ranContaining("kill -TERM 777") {
		t.Fatal("the kernel being replaced was left running with nothing pointing at it")
	}
}

// A running serve resolves providers over the address it was launched with,
// and that address dies with the link that published it. Reusing the process
// across a reconnect that landed on a different port would leave every model
// call dialling a closed one — so the record's broker is part of what makes a
// serve reusable, not just its pid.
func TestEnsureServeWillNotReuseAServeBoundToAnotherBroker(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := ServeState{
		PID: 777, Addr: "127.0.0.1:5000", Workspace: root,
		TokenFile: paths.TokenFile, Broker: "127.0.0.1:40001",
	}
	data, _ := MarshalState(st)
	if err := os.WriteFile(paths.StateJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("existing-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -0 777"):
			return ok("1\n") // still alive, and still the wrong broker
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		}
		return ok("")
	})

	// Install is off, so the relaunch this forces fails rather than running a
	// second serve. What the test reads is that reuse was refused at all.
	_, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", Install: InstallNever,
		Broker: Broker{Addr: "127.0.0.1:40002", Token: "tok"},
	})
	if err == nil {
		t.Fatal("a serve bound to a retired broker was reused")
	}
	if conn.ranContaining("kill -0 777") && !errors.Is(err, ErrInstallDisabled) {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

// The same record with the same broker is reusable: the guard must not turn
// every reconnect into a relaunch.
func TestEnsureServeReusesAServeOnTheSameBroker(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := ServeState{
		PID: 778, Addr: "127.0.0.1:5001", Workspace: root,
		TokenFile: paths.TokenFile, Broker: "127.0.0.1:40001",
	}
	data, _ := MarshalState(st)
	if err := os.WriteFile(paths.StateJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("existing-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -0 778"):
			return ok("1\n")
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		}
		return ok("")
	})

	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", Broker: Broker{Addr: "127.0.0.1:40001", Token: "tok"},
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if !res.Reused {
		t.Fatal("a serve on the same broker was relaunched instead of reused")
	}
}

// The token reaches the remote as a 0600 file, never as an argument, because
// argv is readable by every account on that machine.
func TestEnsureServeWritesTheBrokerTokenPrivately(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.9\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:6001\n"), 0o600)
			_ = os.WriteFile(paths.PidFile, []byte("991\n"), 0o600)
			return ok("991\n")
		case strings.Contains(cmd, "kill -0 991"):
			return ok("1\n")
		}
		return ok("")
	})

	if _, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", Broker: Broker{Addr: "127.0.0.1:40007", Token: "broker-secret"},
	}); err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	data, err := os.ReadFile(paths.BrokerEndpoint)
	if err != nil {
		t.Fatalf("read broker endpoint file: %v", err)
	}
	if strings.TrimSpace(string(data)) != "127.0.0.1:40007\nbroker-secret" {
		t.Fatalf("broker endpoint file holds %q", data)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(paths.BrokerEndpoint)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Fatalf("broker token file is %v, readable beyond its owner", fi.Mode().Perm())
		}
	}
	if conn.ranContaining("broker-secret") {
		t.Fatal("the broker token reached a command line")
	}
}

// Refusing to reuse and leaving the process running are two different things.
// The record naming that pid is the only note this side keeps, and the launch
// replacing it takes the note away — so the kernel a broker mismatch declined
// has to be stopped here, exactly as an outdated one is.
func TestEnsureServeStopsTheKernelABrokerMismatchDeclined(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := ServeState{
		PID: 777, Addr: "127.0.0.1:5000", Workspace: root, Version: "9.9.9",
		TokenFile: paths.TokenFile, Broker: "127.0.0.1:40001",
	}
	data, _ := MarshalState(st)
	if err := os.WriteFile(paths.StateJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("stale\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -0 777"):
			return ok("1\n")
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.9\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:6003\n"), 0o600)
			return ok("999\n")
		case strings.Contains(cmd, "ps -p 999"):
			return ok("1\n")
		}
		return ok("")
	})

	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", Broker: Broker{Addr: "127.0.0.1:40002", Token: "tok"},
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if res.Reused || res.State.PID != 999 {
		t.Fatalf("state = %+v reused=%v, want the freshly launched 999", res.State, res.Reused)
	}
	if res.State.Broker != "127.0.0.1:40002" {
		t.Fatalf("recorded broker = %q, want the one it was launched with", res.State.Broker)
	}
	if !conn.ranContaining("kill -TERM 777") {
		t.Fatal("the kernel a broker mismatch declined was left running with nothing pointing at it")
	}
}

// A serve that reads its broker from a file is not replaced when another
// connect publishes the broker elsewhere: the files are rewritten and the
// running kernel, with the conversations on it, is kept.
func TestEnsureServeRebindsAServeThatFollowsItsBrokerFile(t *testing.T) {
	skipOnWindows(t)
	for name, broker := range map[string]Broker{
		"moved":     {Addr: "127.0.0.1:40002", Token: "tok-2"},
		"new token": {Addr: "127.0.0.1:40001", Token: "tok-2"},
	} {
		t.Run(name, func(t *testing.T) {
			root := testenv.TempDir(t)
			paths := pathsFor(root, root)
			if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
				t.Fatal(err)
			}
			st := ServeState{
				PID: 779, Addr: "127.0.0.1:5002", Workspace: root,
				TokenFile: paths.TokenFile, Broker: "127.0.0.1:40001", BrokerFile: true,
			}
			data, _ := MarshalState(st)
			for path, body := range map[string][]byte{
				paths.StateJSON: data, paths.TokenFile: []byte("existing-token\n"),
				paths.BrokerEndpoint: []byte("127.0.0.1:40001\ntok-1\n"),
			} {
				if err := os.WriteFile(path, body, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
				switch {
				case strings.Contains(cmd, "kill -0 779"):
					return ok("1\n")
				case strings.Contains(cmd, "uname"):
					return ok("Linux x86_64\n")
				}
				return ok("")
			})

			res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", Broker: broker})
			if err != nil {
				t.Fatalf("EnsureServe: %v", err)
			}
			if !res.Reused || res.State.PID != 779 || conn.ranContaining("kill -TERM") {
				t.Fatalf("reused=%v pid=%d: the running kernel was replaced", res.Reused, res.State.PID)
			}
			got, err := os.ReadFile(paths.BrokerEndpoint)
			if want := broker.Addr + "\n" + broker.Token; err != nil || strings.TrimSpace(string(got)) != want {
				t.Fatalf("%s = %q, want %q", filepath.Base(paths.BrokerEndpoint), got, want)
			}
			recorded, err := readStateFile(paths.StateJSON)
			if err != nil || recorded.Broker != broker.Addr || !recorded.BrokerFile {
				t.Fatalf("state = %+v, %v; want it naming the broker now in use", recorded, err)
			}
		})
	}
}

// A fresh launch reads its broker from the file from the start, and says so,
// so the next connect can rebind it rather than replace it.
func TestEnsureServeLaunchesAServeThatFollowsItsBrokerFile(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("bin /usr/bin/reasonix\nver reasonix v9.9.9\n" + allFlagsYes())
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:6002\n"), 0o600)
			_ = os.WriteFile(paths.PidFile, []byte("992\n"), 0o600)
			return ok("992\n")
		case strings.Contains(cmd, "kill -0 992"):
			return ok("1\n")
		}
		return ok("")
	})
	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", Broker: Broker{Addr: "127.0.0.1:40008", Token: "t"},
	})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if !res.State.BrokerFile {
		t.Fatal("a launch reading its broker from a file did not record that it does")
	}
	got, err := os.ReadFile(paths.BrokerEndpoint)
	if err != nil || strings.TrimSpace(string(got)) != "127.0.0.1:40008\nt" {
		t.Fatalf("broker endpoint file = %q, %v", got, err)
	}
	if !conn.ranContaining("--provider-broker-file") {
		t.Fatal("the launch did not point the serve at the endpoint file")
	}
}

func readStateFile(path string) (ServeState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ServeState{}, err
	}
	return UnmarshalState(data)
}

// A link that came back on another remote port re-points the kernel that
// follows its broker file, and leaves one that does not alone.
func TestRepointBrokerRewritesOnlyAServeThatFollowsItsFile(t *testing.T) {
	skipOnWindows(t)
	for _, follows := range []bool{true, false} {
		root := testenv.TempDir(t)
		paths := pathsFor(root, root)
		if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
			t.Fatal(err)
		}
		data, _ := MarshalState(ServeState{PID: 780, Addr: "127.0.0.1:5003", Workspace: root,
			TokenFile: paths.TokenFile, Broker: "127.0.0.1:40001", BrokerFile: follows})
		if err := os.WriteFile(paths.StateJSON, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths.BrokerEndpoint, []byte("127.0.0.1:40001\nt0\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
			if strings.Contains(cmd, "uname") {
				return ok("Linux x86_64\n")
			}
			return ok("")
		})
		if err := RepointBroker(context.Background(), conn, "~", Broker{Addr: "127.0.0.1:40009", Token: "t"}); err != nil {
			t.Fatalf("follows=%v: %v", follows, err)
		}
		got, _ := os.ReadFile(paths.BrokerEndpoint)
		want := "127.0.0.1:40001\nt0"
		if follows {
			want = "127.0.0.1:40009\nt"
		}
		if strings.TrimSpace(string(got)) != want {
			t.Fatalf("follows=%v: address file = %q, want %q", follows, got, want)
		}
	}
}
