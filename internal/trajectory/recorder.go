// Package trajectory appends a run's typed event stream to a JSONL file so a
// run's sequence, timing, and decisions can be replayed and analyzed offline.
// Records reuse the eventwire JSON contract and include content (prompts, tool
// arguments, reasoning) — the file is as sensitive as a session transcript.
package trajectory

import (
	"bufio"
	"encoding/json"
	"os"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/evidence"
)

// SchemaVersion identifies the record layout; bump on breaking changes.
const SchemaVersion = 1

// Record is one observed occurrence. Exactly one payload field is set; Seq
// orders them and TS is the unix-millisecond observation time at the recorder.
type Record struct {
	SchemaVersion       int                  `json:"schema_version"`
	Seq                 uint64               `json:"seq"`
	TS                  int64                `json:"ts"`
	Event               *eventwire.Event     `json:"event,omitempty"`
	ReadinessAudit      *ReadinessAudit      `json:"readiness_audit,omitempty"`
	AnchorSafetyAudit   *AnchorSafetyAudit   `json:"anchor_safety_audit,omitempty"`
	ProtocolRecovery    string               `json:"protocol_recovery,omitempty"`
	TurnCompletion      bool                 `json:"turn_completion,omitempty"`
	ContractShadow      *ContractShadowAudit `json:"contract_shadow,omitempty"`
	CompletionReport    *CompletionReport    `json:"completion_report,omitempty"`
	OutcomeProgress     *OutcomeProgress     `json:"outcome_progress,omitempty"`
	DelegationAdmission *DelegationAdmission `json:"delegation_admission,omitempty"`
	MemoryRecall        *MemoryRecall        `json:"memory_recall,omitempty"`
}

// MemoryRecall mirrors event.MemoryRecallAudit with stable snake_case keys.
type MemoryRecall struct {
	Hits       []MemoryRecallHit `json:"hits,omitempty"`
	UsedChars  int               `json:"used_chars,omitempty"`
	Omitted    int               `json:"omitted,omitempty"`
	Suppressed string            `json:"suppressed,omitempty"`
	ShadowHits []MemoryRecallHit `json:"shadow_hits,omitempty"`
}

type AnchorSafetyAudit struct {
	Mode                  string `json:"mode"`
	TaskMode              string `json:"task_mode"`
	RangeLines            int    `json:"range_lines"`
	ObservationAge        int    `json:"observation_age"`
	LegacyAllowed         bool   `json:"legacy_allowed"`
	ShadowAllowed         bool   `json:"shadow_allowed"`
	Reason                string `json:"reason"`
	SameBatchReadRejected bool   `json:"same_batch_read_rejected,omitempty"`
}

// MemoryRecallHit is one recalled fact's content-free fingerprint.
type MemoryRecallHit struct {
	ID        string  `json:"id"`
	Revision  int     `json:"revision,omitempty"`
	Scope     string  `json:"scope,omitempty"`
	Type      string  `json:"type,omitempty"`
	Freshness string  `json:"freshness,omitempty"`
	Score     float64 `json:"score,omitempty"`
}

// DelegationAdmission mirrors event.DelegationAdmissionAudit with stable keys.
type DelegationAdmission struct {
	Tool    string `json:"tool"`
	Verdict string `json:"verdict"`
	Reason  string `json:"reason,omitempty"`
	Intent  string `json:"intent,omitempty"`
}

