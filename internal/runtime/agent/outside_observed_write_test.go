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

// A plain question that ran one round and answered is a finished turn: no list,
// no change, no debt. Reporting a caveat here would teach the person to skip
// the receipts that matter.
func TestAPlainAnswerEndsWithoutACaveatOrAContinuation(t *testing.T) {
	a, prov := ordinaryTurnAgent(t,
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"calc.py"}`}}},
		testutil.Turn{Text: "这个函数把两个数相加。"},
	)
	if err := a.Run(context.Background(), "这个函数做什么？"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.CallCount() != 2 {
		t.Fatalf("provider calls = %d, want 2 (answer, no forced continuation)", prov.CallCount())
	}
	if receipt := a.CompletionReceipt(); receipt != nil {
		t.Fatalf("a read-only answer carried a completion receipt: %+v", receipt)
	}
}

// A read-only investigation that used several rounds and answered is still a
// finished turn. The rule under test is about writes landing out of sight, not
// about how much a turn read.
func TestAReadOnlyInvestigationEndsClean(t *testing.T) {
	a, prov := ordinaryTurnAgent(t,
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r1", Name: "read_file", Arguments: `{"path":"a.go"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r2", Name: "read_file", Arguments: `{"path":"b.go"}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "r3", Name: "read_file", Arguments: `{"path":"c.go"}`}}},
		testutil.Turn{Text: "三个文件的入口都在 main.go 里注册。"},
	)
	if err := a.Run(context.Background(), "这三个文件怎么串起来的？"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if prov.CallCount() != 4 {
		t.Fatalf("provider calls = %d, want 4", prov.CallCount())
	}
	if receipt := a.CompletionReceipt(); receipt != nil {
		t.Fatalf("a read-only investigation carried a receipt: %+v", receipt)
	}
}

// A write to a scratch directory is a means to an answer, not a delivery. The
// exemption is what keeps every investigation that used a probe from ending
// under a caveat.
func TestAScratchWriteCarriesNoCaveat(t *testing.T) {
	// Built from the host's own temp root rather than a variable this test sets:
	// which names that root goes by is the host's fact on every platform, and
	// the directory sessiontemp creates is always a direct child of it.
	scratch := filepath.Join(os.TempDir(), sessiontemp.Prefix()+"probe", "probe.go")
	a, _ := ordinaryTurnAgent(t,
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q}`, scratch)}}},
		testutil.Turn{Text: "探针跑完了，结论见上面。"},
	)
	if err := a.Run(context.Background(), "用临时脚本验证一下"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := a.outsideObservedWrites(); len(got) != 0 {
		t.Fatalf("outside writes = %v, want none for a temp-directory probe", got)
	}
	if receipt := a.CompletionReceipt(); receipt != nil && hasGap(receipt, "outside_observed_root") {
		t.Fatalf("a scratch write was reported as out of sight: %+v", receipt)
	}
}

// An unfinished task list still stops the turn, and the out-of-sight write is
// reported beside it rather than in place of it. The new gap must not stand in
// for the readiness contract the list already carries.
func TestAnUnfinishedListStillHoldsUnderAnOutOfSightWrite(t *testing.T) {
	a, prov := ordinaryTurnAgent(t,
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q}`, outsideTarget("a.desktop"))}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "t1", Name: "todo_write",
			Arguments: `{"todos":[{"content":"写快捷方式","status":"in_progress"},{"content":"实测启动","status":"pending"}]}`}}},
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w2", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q}`, outsideTarget("b.desktop"))}}},
		testutil.Turn{Text: "写好了。"},
	)
	err := a.Run(context.Background(), "两步：写快捷方式，然后实测启动")
	if !readinessBlocked(err) {
		t.Fatalf("error = %v, want the unfinished list to hold the turn", err)
	}
	if prov.CallCount() != 4 {
		t.Fatalf("provider calls = %d, want the gate to speak instead of a continuation", prov.CallCount())
	}
}

// A turn that wrote out of sight and claimed it was done is the case the rule
// exists for: the receipt must not read as a clean finish.
func TestAnOutOfSightWriteIsNeverReportedAsDone(t *testing.T) {
	a, _ := ordinaryTurnAgent(t,
		testutil.Turn{ToolCalls: []provider.ToolCall{{ID: "w1", Name: "write_file", Arguments: fmt.Sprintf(`{"path":%q}`, outsideTarget("Reasonix CLI.desktop"))}}},
		testutil.Turn{Text: "改用更稳妥的写法。"},
	)
	if err := a.Run(context.Background(), "把快捷方式放到桌面并实测启动"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	receipt := a.CompletionReceipt()
	if receipt == nil {
		t.Fatal("an out-of-sight write ended with no receipt at all")
	}
	if receipt.Verdict == "done" {
		t.Fatalf("verdict = done with a write the host cannot observe: %+v", receipt)
	}
	if !hasGap(receipt, "outside_observed_root") {
		t.Fatalf("gaps = %+v, want the out-of-sight write named", receipt.Gaps)
	}
	if !receipt.SaysSomething() {
		t.Fatal("the receipt has nothing to say, so the caveat reaches nobody")
	}
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

func hasGap(r *event.CompletionReceipt, kind string) bool {
	for _, g := range r.Gaps {
		if g.Kind == kind {
			return true
		}
	}
	return false
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

// The before/after of the fix, on one ledger: without the new gap the report
// says done, and with it the same receipts say partial. This is the whole
// change in one assertion, so a later reader can see what moved.
func TestTheSameReceiptsGoFromDoneToPartial(t *testing.T) {
	ledger := evidence.NewLedger()
	ledger.Record(evidence.Receipt{
		ToolName: "write_file", Success: true, Write: true, Mutation: true,
		MutationEvidence: evidence.MutationProven,
		Paths:            []string{outsideTarget("Reasonix CLI.desktop")},
	})
	c := buildShadowContract("把快捷方式放到桌面并实测启动", ledger.Receipts(), nil)
	inWorkspace := func(path string) bool { return false }

	before := completion.Build(c, ledger, inWorkspace)
	if before.Verdict != completion.VerdictDone {
		t.Fatalf("before = %s, want done: the receipt that made this a silent finish", before.Verdict)
	}
	after := before.WithOutsideObservedWrites([]string{outsideTarget("Reasonix CLI.desktop")})
	if after.Verdict != completion.VerdictPartial {
		t.Fatalf("after = %s, want partial", after.Verdict)
	}
	if len(after.Gaps) != 1 || after.Gaps[0].Kind != completion.GapOutsideObservedRoot {
		t.Fatalf("after gaps = %+v, want the out-of-sight write", after.Gaps)
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

// A turn the host cannot judge still says what it saw. Dropping the gap at the
// unjudged boundary is how an out-of-sight write reads clean a second time.
func TestAnUnjudgedTurnStillCarriesItsGap(t *testing.T) {
	a := &Agent{}
	a.turn.completion = &completion.Report{
		Verdict: completion.VerdictUnknown,
		Gaps:    []completion.Gap{{Kind: completion.GapOutsideObservedRoot, Detail: "x.desktop"}},
	}
	got := a.CompletionReceipt()
	if got == nil {
		t.Fatal("an unjudged turn with a gap produced no receipt")
	}
	if !hasGap(got, "outside_observed_root") {
		t.Fatalf("receipt = %+v, want the out-of-sight write kept", got)
	}
}
