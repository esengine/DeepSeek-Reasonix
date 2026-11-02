package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent/testutil"
	"reasonix/internal/runtime/completion"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/sessiontemp"
)

// outsideTarget is a path outside every root this host watches and outside the
// temp directory, which is exempt from the rule under test. It is never
// written to: the stub writer reports the write instead of performing one. The
// volume comes from the host so the path is absolute on Windows too, where a
// leading separator alone names no drive and the path would read as relative.
func outsideTarget(name string) string {
	vol := filepath.VolumeName(os.TempDir())
	return filepath.Join(vol+string(filepath.Separator), "outside-watch", name)
}

// ordinaryTurnAgent is a workspace with a read tool and a stub writer, so a
// test can script the exact shape it is about.
func ordinaryTurnAgent(t *testing.T, turns ...testutil.Turn) (*Agent, *testutil.MockProvider) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	reg.Add(outsideWriter{})
	for _, bt := range tool.Builtins() {
		if bt.Name() == "todo_write" {
			reg.Add(bt)
		}
	}
	prov := testutil.NewMock("m", turns...)
	a := New(prov, reg, sessionstore.NewSession("sys"),
		Options{WriteWorkspaceRoot: t.TempDir(), CheckTargetAccess: allowTestFileAccess}, event.Discard)
	return a, prov
}

// The write is reported, never enforced: the turn still ends, and the person
// decides. Anything else would turn a receipt into a gate nobody asked for.
func TestAnOutOfSightWriteDoesNotHoldTheTurn(t *testing.T) {
	a, prov := ordinaryTurnAgent(t,
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q}`, outsideTarget("x.desktop"))}}},
		testutil.Turn{Text: "已完成。"},
	)
	if err := a.Run(context.Background(), "写一个桌面快捷方式"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.CallCount() != 2 {
		t.Fatalf("provider calls = %d, want 2: the receipt reports, it does not continue the turn", prov.CallCount())
	}
}

// The cap on named files holds: a turn that touched many out-of-sight paths
// names three and counts the rest, so the card stays readable.
func TestOutsideWriteDetailIsBounded(t *testing.T) {
	got := completion.OutsideWriteDetail([]string{"a.desktop", "b.desktop", "c.desktop", "d.desktop", "e.desktop"})
	if !strings.Contains(got, "and 2 more") {
		t.Fatalf("detail = %q, want the count tail", got)
	}
	if strings.Contains(got, "d.desktop") {
		t.Fatalf("detail = %q, want only the first three named", got)
	}
}

// The exemption is for where the host puts scratch, not for every temporary
// path. A person who asks for a file under the public temp directory asked for
// something the host cannot verify afterwards, and that has to show.
func TestADeliverableInThePublicTempDirectoryIsNotExempt(t *testing.T) {
	// Beside the scratch directories, not inside one: the exemption names the
	// directory sessiontemp hands out, not the root it sits in.
	target := filepath.Join(os.TempDir(), "deliverable.txt")
	if underScratchOrCache(target) {
		t.Fatalf("%s is exempt; a path the host cannot verify afterwards must not be", target)
	}
	a, _ := ordinaryTurnAgent(t,
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q}`, target)}}},
		testutil.Turn{Text: "已生成。"},
	)
	if err := a.Run(context.Background(), "把报告生成到临时目录，我等会儿去取"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := a.outsideObservedWrites(); !slices.Contains(got, evidence.NormalizePath(target)) {
		t.Fatalf("outside writes = %v, want %s: a deliverable in the public temp directory is out of sight", got, target)
	}
}

// The exemption names the directories the host owns, not every path that
// resembles one. A folder a person could create under their own project must
// not buy silence, and neither must an installed executable.
func TestTheExemptionIsNarrowerThanALookalikePath(t *testing.T) {
	// The host's roots are stated rather than steered through variables, which
	// each platform spells differently; the rule under test takes them as input.
	root := t.TempDir()
	tempRoot := filepath.Join(root, "tmp")
	caches := []string{filepath.Join(root, "go")}

	for _, dir := range []string{
		filepath.Join(root, "projects", "reasonix-session-tmp-backup"),
		filepath.Join(tempRoot, "deep", "reasonix-session-tmp-x"),
	} {
		if disposable(filepath.Join(dir, "notes.md"), tempRoot, caches) {
			t.Fatalf("%s was exempt: a directory can claim the scratch name by spelling it", dir)
		}
	}

	installed := filepath.Join(root, "go", "bin", "mytool")
	if disposable(installed, tempRoot, caches) {
		t.Fatalf("%s was exempt: go install puts the user's own executable there", installed)
	}
	cached := filepath.Join(root, "go", "pkg", "mod", "cache", "x.go")
	if !disposable(cached, tempRoot, caches) {
		t.Fatalf("%s was not exempt: a module cache is what the build writes through", cached)
	}
	scratch := filepath.Join(tempRoot, sessiontemp.Prefix()+"run", "probe.go")
	if !disposable(scratch, tempRoot, caches) {
		t.Fatalf("%s was not exempt: the directory sessiontemp hands out is the host's to spend", scratch)
	}
}

// A folder the person granted as workspace is not out of sight. The host
// observes the granted roots and walks them with the root, so a write there is
// work product like any other, and reporting it would caveat exactly the
// deliveries the person widened the fence to allow.
func TestAGrantedRootIsNotReportedAsOutOfSight(t *testing.T) {
	f := newReceiptFixture(t)
	granted := testenv.TempDir(t)
	inGranted := filepath.Join(granted, "out.txt")
	f.record(inGranted)

	if !slices.Contains(f.a.outsideObservedWrites(), evidence.NormalizePath(inGranted)) {
		t.Fatalf("without a grant %s must read as out of sight", inGranted)
	}

	f.a.extraRoots = []string{granted}
	if got := f.a.outsideObservedWrites(); len(got) != 0 {
		t.Fatalf("outside writes = %v, want none: a granted root is observed", got)
	}
	rep := f.report().WithOutsideObservedWrites(f.a.outsideObservedWrites())
	if slices.ContainsFunc(rep.Gaps, func(g completion.Gap) bool { return g.Kind == completion.GapOutsideObservedRoot }) {
		t.Fatalf("gaps = %+v, want no out-of-sight gap under a granted root", rep.Gaps)
	}
}
