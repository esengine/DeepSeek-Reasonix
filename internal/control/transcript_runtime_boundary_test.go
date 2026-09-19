package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/session"
	"reasonix/internal/tool"
	"reasonix/internal/transcript"
)

func TestTranscriptFollowAssignsUniqueIdentityToOutsideTurnNotices(t *testing.T) {
	for _, afterTurn := range []bool{false, true} {
		name := "before-turn"
		if afterTurn {
			name = "after-turn"
		}
		t.Run(name, func(t *testing.T) {
			c, _, runtime := newTranscriptBoundaryController(t, testutil.Turn{Text: "answer"}, event.Discard)
			if afterTurn {
				if err := c.RunTurn(t.Context(), "question"); err != nil {
					t.Fatal(err)
				}
			}
			bodies := []string{strings.Repeat("a", 70_000), strings.Repeat("b", 70_000)}
			for _, body := range bodies {
				c.notice(body)
			}
			response, err := c.TranscriptFollow(t.Context(), transcript.FollowRequest{})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_, _ = c.TranscriptFollow(context.Background(), transcript.FollowRequest{Subscription: response.Subscription, Close: true})
			})
			var notices []transcript.Record
			for _, record := range response.Snapshot.Records {
				if record.Message.Role == "notice" && len(record.Message.Content) > 0 {
					notices = append(notices, record)
				}
			}
			if len(notices) != 2 || notices[0].ID == notices[1].ID {
				t.Fatalf("outside-turn notice identities = %+v", notices)
			}
			for index, record := range notices {
				if record.ID == "" || record.Message.RecordID != record.ID || len(record.Refs) != 1 || record.Refs[0].RecordID != record.ID {
					t.Fatalf("notice %d identity/ref mismatch: %+v", index, record)
				}
				chunk, err := runtime.Transcript().Content(transcript.ContentRequest{ContentRef: record.Refs[0]})
				if err != nil || !strings.HasPrefix(bodies[index], chunk.Data) || chunk.Data == "" {
					t.Fatalf("notice %d content alias: chunk=%q err=%v", index, chunk.Data, err)
				}
			}
		})
	}
}

func TestTranscriptFollowMakesAcceptedBatchBeyondDisplayBudgetPageable(t *testing.T) {
	c, _, runtime := newTranscriptBoundaryController(t, testutil.Turn{}, event.Discard)
	var events []session.Event
	for i := range 160 {
		body, _ := json.Marshal(map[string]any{"message": provider.Message{ID: fmt.Sprintf("accepted-%03d", i), Role: provider.RoleAssistant, Content: "accepted body"}})
		events = append(events, session.Event{Kind: "message/complete", Payload: body})
	}
	commit, err := runtime.Session().Append(t.Context(), session.Batch{OperationID: "large-batch", Events: events})
	if err != nil {
		t.Fatal(err)
	}
	// Do not flush: Follow must bridge the accepted/durable boundary itself.
	response, err := c.TranscriptFollow(t.Context(), transcript.FollowRequest{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.TranscriptFollow(context.Background(), transcript.FollowRequest{Subscription: response.Subscription, Close: true})
	if response.Snapshot == nil || response.History == nil || response.History.SnapshotSequence != commit.LastSequence() || response.Snapshot.CoveredThroughSeq != commit.LastSequence() || runtime.Transcript().Boundary().DurableSeq < commit.LastSequence() || !response.History.HasOlder || len(response.History.Messages) != 32 {
		t.Fatalf("accepted history outside the display tail is unreachable: %+v", response)
	}
}

func TestTranscriptCancellationKeepsPartialAnswerAndReasoningRecoverable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var streamedID string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Text {
			streamedID = e.MessageID
			cancel()
		}
	})
	c, service, runtime := newTranscriptBoundaryController(t, testutil.Turn{Reasoning: "partial reasoning", Text: "partial answer"}, sink)
	if err := c.RunTurn(ctx, "question"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled turn error=%v", err)
	}
	if streamedID == "" {
		t.Fatal("fixture did not stream before cancellation")
	}
	cut, err := c.TranscriptSnapshot(transcript.PageRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if cut.Runtime.Status != event.TurnInterrupted || len(cut.ActiveAttempts) != 0 {
		t.Fatalf("cancelled view remains active: status=%q attempts=%d", cut.Runtime.Status, len(cut.ActiveAttempts))
	}
	visible := 0
	for _, record := range cut.Records {
		if record.Message.Content == "partial answer" && record.Message.Reasoning == "partial reasoning" {
			visible++
		}
	}
	if visible != 1 {
		t.Errorf("cancellation lost or duplicated visible prefix: matchingRows=%d records=%+v", visible, cut.Records)
	}
	history, err := service.Query().History(t.Context(), runtime.Ref())
	if err != nil {
		t.Fatal(err)
	}
	recoverable := false
	for _, message := range history {
		recoverable = recoverable || message.LocalOnly && message.Content == "partial answer" && message.ReasoningContent == "partial reasoning"
	}
	if !recoverable {
		t.Fatal("cancelled partial output is absent from persistent recovery history")
	}
	state := runtime.StateSnapshot().Session
	if state.DurableSequence != state.EventSequence {
		t.Fatalf("interrupted terminal published before persistence: durable=%d accepted=%d", state.DurableSequence, state.EventSequence)
	}
}

