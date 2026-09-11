package tool

import (
	"encoding/json"
	"fmt"
)

// OperationDiagnostic is content-free, host-only recovery metadata. It must
// not be used as an authorization or reconstructed from model-written text.
// AllowedRecovery and RetryBudget make a rejection machine-executable: the
// model picks an offered action instead of guessing another wording of the
// same call, and an exhausted budget means the operation belongs to the user.
type OperationDiagnostic struct {
	Code             string      `json:"code"`
	Path             string      `json:"path,omitempty"`
	OperationID      string      `json:"operation_id,omitempty"`
	ExpectedSnapshot string      `json:"expected_snapshot,omitempty"`
	ActualSnapshot   string      `json:"actual_snapshot,omitempty"`
	RequiredRanges   []ReadRange `json:"required_ranges,omitempty"`
	Recovery         string      `json:"recovery"`
	// AvailableReceipts are host-issued IDs the model may cite right now.
	AvailableReceipts []string `json:"available_receipts,omitempty"`
	// AllowedRecovery is the closed set of actions the host will accept, e.g.
	// "use_receipt:r_123", "reread_target", "run_verifier", "mark_manual".
	AllowedRecovery []string `json:"allowed_recovery,omitempty"`
	Retryable       bool     `json:"retryable,omitempty"`
	RetryBudget     int      `json:"retry_budget,omitempty"`
	// State is the operation's host lifecycle state after this failure.
	State string `json:"state,omitempty"`
}

const (
	ReadPartial          = "READ_PARTIAL"
	ReadCursorInvalid    = "READ_CURSOR_INVALID"
	ReadSourceChanged    = "READ_SOURCE_CHANGED"
	ReadHardStop         = "READ_HARD_STOP"
	WriteEvidenceMissing = "WRITE_EVIDENCE_MISSING"
	WriteEvidenceStale   = "WRITE_EVIDENCE_STALE"
	WriteTargetAbsent    = "WRITE_TARGET_ABSENT"
	WriteTargetAmbiguous = "WRITE_TARGET_AMBIGUOUS"
	// OperationNeedsUser is the terminal code for an operation the host stopped
	// automating after the same failure twice.
	OperationNeedsUser = "OPERATION_NEEDS_USER"
	// VerificationReceiptMissing rejects a completion citing verification the
	// host has no successful receipt for.
	VerificationReceiptMissing = "VERIFICATION_RECEIPT_MISSING"
	// VerificationReceiptMismatch rejects a real receipt that does not cover
	// the operation being signed off.
	VerificationReceiptMismatch = "VERIFICATION_RECEIPT_MISMATCH"
)

// Recovery actions the host offers. They are identifiers, not prose, so the
// model selects rather than composes.
const (
	RecoveryRereadTarget = "reread_target"
	RecoveryRunVerifier  = "run_verifier"
	RecoveryMarkManual   = "mark_manual"
	RecoveryAbandonEdit  = "abandon_edit"
	RecoveryUseReceipt   = "use_receipt:"
)

// ModelFacing renders the closed set of recovery choices as compact JSON. The
// model selects an action from allowed_recovery; it never has to reconstruct a
// command string or guess which wording the host will accept.
func (d OperationDiagnostic) ModelFacing() string {
	if len(d.AllowedRecovery) == 0 && len(d.AvailableReceipts) == 0 {
		return ""
	}
	payload := struct {
		Code              string   `json:"code"`
		OperationID       string   `json:"operation_id,omitempty"`
		Path              string   `json:"path,omitempty"`
		State             string   `json:"state,omitempty"`
		AvailableReceipts []string `json:"available_receipts,omitempty"`
		AllowedRecovery   []string `json:"allowed_recovery,omitempty"`
		Retryable         bool     `json:"retryable"`
		RetryBudget       int      `json:"retry_budget"`
	}{d.Code, d.OperationID, d.Path, d.State, d.AvailableReceipts, d.AllowedRecovery, d.Retryable, d.RetryBudget}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return "recovery: " + string(encoded)
}

type OperationError struct {
	Diagnostic OperationDiagnostic
	Cause      error
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("%s: %v; %s", e.Diagnostic.Code, e.Cause, e.Diagnostic.Recovery)
}
func (e *OperationError) Unwrap() error { return e.Cause }
