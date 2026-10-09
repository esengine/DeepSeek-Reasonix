package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
)

func holdWindowsHost(t *testing.T) {
	t.Helper()
	prev := fileutil.HostIsWindows
	fileutil.HostIsWindows = true
	t.Cleanup(func() { fileutil.HostIsWindows = prev })
}

// A POSIX double slash names the same local file, so a real file shows whether
// host bookkeeping looked the path up.
func networkSpelling(t *testing.T, path string) string {
	t.Helper()
	slashed := filepath.ToSlash(path)
	if !strings.HasPrefix(slashed, "/") {
		t.Skip("needs a rooted local path")
	}
	return "/" + slashed
}

func TestNetworkPathCallIsRefusedBeforeAnythingLooksItUp(t *testing.T) {
	holdWindowsHost(t)
	dir := testenv.TempDir(t)
	file := filepath.Join(dir, "evil_test.go")
	if err := os.WriteFile(file, []byte("package p\n\nfunc TestX(t *testing.T) {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls int32
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "write_file", writesPaths: true, calls: &calls})
	a := New(nil, reg, sessionstore.NewSession(""), Options{WriteWorkspaceRoot: dir, ArchiveDir: testenv.TempDir(t)}, event.Discard)

	args, _ := json.Marshal(map[string]string{"path": networkSpelling(t, file), "content": "x"})
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "write_file", Arguments: string(args)})
	if !out.blocked || out.refusalCode != fileutil.CodeNetworkPathOutsideScope {
		t.Fatalf("outcome blocked=%v code=%q, want a refusal with %q", out.blocked, out.refusalCode, fileutil.CodeNetworkPathOutsideScope)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatal("the tool ran for a network path")
	}
	if len(a.task.baselineCriteria) != 0 {
		t.Fatalf("baseline criteria read through a network spelling: %v", a.task.baselineCriteria)
	}
}

func TestHostBookkeepingNeverLooksUpANetworkPath(t *testing.T) {
	holdWindowsHost(t)
	dir := testenv.TempDir(t)
	file := filepath.Join(dir, "evil_test.go")
	if err := os.WriteFile(file, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	net := networkSpelling(t, file)
	if got := statePathOf(net, dir); got.exists {
		t.Error("statePathOf looked up a network path")
	}
	if got := statePathOf(file, dir); !got.exists {
		t.Error("statePathOf lost a local path")
	}
	ledger := evidence.NewLedger()
	snap := snapshotPaths(ledger, dir, []string{net, file})
	for key, w := range snap.state {
		if w.path == net {
			t.Errorf("snapshot watched a network path (%s)", key)
		}
	}
	a, _ := agentWithCriteriaStore(t)
	a.writeWorkspaceRoot = dir
	if err := a.captureCriterionAt(a.baselineCriteriaStore(), net); err != nil || len(a.task.baselineCriteria) != 0 {
		t.Fatalf("captureCriterionAt read a network path: %v %v", err, a.task.baselineCriteria)
	}
}

func TestNetworkPathsUnchangedOffWindows(t *testing.T) {
	dir := testenv.TempDir(t)
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !statePathOf(networkSpelling(t, file), dir).exists {
		t.Error("a double-slash local path stopped resolving off Windows")
	}
}
