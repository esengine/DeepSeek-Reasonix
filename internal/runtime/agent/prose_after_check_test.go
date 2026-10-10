package agent

import (
	"fmt"
	"maps"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/taskpolicy"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/instruction"
)

// proseStep is one receipt shape a real turn produces, named for the sequence
// dump the differential run compares across trees.
type proseStep struct {
	name    string
	receipt evidence.Receipt
	// beyond: a mutation the generic check is owed for — anything but a
	// successful named or fully observed write of prose.
	beyond bool
	prose  bool
	check  string // "pass" or "fail" for a recognized check
}

func proseSteps() []proseStep {
	zero, one := 0, 1
	write := func(path string, ok bool) evidence.Receipt {
		return evidence.Receipt{ToolName: "write_file", Success: ok, Mutation: true, Write: true, MutationEvidence: evidence.MutationProven, Paths: []string{path}}
	}
	scanned := func(path string) evidence.Receipt {
		return evidence.Receipt{ToolName: "bash", Command: "sed -i s/a/b/ " + path, Success: true, Mutation: true, MutationEvidence: evidence.MutationProven, PathsComplete: true, Paths: []string{path}}
	}
	check := func(command string, pass bool) evidence.Receipt {
		r := evidence.Receipt{ToolName: "bash", Command: command, Success: pass, ExitCode: &zero, Verification: evidence.VerificationPassed}
		if !pass {
			r.ExitCode, r.Verification = &one, evidence.VerificationFailed
		}
		return r
	}
	return []proseStep{
		{name: "code", receipt: write("main.go", true), beyond: true},
		{name: "doc", receipt: write("README.md", true), prose: true},
		{name: "code-failed", receipt: write("main.go", false), beyond: true},
		{name: "doc-failed", receipt: write("CHANGELOG.md", false)},
		{name: "bash-code", receipt: scanned("main.go"), beyond: true},
		{name: "bash-doc", receipt: scanned("README.md"), prose: true},
		{name: "bash-unknown", receipt: evidence.Receipt{ToolName: "bash", Command: "./gen.sh", Success: true, Mutation: true, MutationEvidence: evidence.MutationUnknown}, beyond: true},
		{name: "bash-failed", receipt: evidence.Receipt{ToolName: "bash", Command: "echo x >> main.go && exit 3", Success: false, ExitCode: &one, Mutation: true, MutationEvidence: evidence.MutationProven}, beyond: true},
		{name: "test-pass", receipt: check("go test ./...", true), check: "pass"},
		{name: "test-fail", receipt: check("go test ./...", false), check: "fail"},
		{name: "vet-fail", receipt: check("go vet ./...", false), check: "fail"},
		{name: "read", receipt: evidence.Receipt{ToolName: "read_file", Success: true, Read: true, Paths: []string{"main.go"}}},
	}
}

type proseVerdict struct{ gap, stale bool }

func (v proseVerdict) owed() bool { return v.gap || v.stale }

