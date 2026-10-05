package trajectory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reasonix/internal/contract/hostaudit"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
)

type capabilitySink struct {
	events     []event.Event
	readiness  []hostaudit.ReadinessAudit
	recoveries []event.ProtocolRecoveryAudit
	outcomes   []hostaudit.OutcomeSample
	reports    []event.CompletionReportAudit
	turns      int
}

func (s *capabilitySink) Emit(e event.Event) { s.events = append(s.events, e) }
func (s *capabilitySink) RecordReadinessAudit(a hostaudit.ReadinessAudit) {
	s.readiness = append(s.readiness, a)
}
func (s *capabilitySink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	s.recoveries = append(s.recoveries, a)
}
func (s *capabilitySink) RecordTurnCompletion() { s.turns++ }
func (s *capabilitySink) RecordOutcomeProgress(sample hostaudit.OutcomeSample) {
	s.outcomes = append(s.outcomes, sample)
}
func (s *capabilitySink) RecordCompletionReport(a event.CompletionReportAudit) {
	s.reports = append(s.reports, a)
}

func readRecords(t *testing.T, path string) []Record {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trajectory: %v", err)
	}
	var out []Record
	// An empty file means nothing arrived, which is a real outcome to report as
	// itself rather than as a malformed record.
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	for line := range strings.SplitSeq(trimmed, "\n") {
		var r Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("bad record %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

func TestRecorderAppendsOrderedTimestampedRecords(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "run.trajectory.jsonl")
	inner := &capabilitySink{}
	now := time.UnixMilli(1754500000000)
	r, err := New(inner, path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.Emit(event.Event{Kind: event.ToolDispatch, Tool: event.Tool{ID: "c1", Name: "bash", Args: `{"command":"ls"}`}})
	now = now.Add(120 * time.Millisecond)
	r.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{
		ID: "c1", Name: "bash", Output: "ok", DurationMs: 100,
		StartedAt: 1754500000010, EndedAt: 1754500000110,
	}})
	r.Emit(event.Event{Kind: event.Reasoning, Text: "thinking about the next step"})
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	recs := readRecords(t, path)
	if len(recs) != 3 {
		t.Fatalf("got %d records, want 3", len(recs))
	}
	for i, rec := range recs {
		if rec.Seq != uint64(i+1) {
			t.Errorf("record %d seq = %d, want %d", i, rec.Seq, i+1)
		}
		if rec.SchemaVersion != SchemaVersion {
			t.Errorf("record %d schema = %d, want %d", i, rec.SchemaVersion, SchemaVersion)
		}
		if rec.Event == nil {
			t.Fatalf("record %d has no event payload", i)
		}
	}
	if recs[0].TS != 1754500000000 || recs[1].TS != 1754500000120 {
		t.Errorf("timestamps = %d, %d, want recorder-clock values", recs[0].TS, recs[1].TS)
	}
	if recs[1].Event.Tool == nil || recs[1].Event.Tool.StartedAt != 1754500000010 || recs[1].Event.Tool.EndedAt != 1754500000110 {
		t.Errorf("tool result record lost execution bounds: %+v", recs[1].Event.Tool)
	}
	if recs[2].Event.Kind != "reasoning" || recs[2].Event.Text != "thinking about the next step" {
		t.Errorf("reasoning record = %+v", recs[2].Event)
	}
	if len(inner.events) != 3 {
		t.Errorf("inner sink saw %d events, want 3", len(inner.events))
	}
}

