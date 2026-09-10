package tool

import "fmt"

// OperationDiagnostic is content-free, host-only recovery metadata. It must
// not be used as an authorization or reconstructed from model-written text.
type OperationDiagnostic struct {
	Code             string      `json:"code"`
	Path             string      `json:"path,omitempty"`
	OperationID      string      `json:"operation_id,omitempty"`
	ExpectedSnapshot string      `json:"expected_snapshot,omitempty"`
	ActualSnapshot   string      `json:"actual_snapshot,omitempty"`
	RequiredRanges   []ReadRange `json:"required_ranges,omitempty"`
	Recovery         string      `json:"recovery"`
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
)

type OperationError struct {
	Diagnostic OperationDiagnostic
	Cause      error
}

func (e *OperationError) Error() string {
	return fmt.Sprintf("%s: %v; %s", e.Diagnostic.Code, e.Cause, e.Diagnostic.Recovery)
}
func (e *OperationError) Unwrap() error { return e.Cause }