func proseVerdictOf(t *testing.T, root string, seq []proseStep, contract func(*Agent)) proseVerdict {
	t.Helper()
	receipts := make([]evidence.Receipt, 0, len(seq))
	for _, s := range seq {
		r := s.receipt
		r.Paths = slices.Clone(r.Paths)
		receipts = append(receipts, r)
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash"})
	reg.Add(fakeTool{name: "conclude_blocked"})
	a := &Agent{
		task: taskRuntime{ledger: readinessLedger(receipts...)},
		svc:  agentServices{tools: reg},
		turn: turnRuntime{policySet: true, policy: taskpolicy.TaskPolicy{Verification: taskpolicy.VerifyTargeted}},
	}
	a.observeRoot, a.writeWorkspaceRoot = root, root
	if contract != nil {
		contract(a)
	}
	v := proseVerdict{gap: a.finalReadinessCheckFor().missingVerification > 0}
	for _, o := range a.obligations() {
		if o.Kind == evidence.ObligationStaleVerification {
			v.stale = true
		}
	}
	return v
}

func proseWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"main.go", "README.md", "CHANGELOG.md"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("neutral\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func stepsByName(t *testing.T, names ...string) []proseStep {
	t.Helper()
	all := proseSteps()
	var out []proseStep
	for _, n := range names {
		i := slices.IndexFunc(all, func(s proseStep) bool { return s.name == n })
		if i < 0 {
			t.Fatalf("no step %q", n)
		}
		out = append(out, all[i])
	}
	return out
}

func TestProseAfterPassingCheckOwesNoRerun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		steps []string
		owed  bool
	}{
		{name: "code, check, prose", steps: []string{"code", "test-pass", "doc"}},
		{name: "code, check, observed bash prose", steps: []string{"code", "test-pass", "bash-doc"}},
		{name: "code, check, prose, prose", steps: []string{"code", "test-pass", "doc", "bash-doc"}},
		{name: "check failed then passed, prose", steps: []string{"code", "test-fail", "test-pass", "doc"}},
		{name: "check, then a failed prose write", steps: []string{"code", "test-pass", "doc-failed"}},
		{name: "code after the check", steps: []string{"code", "test-pass", "doc", "code", "doc"}, owed: true},
		{name: "failed code write after the check", steps: []string{"code", "test-pass", "code-failed", "doc"}, owed: true},
		{name: "observed bash code after the check", steps: []string{"code", "test-pass", "bash-code", "doc"}, owed: true},
		{name: "unscoped bash after the check", steps: []string{"code", "test-pass", "bash-unknown", "doc"}, owed: true},
		{name: "failed bash after the check", steps: []string{"code", "test-pass", "bash-failed", "doc"}, owed: true},
		{name: "another check stands failed", steps: []string{"code", "vet-fail", "test-pass", "doc"}, owed: true},
		{name: "failed check before observed code the pass covers", steps: []string{"code", "vet-fail", "bash-code", "test-pass", "doc"}, owed: true},
		{name: "no check", steps: []string{"code", "doc"}, owed: true},
		{name: "failing check", steps: []string{"code", "test-fail", "doc"}, owed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := proseVerdictOf(t, proseWorkspace(t), stepsByName(t, tc.steps...), nil)
			if got.owed() != tc.owed {
				t.Errorf("verification gap = %v, stale obligation = %v; want owed = %v", got.gap, got.stale, tc.owed)
			}
		})
	}
}

func TestProseAfterPassingCheckWaivedOnlyWithoutDeclaredChecks(t *testing.T) {
	for _, tc := range []struct {
		name     string
		contract func(*Agent)
	}{
		{name: "declared check", contract: func(a *Agent) { a.projectChecks = []instruction.VerifyCheck{{Command: "go test ./..."}} }},
		{name: "baseline check", contract: func(a *Agent) { a.task.checkpoint.BaselineChecks = []string{"go test ./..."} }},
		{name: "captured criterion", contract: func(a *Agent) {
			a.task.baselineCriteria = map[string]evidence.TestCriterion{"main_test.go": {}}
		}},
		{name: "delivery", contract: func(a *Agent) { a.deliveryProfile = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := proseWorkspace(t)
			seq := stepsByName(t, "code", "test-pass", "doc")
			if got := proseVerdictOf(t, root, seq, tc.contract); !got.stale {
				t.Errorf("stale obligation waived under %s", tc.name)
			}
		})
	}
}

