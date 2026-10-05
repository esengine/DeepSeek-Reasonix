package coordinator

import (
	"context"
	"encoding/json"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
	"sync"
)

type coordinatorGoalRecorder struct {
	reports []tool.GoalReport
}

func (r *coordinatorGoalRecorder) RecordGoalReport(report tool.GoalReport) (string, error) {
	r.reports = append(r.reports, report)
	return "recorded " + report.Status, nil
}

// deepseekThinkingProvider marks a scripted provider as DeepSeek thinking mode
// (provider.ToolCallReasoningPolicy) — the scope within which a reasoning-only
// finish_reason="stop" turn is accepted as a final answer.
type deepseekThinkingProvider struct{ *scriptedProvider }

// echoTool is a trivial read-only tool used to drive a multi-step tool loop:
// each call appends an assistant(tool_call) + tool(result) pair to the history,
// growing the request prefix the way a real multi-turn session does.
type echoTool struct{}

func lastToolResult(s *sessionstore.Session, name string) string {
	var result string
	for _, m := range s.Messages {
		if m.Role == provider.RoleTool && m.Name == name {
			result = m.Content
		}
	}
	return result
}

type recordSink struct {
	mu       sync.Mutex
	evs      []event.Event
	recovery []event.ProtocolRecoveryAudit
}

func (s *recordSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evs = append(s.evs, e)
}

func (s *recordSink) kinds(k event.Kind) []event.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []event.Event
	for _, e := range s.evs {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return out
}

func (s *recordSink) RecordProtocolRecovery(a event.ProtocolRecoveryAudit) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recovery = append(s.recovery, a)
}

type recordingAsker struct {
	questions []event.AskQuestion
}

func (r *recordingAsker) Ask(_ context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	r.questions = questions
	return []event.AskAnswer{{QuestionID: "q1", Selected: []string{"Keep going"}}}, nil
}

// scriptedProvider replays a distinct chunk set per Stream call, so a multi-turn
// Run() sees tool calls on turn 1 and a plain final answer on turn 2.
type scriptedProvider struct {
	name     string
	turns    [][]provider.Chunk
	call     int
	requests []provider.Request
}

func (s *scriptedProvider) Name() string { return s.name }

func (s *scriptedProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	s.requests = append(s.requests, req)
	i := s.call
	if i >= len(s.turns) {
		i = len(s.turns) - 1
	}
	s.call++
	ch := make(chan provider.Chunk, len(s.turns[i]))
	for _, c := range s.turns[i] {
		ch <- c
	}
	close(ch)
	return ch, nil
}

func toolCallChunk(id, name, args string) provider.Chunk {
	return provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: id, Name: name, Arguments: args}}
}

type turnStartProvider struct{}

func (p *turnStartProvider) Name() string { return "turn-start" }

func (p *turnStartProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "answered"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}
func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "echo back the given text" }
func (echoTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
}
func (echoTool) ReadOnly() bool { return true }
func (echoTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var a struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(args, &a)
	return "echoed: " + a.Text, nil
}
