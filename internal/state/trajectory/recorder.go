// Package trajectory appends a run's typed event stream to a JSONL file so a
// run's sequence, timing, and decisions can be replayed and analyzed offline.
// Records reuse the eventwire JSON contract and include content (prompts, tool
// arguments, reasoning) — the file is as sensitive as a session transcript.
package trajectory

import (
	"bufio"
	"encoding/json"
	"os"
	"reasonix/internal/contract/hostaudit"
	"reflect"
	"sync"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
)

// SchemaVersion identifies the record layout; bump on breaking changes.
const SchemaVersion = 1

// Record is one observed occurrence. Exactly one payload field is set; Seq
// orders them and TS is the unix-millisecond observation time at the recorder.
type Record struct {
	SchemaVersion    int                  `json:"schema_version"`
	Seq              uint64               `json:"seq"`
	TS               int64                `json:"ts"`
	RunHeader        *RunHeader           `json:"run_header,omitempty"`
	Event            *eventwire.Event     `json:"event,omitempty"`
	ProtocolRecovery string               `json:"protocol_recovery,omitempty"`
	RecoveryChild    string               `json:"protocol_recovery_child,omitempty"`
	TurnCompletion   bool                 `json:"turn_completion,omitempty"`
	ContractShadow   *ContractShadowAudit `json:"contract_shadow,omitempty"`
	EvidenceBundle   *EvidenceBundle      `json:"evidence_bundle,omitempty"`
	OutcomeProgress  *OutcomeProgress     `json:"outcome_progress,omitempty"`
	MemoryRecall     *MemoryRecall        `json:"memory_recall,omitempty"`
	ProjectCheck     *ProjectCheckProbe   `json:"project_check_probe,omitempty"`
	// Deltas counts the streamed increments merged into this record; absent
	// means one. TS stays the first increment's — that is the observation
	// time-to-first-token readers key on — and EndTS carries the last's.
	Deltas int   `json:"deltas,omitempty"`
	EndTS  int64 `json:"end_ts,omitempty"`
}

// RunHeader is the request-side prefix the event stream alone cannot carry:
// the system prompt and tool schemas the run's rounds were sampled against.
// The hashes are the ones usage events already report, so a reader can prove
// a header belongs to the rounds that follow instead of assuming it.
type RunHeader struct {
	ModelRef string `json:"model_ref,omitempty"`
	// WorkspaceRoot is the run's workspace path. Tool output quotes it
	// verbatim, so a reader that wants portable text needs the exact string to
	// replace rather than a guess at what a temporary path looks like.
	WorkspaceRoot string          `json:"workspace_root,omitempty"`
	System        string          `json:"system"`
	SystemHash    string          `json:"system_hash"`
	Tools         json.RawMessage `json:"tools,omitempty"`
	ToolsHash     string          `json:"tools_hash"`
	PrefixHash    string          `json:"prefix_hash"`
	// Capabilities names every capability version the run had available, and
	// CapabilitiesHash the set, so two runs compare by one field and differ
	// by the entries that changed.
	Capabilities     []CapabilityVersion `json:"capabilities,omitempty"`
	CapabilitiesHash string              `json:"capabilities_hash,omitempty"`
}

