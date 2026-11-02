package trajectory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
)

type capabilitySink struct {
	events []event.Event
	turns  int
}

func (s *capabilitySink) Emit(e event.Event)    { s.events = append(s.events, e) }
func (s *capabilitySink) RecordTurnCompletion() { s.turns++ }
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

	r.RecordTurnCompletion()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	recs := readRecords(t, path)
	if len(recs) != 1 || !recs[0].TurnCompletion {
		t.Fatalf("turn completion records = %+v", recs)
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

func TestMemoryRecallRecordsExplicitZeroBudgetCounts(t *testing.T) {
	path := filepath.Join(testenv.TempDir(t), "recall.jsonl")
	recorder, err := New(event.Discard, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorder.Close() })
	recorder.RecordMemoryRecall(event.MemoryRecallAudit{
		Suppressed:      "empty-store: memory store is empty",
		SuppressionCode: "empty-store",
	})
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record struct {
		MemoryRecall map[string]json.RawMessage `json:"memory_recall"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"char_budget", "omitted_by_limit", "omitted_by_budget"} {
		if raw := record.MemoryRecall[key]; string(raw) != "0" {
			t.Errorf("%s = %s, want explicit zero", key, raw)
		}
	}
	if raw := record.MemoryRecall["suppression_code"]; string(raw) != `"empty-store"` {
		t.Errorf("suppression_code = %s, want the typed code under its own key", raw)
	}
	recalled := readRecords(t, path)
	if len(recalled) != 1 || recalled[0].MemoryRecall.Suppressed != "empty-store: memory store is empty" ||
		recalled[0].MemoryRecall.SuppressionCode != "empty-store" {
		t.Fatal("suppressed recall lost its explanation or typed identity")
	}
}

func TestLegacyMemoryRecallRecordPreservesOmittedTotal(t *testing.T) {
	var record Record
	if err := json.Unmarshal([]byte(`{"schema_version":1,"memory_recall":{"used_chars":1327,"omitted":3}}`), &record); err != nil {
		t.Fatal(err)
	}
	recall := record.MemoryRecall
	if record.SchemaVersion != SchemaVersion || recall == nil || recall.UsedChars != 1327 || recall.Omitted != 3 {
		t.Fatalf("legacy recall was not preserved: %+v", record)
	}
	if recall.CharBudget != 0 || recall.OmittedByLimit != 0 || recall.OmittedByBudget != 0 {
		t.Fatalf("legacy recall invented an omission cause: %+v", recall)
	}
}
