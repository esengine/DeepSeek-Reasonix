package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/sftpfs"
)

const testNonce = "n0nce"

const nvmBin = "/home/u/.nvm/versions/node/v20.20.2/bin"

func fixedNonce(t *testing.T) {
	t.Helper()
	old := newEnvNonce
	newEnvNonce = func() string { return testNonce }
	t.Cleanup(func() { newEnvNonce = old })
}

func block(path string) string {
	return fmt.Sprintf("\n%s:arith=42\n%s:path=%s\n", testNonce, testNonce, path)
}

type scriptConn struct {
	mu    sync.Mutex
	execs []string
	fn    func(cmd string) (remote.ExecResult, error)
}

func (c *scriptConn) Exec(_ context.Context, cmd string) (remote.ExecResult, error) {
	c.mu.Lock()
	c.execs = append(c.execs, cmd)
	c.mu.Unlock()
	return c.fn(cmd)
}

func (c *scriptConn) SFTP() (*sftpfs.FS, error) { return nil, errors.New("no sftp") }

func TestCaptureLoginEnvParsesPathThroughNoise(t *testing.T) {
	fixedNonce(t)
	noise := "Welcome to Ubuntu\x1b[1;32m banner\x1b[0m\n*** motd ***\nnvm: warning\n"
	cases := []struct {
		name string
		res  remote.ExecResult
	}{
		{"banner and ansi before", remote.ExecResult{Stdout: []byte(noise + block(nvmBin+":/usr/bin"))}},
		{"noise after the block", remote.ExecResult{Stdout: []byte(block(nvmBin+":/usr/bin") + "bye\n")}},
		{"noisy rc exits non-zero", remote.ExecResult{Stdout: []byte(noise + block(nvmBin+":/usr/bin")), ExitCode: 1}},
		{"crlf line ends", remote.ExecResult{Stdout: []byte(strings.ReplaceAll(noise+block(nvmBin+":/usr/bin"), "\n", "\r\n"))}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := &scriptConn{fn: func(string) (remote.ExecResult, error) { return c.res, nil }}
			env := captureLoginEnv(context.Background(), conn)
			if env.Outcome != EnvCaptured {
				t.Fatalf("outcome = %v", env.Outcome)
			}
			if len(env.Path) != 2 || env.Path[0] != nvmBin || env.Path[1] != "/usr/bin" {
				t.Fatalf("path = %v", env.Path)
			}
		})
	}
}

func TestCaptureLoginEnvRejectsHostileOutput(t *testing.T) {
	fixedNonce(t)
	huge := strings.Repeat("A", 4<<20)
	forged := "\nwrong:arith=42\nwrong:path=/evil\n"
	cases := []struct {
		name     string
		out      string
		want     EnvOutcome
		wantPath []string
	}{
		{"huge noise keeps the tail block", huge + block("/usr/bin"), EnvCaptured, []string{"/usr/bin"}},
		{"forged delimiter with another nonce is ignored", forged, EnvUnavailable, nil},
		{"forged block before the real one loses", block("/evil") + block("/real"), EnvCaptured, []string{"/real"}},
		{"nul bytes poison the value", block("/usr/bin\x00:/evil"), EnvUnavailable, nil},
		{"relative and empty entries are dropped", block("::bin:/usr/bin:./x"), EnvCaptured, []string{"/usr/bin"}},
		{"oversized path", block("/" + strings.Repeat("a", 20000)), EnvUnavailable, nil},
		{"arith probe missing", fmt.Sprintf("\n%s:path=/usr/bin\n", testNonce), EnvUnavailable, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := &scriptConn{fn: func(string) (remote.ExecResult, error) {
				return remote.ExecResult{Stdout: []byte(c.out)}, nil
			}}
			env := captureLoginEnv(context.Background(), conn)
			if env.Outcome != c.want {
				t.Fatalf("outcome = %v, want %v", env.Outcome, c.want)
			}
			if strings.Join(env.Path, ":") != strings.Join(c.wantPath, ":") {
				t.Fatalf("path = %v, want %v", env.Path, c.wantPath)
			}
			if len(conn.execs) != 1 {
				t.Fatalf("capture ran %d commands, want 1", len(conn.execs))
			}
		})
	}
}

