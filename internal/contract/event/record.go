package event

import (
	"reasonix/internal/base/nilutil"
	"reasonix/internal/contract/hostaudit"
)

// Forwarding a turn's audit signals to sinks that opt into them. Each is a
// capability a sink may or may not implement, and the assertion lives here
// rather than at every call site holding a receipt.

// RecordTurnCompletion records one successfully admitted top-level controller
// run on sinks that opt into completion accounting.
func RecordTurnCompletion(s Sink) {
	if nilutil.IsNil(s) {
		return
	}
	if ts, ok := s.(TurnCompletionSink); ok {
		ts.RecordTurnCompletion()
	}
}

// RecordReadinessAudit forwards a readiness audit receipt to sinks that opt in.
func RecordReadinessAudit(s Sink, a hostaudit.ReadinessAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if rs, ok := s.(ReadinessAuditSink); ok {
		rs.RecordReadinessAudit(a)
	}
}

// ProtocolRecoveryKind is a content-free internal observation about a provider
// protocol repair. It is deliberately separate from Event/Notice so recovery
// stays invisible in chat transcripts and frontends do not need to understand
// provider implementation details.
type ProtocolRecoveryKind string

const (
	ProtocolRecoveryMissingReasoningDetected        ProtocolRecoveryKind = "missing_reasoning_detected"
	ProtocolRecoveryMissingReasoningRetryAttempted  ProtocolRecoveryKind = "missing_reasoning_retry_attempted"
	ProtocolRecoveryMissingReasoningRetryRecovered  ProtocolRecoveryKind = "missing_reasoning_retry_recovered"
	ProtocolRecoveryMissingReasoningRetryReplaced   ProtocolRecoveryKind = "missing_reasoning_retry_replaced_response"
	ProtocolRecoveryMissingReasoningRetrySuppressed ProtocolRecoveryKind = "missing_reasoning_retry_suppressed"
	ProtocolRecoveryMissingReasoningFallback        ProtocolRecoveryKind = "missing_reasoning_fallback_used"
	// ProtocolRecoveryMissingReasoningModelSilent is a tool-call turn the provider
	// billed no thinking tokens for. Recorded to keep the shape visible, never
	// replayed: nothing was lost in transit, so an identical request buys nothing.
	ProtocolRecoveryMissingReasoningModelSilent ProtocolRecoveryKind = "missing_reasoning_model_silent"
)

type ProtocolRecoveryAudit struct {
	Kind ProtocolRecoveryKind
	// ChildID names the delegated run this repair happened inside, stamped by
	// the nesting sink that already knows it; empty is the parent's own loop.
	ChildID string
}

// ContractShadowAudit is the shadow task-contract's end-of-turn summary:
// counts and enums only, never requirement text. Shadow means observed, not
// enforced — the old control logic still decides behavior.
type ContractShadowAudit struct {
	Intent                string
	Requirements          int
	RequirementsSatisfied int
	Checks                int
	ChecksSatisfied       int
	Epoch                 uint64
	Verdict               string
	Complete              bool
	ReadyToFinalize       bool
}

// ContractShadowAuditSink is an optional sink capability; implementations
// must keep it content-free, like every other audit channel.
type ContractShadowAuditSink interface {
	RecordContractShadow(ContractShadowAudit)
}

// RecordContractShadow forwards the shadow contract summary only to sinks
// that explicitly opt in. Ordinary UI sinks receive nothing.
func RecordContractShadow(s Sink, a ContractShadowAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if cs, ok := s.(ContractShadowAuditSink); ok {
		cs.RecordContractShadow(a)
	}
}

// CompletionReportAudit is the host-authored completion report's end-of-turn
// summary: counts, enums, and gap kinds only, never paths or command text.
// The gap counters carry the point — what the turn left unproven.
type CompletionReportAudit struct {
	Verdict             string
	Risk                string
	Criteria            int
	CriteriaSatisfied   int
	Changes             int
	ChangesUnreviewed   int
	Verifications       int
	VerificationsFailed int
	VerificationsStale  int
	// VerificationsInconclusive counts checks that ran behind a shell stage
	// that decided the exit status, so neither outcome was readable.
	VerificationsInconclusive int
	Gaps                      int
	GapKinds                  []string
	// ClaimsVerified counts the turn's own asserted verifications;
	// ClaimsUnbacked is how many of them the ledger did not support.
	ClaimsVerified int
	ClaimsUnbacked int
}

// CompletionReportAuditSink is an optional sink capability; implementations
// must keep it content-free, like every other audit channel.
type CompletionReportAuditSink interface {
	RecordCompletionReport(CompletionReportAudit)
}