// TestProseAfterPassingCheckNeverHidesCode replays every sequence of up to four
// receipts, and random longer ones, against the waiver's definition: prose after
// the last check and the last change beyond prose may release a turn only when a
// check stands passing over that change and the turn without the prose is
// released too. PROSE_DIFF_DUMP writes each verdict for a cross-tree diff.
func TestProseAfterPassingCheckNeverHidesCode(t *testing.T) {
	root := proseWorkspace(t)
	steps := proseSteps()
	var dump strings.Builder
	var seqs [][]proseStep
	var grow func(prefix []proseStep, depth int)
	grow = func(prefix []proseStep, depth int) {
		if len(prefix) > 0 {
			seqs = append(seqs, slices.Clone(prefix))
		}
		if depth == 0 {
			return
		}
		for _, s := range steps {
			grow(append(prefix, s), depth-1)
		}
	}
	grow(nil, 4)
	rng := rand.New(rand.NewSource(12520))
	for range 4000 {
		seq := make([]proseStep, 5+rng.Intn(4))
		for i := range seq {
			seq[i] = steps[rng.Intn(len(steps))]
		}
		seqs = append(seqs, seq)
	}
	for _, seq := range slices.Clone(seqs) {
		if control, ok := withoutTrailingProse(seq); ok {
			seqs = append(seqs, control)
		}
	}
	violations := 0
	for _, seq := range seqs {
		got := proseVerdictOf(t, root, seq, nil)
		names := make([]string, len(seq))
		for i, s := range seq {
			names[i] = s.name
		}
		fmt.Fprintf(&dump, "%s\t%v\t%v\n", strings.Join(names, ","), got.gap, got.stale)
		if got.owed() {
			continue
		}
		control, ok := withoutTrailingProse(seq)
		if !ok {
			continue
		}
		beyond := lastIndex(seq, func(s proseStep) bool { return s.beyond })
		standing := map[string]string{}
		for _, s := range seq[beyond+1:] {
			if s.check != "" {
				standing[s.receipt.Command] = s.check
			}
		}
		outcomes := slices.Collect(maps.Values(standing))
		covered := beyond < 0 || (slices.Contains(outcomes, "pass") && !slices.Contains(outcomes, "fail"))
		if !covered || proseVerdictOf(t, root, control, nil).owed() {
			violations++
			t.Errorf("released %v: the prose at its end hides a change no passing check covers", names)
		}
	}
	if path := os.Getenv("PROSE_DIFF_DUMP"); path != "" {
		if err := os.WriteFile(path, []byte(dump.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%d sequences, %d violations", len(seqs), violations)
}

func lastIndex(seq []proseStep, match func(proseStep) bool) int {
	last := -1
	for i, s := range seq {
		if match(s) {
			last = i
		}
	}
	return last
}

// withoutTrailingProse drops the prose written after both the last check and
// the last change beyond prose, reporting whether there was any.
func withoutTrailingProse(seq []proseStep) ([]proseStep, bool) {
	trailing := max(lastIndex(seq, func(s proseStep) bool { return s.beyond }), lastIndex(seq, func(s proseStep) bool { return s.check != "" }))
	if !slices.ContainsFunc(seq[trailing+1:], func(s proseStep) bool { return s.prose }) {
		return nil, false
	}
	return append(slices.Clone(seq[:trailing+1]), slices.DeleteFunc(slices.Clone(seq[trailing+1:]), func(s proseStep) bool { return s.prose })...), true
}

func TestNamedWriteThroughLinkNamesItsTarget(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.go", "README.md", filepath.Join("src", "a.md")} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte("neutral\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(root, "main.go"), filepath.Join(root, "notes.md")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "src"), filepath.Join(root, "docs")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		path    string
		success bool
		want    string
	}{
		{name: "linked file", path: "notes.md", success: true, want: filepath.Join(root, "main.go")},
		{name: "linked file, write reported failed", path: "notes.md", want: filepath.Join(root, "main.go")},
		{name: "linked directory", path: filepath.Join("docs", "a.md"), success: true, want: filepath.Join(root, "src", "a.md")},
		{name: "plain file", path: "README.md", success: true},
		{name: "missing file", path: "gone.md", success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := evidence.Receipt{ToolName: "write_file", Success: tc.success, Mutation: true, Write: true, MutationEvidence: evidence.MutationProven, Paths: []string{tc.path}}
			decorateObservedPaths(&rec, &toolCallPlan{pathsBefore: pathSnapshot{root: root}})
			added := slices.DeleteFunc(slices.Clone(rec.Paths), func(p string) bool { return p == tc.path })
			if tc.want == "" && len(added) != 0 {
				t.Errorf("paths = %v, want only %s", rec.Paths, tc.path)
			}
			if tc.want != "" && !slices.Contains(added, tc.want) {
				t.Errorf("paths = %v, want the link's target %s", rec.Paths, tc.want)
			}
		})
	}
}
