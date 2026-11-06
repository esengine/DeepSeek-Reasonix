package agent

import (
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestSnipStrategyStillAvailableForFirstVisibleAndSummaryInput(t *testing.T) {
	a := &Agent{svc: agentServices{tools: tool.NewRegistry()}}
	s := a.window().snipStrategyFor("read_file")
	if s.head <= 0 || s.tail <= 0 {
		t.Fatalf("snip strategy for read_file = %+v", s)
	}
	body, notice := truncateToolOutputFor(strings.Repeat("x", MaxToolOutputBytes+100), "read_file", "call-1", MaxToolOutputBytes, s)
	if notice == "" || !strings.Contains(body, "call_id=call-1") {
		t.Fatalf("first-visible truncation missing marker: notice=%q body=%.200q", notice, body)
	}
	if len(body) > MaxToolOutputBytes+200 {
		t.Fatalf("bounded body still oversized: %d", len(body))
	}
}
