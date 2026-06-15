package codegraph

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"reasonix/internal/plugin"
	"reasonix/internal/tool"
)

// TestE2ECodegraphMCP drives the whole integration against a real CodeGraph
// bundle: index a fixture project, connect via the MCP client pinned to that
// project (Spec.Dir), and actually call codegraph_search. It is gated on
// REASONIX_CODEGRAPH_E2E so the normal `go test ./...` skips it (no network, no
// external binary), yet it still compiles every build so it can't bit-rot.
//
// Run it with `make e2e-codegraph` (fetches the matching bundle), or manually:
//
//	REASONIX_CODEGRAPH_E2E=1 REASONIX_CODEGRAPH_BIN=/path/to/codegraph \
//	  go test ./internal/codegraph/ -run E2E -v -count=1
//
// With REASONIX_CODEGRAPH_BIN unset it falls back to Resolve("") (bundle / PATH).
func TestE2ECodegraphMCP(t *testing.T) {
	if os.Getenv("REASONIX_CODEGRAPH_E2E") == "" {
		t.Skip("set REASONIX_CODEGRAPH_E2E=1 to run the CodeGraph MCP end-to-end test")
	}
	bin := os.Getenv("REASONIX_CODEGRAPH_BIN")
	if bin == "" {
		var ok bool
		if bin, ok = Resolve(""); !ok {
			t.Fatal("REASONIX_CODEGRAPH_E2E is set but no codegraph binary found — set REASONIX_CODEGRAPH_BIN to the launcher path")
		}
	}
	t.Logf("codegraph binary: %s", bin)

	// A fixture project carrying a known symbol the search must surface.
	root := t.TempDir()
	src := "package demo\n\n// Greet builds a greeting.\nfunc Greet(name string) string { return \"hi \" + name }\n"
	if err := os.WriteFile(filepath.Join(root, "greet.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// 1) Initialise .codegraph/ (fast, no indexing — serve's daemon does that).
	if err := EnsureInit(ctx, bin, root); err != nil {
		t.Fatalf("EnsureInit: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(root, ".codegraph")); err != nil || !fi.IsDir() {
		t.Fatalf(".codegraph was not created by EnsureInit: %v", err)
	}

	// 2) Connect through the real MCP client, pinned to the project root via Dir —
	//    the same wiring boot uses for the built-in server.
	host, tools, err := plugin.StartAll(ctx, []plugin.Spec{{
		Name:              "codegraph",
		Command:           bin,
		Args:              []string{"serve", "--mcp"},
		Dir:               root,
		ReadOnlyToolNames: ReadOnlyToolNames(),
	}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer func() {
		host.Close()
		// Reap the daemon that escapes process-group kill via setsid.
		KillDaemon(root)
	}()
	if len(tools) == 0 {
		t.Fatal("codegraph exposed no MCP tools")
	}

	// 3) Locate the search tool and actually invoke it through Tool.Execute.
	var search tool.Tool
	names := make([]string, 0, len(tools))
	for _, tl := range tools {
		names = append(names, tl.Name())
		if strings.Contains(tl.Name(), "codegraph_search") {
			search = tl
		}
	}
	if search == nil {
		t.Fatalf("no codegraph_search tool among %v", names)
	}
	if !search.ReadOnly() {
		t.Fatalf("codegraph_search should be read-only under the built-in CodeGraph override; tools=%v", names)
	}

	// serve indexes in the background, so poll the search for a few seconds until
	// the known symbol surfaces (rather than assuming it is ready at handshake).
	var out string
	deadline := time.Now().Add(15 * time.Second)
	for {
		var err error
		if out, err = search.Execute(ctx, json.RawMessage(`{"query":"Greet"}`)); err != nil {
			t.Fatalf("codegraph_search Execute: %v\nschema: %s", err, search.Schema())
		}
		if strings.Contains(out, "Greet") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("codegraph_search never surfaced the known symbol Greet within 15s:\n%s", out)
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Logf("e2e ok — %d tools (%v); search surfaced Greet", len(tools), names)
}

// TestE2ECodegraphKillDaemon verifies that KillDaemon can reap a real CodeGraph
// daemon that escapes the process-group kill in KillTree. The daemon does
// setsid(2) to detach into its own process group, so the negative-PID SIGKILL
// that KillTree sends to the launcher's group misses it. KillDaemon verifies the
// daemon lockfile against the socket hello before sending a direct SIGKILL.
//
// Gated on REASONIX_CODEGRAPH_E2E=1 like the other E2E test; skipped on Windows
// because the Job Object (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE) already captures
// every descendant regardless of reparenting.
func TestE2ECodegraphKillDaemon(t *testing.T) {
	if os.Getenv("REASONIX_CODEGRAPH_E2E") == "" {
		t.Skip("set REASONIX_CODEGRAPH_E2E=1 to run the CodeGraph E2E daemon-kill test")
	}
	if runtime.GOOS == "windows" {
		t.Skip("daemon escape is Unix-only; Windows Job Objects capture all descendants")
	}

	bin := os.Getenv("REASONIX_CODEGRAPH_BIN")
	if bin == "" {
		var ok bool
		if bin, ok = Resolve(""); !ok {
			t.Fatal("REASONIX_CODEGRAPH_E2E is set but no codegraph binary found — set REASONIX_CODEGRAPH_BIN to the launcher path")
		}
	}
	t.Logf("codegraph binary: %s", bin)

	root := t.TempDir()
	src := "package demo\n\nfunc Greet(name string) string { return \"hi \" + name }\n"
	if err := os.WriteFile(filepath.Join(root, "greet.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := EnsureInit(ctx, bin, root); err != nil {
		t.Fatalf("EnsureInit: %v", err)
	}

	// Start the codegraph server — this launches the MCP bridge (foreground) and
	// a daemon (background). The daemon records its PID in its lockfile on startup.
	host, _, err := plugin.StartAll(ctx, []plugin.Spec{{
		Name:              "codegraph",
		Command:           bin,
		Args:              []string{"serve", "--mcp"},
		Dir:               root,
		ReadOnlyToolNames: ReadOnlyToolNames(),
	}})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}

	// Give the daemon a moment to start and write its PID lockfile.
	var daemonPID int
	var daemonOK bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if daemonPID, daemonOK = DaemonPID(root); daemonOK {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !daemonOK {
		host.Close()
		t.Fatal("daemon PID never appeared in .codegraph/daemon.pid")
	}
	t.Logf("daemon PID: %d", daemonPID)

	// Verify the daemon is alive before we close.
	if !processExists(daemonPID) {
		host.Close()
		t.Fatalf("daemon PID %d not alive before Close", daemonPID)
	}
	t.Logf("daemon %d alive before host.Close()", daemonPID)

	// host.Close kills the launcher via KillTree (process-group SIGKILL), but the
	// daemon has detached into its own process group via setsid and escapes.
	host.Close()

	// Give the kernel a moment to deliver the SIGKILL and reap the launcher.
	time.Sleep(300 * time.Millisecond)

	if processExists(daemonPID) {
		t.Logf("daemon %d survived process-group kill (as expected — it escaped via setsid)", daemonPID)

		// Now explicitly kill the daemon via its logged PID.
		KillDaemon(root)

		// Poll for exit with a short deadline.
		deadline := time.Now().Add(5 * time.Second)
		for processExists(daemonPID) {
			if time.Now().After(deadline) {
				t.Fatalf("daemon %d still alive after KillDaemon", daemonPID)
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Logf("daemon %d killed by KillDaemon", daemonPID)
	} else {
		// The daemon didn't escape — process-group kill worked. This can happen
		// if codegraph is built without daemonization or if the OS doesn't
		// support setsid semantics. KillDaemon is still a safe no-op here.
		t.Logf("daemon %d was already dead after host.Close() (process-group kill succeeded)", daemonPID)
		KillDaemon(root) // no-op — best-effort is safe
	}
}

// processExists reports whether a process with the given PID is currently
// running. It sends signal 0 (null signal) on Unix; on Windows it always
// returns false (but the daemon-kill test skips Windows anyway).
func processExists(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
