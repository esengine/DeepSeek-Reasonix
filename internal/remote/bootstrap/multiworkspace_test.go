package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/remote"
)

func (f *fakeConn) execsContaining(sub string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.execs {
		if strings.Contains(c, sub) {
			out = append(out, c)
		}
	}
	return out
}

// multiWS is one fake remote host (one Conn, one home) with two workspaces,
// each backed by its own fake serve: the launch handler keys on the cd
// operand, writes that workspace's port file, and echoes a per-workspace pid.
type multiWS struct {
	conn           *fakeConn
	root, wsA, wsB string
	pathsA, pathsB StatePaths
}

func newMultiWS(t *testing.T) *multiWS {
	t.Helper()
	root := t.TempDir()
	m := &multiWS{
		root: root,
		wsA:  filepath.Join(root, "srv-a"),
		wsB:  filepath.Join(root, "srv-b"),
	}
	for _, ws := range []string{m.wsA, m.wsB} {
		if err := os.MkdirAll(ws, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m.pathsA = pathsFor(root, m.wsA)
	m.pathsB = pathsFor(root, m.wsB)
	m.conn = newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		switch {
		case strings.Contains(cmd, "uname"):
			return ok("Linux x86_64\n")
		case strings.Contains(cmd, "command -v reasonix"):
			return ok("/usr/bin/reasonix\nreasonix v9.9.0\nportfile:yes\n")
		case strings.Contains(cmd, "nohup"):
			if strings.Contains(cmd, "cd '"+m.wsA+"'") {
				_ = os.WriteFile(m.pathsA.PortFile, []byte("127.0.0.1:44321\n"), 0o600)
				return ok("111\n")
			}
			_ = os.WriteFile(m.pathsB.PortFile, []byte("127.0.0.1:44322\n"), 0o600)
			return ok("222\n")
		case strings.Contains(cmd, "ps -p 111"), strings.Contains(cmd, "ps -p 222"):
			return ok("1\n")
		default:
			return ok("")
		}
	})
	return m
}

func (m *multiWS) ensure(t *testing.T, ws string) Result {
	t.Helper()
	res, err := EnsureServe(context.Background(), m.conn, Options{
		Workspace:  ws,
		MinVersion: "1.0.0",
		Clock:      time.Now,
	})
	if err != nil {
		t.Fatalf("EnsureServe(%s): %v", ws, err)
	}
	return res
}

// TestEnsureServeTwoWorkspacesCoexist: one host, two workspaces, two serves —
// state/token files stay side by side and neither ensure disturbs the other.
func TestEnsureServeTwoWorkspacesCoexist(t *testing.T) {
	skipOnWindows(t)
	m := newMultiWS(t)

	resA := m.ensure(t, m.wsA)
	resB := m.ensure(t, m.wsB)

	if resA.Reused || resB.Reused {
		t.Fatalf("cold starts should not reuse: %+v %+v", resA, resB)
	}
	if resA.State.PID != 111 || resB.State.PID != 222 {
		t.Fatalf("pids wrong: A=%d B=%d", resA.State.PID, resB.State.PID)
	}
	if resA.State.Addr == resB.State.Addr {
		t.Fatalf("both serves share addr %q", resA.State.Addr)
	}
	if m.pathsA.StateJSON == m.pathsB.StateJSON {
		t.Fatal("workspaces share one state file")
	}
	for _, p := range []struct {
		stateJSON, token string
		pid              int
	}{
		{m.pathsA.StateJSON, m.pathsA.TokenFile, 111},
		{m.pathsB.StateJSON, m.pathsB.TokenFile, 222},
	} {
		data, err := os.ReadFile(p.stateJSON)
		if err != nil {
			t.Fatal(err)
		}
		st, err := UnmarshalState(data)
		if err != nil || st.PID != p.pid {
			t.Fatalf("persisted state wrong: %+v (%v)", st, err)
		}
		if _, err := os.Stat(p.token); err != nil {
			t.Fatal(err)
		}
	}

	// A third ensure of workspace A reuses A's live serve — B never leaks in.
	resA2 := m.ensure(t, m.wsA)
	if !resA2.Reused || resA2.State.PID != 111 {
		t.Fatalf("re-ensure of A should reuse pid 111: %+v", resA2.State)
	}

	// Both remain independently alive.
	for _, ws := range []struct {
		path string
		pid  int
	}{
		{m.wsA, 111},
		{m.wsB, 222},
	} {
		st, alive, err := Status(context.Background(), m.conn, ws.path)
		if err != nil || !alive || st.PID != ws.pid {
			t.Fatalf("Status(%s) = %+v alive=%v (%v)", ws.path, st, alive, err)
		}
	}
}

// TestStopOneWorkspaceLeavesOther: stopping A removes only A's state; B keeps
// serving.
func TestStopOneWorkspaceLeavesOther(t *testing.T) {
	skipOnWindows(t)
	m := newMultiWS(t)
	m.ensure(t, m.wsA)
	m.ensure(t, m.wsB)

	if err := Stop(context.Background(), m.conn, m.wsA); err != nil {
		t.Fatalf("Stop(A): %v", err)
	}
	if _, alive, _ := Status(context.Background(), m.conn, m.wsA); alive {
		t.Fatal("workspace A still alive after Stop")
	}
	if _, err := os.Stat(m.pathsA.StateJSON); !os.IsNotExist(err) {
		t.Error("A state file not removed")
	}
	if _, err := os.Stat(m.pathsA.TokenFile); !os.IsNotExist(err) {
		t.Error("A token file not removed")
	}

	if _, err := os.Stat(m.pathsB.StateJSON); err != nil {
		t.Fatalf("B state file disturbed: %v", err)
	}
	st, alive, err := Status(context.Background(), m.conn, m.wsB)
	if err != nil || !alive || st.PID != 222 {
		t.Fatalf("workspace B should survive: %+v alive=%v (%v)", st, alive, err)
	}
}

// TestLaunchCommandsCdIntoEachWorkspace: each workspace's launch script gets
// only its own state paths as concrete operands and cds into its own tree.
func TestLaunchCommandsCdIntoEachWorkspace(t *testing.T) {
	skipOnWindows(t)
	m := newMultiWS(t)
	m.ensure(t, m.wsA)
	m.ensure(t, m.wsB)

	for _, ws := range []struct {
		cd         string
		own, other StatePaths
	}{
		{"cd '" + m.wsA + "'", m.pathsA, m.pathsB},
		{"cd '" + m.wsB + "'", m.pathsB, m.pathsA},
	} {
		var launch string
		for _, cmd := range m.conn.execsContaining("nohup") {
			if strings.Contains(cmd, ws.cd) {
				launch = cmd
				break
			}
		}
		if launch == "" {
			t.Fatalf("no launch command for %s", ws.cd)
		}
		if !strings.Contains(launch, ws.own.TokenFile) || !strings.Contains(launch, ws.own.PortFile) {
			t.Fatalf("launch for %s misses its own state paths: %s", ws.cd, launch)
		}
		if strings.Contains(launch, ws.other.TokenFile) || strings.Contains(launch, ws.other.PortFile) {
			t.Fatalf("launch for %s references the other workspace's paths: %s", ws.cd, launch)
		}
	}
}
