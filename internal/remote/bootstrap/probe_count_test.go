package bootstrap

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/remote"
)

// probeCounts tallies EnsureServe's remote exec traffic by category. The
// Capability commands include their own liveness-shaped shell fragments, so
// classify them before the general process-liveness shape.
type probeCounts struct {
	mu         sync.Mutex
	pidAlive   int // kill -0 / ps -p pid liveness probes
	capability int // readlink / serve --help capability probes
	other      int
}

func (c *probeCounts) record(cmd string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case strings.Contains(cmd, "readlink"), strings.Contains(cmd, "serve --help"):
		c.capability++
	case strings.Contains(cmd, "kill -0"), strings.Contains(cmd, "ps -p"):
		c.pidAlive++
	default:
		c.other++
	}
}

func (c *probeCounts) snapshot() (pidAlive, capability, other int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pidAlive, c.capability, c.other
}

// TestEnsureServeReuseProbeCount pins the reuse path's exec budget:
// EnsureServe's retire and reuse decisions share ONE probe round — one
// liveness exec and one capability exec. More means the duplication crept
// back and every cold start pays it again.
func TestEnsureServeReuseProbeCount(t *testing.T) {
	skipOnWindows(t)
	root := t.TempDir()
	paths := pathsFor(root, root)
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

	var c probeCounts
	conn := newFakeConn(t, root, func(cmd string) (remote.ExecResult, error) {
		c.record(cmd)
		switch {
		case strings.Contains(cmd, "readlink"), strings.Contains(cmd, "serve --help"):
			return ok("yes\n")
		case strings.Contains(cmd, "kill -0 777"), strings.Contains(cmd, "ps -p 777"):
			return ok("1\n") // alive
		default:
			return ok("")
		}
	})

	res, err := EnsureServe(context.Background(), conn, Options{Workspace: "~"})
	if err != nil {
		t.Fatalf("EnsureServe: %v", err)
	}
	if !res.Reused {
		t.Fatal("expected reuse of live process")
	}
	pidAlive, capability, other := c.snapshot()
	if pidAlive != 1 || capability != 1 || other != 0 {
		t.Fatalf("reuse-path exec budget regressed: pidAlive=%d capability=%d other=%d, want 1/1/0 (single shared probe round)", pidAlive, capability, other)
	}
}