func TestRecorderRecordsAndForwardsOptionalCapabilities(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "run.trajectory.jsonl")
	inner := &capabilitySink{}
	r, err := New(inner, path, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	r.RecordReadinessAudit(hostaudit.ReadinessAudit{Result: hostaudit.ReadinessBlocked, MissingVerification: 2})
	r.RecordProtocolRecovery(event.ProtocolRecoveryAudit{Kind: event.ProtocolRecoveryMissingReasoningDetected})
	r.RecordTurnCompletion()
	r.RecordOutcomeProgress(hostaudit.OutcomeSample{Round: 3, Exploration: 2, Objective: 1, LegacyGain: 4})
	r.RecordCompletionReport(event.CompletionReportAudit{
		Verdict: "partial", Changes: 2, ChangesUnreviewed: 1, Gaps: 1, GapKinds: []string{"unreviewed_change"},
	})
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	recs := readRecords(t, path)
	if len(recs) != 5 {
		t.Fatalf("got %d records, want 5", len(recs))
	}
	if recs[0].ReadinessAudit == nil || recs[0].ReadinessAudit.Result != "blocked" || recs[0].ReadinessAudit.MissingVerification != 2 {
		t.Errorf("readiness record = %+v", recs[0].ReadinessAudit)
	}
	if recs[1].ProtocolRecovery != string(event.ProtocolRecoveryMissingReasoningDetected) {
		t.Errorf("protocol recovery record = %q", recs[1].ProtocolRecovery)
	}
	if !recs[2].TurnCompletion {
		t.Errorf("turn completion record = %+v", recs[2])
	}
	if recs[3].OutcomeProgress == nil || recs[3].OutcomeProgress.Round != 3 || recs[3].OutcomeProgress.Objective != 1 || recs[3].OutcomeProgress.LegacyGain != 4 {
		t.Errorf("outcome progress record = %+v", recs[3].OutcomeProgress)
	}
	if rec := recs[4].CompletionReport; rec == nil || rec.Verdict != "partial" || rec.ChangesUnreviewed != 1 || len(rec.GapKinds) != 1 {
		t.Errorf("completion report record = %+v", recs[4].CompletionReport)
	}
	if len(inner.readiness) != 1 || len(inner.recoveries) != 1 || inner.turns != 1 || len(inner.outcomes) != 1 || len(inner.reports) != 1 {
		t.Errorf("inner capabilities = %d/%d/%d/%d/%d, want 1/1/1/1/1", len(inner.readiness), len(inner.recoveries), inner.turns, len(inner.outcomes), len(inner.reports))
	}
}

func TestRecorderForwardsAfterCloseWithoutRecording(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "run.trajectory.jsonl")
	inner := &capabilitySink{}
	r, err := New(inner, path, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	r.Emit(event.Event{Kind: event.Text, Text: "before"})
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	r.Emit(event.Event{Kind: event.Text, Text: "after"})

	if len(readRecords(t, path)) != 1 {
		t.Fatalf("post-close event must not be recorded")
	}
	if len(inner.events) != 2 {
		t.Fatalf("inner sink saw %d events, want 2 (forwarding survives Close)", len(inner.events))
	}
}

func TestNewFailsOnUnwritablePath(t *testing.T) {
	if _, err := New(event.Discard, filepath.Join(testenv.TempDir(t), "missing", "run.jsonl"), nil); err == nil {
		t.Fatal("New must fail when the parent directory does not exist")
	}
}

func TestRecordRunHeaderPersistsRequestSidePrefix(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "t.jsonl")
	rec, err := New(&capabilitySink{}, path, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec.RecordRunHeader(RunHeader{
		ModelRef: "deepseek/v4", System: "you are a coding agent",
		SystemHash: "aaaa", Tools: json.RawMessage(`[{"name":"bash"}]`),
		ToolsHash: "bbbb", PrefixHash: "cccc",
	})
	rec.Emit(event.Event{Kind: event.TurnStarted})
	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got := readRecords(t, path)
	if len(got) != 2 {
		t.Fatalf("records = %d, want 2", len(got))
	}
	// The header must precede the rounds it describes; a reader keys on that.
	h := got[0].RunHeader
	if h == nil {
		t.Fatalf("first record carries no run header: %+v", got[0])
	}
	if h.ModelRef != "deepseek/v4" || h.SystemHash != "aaaa" || h.ToolsHash != "bbbb" || h.PrefixHash != "cccc" {
		t.Errorf("header round-tripped wrong: %+v", h)
	}
	if string(h.Tools) != `[{"name":"bash"}]` {
		t.Errorf("tools = %s", h.Tools)
	}
	if got[1].RunHeader != nil || got[1].Event == nil {
		t.Errorf("second record should be the event, got %+v", got[1])
	}
}