func TestWrappedCommandQuotesHostilePath(t *testing.T) {
	var got string
	inner := &scriptConn{fn: func(cmd string) (remote.ExecResult, error) { got = cmd; return ok("") }}
	w := &envConn{Conn: inner, env: loginEnv{Outcome: EnvCaptured, Path: []string{"/a b/$(touch x)/'q'"}}}
	if _, err := w.Exec(context.Background(), "echo hi"); err != nil {
		t.Fatal(err)
	}
	want := "export PATH=" + shellQuote("/a b/$(touch x)/'q'") + `:"$PATH"; echo hi`
	if got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
}

func TestCaptureLoginEnvDegradesOnNonPOSIXShell(t *testing.T) {
	fixedNonce(t)
	cases := []struct {
		name string
		out  string
		err  error
		want EnvOutcome
	}{
		{"shell reported unable to evaluate posix", testNonce + ":nonposix\n", nil, EnvNonPOSIX},
		{"no shell resolved", testNonce + ":noshell\n", nil, EnvUnavailable},
		{"shell ran but gave nothing", testNonce + ":failed\n", nil, EnvUnavailable},
		{"transport error", "", errors.New("boom"), EnvUnavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn := &scriptConn{fn: func(string) (remote.ExecResult, error) {
				return remote.ExecResult{Stdout: []byte(c.out)}, c.err
			}}
			env := captureLoginEnv(context.Background(), conn)
			if env.Outcome != c.want || len(env.Path) != 0 {
				t.Fatalf("env = %+v, want outcome %v and no path", env, c.want)
			}
			if got := withLoginEnv(env, conn); got != Conn(conn) {
				t.Fatal("a failed capture must leave the connection unwrapped")
			}
		})
	}
}

func TestCaptureCommandCarriesNoRemoteInput(t *testing.T) {
	fixedNonce(t)
	var cmd string
	conn := &scriptConn{fn: func(c string) (remote.ExecResult, error) { cmd = c; return ok("") }}
	captureLoginEnv(context.Background(), conn)
	for _, want := range []string{testNonce, "tail -c", "-i -l -c", "getent passwd"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("capture command lacks %q:\n%s", want, cmd)
		}
	}
}

// npmOnlyUnderNVM models a remote whose npm and reasonix exist only when the
// command carries the captured PATH.
func npmOnlyUnderNVM(t *testing.T, root string, withNPM bool, captures *int) *fakeConn {
	t.Helper()
	paths := pathsFor(root, root)
	installed := false
	return newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		has := strings.Contains(cmd, "export PATH='"+nvmBin+":")
		switch {
		case strings.Contains(cmd, "getent passwd"):
			*captures++
			return ok("rc banner\n" + block(nvmBin+":/usr/bin"))
		case strings.Contains(cmd, "uname"):
			return ok("Linux aarch64\n")
		case strings.Contains(cmd, "npm i -g reasonix"):
			if !has || !withNPM {
				return remote.ExecResult{Stdout: []byte("sh: 1: npm: not found"), ExitCode: 127}, nil
			}
			installed = true
			return ok("added 1 package\n")
		case strings.Contains(cmd, "command -v reasonix"):
			if installed && has {
				return ok("bin " + nvmBin + "/reasonix\nver reasonix v9.9.0\n" + allFlagsYes())
			}
			return ok("")
		case strings.Contains(cmd, "npm --version"):
			if has && withNPM {
				return ok("10.8.2\n")
			}
			return remote.ExecResult{ExitCode: 127}, nil
		case strings.Contains(cmd, "nohup"):
			if !has {
				return remote.ExecResult{ExitCode: 127}, nil
			}
			_ = os.WriteFile(paths.PortFile, []byte("127.0.0.1:44321\n"), 0o600)
			return ok("54321\n")
		case strings.Contains(cmd, "ps -p 54321"), strings.Contains(cmd, "kill -0 54321"):
			return ok("1\n")
		}
		return ok("")
	})
}

func TestEnsureServeInstallsThroughCapturedPath(t *testing.T) {
	fixedNonce(t)
	root := testenv.TempDir(t)
	captures := 0
	conn := npmOnlyUnderNVM(t, root, true, &captures)
	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", Install: InstallNPM})
	if err != nil {
		t.Fatal(err)
	}
	if res.State.PID != 54321 || res.State.Addr != "127.0.0.1:44321" {
		t.Fatalf("state = %+v", res.State)
	}
	if captures != 1 {
		t.Fatalf("env captured %d times, want once for the connection", captures)
	}
	for _, c := range []string{"npm i -g reasonix", "nohup"} {
		if !conn.ranContaining(c) {
			t.Fatalf("never ran %q", c)
		}
	}
}