// OutcomeProgress mirrors evidence.OutcomeSample with stable snake_case keys.
type OutcomeProgress struct {
	Round            int  `json:"round"`
	Exploration      int  `json:"exploration,omitempty"`
	Verification     int  `json:"verification,omitempty"`
	Objective        int  `json:"objective,omitempty"`
	Regression       int  `json:"regression,omitempty"`
	Churn            int  `json:"churn,omitempty"`
	LegacyGain       int  `json:"legacy_gain,omitempty"`
	Discriminating   int  `json:"discriminating,omitempty"`
	DebtAge          int  `json:"debt_age,omitempty"`
	BlindMutations   int  `json:"blind_mutations,omitempty"`
	EBMEligible      bool `json:"ebm_eligible,omitempty"`
	EBMFired         bool `json:"ebm_fired,omitempty"`
	LocalExecSeen    bool `json:"local_exec_seen,omitempty"`
	GovernorEligible bool `json:"governor_eligible,omitempty"`
	GovernorEngaged  bool `json:"governor_engaged,omitempty"`
	// Runway is a pointer so old records (nil: not observed) stay distinct from
	// a new record whose counterfactual account genuinely reached zero.
	Runway      *int `json:"runway,omitempty"`
	RunwayDry   int  `json:"runway_dry,omitempty"`
	RunwayIdle  int  `json:"runway_idle,omitempty"`
	RunwaySpent bool `json:"runway_spent,omitempty"`
}

// ContractShadowAudit mirrors event.ContractShadowAudit with stable keys.
type ContractShadowAudit struct {
	Intent                string `json:"intent"`
	Requirements          int    `json:"requirements,omitempty"`
	RequirementsSatisfied int    `json:"requirements_satisfied,omitempty"`
	Checks                int    `json:"checks,omitempty"`
	ChecksSatisfied       int    `json:"checks_satisfied,omitempty"`
	Epoch                 uint64 `json:"epoch,omitempty"`
	Verdict               string `json:"verdict"`
	Complete              bool   `json:"complete,omitempty"`
	ReadyToFinalize       bool   `json:"ready_to_finalize,omitempty"`
}

// CompletionReport mirrors event.CompletionReportAudit with stable keys.
type CompletionReport struct {
	Verdict             string   `json:"verdict"`
	Risk                string   `json:"risk,omitempty"`
	Criteria            int      `json:"criteria,omitempty"`
	CriteriaSatisfied   int      `json:"criteria_satisfied,omitempty"`
	Changes             int      `json:"changes,omitempty"`
	ChangesUnreviewed   int      `json:"changes_unreviewed,omitempty"`
	Verifications       int      `json:"verifications,omitempty"`
	VerificationsFailed int      `json:"verifications_failed,omitempty"`
	VerificationsStale  int      `json:"verifications_stale,omitempty"`
	Gaps                int      `json:"gaps,omitempty"`
	GapKinds            []string `json:"gap_kinds,omitempty"`
	ClaimsVerified      int      `json:"claims_verified,omitempty"`
	ClaimsUnbacked      int      `json:"claims_unbacked,omitempty"`
}

// ReadinessAudit mirrors evidence.ReadinessAudit with stable snake_case keys.
type ReadinessAudit struct {
	Result                    string `json:"result"`
	Recovered                 bool   `json:"recovered,omitempty"`
	MissingProjectChecks      int    `json:"missing_project_checks,omitempty"`
	IncompleteTodos           int    `json:"incomplete_todos,omitempty"`
	CommandMismatchMissing    int    `json:"command_mismatch_missing,omitempty"`
	MissingAcceptanceCriteria int    `json:"missing_acceptance_criteria,omitempty"`
	MissingVerification       int    `json:"missing_verification,omitempty"`
	MissingReview             int    `json:"missing_review,omitempty"`
	MissingSignoff            int    `json:"missing_signoff,omitempty"`
	MissingActionEvidence     int    `json:"missing_action_evidence,omitempty"`
	MissingMutation           int    `json:"missing_mutation,omitempty"`
	MissingCapabilities       int    `json:"missing_capabilities,omitempty"`
}

// Recorder is an event.Sink decorator: every event (and optional-capability
// audit) is appended as one JSONL record, then forwarded to the inner sink.
// Recording failures never block forwarding — the first error is kept and
// returned by Close.
type Recorder struct {
	inner event.Sink
	clock func() time.Time

	mu     sync.Mutex
	file   *os.File
	buf    *bufio.Writer
	enc    *json.Encoder
	seq    uint64
	err    error
	closed bool
}

