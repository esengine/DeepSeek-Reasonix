package delegation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/tools/jobs"
)

type reportingFleetProvider struct{}

func (reportingFleetProvider) Name() string { return "reporting-fleet" }
func (reportingFleetProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	reported := false
	available := false
	for _, message := range req.Messages {
		reported = reported || message.Role == provider.RoleTool
	}
	for _, schema := range req.Tools {
		available = available || schema.Name == "reply_to_parent"
	}
	if available && !reported {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "reply", Name: "reply_to_parent", Arguments: `{"message":"working"}`}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func TestBackgroundFleetKeepsItsParentMessageRoute(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := newTestTaskTool(t, reportingFleetProvider{}, reg, "sys", "", "", nil).
		WithTranscripts(mustSubagentStore(t), testenv.TempDir(t), "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(2, 2))
	manager := jobs.NewManager(event.Discard)
	defer manager.Close()
	reports := make(chan agent.ChildMessage, 2)
	ctx := agent.WithChildMessageReceiver(context.Background(), func(_ context.Context, message agent.ChildMessage) error { reports <- message; return nil })
	ctx = jobs.WithSession(jobs.WithManager(ctx, manager), "parent")
	ctx = agent.WithParentSession(agent.WithCallContext(ctx, "fleet", event.Discard, nil, false), "parent")
	out, err := NewFleetTool(task).Execute(ctx, json.RawMessage(`{"run_in_background":true,"tasks":[{"prompt":"one","read_only":true},{"prompt":"two","read_only":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(out, `"`)
	if len(parts) < 3 {
		t.Fatalf("missing job reference: %s", out)
	}
	results, _ := manager.WaitForSession(context.Background(), "parent", []string{parts[1]}, jobs.WaitOptions{Timeout: 5 * time.Second})
	if len(results) != 1 || results[0].Status != jobs.Done {
		t.Fatalf("job = %+v", results)
	}
	if len(reports) != 2 {
		t.Fatalf("received %d reports, want both background children", len(reports))
	}
	a, b := <-reports, <-reports
	if a.Ref == "" || a.Ref == b.Ref {
		t.Fatalf("sender identities are not distinct: %+v %+v", a, b)
	}
}