// RecordCompletionReport forwards the completion summary only to sinks that
// explicitly opt in. Ordinary UI sinks receive nothing.
func RecordCompletionReport(s Sink, a CompletionReportAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if cs, ok := s.(CompletionReportAuditSink); ok {
		cs.RecordCompletionReport(a)
	}
}

// EvidenceBundleAudit is the end-of-turn shadow bundle's seal: its record
// identity in Trusted Host State, never the bundle's content. FailureCode is
// the store's typed cause when nothing was sealed.
type EvidenceBundleAudit struct {
	Sealed      bool
	Record      string
	Generation  uint64
	Integrity   string
	Receipts    int
	FailureCode string
	// SnapshotComplete says the turn-end workspace snapshot saw the whole tree.
	SnapshotComplete bool
	// UnobservedChanges counts paths that changed between the previous sealed
	// turn and this one, when UnobservedCompared says that was established.
	UnobservedCompared bool
	UnobservedChanges  int
	// Outcome is the host-evidence outcome; DivergenceClass and its reason
	// codes say how it differs from the completion report's verdict.
	Outcome           string
	DivergenceClass   string
	DivergenceReasons []string
	// ContractRevision is the accepted contract revision in force, and
	// ContractDecision what host policy did with this turn's derivation.
	ContractRevision int
	ContractDecision string
	ContractFailure  string
}

// EvidenceBundleAuditSink is an optional sink capability; implementations must
// keep it content-free, like every other audit channel.
type EvidenceBundleAuditSink interface {
	RecordEvidenceBundle(EvidenceBundleAudit)
}

// RecordEvidenceBundle forwards the seal only to sinks that opt in.
func RecordEvidenceBundle(s Sink, a EvidenceBundleAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if es, ok := s.(EvidenceBundleAuditSink); ok {
		es.RecordEvidenceBundle(a)
	}
}

// MemoryRecallAudit summarizes one automatic-recall decision: identifiers,
// scores, and budget numbers only — never the query or fact text.
type MemoryRecallAudit struct {
	Hits       []MemoryRecallHit
	UsedChars  int
	Omitted    int
	Suppressed string // reason recall stayed silent; "" when hits were injected
	// Shadow is the Retrieval V2 ranking (telemetry only, never served).
	Shadow []MemoryRecallHit
}

// MemoryRecallHit is one recalled fact's content-free fingerprint.
type MemoryRecallHit struct {
	ID        string
	Revision  int
	Scope     string
	Type      string
	Freshness string
	Score     float64
}

// MemoryRecallSink is an optional sink capability; implementations must keep
// it content-free, like every other audit channel.
type MemoryRecallSink interface {
	RecordMemoryRecall(MemoryRecallAudit)
}

// RecordMemoryRecall forwards a recall decision only to sinks that explicitly
// opt in. Ordinary UI sinks receive nothing.
func RecordMemoryRecall(s Sink, a MemoryRecallAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if mr, ok := s.(MemoryRecallSink); ok {
		mr.RecordMemoryRecall(a)
	}
}

// OutcomeProgressSink is an optional sink capability for the shadow outcome
// scorer's per-round samples: counts only, never paths or commands. Shadow
// means observed, not enforced — the novelty guard still decides behavior.
type OutcomeProgressSink interface {
	RecordOutcomeProgress(hostaudit.OutcomeSample)
}

// RecordOutcomeProgress forwards a shadow outcome sample only to sinks that
// explicitly opt in. Ordinary UI sinks receive nothing.
func RecordOutcomeProgress(s Sink, sample hostaudit.OutcomeSample) {
	if nilutil.IsNil(s) {
		return
	}
	if op, ok := s.(OutcomeProgressSink); ok {
		op.RecordOutcomeProgress(sample)
	}
}

// ProtocolRecoveryAuditSink is an optional sink capability. Implementations
// must keep it content-free; prompts, responses, endpoints, model names, and
// tool arguments do not belong in this audit channel.
type ProtocolRecoveryAuditSink interface {
	RecordProtocolRecovery(ProtocolRecoveryAudit)
}

// RecordProtocolRecovery forwards a content-free recovery observation only to
// sinks that explicitly opt in. Ordinary UI sinks receive nothing.
func RecordProtocolRecovery(s Sink, a ProtocolRecoveryAudit) {
	if nilutil.IsNil(s) {
		return
	}
	if rs, ok := s.(ProtocolRecoveryAuditSink); ok {
		rs.RecordProtocolRecovery(a)
	}
}