func TestEnsureServeNoNPMReportsSearchedPath(t *testing.T) {
	fixedNonce(t)
	root := testenv.TempDir(t)
	captures := 0
	conn := npmOnlyUnderNVM(t, root, false, &captures)
	_, err := EnsureServe(context.Background(), conn, Options{Workspace: "~", Install: InstallNPM})
	if !errors.Is(err, ErrNPMUnavailable) {
		t.Fatalf("err = %v, want ErrNPMUnavailable", err)
	}
	var ne *NPMUnavailableError
	if !errors.As(err, &ne) {
		t.Fatalf("err carries no NPMUnavailableError: %v", err)
	}
	if ne.Outcome != EnvCaptured || len(ne.Searched) != 2 || ne.Searched[0] != nvmBin {
		t.Fatalf("typed data = %+v", ne)
	}
}

func TestProbeSeesNPMOnCapturedPath(t *testing.T) {
	fixedNonce(t)
	root := testenv.TempDir(t)
	captures := 0
	conn := npmOnlyUnderNVM(t, root, true, &captures)
	rep, err := Probe(context.Background(), conn, Options{Install: InstallNPM})
	if err != nil {
		t.Fatal(err)
	}
	if rep.NPM != "10.8.2" || captures != 1 {
		t.Fatalf("npm = %q, captures = %d", rep.NPM, captures)
	}
}

func TestProbeNoNPMCarriesSearchedPath(t *testing.T) {
	fixedNonce(t)
	root := testenv.TempDir(t)
	captures := 0
	conn := npmOnlyUnderNVM(t, root, false, &captures)
	rep, err := Probe(context.Background(), conn, Options{Install: InstallNPM})
	if err != nil {
		t.Fatal(err)
	}
	var ne *NPMUnavailableError
	if len(rep.Routes) != 1 || !errors.As(rep.Routes[0].Err, &ne) || len(ne.Searched) != 2 {
		t.Fatalf("routes = %+v", rep.Routes)
	}
	if !errors.Is(rep.Routes[0].Err, ErrNPMUnavailable) {
		t.Fatal("route error lost its sentinel")
	}
}

func countExecs(c *fakeConn, sub string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.execs {
		if strings.Contains(e, sub) {
			n++
		}
	}
	return n
}

func TestReusePerformsNoCapture(t *testing.T) {
	skipOnWindows(t)
	root := testenv.TempDir(t)
	paths := pathsFor(root, root)
	if err := os.MkdirAll(paths.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := MarshalState(ServeState{PID: 777, Addr: "127.0.0.1:5000", Workspace: root, TokenFile: paths.TokenFile})
	if err := os.WriteFile(paths.StateJSON, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.TokenFile, []byte("tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		if strings.Contains(cmd, "uname") {
			return ok("Linux x86_64\n")
		}
		if strings.Contains(cmd, "kill -0 777") {
			return ok("1\n")
		}
		return ok("")
	})
	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"})
	if err != nil || !res.Reused {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if n := countExecs(conn, "getent passwd"); n != 0 {
		t.Fatalf("reuse ran %d captures, want 0", n)
	}
}

func TestEachEntryPointCapturesOnceOnTheRawConnection(t *testing.T) {
	fixedNonce(t)
	for _, name := range []string{"EnsureServe", "Probe"} {
		t.Run(name, func(t *testing.T) {
			captures := 0
			conn := npmOnlyUnderNVM(t, testenv.TempDir(t), true, &captures)
			var err error
			if name == "Probe" {
				_, err = Probe(context.Background(), conn, Options{Install: InstallNPM})
			} else {
				_, err = EnsureServe(context.Background(), conn, Options{Workspace: "~", Install: InstallNPM})
			}
			if err != nil {
				t.Fatal(err)
			}
			if n := countExecs(conn, "getent passwd"); n != 1 {
				t.Fatalf("%d captures on the raw connection, want 1", n)
			}
		})
	}
}
