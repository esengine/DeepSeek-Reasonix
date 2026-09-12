package transcript

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/turnevent"
)

func TestProjectionAttachesCompletedTurnUsageAndTimingToFinalAnswer(t *testing.T) {
	p, err := NewProjection(testIdentity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(sequence uint64, kind string, createdAt int64, wire eventwire.Event, status event.TurnStatus) {
		t.Helper()
		wire.Kind = kind
		if err := p.Apply(turnevent.Envelope{SessionID: "session", RuntimeEpoch: "runtime", TurnID: "turn", Sequence: sequence, Kind: kind, Status: status, CreatedAt: createdAt, Event: wire}); err != nil {
			t.Fatal(err)
		}
	}
	apply(1, "turn_started", 1_000, eventwire.Event{}, event.TurnInProgress)
	apply(2, "message", 1_500, eventwire.Event{MessageID: "answer", Text: "done"}, event.TurnInProgress)
	apply(3, "usage", 2_000, eventwire.Event{Usage: &eventwire.Usage{
		PromptTokens: 100_000, CompletionTokens: 2_000, TotalTokens: 102_000,
		CacheHitTokens: 80_000, CacheMissTokens: 20_000, ReasoningTokens: 1_000,
		CostQuote: &billing.CostQuote{ModelRef: "deepseek-official/deepseek-flash"},
	}}, event.TurnInProgress)
	apply(4, "usage", 3_000, eventwire.Event{Usage: &eventwire.Usage{
		PromptTokens: 80_000, CompletionTokens: 3_225, TotalTokens: 83_225,
		CacheHitTokens: 75_520, CacheMissTokens: 4_480, ReasoningTokens: 909,
		CostQuote: &billing.CostQuote{ModelRef: "deepseek-official/deepseek-flash"},
	}}, event.TurnInProgress)
	apply(5, "turn_done", 4_000, eventwire.Event{}, event.TurnCompleted)

	records := snapshot(t, p).Records
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	message := records[0].Message
	if message.CreatedAt != 1_500 || message.TurnDurationMs != 3_000 {
		t.Fatalf("message timing = created %d duration %d", message.CreatedAt, message.TurnDurationMs)
	}
	usage := message.TurnUsage
	if usage == nil || usage.TotalTokens != 185_225 || usage.UncachedInputTokens != 24_480 || usage.OutputTokens != 5_225 {
		t.Fatalf("turn usage = %+v", usage)
	}
	if usage.CacheReadTokens == nil || *usage.CacheReadTokens != 155_520 || usage.ReasoningTokens == nil || *usage.ReasoningTokens != 1_909 {
		t.Fatalf("turn usage optional buckets = %+v", usage)
	}
	if !reflect.DeepEqual(usage.Routes, []string{"deepseek-official/deepseek-flash"}) {
		t.Fatalf("routes = %v", usage.Routes)
	}
}

func TestProjectionQueuedStatusDoesNotReusePreviousTurnStart(t *testing.T) {
	p, err := NewProjection(testIdentity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	apply := func(sequence uint64, turnID, kind string, createdAt int64, wire eventwire.Event, status event.TurnStatus) {
		t.Helper()
		wire.Kind = kind
		if err := p.Apply(turnevent.Envelope{SessionID: "session", RuntimeEpoch: "runtime", TurnID: turnID, Sequence: sequence, Kind: kind, Status: status, CreatedAt: createdAt, Event: wire}); err != nil {
			t.Fatal(err)
		}
	}
	apply(1, "first", "turn_started", 1_000, eventwire.Event{}, event.TurnInProgress)
	apply(2, "first", "message", 1_500, eventwire.Event{MessageID: "first-answer", Text: "first"}, event.TurnInProgress)
	apply(3, "first", "turn_done", 2_000, eventwire.Event{}, event.TurnCompleted)
	apply(4, "second", "turn_status", 100_000, eventwire.Event{}, event.TurnQueued)
	apply(5, "second", "turn_started", 100_000, eventwire.Event{}, event.TurnInProgress)
	apply(6, "second", "message", 101_000, eventwire.Event{MessageID: "second-answer", Text: "second"}, event.TurnInProgress)
	apply(7, "second", "turn_done", 102_500, eventwire.Event{}, event.TurnCompleted)

	records := snapshot(t, p).Records
	if got := records[len(records)-1].Message.TurnDurationMs; got != 2_500 {
		t.Fatalf("second turn duration = %d, want 2500", got)
	}
}

var testIdentity = Identity{SessionID: "session", HeadID: "head", RuntimeEpoch: "runtime", RewriteEpoch: 1}

func projectEvent(t *testing.T, p *Projection, seq uint64, e event.Event) {
	t.Helper()
	w := eventwire.ToWire(e)
	status := event.TurnInProgress
	if e.Kind == event.TurnDone {
		status = event.TurnCompleted
	}
	if err := p.Apply(turnevent.Envelope{SessionID: "session", RuntimeEpoch: "runtime", TurnID: "turn", Sequence: seq, Kind: w.Kind, Status: status, Event: w}); err != nil {
		t.Fatal(err)
	}
}

func snapshot(t *testing.T, p *Projection) Snapshot {
	t.Helper()
	s, err := p.Snapshot(PageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestProjectionSnapshotCoverageMatchesEveryStreamCut(t *testing.T) {
	p, err := NewProjection(testIdentity, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := []event.Event{
		{Kind: event.UserMessage, MessageID: "user", Text: "question"},
		{Kind: event.StreamAttempt, MessageID: "failed", AttemptID: "failed", StreamAttempt: event.StreamAttemptInfo{ID: "failed", Action: event.StreamAttemptBegin}},
		{Kind: event.Text, MessageID: "failed", AttemptID: "failed", Text: "rejected"},
		{Kind: event.Message, MessageID: "failed", AttemptID: "failed", Text: "rejected full response"},
		{Kind: event.StreamAttempt, MessageID: "failed", AttemptID: "failed", StreamAttempt: event.StreamAttemptInfo{ID: "failed", Action: event.StreamAttemptDiscard}},
		{Kind: event.StreamAttempt, MessageID: "answer", AttemptID: "answer", StreamAttempt: event.StreamAttemptInfo{ID: "answer", Action: event.StreamAttemptBegin}},
		{Kind: event.Reasoning, MessageID: "answer", AttemptID: "answer", Text: "think"},
		{Kind: event.Text, MessageID: "answer", AttemptID: "answer", Text: "same"},
		{Kind: event.Text, MessageID: "answer", AttemptID: "answer", Text: " answer"},
		{Kind: event.Message, MessageID: "answer", AttemptID: "answer", Text: "same answer", Reasoning: "think"},
		{Kind: event.StreamAttempt, MessageID: "answer", AttemptID: "answer", StreamAttempt: event.StreamAttemptInfo{ID: "answer", Action: event.StreamAttemptCommit}},
		{Kind: event.ToolDispatch, MessageID: "answer", Tool: event.Tool{ID: "call", Name: "read_file", Args: `{}`}},
		{Kind: event.ToolResult, MessageID: "answer", Tool: event.Tool{ID: "call", Name: "read_file", Output: "result"}},
		{Kind: event.Message, MessageID: "second", Text: "same answer"},
		{Kind: event.TurnDone},
	}
	var cuts []Snapshot
	cuts = append(cuts, snapshot(t, p))
	for i, e := range events {
		projectEvent(t, p, uint64(i+1), e)
		cut := snapshot(t, p)
		if cut.CoveredThroughSeq != uint64(i+1) {
			t.Fatalf("cut %d has coverage %d", i, cut.CoveredThroughSeq)
		}
		cuts = append(cuts, cut)
	}
	want := cuts[len(cuts)-1].Records
	if len(want) != 4 || want[1].Message.Content != "same answer" || want[3].Message.Content != "same answer" {
		t.Fatalf("final projection: %+v", want)
	}
	for cut, state := range cuts {
		var baseline []Message
		for _, record := range state.Records {
			baseline = append(baseline, record.Message)
		}
		restored, err := NewProjection(testIdentity, baseline, state.CoveredThroughSeq)
		if err != nil {
			t.Fatalf("cut %d: %v", cut, err)
		}
		for i := cut; i < len(events); i++ {
			projectEvent(t, restored, uint64(i+1), events[i])
		}
		if got := snapshot(t, restored).Records; !reflect.DeepEqual(got, want) {
			t.Fatalf("cut %d changed the final projection\ngot: %#v\nwant: %#v", cut, got, want)
		}
	}
}

func TestProjectionRejectsWrongIdentityAndGapWithoutChangingSnapshot(t *testing.T) {
	p, _ := NewProjection(testIdentity, nil, 0)
	before := snapshot(t, p)
	for _, envelope := range []turnevent.Envelope{
		{SessionID: "another", RuntimeEpoch: "runtime", Sequence: 1},
		{SessionID: "session", RuntimeEpoch: "old-runtime", Sequence: 1},
		{SessionID: "session", RuntimeEpoch: "runtime", Sequence: 2},
	} {
		if err := p.Apply(envelope); err == nil {
			t.Fatal("invalid envelope accepted")
		}
		if after := snapshot(t, p); !reflect.DeepEqual(after, before) {
			t.Fatal("rejected event changed projection")
		}
	}
}

func TestProjectionPagingContentAndImmutability(t *testing.T) {
	body := strings.Repeat("你好🧪", 20000)
	rows := []Message{{MessageID: "u", Role: "user", Content: "question"}, {MessageID: "a", Role: "assistant", Content: body, ToolCalls: []ToolCall{{ID: "call", Name: "write_file", Arguments: body}}}}
	p, err := NewProjection(testIdentity, rows, 0)
	if err != nil {
		t.Fatal(err)
	}
	rows[1].ToolCalls[0].Name = "caller mutated input"
	s, err := p.Snapshot(PageRequest{Records: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasOlder || s.Before != 1 || len(s.Records) != 1 || len(s.Records[0].Refs) != 2 {
		t.Fatalf("bounded page: %+v", s)
	}
	if s.Records[0].Message.ToolCalls[0].Name != "write_file" {
		t.Fatal("input alias retained")
	}
	for _, ref := range s.Records[0].Refs {
		var full strings.Builder
		for offset := 0; ; {
			chunk, err := p.Content(ContentRequest{ContentRef: ref, Offset: offset})
			if err != nil {
				t.Fatal(err)
			}
			if !utf8.ValidString(chunk.Data) || len(chunk.Data) > contentChunkBytes {
				t.Fatal("invalid content chunk")
			}
			full.WriteString(chunk.Data)
			if chunk.Done {
				break
			}
			offset = chunk.NextOffset
		}
		if full.String() != body {
			t.Fatal("content was truncated or duplicated")
		}
	}
	s.Records[0].Message.ToolCalls[0].Name = "caller mutated snapshot"
	if next := snapshot(t, p); next.Records[1].Message.ToolCalls[0].Name != "write_file" {
		t.Fatal("snapshot alias retained")
	}
	older, err := p.Snapshot(PageRequest{SnapshotID: s.SnapshotID, Before: s.Before})
	if err != nil || len(older.Records) != 1 || older.Records[0].ID != "m:u" {
		t.Fatalf("older page: %+v %v", older, err)
	}
	projectEvent(t, p, 1, event.Event{Kind: event.Text, MessageID: "b", Text: "new"})
	retained, err := p.Snapshot(PageRequest{SnapshotID: s.SnapshotID, Before: s.Before})
	if err != nil || retained.Stale || len(retained.Records) != 1 || retained.Records[0].ID != "m:u" {
		t.Fatal("stream mutation invalidated immutable page")
	}
	retainedChunk, err := p.Content(ContentRequest{ContentRef: s.Records[0].Refs[0]})
	if err != nil || retainedChunk.Stale || retainedChunk.Data == "" {
		t.Fatal("stream mutation invalidated immutable content")
	}
	for seq := uint64(2); seq <= 4; seq++ {
		projectEvent(t, p, seq, event.Event{Kind: event.Text, MessageID: "b", Text: "more"})
		snapshot(t, p)
	}
	stale, err := p.Snapshot(PageRequest{SnapshotID: s.SnapshotID, Before: s.Before})
	if err != nil || !stale.Stale || len(stale.Records) != 0 {
		t.Fatal("old page was combined with new state")
	}
	chunk, err := p.Content(ContentRequest{ContentRef: s.Records[0].Refs[0]})
	if err != nil || !chunk.Stale {
		t.Fatal("old content ref did not become stale")
	}
}

func TestProjectionSnapshotDoesNotDuplicateActiveRecordInPage(t *testing.T) {
	p, err := NewProjection(testIdentity, []Message{{
		RecordID: "m:assistant", MessageID: "assistant", Role: "assistant", Content: "partial", Pending: true,
	}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := p.Snapshot(PageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Records) != 1 || len(snapshot.ActiveRecords) != 0 {
		t.Fatalf("active record duplicated across snapshot arrays: records=%d active=%d", len(snapshot.Records), len(snapshot.ActiveRecords))
	}
	if _, err := json.Marshal(snapshot); err != nil {
		t.Fatal(err)
	}
}

func TestActiveRecordIndexesDoNotScanSettledTurns(t *testing.T) {
	messages := make([]*bufferedMessage, 0, 10000)
	for i := range 9990 {
		messages = append(messages, &bufferedMessage{message: Message{RecordID: fmt.Sprintf("m:%d", i), Role: "assistant", TurnID: "old"}})
	}
	messages = append(messages,
		&bufferedMessage{message: Message{RecordID: "m:user", Role: "user", TurnID: "current"}},
		&bufferedMessage{message: Message{RecordID: "m:assistant", Role: "assistant", TurnID: "current", Pending: true}},
	)
	indexes := activeRecordIndexes(messages, len(messages), Runtime{TurnID: "current", Status: event.TurnInProgress})
	if len(indexes) != 2 || indexes[0] != len(messages)-1 || indexes[1] != len(messages)-2 {
		t.Fatalf("active indexes = %v, want only current turn owners", indexes)
	}
}

func TestProjectionConcurrentSnapshotNeverClaimsUnappliedText(t *testing.T) {
	p, _ := NewProjection(testIdentity, nil, 0)
	var wg sync.WaitGroup
	wg.Go(func() {
		for seq := uint64(1); seq <= 100; seq++ {
			projectEvent(t, p, seq, event.Event{Kind: event.Text, MessageID: "a", Text: "x"})
		}
	})
	for range 100 {
		s := snapshot(t, p)
		if len(s.Records) > 0 && len(s.Records[0].Message.Content) != int(s.CoveredThroughSeq) {
			t.Fatalf("coverage %d does not cover text", s.CoveredThroughSeq)
		}
		if _, err := json.Marshal(s); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
