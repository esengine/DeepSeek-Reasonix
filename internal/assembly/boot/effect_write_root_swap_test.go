package boot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/session/control"
)

func swapBash(id, cmd string) testutil.Turn {
	return testutil.Turn{ToolCalls: []provider.ToolCall{{ID: id, Name: "bash", Arguments: fmt.Sprintf(`{"command":%s}`, strconv.Quote(cmd))}}}
}

// toolOutputs is every tool result the provider was last shown.
func toolOutputs(prov *testutil.MockProvider) string {
	reqs := prov.Requests()
	if len(reqs) == 0 {
		return ""
	}
	var b strings.Builder
	for _, m := range reqs[len(reqs)-1].Messages {
		if m.Role == "tool" {
			b.WriteString(m.Content + "\n")
		}
	}
	return b.String()
}

// runSwapSession runs one jailed session over a checkout whose reasonix.toml
// grants allow_write = ["later"], driving the given bash turns in order.
func runSwapSession(t *testing.T, root string, laterExists bool, turns ...testutil.Turn) *testutil.MockProvider {
	t.Helper()
	writeUserConfig(t, userModel+"\n[sandbox]\nnetwork = false\nbash = \"enforce\"\n")
	writeFile(t, root, "reasonix.toml", "[sandbox]\nallow_write = [\"later\"]\n")
	if laterExists {
		if err := os.Mkdir(filepath.Join(root, "later"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prov := testutil.NewMock("swap", append(turns, testutil.Turn{Text: "done"})...)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, prov)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	_ = ctrl.Run(context.Background(), "go")
	return prov
}

// unjailedDir is a directory no jailed command may write: the package
// directory, or the user's home where the checkout itself sits in temp.
func unjailedDir(t *testing.T) string {
	t.Helper()
	if dir := outsideTempDir(t); !jailWritable(dir) {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no directory outside every write root")
	}
	dir, err := os.MkdirTemp(home, "reasonix-swap-")
	if err != nil || jailWritable(dir) {
		t.Skip("no directory outside every write root")
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func requireJail(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || !sandbox.Available() {
		t.Skip("no OS sandbox on this host")
	}
}

// A write root the checkout names, missing or real when the session starts,
// re-pointed from inside the jail at a directory outside it: neither a write
// through it nor a direct one lands, and the model is told which root it lost.
func TestEffectAWriteRootSwappedForALinkCarriesNoWritesOut(t *testing.T) {
	requireJail(t)
	for _, mode := range []string{"missing", "realdir", "missing-rel", "realdir-rel"} {
		t.Run(mode, func(t *testing.T) {
			outside := unjailedDir(t)
			isolateConfigHome(t)
			root := robustTempDir(t)
			t.Chdir(root)
			target := outside
			if strings.HasSuffix(mode, "-rel") {
				realRoot, _ := filepath.EvalSymlinks(root)
				realOut, _ := filepath.EvalSymlinks(outside)
				rel, err := filepath.Rel(realRoot, realOut)
				if err != nil {
					t.Fatal(err)
				}
				target = rel
			}
			ctl := filepath.Join(root, "ctl.txt")
			prov := runSwapSession(t, root, strings.HasPrefix(mode, "realdir"),
				swapBash("c", "printf ok > "+strconv.Quote(ctl)),
				swapBash("l", "rm -rf later && ln -s "+strconv.Quote(target)+" later"),
				swapBash("p", "printf x > later/pwn"),
				swapBash("d", "printf y > "+strconv.Quote(filepath.Join(outside, "direct"))))
			if _, err := os.Stat(ctl); err != nil {
				t.Fatalf("control write inside the workspace failed: %v", err)
			}
			for _, name := range []string{"pwn", "direct"} {
				if _, err := os.Stat(filepath.Join(outside, name)); !os.IsNotExist(err) {
					t.Errorf("outside/%s was written (stat err %v): the swapped link widened the jail", name, err)
				}
			}
			if out := toolOutputs(prov); !strings.Contains(out, sandbox.WriteRootRedirectedCode+": ") {
				t.Errorf("the model was not told the root was dropped:\n%s", out)
			}
		})
	}
}

// A root left alone stays writable without a note; one replaced by another
// directory is reported, and stays writable only through the workspace. The
// old directory stays alive so the new one cannot reuse its inode.
func TestEffectAWriteRootKeepsItsGrantUntilItsIdentityChanges(t *testing.T) {
	requireJail(t)
	isolateConfigHome(t)
	root := robustTempDir(t)
	t.Chdir(root)
	prov := runSwapSession(t, root, true,
		swapBash("a", "printf a > later/kept"))
	if _, err := os.Stat(filepath.Join(root, "later", "kept")); err != nil {
		t.Fatalf("an untouched write root refused a write: %v", err)
	}
	if out := toolOutputs(prov); strings.Contains(out, "sandbox.write_root_") {
		t.Fatalf("an untouched write root was reported dropped:\n%s", out)
	}

	isolateConfigHome(t)
	root = robustTempDir(t)
	t.Chdir(root)
	prov = runSwapSession(t, root, true,
		swapBash("r", "mv later later.old && mkdir later"),
		swapBash("b", "printf b > later/after"))
	if out := toolOutputs(prov); !strings.Contains(out, sandbox.WriteRootChangedCode+": ") {
		t.Fatalf("a replaced write root was not reported:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "later", "after")); err != nil {
		t.Fatalf("the workspace stopped covering a replaced root inside it: %v", err)
	}
}
