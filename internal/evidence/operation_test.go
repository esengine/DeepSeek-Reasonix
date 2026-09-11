package evidence

import (
	"encoding/json"
	"testing"
)

func TestOperationIDIgnoresCosmeticArgumentDifferences(t *testing.T) {
	a := OperationID("edit_file", json.RawMessage(`{"path":"a.go","old":"x","new":"y"}`))
	b := OperationID("edit_file", json.RawMessage(`{ "new" : "y", "old": "x", "path":"a.go" }`))
	if a != b {
		t.Fatalf("same edit forked into two operations: %s vs %s", a, b)
	}
	if c := OperationID("edit_file", json.RawMessage(`{"path":"b.go","old":"x","new":"y"}`)); c == a {
		t.Fatal("different target must be a different operation")
	}
}

func TestOperationIDIgnoresAttemptOnlyArguments(t *testing.T) {
	base := OperationID("edit_file", json.RawMessage(`{"path":"a.go","old":"x","new":"y"}`))
	for _, args := range []string{
		`{"path":"a.go","old":"x","new":"y","source_token":"r1"}`,
		`{"path":"a.go","old":"x","new":"y","expected_digest":"d1"}`,
		`{"path":"a.go","old":"x","new":"y","cursor":"next"}`,
		`{"path":"a.go","old":"x","new":"y","operationId":"attempt-1","documentToken":"doc-1"}`,
	} {
		if got := OperationID("edit_file", json.RawMessage(args)); got != base {
			t.Fatalf("attempt-only argument changed operation identity: %q != %q for %s", got, base, args)
		}
	}
}

func TestOperationLifecycleSettlesOnSuccessfulVerification(t *testing.T) {
	ops := NewOperationLedger()
	op := ops.Open("op_1", "edit_file", []string{"a.go"})
	if op.State != OperationPrepared {
		t.Fatalf("state = %q, want prepared", op.State)
	}
	if op = ops.Apply("op_1", ReceiptRef{ID: "r_1", Kind: ReceiptKindMutation, Success: true}); op.State != OperationApplied {
		t.Fatalf("state = %q, want applied", op.State)
	}
	if op = ops.AttachVerification("op_1", ReceiptRef{ID: "r_2", Kind: ReceiptKindVerification, Success: true}); op.State != OperationSettled {
		t.Fatalf("state = %q, want settled", op.State)
	}
	// Repeat settle is idempotent, so a duplicate tool result cannot double-count.
	if op = ops.Settle("op_1"); op.State != OperationSettled || op.Verification == nil {
		t.Fatalf("resettle lost state: %+v", op)
	}
}

func TestOperationFailedVerificationDoesNotSettle(t *testing.T) {
	ops := NewOperationLedger()
	ops.Open("op_1", "edit_file", []string{"a.go"})
	ops.Apply("op_1", ReceiptRef{ID: "r_1", Success: true})
	op := ops.AttachVerification("op_1", ReceiptRef{ID: "r_2", Kind: ReceiptKindVerification, Success: false})
	if op.State != OperationVerificationPending {
		t.Fatalf("state = %q, want verification_pending", op.State)
	}
	if len(ops.Unsettled()) != 1 {
		t.Fatalf("unsettled = %d, want 1", len(ops.Unsettled()))
	}
}

func TestOperationSameFailureTwiceStopsAutomaticRecovery(t *testing.T) {
	ops := NewOperationLedger()
	ops.Open("op_1", "edit_file", []string{"a.go"})

	first := ops.Fail("op_1", "WRITE_EVIDENCE_STALE")
	if !first.Retryable || first.Attempt != 1 || first.Budget != 1 {
		t.Fatalf("first failure = %+v, want one retry offered", first)
	}
	second := ops.Fail("op_1", "WRITE_EVIDENCE_STALE")
	if second.Retryable || second.State != OperationNeedsUser {
		t.Fatalf("second identical failure = %+v, want needs_user", second)
	}
	if got := ops.NeedsUser(); len(got) != 1 || got[0].ID != "op_1" {
		t.Fatalf("needs-user list = %+v", got)
	}

	// A different failure code is new information and gets its own budget.
	other := ops.Fail("op_1", "WRITE_TARGET_ABSENT")
	if other.Attempt != 1 {
		t.Fatalf("distinct failure code shares a budget: %+v", other)
	}
}

