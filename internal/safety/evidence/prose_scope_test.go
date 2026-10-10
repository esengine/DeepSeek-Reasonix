package evidence

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProseMutationPathScope(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	for _, tc := range []struct {
		name string
		root string
		path string
		want bool
	}{
		{name: "relative prose", root: root, path: "notes.md", want: true},
		{name: "deleted nested prose", root: root, path: "gone/note.rst", want: true},
		{name: "absolute prose", root: root, path: filepath.Join(root, "notes.md"), want: true},
		{name: "unknown root", path: "notes.md"},
		{name: "outside prose", root: root, path: filepath.Join(outside, "REASONIX.md")},
		{name: "relative escape", root: root, path: "../outside.md"},
		{name: "dotdot cannot conceal links", root: root, path: "alias/../notes.md"},
		{name: "VCS hooks", root: root, path: ".git/hooks/run.md"},
		{name: "nested VCS", root: root, path: "project/.hg/hooks/run.rst"},
		{name: "root inside VCS", root: filepath.Join(root, ".git"), path: "hooks/run.md"},
		{name: "code suffix", root: root, path: "gone.go"},
		{name: "uppercase suffix", root: root, path: "gone.MD", want: true},
		{name: "text input", root: root, path: "requirements.txt"},
		{name: "MDX component", root: root, path: "page.mdx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := proseMutationPath(tc.root, tc.path); got != tc.want {
				t.Errorf("scope = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestProseMutationPathRejectsLinkedAncestor(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "alias")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if proseMutationPath(root, "alias/deleted.md") {
		t.Fatal("linked ancestor waived verification")
	}
}

func TestProseWaiverPreservesSpellingAcrossReceiptReplay(t *testing.T) {
	root := t.TempDir()
	r := ReceiptFromToolCall("move_file", json.RawMessage(`{"source_path":"gone.GO","destination_path":"notes.md"}`), true, ToolFacts{WritesNamedPaths: true})
	l := ledgerOf(r)
	data, err := json.Marshal(l.Receipts()[0])
	if err != nil {
		t.Fatal(err)
	}
	var replay Receipt
	if err := json.Unmarshal(data, &replay); err != nil {
		t.Fatal(err)
	}
	l = ledgerOf(replay)
	if l.ProseOnlyWithoutChecks(CaptureCheckContract(nil, nil).WithDelivery(false).WithObserveRoot(root)) {
		t.Fatal("replayed receipt lost uppercase source spelling")
	}
}

func TestVerifiedBeneathProse(t *testing.T) {
	zero, one := 0, 1
	code := Receipt{ToolName: "write_file", Success: true, Mutation: true, Write: true, MutationEvidence: MutationProven, Paths: []string{"main.go"}}
	doc := Receipt{ToolName: "write_file", Success: true, Mutation: true, Write: true, MutationEvidence: MutationProven, Paths: []string{"README.md"}}
	pass := Receipt{ToolName: "bash", Command: "go test ./...", Success: true, ExitCode: &zero, Verification: VerificationPassed}
	vetFail := Receipt{ToolName: "bash", Command: "go vet ./...", ExitCode: &one, Verification: VerificationFailed}
	unscoped := Receipt{ToolName: "bash", Command: "./gen.sh", Success: true, Mutation: true, MutationEvidence: MutationUnknown}
	free := CaptureCheckContract(nil, nil)
	for _, tc := range []struct {
		name     string
		receipts []Receipt
		contract CheckContract
		prior    int
		want     bool
	}{
		{name: "prose over a passing check", receipts: []Receipt{code, pass, doc}, contract: free, prior: 0, want: true},
		{name: "no earlier writer", receipts: []Receipt{unscoped, pass, doc}, contract: free, prior: -1, want: true},
		{name: "earlier writer under a failed check", receipts: []Receipt{code, vetFail, unscoped, pass, doc}, contract: free, prior: 0},
		{name: "code after the check", receipts: []Receipt{code, pass, doc, code, doc}, contract: free, prior: 3},
		{name: "unscoped change after the check", receipts: []Receipt{code, pass, unscoped, doc}, contract: free, prior: 0},
		{name: "prose only", receipts: []Receipt{doc, pass, doc}, contract: free, prior: 0},
		{name: "no check", receipts: []Receipt{code, doc}, contract: free, prior: 0},
		{name: "declared check", receipts: []Receipt{code, pass, doc}, contract: CaptureCheckContract(nil, []string{"go test ./..."}), prior: 0},
		{name: "delivery", receipts: []Receipt{code, pass, doc}, contract: free.WithDelivery(true), prior: 0},
		{name: "tool hook", receipts: []Receipt{code, pass, doc}, contract: free.WithUnseenWriter(true), prior: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := ledgerOf(tc.receipts...)
			anchor := len(tc.receipts) - 1
			prior := func(int) (int, bool) { return tc.prior, tc.prior >= 0 }
			if got := l.VerifiedBeneathProse(tc.contract.WithObserveRoot(t.TempDir()), anchor, prior); got != tc.want {
				t.Errorf("VerifiedBeneathProse = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestObligationsOweNoStaleCheckForProseOverAPass(t *testing.T) {
	zero := 0
	root := t.TempDir()
	code := Receipt{ToolName: "write_file", Success: true, Mutation: true, Write: true, MutationEvidence: MutationProven, Paths: []string{"main.go"}}
	doc := Receipt{ToolName: "write_file", Success: true, Mutation: true, Write: true, MutationEvidence: MutationProven, Paths: []string{"README.md"}}
	pass := Receipt{ToolName: "bash", Command: "go test ./...", Success: true, ExitCode: &zero, Verification: VerificationPassed}
	contract := CaptureCheckContract(nil, nil).WithObserveRoot(root)
	if owed := ledgerOf(code, pass, doc).Obligations(contract); len(owed) != 0 {
		t.Errorf("prose over a passing check owes %v", owed)
	}
	if owed := ledgerOf(code, pass, doc, code).Obligations(contract); len(owed) != 1 || owed[0].Kind != ObligationStaleVerification {
		t.Errorf("code after the check owes %v, want stale verification", owed)
	}
	if at, ok := ledgerOf(code, pass, doc, code).LatestSuccessfulMutationIndexThrough(2); !ok || at != 2 {
		t.Errorf("latest mutation through 2 = %d, %v", at, ok)
	}
	if at, ok := ledgerOf(code, pass, doc).LatestSuccessfulWriterIndexThrough(1, nil); !ok || at != 0 {
		t.Errorf("latest writer through 1 = %d, %v", at, ok)
	}
}

func TestUnseenWriterLiftsTheWholeLedgerProseWaiver(t *testing.T) {
	doc := Receipt{ToolName: "write_file", Success: true, Mutation: true, Write: true, MutationEvidence: MutationProven, Paths: []string{"README.md"}}
	contract := CaptureCheckContract(nil, nil).WithObserveRoot(t.TempDir())
	if !ledgerOf(doc).ProseOnlyWithoutChecks(contract) {
		t.Fatal("prose-only turn owes a check without a tool hook")
	}
	if ledgerOf(doc).ProseOnlyWithoutChecks(contract.WithUnseenWriter(true)) || !contract.WithUnseenWriter(true).UnseenWriter() {
		t.Error("prose waived while a tool hook may write unseen")
	}
}
