package evidence

import (
	"slices"
	"strings"
)

// LookupReceipt returns a copy of a receipt by host-issued ID. Arguments are
// dropped: a citation resolves a fact, it never reopens the original call.
func (l *Ledger) LookupReceipt(id string) (Receipt, bool) {
	id = strings.TrimSpace(id)
	if l == nil || id == "" {
		return Receipt{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range l.receipts {
		if r.ID == id {
			r.Args = nil
			return r, true
		}
	}
	return Receipt{}, false
}

// ReceiptRef returns a bounded model-safe receipt projection.
func (l *Ledger) ReceiptRef(id string) (ReceiptRef, bool) {
	r, ok := l.LookupReceipt(id)
	if !ok {
		return ReceiptRef{}, false
	}
	return r.Ref(), true
}

// ReceiptCoversOperation reports whether a cited receipt actually belongs to
// the operation being completed. This replaces command-text matching: shell
// prefixes, quoting, argument order, and working directory stop mattering
// because identity is the ID the host issued, not the text the model retyped.
func (l *Ledger) ReceiptCoversOperation(receiptID, operationID string) bool {
	r, ok := l.LookupReceipt(receiptID)
	if !ok || !r.Success {
		return false
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return false
	}
	op, ok := l.Operations().Get(operationID)
	if !ok {
		return r.OperationID == operationID
	}

	if op.Mutation != nil && op.Mutation.ID == r.ID {
		return true
	}
	if op.Verification != nil && op.Verification.ID == r.ID {
		return l.verificationReceiptCoversOperation(r, op)
	}
	if r.OperationID == operationID {
		switch r.Kind() {
		case ReceiptKindVerification, ReceiptKindReview:
			return l.verificationReceiptCoversOperation(r, op)
		default:
			return true
		}
	}

	// Compatibility for hand-built receipts created before operation IDs were
	// stamped at runtime. A legacy receipt may cover an operation only when it
	// explicitly names every target path; a pathless receipt has no safe link.
	return r.OperationID == "" &&
		len(r.Paths) > 0 &&
		receiptPathsCoverOperation(r.Paths, op.TargetPaths) &&
		l.verificationReceiptFollowsMutation(r, op)
}

func (l *Ledger) verificationReceiptCoversOperation(r Receipt, op Operation) bool {
	return receiptPathsCoverOperation(r.Paths, op.TargetPaths) &&
		l.verificationReceiptFollowsMutation(r, op)
}

func (l *Ledger) verificationReceiptFollowsMutation(r Receipt, op Operation) bool {
	if op.Mutation == nil {
		return true
	}
	mutation, ok := l.LookupReceipt(op.Mutation.ID)
	return ok && r.Sequence > mutation.Sequence
}

// receiptPathsCoverOperation treats a pathless verifier as a whole-operation
// check. A path-scoped verifier must cover every target, not merely overlap one
// of them, otherwise a test for file A could be cited for a change to A and B.
func receiptPathsCoverOperation(receiptPaths, targetPaths []string) bool {
	if len(receiptPaths) == 0 || len(targetPaths) == 0 {
		return true
	}
	for _, target := range targetPaths {
		if !slices.Contains(receiptPaths, target) {
			return false
		}
	}
	return true
}

// CitableReceipts returns up to limit successful receipts from this turn, most
// recent first, so a rejection can list what the model may cite instead of
// asking it to guess a command string.
func (l *Ledger) CitableReceipts(limit int) []ReceiptRef {
	if l == nil || limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]ReceiptRef, 0, limit)
	for i := len(l.receipts) - 1; i >= 0 && len(out) < limit; i-- {
		r := l.receipts[i]
		if !r.Success || r.ToolName == "complete_step" || r.ToolName == "todo_write" {
			continue
		}
		r.Args = nil
		out = append(out, r.Ref())
	}
	return out
}

// Operations exposes the turn's operation lifecycle, lazily created so every
// existing Ledger construction site keeps working unchanged. Never call it
// while holding l.mu.
func (l *Ledger) Operations() *OperationLedger {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.ops == nil {
		l.ops = NewOperationLedger()
	}
	return l.ops
}
