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

// Record is one observed occurrence. Exactly one of Event, ReadinessAudit,
// ProtocolRecovery, or TurnCompletion is set; Seq orders them and TS is the
// unix-millisecond observation time at the recorder.
type Record struct {
	SchemaVersion    int              `json:"schema_version"`
	Seq              uint64           `json:"seq"`
	TS               int64            `json:"ts"`
	Event            *eventwire.Event `json:"event,omitempty"`
	ReadinessAudit   *ReadinessAudit  `json:"readiness_audit,omitempty"`
	ProtocolRecovery string           `json:"protocol_recovery,omitempty"`
	TurnCompletion   bool             `json:"turn_completion,omitempty"`
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

func (r *Recorder) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	r.append(Record{ProtocolRecovery: string(a.Kind)})
	event.RecordProtocolRecovery(r.inner, a)
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
	r.closed = true
	if err := r.buf.Flush(); err != nil && r.err == nil {
		r.err = err
	}
	if err := r.file.Close(); err != nil && r.err == nil {
		r.err = err
	}
	return r.err
}