func TestOperationNewFailureBudgetIsPerOperation(t *testing.T) {
	ops := NewOperationLedger()
	ops.Fail("op_1", "WRITE_EVIDENCE_STALE")
	ops.Fail("op_1", "WRITE_EVIDENCE_STALE")
	if d := ops.Fail("op_2", "WRITE_EVIDENCE_STALE"); !d.Retryable {
		t.Fatalf("a new operation inherited an old block: %+v", d)
	}
}

func TestOperationNewEpochReopensRecoveryAfterExplicitContinue(t *testing.T) {
	ops := NewOperationLedger()
	ops.Open("op_1", "edit_file", []string{"a.go"})
	ops.Fail("op_1", "WRITE_EVIDENCE_STALE")
	ops.Fail("op_1", "WRITE_EVIDENCE_STALE")

	ops.NewEpoch("op_1")
	op, _ := ops.Get("op_1")
	if op.State == OperationNeedsUser || op.RecoveryCount != 0 {
		t.Fatalf("epoch did not clear the block: %+v", op)
	}
	if d := ops.Fail("op_1", "WRITE_EVIDENCE_STALE"); !d.Retryable || d.Attempt != 1 {
		t.Fatalf("post-epoch failure = %+v, want a fresh budget", d)
	}
}

func TestOperationTerminalStateIsNotReopenedByLateResults(t *testing.T) {
	ops := NewOperationLedger()
	ops.Open("op_1", "bash", nil)
	ops.Fail("op_1", "X")
	ops.Fail("op_1", "X") // needs_user
	if op := ops.Apply("op_1", ReceiptRef{ID: "r_9", Success: true}); op.State != OperationNeedsUser {
		t.Fatalf("needs_user was overwritten by a late apply: %+v", op)
	}
}

func TestOperationOpenIsIdempotent(t *testing.T) {
	ops := NewOperationLedger()
	ops.Open("op_1", "write_file", []string{"a.go"})
	ops.Apply("op_1", ReceiptRef{ID: "r_1", Success: true})
	again := ops.Open("op_1", "write_file", []string{"a.go", "b.go"})
	if again.State != OperationApplied {
		t.Fatalf("resubmitting an operation restarted it: %+v", again)
	}
	if len(again.TargetPaths) != 2 {
		t.Fatalf("target paths = %v, want both recorded", again.TargetPaths)
	}
	if len(ops.Snapshot()) != 1 {
		t.Fatalf("resubmission created a second operation: %+v", ops.Snapshot())
	}
}

func TestLedgerReceiptCoversOperationByIDNotCommandText(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "bash", Command: "cd repo && go test ./...", Success: true, OperationID: "op_1"})
	receipts := l.Receipts()
	id := receipts[0].ID
	if id == "" {
		t.Fatal("receipt got no host id")
	}
	if !l.ReceiptCoversOperation(id, "op_1") {
		t.Fatal("receipt issued for the operation was not accepted")
	}
	if l.ReceiptCoversOperation(id, "op_other") {
		t.Fatal("receipt for another operation was accepted")
	}
	if l.ReceiptCoversOperation("r_missing", "op_1") {
		t.Fatal("unknown receipt id was accepted")
	}
}

func TestLedgerReceiptCoversOperationByTargetPath(t *testing.T) {
	l := NewLedger()
	l.Operations().Open("op_1", "edit_file", []string{"internal/auth/login.go"})
	l.Record(Receipt{ToolName: "bash", Command: "go test ./internal/auth", Success: true, Paths: []string{"internal/auth/login.go"}})
	id := l.Receipts()[0].ID
	if !l.ReceiptCoversOperation(id, "op_1") {
		t.Fatal("a receipt covering the operation's target path was rejected")
	}
}

func TestLedgerAttachedVerificationCoversOperationAfterMutation(t *testing.T) {
	l := NewLedger()
	ops := l.Operations()
	ops.Open("op_write", "write_file", []string{"a.go"})
	mutation := l.Record(Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}, OperationID: "op_write"})
	ops.Apply("op_write", mutation.Ref())
	verification := l.Record(Receipt{ToolName: "bash", Command: "go test ./...", Success: true, OperationID: "op_verify"})
	ops.AttachVerification("op_write", verification.Ref())

	if !l.ReceiptCoversOperation(verification.ID, "op_write") {
		t.Fatal("the verification attached after the mutation was rejected")
	}
}

