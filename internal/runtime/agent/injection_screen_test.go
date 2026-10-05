package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

// screenStub answers a screen by the one fixture phrase the tests plant; it is
// a stand-in for the triage model, not a detector.
type screenStub struct {
	mu     sync.Mutex
	inputs []string
	fail   bool
}

func (s *screenStub) Name() string { return "screen-stub" }

func (s *screenStub) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	input := req.Messages[len(req.Messages)-1].Content
	s.mu.Lock()
	s.inputs = append(s.inputs, input)
	s.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	switch {
	case s.fail:
		ch <- provider.Chunk{Type: provider.ChunkError, Err: context.DeadlineExceeded}
	case strings.Contains(input, "PLANTED-INSTRUCTION"):
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "INJECTION"}
	default:
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "CLEAN"}
	}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{TotalTokens: 7}}
	close(ch)
	return ch, nil
}

type screenSink struct {
	mu     sync.Mutex
	events []event.Event
}

func (s *screenSink) Emit(e event.Event) {
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

func screenBatch() ([]provider.ToolCall, batchExecution) {
	calls := []provider.ToolCall{{ID: "1", Name: "web_fetch"}, {ID: "2", Name: "read_file"}, {ID: "3", Name: "mcp__docs__get"}}
	return calls, batchExecution{
		results: []string{"PLANTED-INSTRUCTION", "PLANTED-INSTRUCTION", "an ordinary page"},
		outcomes: []toolOutcome{
			{provenance: tool.Provenance{Kind: tool.ProvenanceWeb, Source: "example.com"}},
			{},
			{provenance: tool.Provenance{Kind: tool.ProvenanceMCP, Source: "docs"}},
		},
	}
}

// Only external results are screened, only a flagged one raises the notice,
// and the screen bills its own source rather than the classifier's.
func TestScreenExternalFlagsOnlyExternalResultsTheScreenFlags(t *testing.T) {
	stub, sink := &screenStub{}, &screenSink{}
	a := &Agent{svc: agentServices{triage: stub, screenExternal: true, sink: sink}}
	calls, batch := screenBatch()
	got := a.screenExternal(t.Context(), calls, batch)
	if want := []bool{true, false, false}; got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("flagged = %v, want %v", got, want)
	}
	if len(stub.inputs) != 2 {
		t.Fatalf("screened %d results, want the 2 external ones: %q", len(stub.inputs), stub.inputs)
	}
	var notices, usage int
	for _, e := range sink.events {
		switch {
		case e.Kind == event.Notice && e.Code == event.NoticeCodeSuspectedInjection:
			notices++
			if e.Detail != "web_fetch · web:example.com" {
				t.Fatalf("notice detail = %q", e.Detail)
			}
		case e.Kind == event.Usage:
			usage++
			if e.UsageSource != event.UsageSourceInjectionScreen {
				t.Fatalf("screen usage billed to %q", e.UsageSource)
			}
		}
	}
	if notices != 1 || usage != 2 {
		t.Fatalf("notices = %d, usage events = %d; want 1 and 2", notices, usage)
	}
}

// Off by default, and a screen that fails flags nothing: the verdict is advice,
// so its absence may lose a hint but never invents one.
func TestScreenExternalIsOffByDefaultAndSilentOnFailure(t *testing.T) {
	calls, batch := screenBatch()
	stub := &screenStub{}
	off := &Agent{svc: agentServices{triage: stub, sink: &screenSink{}}}
	if got := off.screenExternal(t.Context(), calls, batch); got[0] || len(stub.inputs) != 0 {
		t.Fatalf("screen ran while off: flagged %v, %d requests", got, len(stub.inputs))
	}
	sink := &screenSink{}
	failing := &Agent{svc: agentServices{triage: &screenStub{fail: true}, screenExternal: true, sink: sink}}
	if got := failing.screenExternal(t.Context(), calls, batch); got[0] || got[2] {
		t.Fatalf("a failed screen flagged %v", got)
	}
	for _, e := range sink.events {
		if e.Kind == event.Notice {
			t.Fatalf("a failed screen raised a notice: %+v", e)
		}
	}
}

func TestClipForScreenKeepsHeadAndTail(t *testing.T) {
	long := "HEAD" + strings.Repeat("x", 20*1024) + "TAIL"
	got := clipForScreen(long)
	if !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") || len(got) > injectionScreenHead+injectionScreenTail+8 {
		t.Fatalf("clip kept %d bytes, head %q tail %q", len(got), got[:4], got[len(got)-4:])
	}
}