var _ event.OptionalSinkCapabilities = (*Recorder)(nil)

// New opens (or truncates) path and returns a Recorder forwarding to inner.
// A nil clock means time.Now.
func New(inner event.Sink, path string, clock func() time.Time) (*Recorder, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	if clock == nil {
		clock = time.Now
	}
	buf := bufio.NewWriter(f)
	return &Recorder{inner: inner, clock: clock, file: f, buf: buf, enc: json.NewEncoder(buf)}, nil
}

func (r *Recorder) append(rec Record) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.err != nil {
		return
	}
	r.seq++
	rec.SchemaVersion = SchemaVersion
	rec.Seq = r.seq
	rec.TS = r.clock().UnixMilli()
	if err := r.enc.Encode(rec); err != nil {
		r.err = err
		return
	}
	// Flush per record so a killed run still leaves every completed line.
	if err := r.buf.Flush(); err != nil {
		r.err = err
	}
}

func (r *Recorder) Emit(e event.Event) {
	w := eventwire.ToWire(e)
	r.append(Record{Event: &w})
	r.inner.Emit(e)
}

// RecordDelegationAudit forwards without persisting: delegation receipts are
// aggregated by run metrics, and the trajectory schema stays unchanged.
func (r *Recorder) RecordDelegationAudit(a evidence.DelegationAudit) {
	event.RecordDelegationAudit(r.inner, a)
}

func (r *Recorder) RecordReadinessAudit(a evidence.ReadinessAudit) {
	r.append(Record{ReadinessAudit: &ReadinessAudit{
		Result:                    string(a.Result),
		Recovered:                 a.Recovered,
		MissingProjectChecks:      a.MissingProjectChecks,
		IncompleteTodos:           a.IncompleteTodos,
		CommandMismatchMissing:    a.CommandMismatchMissing,
		MissingAcceptanceCriteria: a.MissingAcceptanceCriteria,
		MissingVerification:       a.MissingVerification,
		MissingReview:             a.MissingReview,
		MissingSignoff:            a.MissingSignoff,
		MissingActionEvidence:     a.MissingActionEvidence,
		MissingMutation:           a.MissingMutation,
		MissingCapabilities:       a.MissingCapabilities,
	}})
	event.RecordReadinessAudit(r.inner, a)
}

func (r *Recorder) RecordAnchorSafetyAudit(a event.AnchorSafetyAudit) {
	r.append(Record{AnchorSafetyAudit: &AnchorSafetyAudit{
		Mode: a.Mode, TaskMode: a.TaskMode, RangeLines: a.RangeLines,
		ObservationAge: a.ObservationAge, LegacyAllowed: a.LegacyAllowed,
		ShadowAllowed: a.ShadowAllowed, Reason: a.Reason,
		SameBatchReadRejected: a.SameBatchReadRejected,
	}})
	event.RecordAnchorSafetyAudit(r.inner, a)
}

func (r *Recorder) RecordContractShadow(a event.ContractShadowAudit) {
	r.append(Record{ContractShadow: &ContractShadowAudit{
		Intent:                a.Intent,
		Requirements:          a.Requirements,
		RequirementsSatisfied: a.RequirementsSatisfied,
		Checks:                a.Checks,
		ChecksSatisfied:       a.ChecksSatisfied,
		Epoch:                 a.Epoch,
		Verdict:               a.Verdict,
		Complete:              a.Complete,
		ReadyToFinalize:       a.ReadyToFinalize,
	}})
	event.RecordContractShadow(r.inner, a)
}

