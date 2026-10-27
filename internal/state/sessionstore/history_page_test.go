package sessionstore

import (
	"errors"
	"sync"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surfacespec"
)

func TestHistoryPageKeepsAbsoluteRangeAndCrossPageToolContext(t *testing.T) {
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "question", CreatedAt: 123})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call", Name: "read_file", Arguments: `{"path":"a"}`}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "call", Name: "read_file", Content: "answer"})
	page, err := s.ReadHistoryPage(HistoryPageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Start != 3 || page.End != 4 || page.Total != 4 || len(page.Messages) != 1 || len(page.ToolContext) != 1 || page.ToolContext[0].MessageIndex != 2 || page.TurnStartedAt != 123 {
		t.Fatalf("page=%+v", page)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	previous, err := s.ReadHistoryPage(HistoryPageRequest{Identity: page.Identity, Anchor: page.Total, Before: page.Start, Limit: 1})
	if err != nil || previous.Start != 2 || previous.Total != 4 {
		t.Fatalf("previous=%+v err=%v", previous, err)
	}
	s.Replace([]provider.Message{{Role: provider.RoleUser, Content: "new binding"}})
	if _, err := s.ReadHistoryPage(HistoryPageRequest{Identity: page.Identity, Anchor: page.Total, Before: page.Start, Limit: 1}); !errors.Is(err, ErrHistorySnapshotChanged) {
		t.Fatalf("error=%v", err)
	}
}

func TestHistoryPageWorkDurationMatchesAnchoredTranscript(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "question", CreatedAt: 12})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "first", WorkDurationMs: 100})
	first, err := s.ReadHistoryPage(HistoryPageRequest{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	s.Add(provider.Message{Role: provider.RoleUser, Content: "host follow-up", HostAuthored: true})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "second", WorkDurationMs: 200})
	old, err := s.ReadHistoryPage(HistoryPageRequest{Identity: first.Identity, Anchor: first.Total, Before: first.Total, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if old.WorkMs[1] != 100 {
		t.Fatalf("old work=%v", old.WorkMs)
	}
	latest, err := s.ReadHistoryPage(HistoryPageRequest{Limit: 4})
	if err != nil {
		t.Fatal(err)
	}
	want := TurnWorkMs(s.Snapshot())
	for i := range want {
		if latest.WorkMs[i] != want[i] {
			t.Fatalf("work=%v want=%v", latest.WorkMs, want)
		}
	}
}

func TestHistoryPageDoesNotResurrectLocalOnlyToolRecords(t *testing.T) {
	s := NewSession("")
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call", Name: "read_file"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "call", Content: "visible"})
	s.Add(provider.Message{Role: provider.RoleTool, Name: surfacespec.ActionMessageName, LocalOnly: true, ToolCallID: "call", Content: "local"})
	page, err := s.ReadHistoryPage(HistoryPageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.ToolContext) != 0 {
		t.Fatalf("context=%+v", page.ToolContext)
	}
	if output, ok := s.ReadHistoryToolOutput("call"); !ok || output != "visible" {
		t.Fatalf("output=%q known=%v", output, ok)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Name: surfacespec.ActionMessageName, LocalOnly: true, ToolCalls: []provider.ToolCall{{ID: "hidden", Name: "read_file"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "hidden", Content: "result"})
	page, err = s.ReadHistoryPage(HistoryPageRequest{Limit: 1})
	if err != nil || len(page.ToolContext) != 0 {
		t.Fatalf("context=%+v err=%v", page.ToolContext, err)
	}
}

func TestHistoryPageCursorSurvivesConcurrentAppends(t *testing.T) {
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "anchor"})
	page, err := s.ReadHistoryPage(HistoryPageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 1000 {
			s.Add(provider.Message{Role: provider.RoleAssistant, Content: "appended"})
		}
	})
	for range 100 {
		read, err := s.ReadHistoryPage(HistoryPageRequest{Identity: page.Identity, Anchor: page.Total, Before: page.End, Limit: 1})
		if err != nil || read.Total != page.Total || read.Messages[0].Content != "anchor" {
			t.Fatalf("read=%+v err=%v", read, err)
		}
	}
	workers.Wait()
}