func TestTranscriptFollowInitialHistoryIsReadyAndPinnedToDurableCut(t *testing.T) {
	c, _, runtime := newTranscriptBoundaryController(t, testutil.Turn{Reasoning: "saved reasoning", Text: "saved final answer"}, event.Discard)
	if err := c.RunTurn(t.Context(), "question"); err != nil {
		t.Fatal(err)
	}
	response, err := c.TranscriptFollow(t.Context(), transcript.FollowRequest{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = c.TranscriptFollow(context.Background(), transcript.FollowRequest{Subscription: response.Subscription, Close: true})
	})
	if response.Snapshot == nil || response.History == nil || response.History.Status != "ready" {
		t.Fatalf("initial follow exposed an unready history page: %+v", response)
	}
	cut := response.Snapshot
	if cut.DurableSeq == 0 || cut.DurableSeq != runtime.StateSnapshot().Session.DurableSequence || response.History.SnapshotSequence != cut.DurableSeq {
		t.Fatalf("history and view were sampled at different durable cuts: view=%d history=%d durable=%d", cut.DurableSeq, response.History.SnapshotSequence, runtime.StateSnapshot().Session.DurableSequence)
	}
	var finalID string
	for _, row := range cut.Records {
		if row.Message.Content == "saved final answer" {
			finalID = row.Message.MessageID
		}
	}
	if finalID == "" {
		t.Fatal("initial follow omitted the saved final answer")
	}
	found := false
	for _, message := range response.History.Messages {
		if message.EventSequence > cut.DurableSeq {
			t.Fatal("history page includes a message beyond its declared cut")
		}
		found = found || message.MessageID == finalID
	}
	if !found {
		t.Fatal("initial historical page omitted the completed view's final message")
	}
}

// Exercise the same exclusive runtime used by Desktop and Serve. The legacy
// controller-only ledger has a different sequence space and must not hide
// regressions in this path. Only provider output is scripted; persistence,
// the agent, runtime and display projection are real.
func newTranscriptBoundaryController(t *testing.T, turn testutil.Turn, sink event.Sink) (*Controller, *session.Service, *session.Runtime) {
	t.Helper()
	service, err := session.NewService("desktop", session.NewFilesystemPersistence(filepath.Join(t.TempDir(), "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := service.Create(t.Context(), session.CreateOptions{SessionID: "transcript-boundary"})
	if err != nil {
		t.Fatal(err)
	}
	executor := agent.New(testutil.NewMock("test", turn), tool.NewRegistry(), agent.NewSession("system"), agent.Options{}, event.Discard)
	c := newOwnedTestController(t, Options{
		Runner: executor, Executor: executor, Sink: sink,
		SessionService: service, SessionRuntime: runtime, ExclusiveSession: true,
	})
	return c, service, runtime
}

func transcriptBoundaryMessage(snapshot transcript.Snapshot, id string) (transcript.Message, bool) {
	for _, records := range [][]transcript.Record{snapshot.Records, snapshot.ActiveRecords} {
		for _, record := range records {
			if record.Message.MessageID == id {
				return record.Message, true
			}
		}
	}
	return transcript.Message{}, false
}

func TestTranscriptRuntimeSnapshotRetainsStreamingPrefixAndState(t *testing.T) {
	var c *Controller
	var runtime *session.Runtime
	observed := false
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.Text {
			return
		}
		observed = true
		snapshot, err := c.TranscriptSnapshot(transcript.PageRequest{})
		if err != nil {
			t.Errorf("snapshot during output: %v", err)
			return
		}
		if phase := runtime.StateSnapshot().Phase; phase != session.RuntimeRunning {
			t.Errorf("fixture is not streaming: phase=%q", phase)
		}
		if snapshot.Runtime.Status != event.TurnInProgress {
			t.Errorf("streaming snapshot loses running state: status=%q", snapshot.Runtime.Status)
		}
		message, found := transcriptBoundaryMessage(snapshot, e.MessageID)
		if !found || message.Content != "visible answer" || message.Reasoning != "visible thinking" {
			t.Errorf("streaming snapshot loses assistant prefix: found=%v content=%q reasoning=%q", found, message.Content, message.Reasoning)
		}
		matched := false
		for _, attempt := range snapshot.ActiveAttempts {
			if attempt.MessageID == e.MessageID && attempt.ID != "" {
				matched = true
			}
		}
		if !matched {
			t.Error("streaming snapshot omits the active assistant attempt")
		}
	})
	c, _, runtime = newTranscriptBoundaryController(t, testutil.Turn{Reasoning: "visible thinking", Text: "visible answer"}, sink)
	if err := c.RunTurn(t.Context(), "question"); err != nil {
		t.Fatal(err)
	}
	if !observed {
		t.Fatal("provider output was never published")
	}
}

func TestTranscriptRuntimeTransientRevisionDoesNotAdvanceBusinessCoverage(t *testing.T) {
	var c *Controller
	var runtime *session.Runtime
	var cuts []transcript.Snapshot
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind != event.Text {
			return
		}
		snapshot, err := c.TranscriptSnapshot(transcript.PageRequest{})
		if err != nil {
			t.Errorf("snapshot during output: %v", err)
			return
		}
		if want := runtime.StateSnapshot().Session.EventSequence; snapshot.CoveredThroughSeq != want {
			t.Errorf("snapshot mixes business and transient sequences: coverage=%d business=%d", snapshot.CoveredThroughSeq, want)
		}
		cuts = append(cuts, snapshot)
	})
	c, _, runtime = newTranscriptBoundaryController(t, testutil.Turn{Chunks: []provider.Chunk{
		{Type: provider.ChunkText, Text: "first "},
		{Type: provider.ChunkText, Text: "second"},
		{Type: provider.ChunkDone},
	}}, sink)
	if err := c.RunTurn(t.Context(), "question"); err != nil {
		t.Fatal(err)
	}
	if len(cuts) != 2 {
		t.Fatalf("expected two streaming cuts, got %d", len(cuts))
	}
	if cuts[0].CoveredThroughSeq != cuts[1].CoveredThroughSeq {
		t.Errorf("text-only chunks advance business coverage: before=%d after=%d", cuts[0].CoveredThroughSeq, cuts[1].CoveredThroughSeq)
	}
	if cuts[1].ProjectionRevision <= cuts[0].ProjectionRevision {
		t.Errorf("new streaming prefix reused the previous view revision: before=%d after=%d", cuts[0].ProjectionRevision, cuts[1].ProjectionRevision)
	}
}

