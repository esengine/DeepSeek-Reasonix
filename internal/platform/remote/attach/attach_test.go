package attach

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote"
	"reasonix/internal/platform/remote/bootstrap"
	"reasonix/internal/platform/remote/sshtest"
)

var portFileArg = regexp.MustCompile(`--port-file '([^']*)'`)

// machine is a host to attach to: a real SSH server with SFTP and forwarding,
// and an HTTP server standing in for the serve the bootstrap thinks it started.
type machine struct {
	home   string
	kernel *httptest.Server
	dial   func(host string, prompts Prompts) (*remote.Client, error)

	mu       sync.Mutex
	launched int
}

func fakeMachine(t *testing.T) *machine {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the bootstrap harness models a POSIX remote; exercised on Linux/macOS")
	}
	m := &machine{home: testenv.TempDir(t)}
	m.kernel = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "remote kernel: %s", r.URL.Path)
	}))
	t.Cleanup(m.kernel.Close)

	srv := sshtest.Start(t, sshtest.Options{
		Password: "pw",
		SFTPRoot: m.home,
		Exec: func(cmd string) (string, string, int) {
			switch {
			case strings.Contains(cmd, "uname"):
				return "Linux x86_64\n", "", 0
			case strings.Contains(cmd, "command -v reasonix"):
				// LocateCommand's three lines: a path, a fresh version, and the
				// --port-file flag the launch needs.
				return "bin /usr/bin/reasonix\nver reasonix v9.9.0\n" + kernelFlagsYes(), "", 0
			case strings.Contains(cmd, "nohup"):
				// Stand in for serve coming up: publish the address of the HTTP
				// server the forward will actually reach.
				if hit := portFileArg.FindStringSubmatch(cmd); hit != nil {
					_ = os.WriteFile(hit[1], []byte(m.kernel.Listener.Addr().String()+"\n"), 0o600)
				}
				m.mu.Lock()
				m.launched++
				m.mu.Unlock()
				return "54321\n", "", 0
			case strings.Contains(cmd, "ps -p"):
				return "1\n", "", 0
			}
			return "", "", 0
		},
	})

	known := testenv.TempDir(t)
	m.dial = func(string, Prompts) (*remote.Client, error) {
		host, err := remote.ResolveHost(nil, "test@"+srv.Addr, nil)
		if err != nil {
			return nil, err
		}
		return remote.New(remote.Options{
			Host: host,
			Auth: remote.AuthOptions{Password: func() (string, error) { return "pw", nil }},
			HostKeys: &remote.HostKeyPolicy{
				SystemKnownHosts: []string{filepath.Join(known, "absent")},
				ManagedPath:      filepath.Join(known, "managed"),
				Prompt:           func(context.Context, remote.HostKeyQuestion) (bool, error) { return true, nil },
			},
		})
	}
	return m
}

func (m *machine) launches() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.launched
}

func testPool(t *testing.T, m *machine) *Pool {
	t.Helper()
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	p := NewPool(ctx, Options{Dial: m.dial})
	t.Cleanup(p.Close)
	return p
}

