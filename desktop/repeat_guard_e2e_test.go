package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/agent"
	agenttest "reasonix/internal/agent/testutil"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type desktopCountingTool struct {
	name     string
	readOnly bool
	calls    *int32
}

func (t desktopCountingTool) Name() string        { return t.name }
func (t desktopCountingTool) Description() string { return "test tool" }
func (t desktopCountingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}}}`)
}
func (t desktopCountingTool) ReadOnly() bool { return t.readOnly }
func (t desktopCountingTool) Execute(context.Context, json.RawMessage) (string, error) {
	atomic.AddInt32(t.calls, 1)
	return "ok", nil
}

func TestDesktopE2EBlocksRepeatedSuccessfulBashFileWrite(t *testing.T) {
	var calls int32
	reg := tool.NewRegistry()
	reg.Add(desktopCountingTool{name: "bash", calls: &calls})
	args := `{"command":"python -c \"with open('prompt.txt', 'w') as f: f.write('hello')\""}`
	prov := agenttest.NewMock("scripted-desktop",
		agenttest.Turn{ToolCalls: []provider.ToolCall{{ID: "c1", Name: "bash", Arguments: args}}},
		agenttest.Turn{ToolCalls: []provider.ToolCall{{ID: "c2", Name: "bash", Arguments: args}}},
		agenttest.Turn{ToolCalls: []provider.ToolCall{{ID: "c3", Name: "bash", Arguments: args}}},
		agenttest.Turn{Text: "done"},
	)
	events := make(chan event.Event, 32)
	sink := event.FuncSink(func(e event.Event) { events <- e })
	ag := agent.New(prov, reg, agent.NewSession(""), agent.Options{}, sink)
	ctrl := control.New(control.Options{Runner: ag, Executor: ag, Sink: sink})
	app := NewApp()
	app.setTestCtrl(ctrl, "scripted-desktop")

	app.SubmitToTab("test", "update the prompt file")

	var results []event.Event
	deadline := time.After(5 * time.Second)
	for {
		select {
		case e := <-events:
			if e.Kind == event.ToolResult {
				results = append(results, e)
			}
			if e.Kind == event.TurnDone {
				if e.Err != nil {
					t.Fatalf("turn failed: %v", e.Err)
				}
				if got := atomic.LoadInt32(&calls); got != 2 {
					t.Fatalf("bash executed %d times, want 2 before the repeat guard blocks", got)
				}
				if len(results) != 3 {
					t.Fatalf("tool results = %d, want 3", len(results))
				}
				last := results[len(results)-1].Tool.Output
				if !strings.Contains(last, "[loop guard]") || !strings.Contains(last, "edit_file") {
					t.Fatalf("third repeated write should nudge the model to change tools, got %q", last)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for desktop turn to finish")
		}
	}
}