func TestTranscriptRuntimePublishesCommittedAnswerBeforeCompletion(t *testing.T) {
	var c *Controller
	var service *session.Service
	var runtime *session.Runtime
	var finalID string
	completed := false
	sink := event.FuncSink(func(e event.Event) {
		switch e.Kind {
		case event.Message:
			if e.Text != "final answer" {
				return
			}
			finalID = e.MessageID
			messages, err := service.Query().History(t.Context(), runtime.Ref())
			if err != nil {
				t.Errorf("read committed answer: %v", err)
				return
			}
			found := false
			for _, message := range messages {
				if message.ID == finalID && message.Content == "final answer" {
					found = true
				}
			}
			if !found {
				t.Error("final message was published before its business commit")
			}
		case event.TurnDone:
			completed = true
			state := runtime.StateSnapshot().Session
			if state.DurableSequence != state.EventSequence {
				t.Errorf("completion precedes persistence barrier: durable=%d committed=%d", state.DurableSequence, state.EventSequence)
			}
			snapshot, err := c.TranscriptSnapshot(transcript.PageRequest{})
			if err != nil {
				t.Errorf("completion snapshot: %v", err)
				return
			}
			message, found := transcriptBoundaryMessage(snapshot, finalID)
			if !found || message.Content != "final answer" || message.Reasoning != "final reasoning" {
				t.Errorf("completion snapshot loses committed answer: found=%v content=%q reasoning=%q", found, message.Content, message.Reasoning)
			}
			if snapshot.Runtime.Status != event.TurnCompleted || len(snapshot.ActiveAttempts) != 0 {
				t.Errorf("completion snapshot is not settled: status=%q activeAttempts=%d", snapshot.Runtime.Status, len(snapshot.ActiveAttempts))
			}
		}
	})
	c, service, runtime = newTranscriptBoundaryController(t, testutil.Turn{Reasoning: "final reasoning", Text: "final answer"}, sink)
	if err := c.RunTurn(t.Context(), "question"); err != nil {
		t.Fatal(err)
	}
	if finalID == "" || !completed {
		t.Fatalf("missing final publications: message=%q completed=%v", finalID, completed)
	}
	// A repeated read must not hit a cache that advertises the final business
	// sequence but still contains only the history preceding the final answer.
	for range 2 {
		snapshot, err := c.TranscriptSnapshot(transcript.PageRequest{})
		if err != nil {
			t.Fatal(err)
		}
		message, found := transcriptBoundaryMessage(snapshot, finalID)
		if !found || message.Content != "final answer" {
			t.Fatal("settled history reread lost the final answer")
		}
	}
}