func (m *machine) workspace(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(m.home, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mustAttach(t *testing.T, p *Pool, host, workspace string) *Endpoint {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ep, err := p.Attach(ctx, host, workspace, Call{})
	if err != nil {
		t.Fatalf("attach %s: %v", workspace, err)
	}
	return ep
}

// The whole point of the layer: what comes back is an address this machine can
// call, and what answers is the kernel on the other side of the link.
func TestAttachReachesTheRemoteKernelOverTheForward(t *testing.T) {
	m := fakeMachine(t)
	p := testPool(t, m)
	ep := mustAttach(t, p, "box", m.workspace(t, "proj"))
	defer ep.Release()

	if ep.Token == "" {
		t.Fatal("no token came back, so nothing could authenticate to the remote gate")
	}
	resp, err := http.Get("http://" + ep.Addr + "/status")
	if err != nil {
		t.Fatalf("call the forwarded kernel: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if got := string(body); got != "remote kernel: /status" {
		t.Fatalf("answer = %q, want the remote kernel's", got)
	}
}

// Two workspaces on one machine are one login, not two. A connection per
// workspace would re-prompt for the passphrase on the second folder opened.
func TestWorkspacesOnOneHostShareItsConnection(t *testing.T) {
	m := fakeMachine(t)
	p := testPool(t, m)
	first := mustAttach(t, p, "box", m.workspace(t, "one"))
	defer first.Release()
	second := mustAttach(t, p, "box", m.workspace(t, "two"))
	defer second.Release()

	p.mu.Lock()
	links := len(p.links)
	spaces := len(p.links["box"].spaces)
	p.mu.Unlock()
	if links != 1 {
		t.Fatalf("pool holds %d connections for one host, want 1", links)
	}
	if spaces != 2 {
		t.Fatalf("connection carries %d workspaces, want 2", spaces)
	}
	if first.Addr == second.Addr {
		t.Fatal("both workspaces answered on one forward: they are separate kernels")
	}
}

// A second pane on a folder rides the forward the first one bound. Binding a
// second would start a second serve for one workspace on the far side.
func TestSecondPaneOnAWorkspaceReusesItsForward(t *testing.T) {
	m := fakeMachine(t)
	p := testPool(t, m)
	ws := m.workspace(t, "proj")
	first := mustAttach(t, p, "box", ws)
	defer first.Release()
	second := mustAttach(t, p, "box", ws)
	defer second.Release()

	if first.Addr != second.Addr {
		t.Fatalf("second pane bound its own forward: %s vs %s", first.Addr, second.Addr)
	}
	if n := m.launches(); n != 1 {
		t.Fatalf("the remote was launched %d times for one workspace, want 1", n)
	}
}

// The last holder out closes the link. Releasing one pane while another is
// still open must not, which is the failure that makes the second pane go dark.
func TestTheLastReleaseClosesTheConnection(t *testing.T) {
	m := fakeMachine(t)
	p := testPool(t, m)
	first := mustAttach(t, p, "box", m.workspace(t, "one"))
	second := mustAttach(t, p, "box", m.workspace(t, "two"))

	first.Release()
	p.mu.Lock()
	held := len(p.links)
	p.mu.Unlock()
	if held != 1 {
		t.Fatalf("releasing one of two panes dropped the connection (%d links)", held)
	}
	resp, err := http.Get("http://" + second.Addr + "/status")
	if err != nil {
		t.Fatalf("the surviving pane lost its forward: %v", err)
	}
	resp.Body.Close()

	second.Release()
	p.mu.Lock()
	held = len(p.links)
	p.mu.Unlock()
	if held != 0 {
		t.Fatalf("the last release left %d connections open", held)
	}
}

// A pane can be closed twice — by the user and by a shutdown sweep. The second
// close must not give back a reference the first one already returned.
func TestReleaseIsIdempotent(t *testing.T) {
	m := fakeMachine(t)
	p := testPool(t, m)
	shared := mustAttach(t, p, "box", m.workspace(t, "one"))
	doomed := mustAttach(t, p, "box", m.workspace(t, "two"))
	defer shared.Release()

	doomed.Release()
	doomed.Release()

	p.mu.Lock()
	refs := p.links["box"].refs
	p.mu.Unlock()
	if refs != 1 {
		t.Fatalf("connection holds %d references after a double release, want 1", refs)
	}
}

// A refused login must not poison the host. Caching the failure would answer
// every later attempt with it, and the fix — type the password again — could
// never be reached without restarting the app.
func TestAFailedDialIsNotCached(t *testing.T) {
	m := fakeMachine(t)
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	ctx := t.Context()

	refuse := true
	p := NewPool(ctx, Options{Dial: func(host string, prompts Prompts) (*remote.Client, error) {
		if refuse {
			return nil, fmt.Errorf("authentication failed")
		}
		return m.dial(host, prompts)
	}})
	defer p.Close()

	if _, err := p.Attach(ctx, "box", m.workspace(t, "proj"), Call{}); err == nil {
		t.Fatal("a refused dial reported success")
	}
	p.mu.Lock()
	held := len(p.links)
	p.mu.Unlock()
	if held != 0 {
		t.Fatalf("the failed host stayed in the table (%d links)", held)
	}

	refuse = false
	ep := mustAttach(t, p, "box", m.workspace(t, "proj"))
	defer ep.Release()
}

// kernelFlagsYes is a current kernel's answer to the probe: every flag the
// launch passes, present. Read from the launch's own list rather than copied,
// because a copy is what let a kernel pass the probe and fail to start.
func kernelFlagsYes() string {
	var b strings.Builder
	for _, f := range bootstrap.LaunchFlags(true) {
		b.WriteString("flag " + f + " yes\n")
	}
	return b.String()
}
