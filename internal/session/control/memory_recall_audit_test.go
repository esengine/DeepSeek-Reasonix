package control

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/memory"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/trajectory"
)

type recallAuditSink struct {
	event.Sink
	audits []event.MemoryRecallAudit
}

func (s *recallAuditSink) RecordMemoryRecall(a event.MemoryRecallAudit) {
	s.audits = append(s.audits, a)
}

func TestComposeEmitsMemoryRecallAudit(t *testing.T) {
	ag := agent.New(nil, tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	sink := &recallAuditSink{Sink: event.Discard}
	c := New(Options{Runner: ag, Executor: ag, Sink: sink})
	_ = c.Compose("what is the canonical fast check command")
	if len(sink.audits) != 1 {
		t.Fatalf("audits = %d, want exactly one recall audit per composed user turn", len(sink.audits))
	}
	if sink.audits[0].SuppressionCode == "" && len(sink.audits[0].Hits) == 0 {
		t.Fatalf("audit must explain itself: %+v", sink.audits[0])
	}
}

func TestMemoryRecallTrajectoryDistinguishesOmissionReasons(t *testing.T) {
	store := memory.Store{Dir: testenv.TempDir(t)}
	for i := range 8 {
		if _, err := store.Save(memory.Memory{
			Name: fmt.Sprintf("audit-%d", i), Title: fmt.Sprintf("Audit fact %d", i),
			Description: "matched audit fact", Type: memory.TypeReference,
			Body: fmt.Sprintf("private-audit-body-%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	const query = "matched audit fact"
	now := time.Now()
	full := memory.AutoRecall(store, query, memory.RecallOptions{Now: now, Limit: 5, MaxChars: 20000})
	budget := full.UsedChars
	byLimit := memory.AutoRecall(store, query, memory.RecallOptions{Now: now, Limit: 5, MaxChars: budget})
	byBudget := memory.AutoRecall(store, query, memory.RecallOptions{Now: now, Limit: 8, MaxChars: budget})
	if byLimit.OmittedByLimit != 3 || byLimit.OmittedByBudget != 0 ||
		byBudget.OmittedByLimit != 0 || byBudget.OmittedByBudget != 3 {
		t.Fatalf("fixture did not split the omission causes: limit=%+v, budget=%+v", byLimit, byBudget)
	}
	if byLimit.Block() != byBudget.Block() {
		t.Fatal("fixture must produce identical provider-visible recall blocks")
	}
	path := filepath.Join(testenv.TempDir(t), "recall.jsonl")
	inner := &recallAuditSink{Sink: event.Discard}
	recorder, err := trajectory.New(inner, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recorder.Close() })
	results := []memory.RecallResult{byLimit, byBudget}
	for _, result := range results {
		event.RecordMemoryRecall(recorder, memoryRecallAudit(result))
	}
	if err := recorder.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{query, "private-audit-body"} {
		if bytes.Contains(data, []byte(private)) {
			t.Errorf("recall audit archived private content %q", private)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var payloads []json.RawMessage
	for i, result := range results {
		var record struct {
			MemoryRecall json.RawMessage `json:"memory_recall"`
		}
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, record.MemoryRecall)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(record.MemoryRecall, &fields); err != nil {
			t.Fatal(err)
		}
		counts := map[string]int{
			"char_budget": result.CharBudget, "used_chars": result.UsedChars,
			"omitted":          result.OmittedByLimit + result.OmittedByBudget,
			"omitted_by_limit": result.OmittedByLimit, "omitted_by_budget": result.OmittedByBudget,
		}
		for key, want := range counts {
			raw, ok := fields[key]
			if !ok {
				t.Errorf("record %d: missing %s", i, key)
				continue
			}
			var got int
			if err := json.Unmarshal(raw, &got); err != nil || got != want {
				t.Errorf("record %d: %s = %s, want %d (decode error: %v)", i, key, raw, want, err)
			}
		}
	}
	if bytes.Equal(payloads[0], payloads[1]) {
		t.Error("different omission causes produced identical archived recall records")
	}
	if len(inner.audits) != len(results) {
		t.Fatalf("forwarded %d audits, want %d", len(inner.audits), len(results))
	}
	for i, result := range results {
		got := inner.audits[i]
		if got.CharBudget != result.CharBudget || got.OmittedByLimit != result.OmittedByLimit ||
			got.OmittedByBudget != result.OmittedByBudget || got.Omitted != result.OmittedByLimit+result.OmittedByBudget {
			t.Errorf("forwarded audit %d lost its budget decision: %+v", i, got)
		}
	}
}
