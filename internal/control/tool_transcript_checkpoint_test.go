package control

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reasonix/internal/agent"
	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"testing"
	"time"
)

type checkpointProbeTool struct {
	name    string
	started chan struct{}
}

func (t checkpointProbeTool) Name() string            { return t.name }
func (t checkpointProbeTool) Description() string     { return "checkpoint test" }
func (t checkpointProbeTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (t checkpointProbeTool) ReadOnly() bool          { return false }
func (t checkpointProbeTool) Execute(ctx context.Context, _ json.RawMessage) (string, error) {
	if t.started != nil {
		close(t.started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	return "write completed", nil
}

func TestToolCheckpointSurvivesReloadWhileNextWriterRuns(t *testing.T) {
	started := make(chan struct{})
	reg := tool.NewRegistry()
	reg.Add(checkpointProbeTool{name: "first"})
	reg.Add(checkpointProbeTool{name: "second", started: started})
	calls := []provider.ToolCall{{ID: "c1", Name: "first", Arguments: `{}`}, {ID: "c2", Name: "second", Arguments: `{}`}}
	mock := testutil.NewMock("test", testutil.Turn{Reasoning: "original reasoning", ToolCalls: calls})
	session := agent.NewSession("system")
	exec := agent.New(mock, reg, session, agent.Options{}, event.Discard)
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	sink, done, _ := collectSink()
	c := New(Options{Runner: exec, Executor: exec, Sink: sink, SessionDir: dir, SessionPath: path})
	t.Cleanup(c.Close)
	c.Submit("run both")
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("second tool did not start")
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatal(err)
	}
	completed, unknown := false, false
	for _, m := range loaded.Snapshot() {
		if m.Role != provider.RoleTool {
			continue
		}
		if m.ToolCallID == "c1" {
			completed = m.Content == "write completed" && provider.ToolResultRunState(m) == provider.ToolRunCompleted
		}
		if m.ToolCallID == "c2" {
			unknown = provider.ToolResultRunState(m) == provider.ToolRunUnknown
		}
	}
	if !completed || !unknown {
		t.Fatalf("completed=%v unknown=%v history=%+v", completed, unknown, loaded.Snapshot())
	}
	c.Cancel()
	waitForDone(t, done)
}
