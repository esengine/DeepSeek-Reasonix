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

	"reasonix/internal/remote"
	"reasonix/internal/remote/sftpfs"
	"reasonix/internal/remote/sshtest"
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
	root := t.TempDir()
	var portFile string
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			// LocateCommand: report a path and a fresh version.
			return ok("/usr/bin/reasonix\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
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
	root := t.TempDir()
	paths := pathsFor(root, root)
	// Pre-write state + token as if a serve is already running.
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, ServeCaps: ServeCapsToken, TokenFile: paths.TokenFile}
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
		if strings.Contains(cmd, "readlink /proc/777/exe") {
			t.Fatal("managed capability token should avoid re-executing the live image")
		}
		if strings.Contains(cmd, "uname") || strings.Contains(cmd, "nohup") {
			t.Errorf("reuse path should not detect/launch; ran: %s", cmd)
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
	root := t.TempDir()
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
			return ok("/usr/bin/reasonix\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
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
	root := t.TempDir()
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
	if err == nil || !strings.Contains(err.Error(), "serve_install = never") {
		t.Fatalf("expected install-never error, got %v", err)
	}
}

func TestEnsureServeUpgradeFailurePreservesOutdatedProcess(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	state, _ := MarshalState(ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, TokenFile: paths.TokenFile})
	_ = os.WriteFile(paths.StateJSON, state, 0o600)
	_ = os.WriteFile(paths.TokenFile, []byte("existing-token\n"), 0o600)
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -TERM 777"):
			t.Fatal("outdated Serve was stopped before a replacement was available")
		case strings.Contains(cmd, "ps -p 777"):
			return ok("1\n")
		case strings.Contains(cmd, "readlink /proc/777/exe"):
			return ok("no\n")
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("\n")
		}
		return ok("")
	})
	_, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", Install: InstallNever})
	if err == nil || !strings.Contains(err.Error(), "serve_install = never") {
		t.Fatalf("upgrade error = %v, want install-never failure", err)
	}
}

func TestEnsureServeTokenStageFailurePreservesOutdatedProcess(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	state, _ := MarshalState(ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, TokenFile: paths.TokenFile})
	if err := os.WriteFile(paths.StateJSON, state, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("existing-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(paths.TokenFile+".next", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.TokenFile+".next", "block-rename"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -TERM 777"):
			t.Fatal("outdated Serve was stopped before the token was staged")
		case strings.Contains(cmd, "kill -0 777"):
			return ok("1\n")
		case strings.Contains(cmd, "readlink /proc/777/exe"):
			return ok("no\n")
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("/usr/bin/reasonix\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			t.Fatal("replacement launched after token staging failed")
		}
		return ok("")
	})

	_, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"})
	if err == nil || !strings.Contains(err.Error(), "stage token") {
		t.Fatalf("staging error = %v, want token staging failure", err)
	}
	token, readErr := os.ReadFile(paths.TokenFile)
	if readErr != nil || string(token) != "existing-token\n" {
		t.Fatalf("token after staging failure = %q, %v", token, readErr)
	}
}

func TestEnsureServeRetirementFailurePreservesExistingToken(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	state, _ := MarshalState(ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, TokenFile: paths.TokenFile})
	if err := os.WriteFile(paths.StateJSON, state, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("existing-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -TERM 777"):
			return remote.ExecResult{}, errors.New("temporary SSH failure")
		case strings.Contains(cmd, "kill -0 777"):
			return ok("1\n")
		case strings.Contains(cmd, "readlink /proc/777/exe"):
			return ok("no\n")
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("/usr/bin/reasonix\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "nohup"):
			t.Fatal("replacement launched after retirement failed")
		}
		return ok("")
	})

	_, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"})
	if err == nil || !strings.Contains(err.Error(), "stop outdated serve") {
		t.Fatalf("retirement error = %v, want stop failure", err)
	}
	token, readErr := os.ReadFile(paths.TokenFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if got := string(token); got != "existing-token\n" {
		t.Fatalf("token after failed retirement = %q, want existing token", got)
	}
}

func TestEnsureServeDarwinRetiresOldPIDAfterBinaryReplacement(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	state, _ := MarshalState(ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, TokenFile: paths.TokenFile})
	if err := os.WriteFile(paths.StateJSON, state, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("existing-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(root, "local-reasonix")
	if err := os.WriteFile(local, []byte("fresh-darwin-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	uploaded := uploadedBinPath(root)
	stopped := false
	capabilityProbes := 0
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "kill -TERM 777"):
			stopped = true
			return ok("")
		case strings.Contains(cmd, "kill -0 777"):
			return ok("1\n")
		case strings.Contains(cmd, "readlink /proc/777/exe"):
			capabilityProbes++
			if strings.Contains(cmd, "ps -p 777") {
				t.Fatal("Darwin capability probe fell back to the replaced pathname")
			}
			return ok("no\n")
		case strings.Contains(cmd, "uname"):
			return ok("Darwin arm64\n")
		case strings.Contains(cmd, "BIN=; if [ -x "+shellQuote(uploaded)):
			return ok(uploaded + "\nreasonix v9.9.0\nportfile:yes\nsessionevents:yes\ndetachedheal:yes\ncaps:yes\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok(uploaded + "\nreasonix v1.0.0\nportfile:yes\nsessionevents:no\ndetachedheal:no\ncaps:no\n")
		case strings.Contains(cmd, "nohup"):
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:6002\n"), 0o600)
			return ok("999\n")
		case strings.Contains(cmd, "kill -0 999"):
			return ok("1\n")
		default:
			return ok("")
		}
	})

	res, err := EnsureServe(context.Background(), conn, Options{
		Workspace: "~", Install: InstallUpload, LocalBinary: local, LocalGOOS: "darwin", LocalGOARCH: "arm64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Reused || !stopped || capabilityProbes < 2 || res.State.PID != 999 {
		t.Fatalf("darwin replacement result = reused:%v stopped:%v probes:%d state:%+v", res.Reused, stopped, capabilityProbes, res.State)
	}
	if res.State.ServeCaps != ServeCapsToken {
		t.Fatalf("launched state caps = %q, want %q", res.State.ServeCaps, ServeCapsToken)
	}
}

func TestStopRemovesStateFiles(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
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