func TestLedgerVerificationBeforeMutationDoesNotCoverOperation(t *testing.T) {
	l := NewLedger()
	ops := l.Operations()
	ops.Open("op_write", "write_file", []string{"a.go"})
	verification := l.Record(Receipt{ToolName: "bash", Command: "go test ./...", Success: true, OperationID: "op_verify"})
	mutation := l.Record(Receipt{ToolName: "write_file", Success: true, Write: true, Paths: []string{"a.go"}, OperationID: "op_write"})
	ops.Apply("op_write", mutation.Ref())
	ops.AttachVerification("op_write", verification.Ref())

	if l.ReceiptCoversOperation(verification.ID, "op_write") {
		t.Fatal("a verification older than the mutation was accepted")
	}
}

func TestLedgerPathScopedVerificationMustCoverEveryOperationTarget(t *testing.T) {
	l := NewLedger()
	ops := l.Operations()
	ops.Open("op_write", "multi_edit", []string{"a.go", "b.go"})
	mutation := l.Record(Receipt{ToolName: "multi_edit", Success: true, Write: true, Paths: []string{"a.go", "b.go"}, OperationID: "op_write"})
	ops.Apply("op_write", mutation.Ref())
	verification := l.Record(Receipt{ToolName: "review", Success: true, Paths: []string{"a.go"}, OperationID: "op_review"})
	ops.AttachVerification("op_write", verification.Ref())

	if l.ReceiptCoversOperation(verification.ID, "op_write") {
		t.Fatal("a path-scoped verification covering only one target was accepted")
	}
}

func TestOperationLedgerDoesNotSettleOnPartialPathVerification(t *testing.T) {
	ops := NewOperationLedger()
	ops.Open("op_write", "multi_edit", []string{"a.go", "b.go"})
	ops.Apply("op_write", ReceiptRef{ID: "r_mutation", Kind: ReceiptKindMutation, Success: true, Paths: []string{"a.go", "b.go"}})

	if op, attached := ops.AttachLatestVerification(ReceiptRef{
		ID: "r_partial", Kind: ReceiptKindReview, Success: true, Paths: []string{"a.go"},
	}); attached || op.ID != "" {
		t.Fatalf("partial verifier attached to a multi-target operation: attached=%v op=%+v", attached, op)
	}
	op, _ := ops.Get("op_write")
	if op.State != OperationApplied || op.Verification != nil {
		t.Fatalf("partial verification changed operation state: %+v", op)
	}

	if op, attached := ops.AttachLatestVerification(ReceiptRef{
		ID: "r_all", Kind: ReceiptKindReview, Success: true, Paths: []string{"a.go", "b.go"},
	}); !attached || op.State != OperationSettled {
		t.Fatalf("full path verification was not attached: attached=%v op=%+v", attached, op)
	}
}

func TestLedgerFailedReceiptNeverCoversOperation(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "bash", Command: "go test ./...", Success: false, OperationID: "op_1"})
	id := l.Receipts()[0].ID
	if l.ReceiptCoversOperation(id, "op_1") {
		t.Fatal("a failed receipt was accepted as proof")
	}
}

func TestLedgerCitableReceiptsListsWhatTheModelMayCite(t *testing.T) {
	l := NewLedger()
	l.Record(Receipt{ToolName: "bash", Command: "go test ./...", Success: true})
	l.Record(Receipt{ToolName: "complete_step", Step: "1", Success: true})
	l.Record(Receipt{ToolName: "write_file", Paths: []string{"a.go"}, Write: true, Success: true})
	refs := l.CitableReceipts(8)
	if len(refs) != 2 {
		t.Fatalf("citable = %+v, want the command and the write", refs)
	}
	if refs[0].Kind != ReceiptKindMutation {
		t.Fatalf("most recent citable kind = %q, want mutation", refs[0].Kind)
	}
	if refs[1].Kind != ReceiptKindVerification {
		t.Fatalf("recognized verifier kind = %q, want verification", refs[1].Kind)
	}
}

func TestReceiptKindLeavesUnknownCommandsUnclassified(t *testing.T) {
	r := Receipt{ToolName: "bash", Command: "./company-ci.sh", Success: true}
	if got := r.Kind(); got != ReceiptKindCommand {
		t.Fatalf("kind = %q, want command — an unknown verifier must not be promoted", got)
	}
}