// CapabilityVersion is one capability by kind, name and content digest.
type CapabilityVersion struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// MemoryRecall mirrors event.MemoryRecallAudit with stable snake_case keys.
type MemoryRecall struct {
	Hits            []MemoryRecallHit `json:"hits,omitempty"`
	CharBudget      int               `json:"char_budget"`
	UsedChars       int               `json:"used_chars,omitempty"`
	Omitted         int               `json:"omitted,omitempty"`
	OmittedByLimit  int               `json:"omitted_by_limit"`
	OmittedByBudget int               `json:"omitted_by_budget"`
	Suppressed      string            `json:"suppressed,omitempty"`
	SuppressionCode string            `json:"suppression_code,omitempty"`
	ShadowHits      []MemoryRecallHit `json:"shadow_hits,omitempty"`
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

// OutcomeProgress mirrors hostaudit.OutcomeSample with stable snake_case keys.
type OutcomeProgress struct {
	Round          int `json:"round"`
	Exploration    int `json:"exploration,omitempty"`
	Verification   int `json:"verification,omitempty"`
	Objective      int `json:"objective,omitempty"`
	Regression     int `json:"regression,omitempty"`
	Churn          int `json:"churn,omitempty"`
	LegacyGain     int `json:"legacy_gain,omitempty"`
	Discriminating int `json:"discriminating,omitempty"`
	DebtAge        int `json:"debt_age,omitempty"`
	BlindMutations int `json:"blind_mutations,omitempty"`
	Stall          int `json:"stall,omitempty"`
	StallAge       int `json:"stall_age,omitempty"`
	StallMutations int `json:"stall_mutations,omitempty"`
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

// ProjectCheckProbe mirrors event.ProjectCheckProbe: the shadow comparison
// between the readiness gate's project-check derivation and the ledger's
// obligations. Identities are recorded, not counted — a class is only
// judgeable against the criterion it was about.
type ProjectCheckProbe struct {
	Declared         int                `json:"declared,omitempty"`
	Baseline         int                `json:"baseline,omitempty"`
	LegacyBlocked    bool               `json:"legacy_blocked,omitempty"`
	CandidateBlocked bool               `json:"candidate_blocked,omitempty"`
	AgreedMissing    int                `json:"agreed_missing,omitempty"`
	Diffs            []ProjectCheckDiff `json:"diffs,omitempty"`
	LegacyAfter      int                `json:"legacy_after"`
	CandidateAfter   int                `json:"candidate_after"`
}

// ProjectCheckDiff is one criterion the two derivations disagreed about.
type ProjectCheckDiff struct {
	Identity string `json:"identity"`
	Class    string `json:"class"`
}

// EvidenceBundle mirrors event.EvidenceBundleAudit with stable keys.
type EvidenceBundle struct {
	Sealed      bool   `json:"sealed"`
	Record      string `json:"record,omitempty"`
	Generation  uint64 `json:"generation,omitempty"`
	Integrity   string `json:"integrity,omitempty"`
	Receipts    int    `json:"receipts,omitempty"`
	FailureCode string `json:"failure_code,omitempty"`

	SnapshotComplete   bool `json:"snapshot_complete,omitempty"`
	UnobservedCompared bool `json:"unobserved_compared,omitempty"`
	UnobservedChanges  int  `json:"unobserved_changes,omitempty"`

	Outcome           string   `json:"outcome,omitempty"`
	DivergenceClass   string   `json:"divergence_class,omitempty"`
	DivergenceReasons []string `json:"divergence_reasons,omitempty"`

	ContractRevision int    `json:"contract_revision,omitempty"`
	ContractDecision string `json:"contract_decision,omitempty"`
	ContractFailure  string `json:"contract_failure,omitempty"`
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

	// The run in flight, held only while consecutive same-kind deltas arrive.
	pend    *eventwire.Event
	pendTS  int64
	pendEnd int64
	pendN   int
}

// coalesceCap bounds what one merged record can hold. A kill loses the run in
// flight, so the cap is what keeps that loss bounded; every ordinary turn ends
// on a usage or message event, which flushes long before this is reached.
const coalesceCap = 256

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
	// Anything that is not a delta ends the run in flight, so the file keeps
	// the order the events arrived in.
	r.flushPendingLocked()
	r.appendLocked(rec)
}

func (r *Recorder) appendLocked(rec Record) {
	if r.closed || r.err != nil {
		return
	}
	r.seq++
	rec.SchemaVersion = SchemaVersion
	rec.Seq = r.seq
	if rec.TS == 0 {
		rec.TS = r.clock().UnixMilli()
	}
	if err := r.enc.Encode(rec); err != nil {
		r.err = err
		return
	}
	// Flush per record so a killed run still leaves every completed line.
	if err := r.buf.Flush(); err != nil {
		r.err = err
	}
}

// plainDelta reports whether w is a streamed increment carrying nothing but
// its text. Citations, a tool, an error — anything else set means it is not
// one, so a field added to the wire later stops merging instead of being
// silently dropped into a neighbour's record.
func plainDelta(w *eventwire.Event) bool {
	if w.Kind != "reasoning" && w.Kind != "text" {
		return false
	}
	if w.Text == "" {
		return false
	}
	bare := *w
	bare.Kind, bare.Text = "", ""
	return reflect.DeepEqual(bare, eventwire.Event{})
}

// A run emits these by the hundred — 832 reasoning increments carrying 6.5k
// characters in one observed turn — and one record each is what made a
// two-minute benchmark cost a 484KB trajectory whose content was 19KB. Merging
// adjacent same-kind deltas keeps every character and every reader: the only
// timestamp anything consumes is the first one, which TS still carries.
func (r *Recorder) appendDelta(w eventwire.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.err != nil {
		return
	}
	now := r.clock().UnixMilli()
	if r.pend != nil && r.pend.Kind == w.Kind && r.pendN < coalesceCap {
		r.pend.Text += w.Text
		r.pendEnd = now
		r.pendN++
		return
	}
	r.flushPendingLocked()
	held := w
	r.pend, r.pendTS, r.pendEnd, r.pendN = &held, now, now, 1
}

func (r *Recorder) flushPendingLocked() {
	if r.pend == nil {
		return
	}
	rec := Record{Event: r.pend, TS: r.pendTS}
	if r.pendN > 1 {
		rec.Deltas, rec.EndTS = r.pendN, r.pendEnd
	}
	r.pend, r.pendTS, r.pendEnd, r.pendN = nil, 0, 0, 0
	r.appendLocked(rec)
}

func (r *Recorder) Emit(e event.Event) {
	w := eventwire.ToWire(e)
	if w.NonPersistable {
		r.inner.Emit(e)
		return
	}
	if plainDelta(&w) {
		r.appendDelta(w)
	} else {
		r.append(Record{Event: &w})
	}
	r.inner.Emit(e)
}

// RecordDelegationAudit forwards without persisting: delegation receipts are
// aggregated by run metrics, and the trajectory schema stays unchanged.
func (r *Recorder) RecordDelegationAudit(a hostaudit.DelegationAudit) {
	event.RecordDelegationAudit(r.inner, a)
}

func (r *Recorder) RecordMemoryRecall(a event.MemoryRecallAudit) {
	rec := &MemoryRecall{
		CharBudget: a.CharBudget, UsedChars: a.UsedChars, Omitted: a.Omitted,
		OmittedByLimit: a.OmittedByLimit, OmittedByBudget: a.OmittedByBudget,
		Suppressed:      a.Suppressed,
		SuppressionCode: a.SuppressionCode,
	}
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

// RecordRunHeader persists the run's request-side prefix. It is written by the
// frontend that owns the recorder, not forwarded through the sink chain, so it
// adds no optional-capability contract for every wrapper to carry.
func (r *Recorder) RecordRunHeader(h RunHeader) {
	r.append(Record{RunHeader: &h})
}

func (r *Recorder) RecordTurnCompletion() {
	r.append(Record{TurnCompletion: true})
	event.RecordTurnCompletion(r.inner)
}

// Close flushes and closes the file, returning the first error seen. Events
// arriving after Close (late background jobs) are forwarded but not recorded.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.err
	}
	r.flushPendingLocked()
	r.closed = true
	if err := r.buf.Flush(); err != nil && r.err == nil {
		r.err = err
	}
	if err := r.file.Close(); err != nil && r.err == nil {
		r.err = err
	}
	return r.err
}