func (r *Recorder) RecordCompletionReport(a event.CompletionReportAudit) {
	r.append(Record{CompletionReport: &CompletionReport{
		Verdict:             a.Verdict,
		Risk:                a.Risk,
		Criteria:            a.Criteria,
		CriteriaSatisfied:   a.CriteriaSatisfied,
		Changes:             a.Changes,
		ChangesUnreviewed:   a.ChangesUnreviewed,
		Verifications:       a.Verifications,
		VerificationsFailed: a.VerificationsFailed,
		VerificationsStale:  a.VerificationsStale,
		Gaps:                a.Gaps,
		GapKinds:            a.GapKinds,
		ClaimsVerified:      a.ClaimsVerified,
		ClaimsUnbacked:      a.ClaimsUnbacked,
	}})
	event.RecordCompletionReport(r.inner, a)
}

func (r *Recorder) RecordOutcomeProgress(sample evidence.OutcomeSample) {
	runway := sample.Runway
	r.append(Record{OutcomeProgress: &OutcomeProgress{
		Round:            sample.Round,
		Exploration:      sample.Exploration,
		Verification:     sample.Verification,
		Objective:        sample.Objective,
		Regression:       sample.Regression,
		Churn:            sample.Churn,
		LegacyGain:       sample.LegacyGain,
		Discriminating:   sample.Discriminating,
		DebtAge:          sample.DebtAge,
		BlindMutations:   sample.BlindMutations,
		EBMEligible:      sample.EBMEligible,
		EBMFired:         sample.EBMFired,
		LocalExecSeen:    sample.LocalExecSeen,
		GovernorEligible: sample.GovernorEligible,
		GovernorEngaged:  sample.GovernorEngaged,
		Runway:           &runway,
		RunwayDry:        sample.RunwayDry,
		RunwayIdle:       sample.RunwayIdle,
		RunwaySpent:      sample.RunwaySpent,
	}})
	event.RecordOutcomeProgress(r.inner, sample)
}

func (r *Recorder) RecordMemoryRecall(a event.MemoryRecallAudit) {
	rec := &MemoryRecall{UsedChars: a.UsedChars, Omitted: a.Omitted, Suppressed: a.Suppressed}
	for _, hit := range a.Hits {
		rec.Hits = append(rec.Hits, MemoryRecallHit{
			ID: hit.ID, Revision: hit.Revision, Scope: hit.Scope,
			Type: hit.Type, Freshness: hit.Freshness, Score: hit.Score,
		})
	}
	for _, hit := range a.Shadow {
		rec.ShadowHits = append(rec.ShadowHits, MemoryRecallHit{ID: hit.ID, Score: hit.Score})
	}
	r.append(Record{MemoryRecall: rec})
	event.RecordMemoryRecall(r.inner, a)
}

func (r *Recorder) RecordDelegationAdmission(a event.DelegationAdmissionAudit) {
	r.append(Record{DelegationAdmission: &DelegationAdmission{
		Tool: a.Tool, Verdict: a.Verdict, Reason: a.Reason, Intent: a.Intent,
	}})
	event.RecordDelegationAdmission(r.inner, a)
}

func (r *Recorder) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	r.append(Record{ProtocolRecovery: string(a.Kind)})
	event.RecordProtocolRecovery(r.inner, a)
}

func (r *Recorder) RecordTurnCompletion() {
	r.append(Record{TurnCompletion: true})
	event.RecordTurnCompletion(r.inner)
}

func (r *Recorder) RecordWorkspaceMutation(m event.WorkspaceMutation) {
	event.RecordWorkspaceMutation(r.inner, m)
}

func (r *Recorder) RecordRunBudget(sample event.RunBudgetSample) {
	event.RecordRunBudget(r.inner, sample)
}

// Close flushes and closes the file, returning the first error seen. Events
// arriving after Close (late background jobs) are forwarded but not recorded.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.err
	}
	r.closed = true
	if err := r.buf.Flush(); err != nil && r.err == nil {
		r.err = err
	}
	if err := r.file.Close(); err != nil && r.err == nil {
		r.err = err
	}
	return r.err
}
