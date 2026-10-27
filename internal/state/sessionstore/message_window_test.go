package sessionstore

import (
	"reasonix/internal/contract/provider"
	"testing"
)

func TestMessageWindowKeepsCrossPageCallsAndDetectsRewrites(t *testing.T) {
	s := NewSession("system")
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "call", Name: "read_file", Arguments: `{"path":"x"}`}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "call", Content: "result"})
	page := s.ReadMessageWindow(-1, 1)
	if len(page.Messages) != 1 || len(page.ToolContext) != 1 || page.ToolContext[0].Call.Name != "read_file" {
		t.Fatalf("missing cross-page context: %+v", page)
	}
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if next := s.ReadMessageWindow(page.End, 1); next.Identity != page.Identity || next.Start != page.End || len(next.Messages) != 1 {
		t.Fatalf("append changed cursor: %+v", next)
	}
	s.Replace([]provider.Message{{Role: provider.RoleUser, Content: "compacted"}})
	if rewritten := s.ReadMessageWindow(page.End, 1); rewritten.Identity == page.Identity || rewritten.Total != 1 {
		t.Fatalf("rewrite did not invalidate cursor: %+v", rewritten)
	}
}
