package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func testAgentCtrl(t *testing.T, name string, msgs []provider.Message) *control.Controller {
	t.Helper()
	sess := agent.NewSession("")
	for _, m := range msgs {
		sess.Add(m)
	}
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	return control.New(control.Options{
		Sink:     event.Discard,
		Executor: exec,
		Runner:   &mockRunner{},
	})
}

func TestOrchestratorTools(t *testing.T) {
	orc := New(event.Discard)
	tools := OrchestratorTools(orc)

	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}
}

func TestOrchestratorToolNames(t *testing.T) {
	names := OrchestratorToolNames()
	expected := []string{"agent_spawn", "agent_send", "agent_status", "agent_stats"}

	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for _, want := range expected {
		found := false
		for _, got := range names {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected tool name %q not found in %v", want, names)
		}
	}
}

func TestAgentSpawnToolMetadata(t *testing.T) {
	orc := New(event.Discard)
	spawn := &agentSpawnTool{orc: orc}

	if spawn.Name() != "agent_spawn" {
		t.Fatalf("expected name 'agent_spawn', got %q", spawn.Name())
	}
	if spawn.ReadOnly() {
		t.Fatal("agent_spawn should not be read-only")
	}
	if spawn.Description() == "" {
		t.Fatal("description should not be empty")
	}
	if spawn.Schema() == nil {
		t.Fatal("schema should not be nil")
	}
}

func TestAgentSendToolMetadata(t *testing.T) {
	orc := New(event.Discard)
	send := &agentSendTool{orc: orc}

	if send.Name() != "agent_send" {
		t.Fatalf("expected name 'agent_send', got %q", send.Name())
	}
	if send.ReadOnly() {
		t.Fatal("agent_send should not be read-only")
	}
	if send.Description() == "" {
		t.Fatal("description should not be empty")
	}
	if send.Schema() == nil {
		t.Fatal("schema should not be nil")
	}
}

func TestAgentStatusToolMetadata(t *testing.T) {
	orc := New(event.Discard)
	st := &agentStatusTool{orc: orc}

	if st.Name() != "agent_status" {
		t.Fatalf("expected name 'agent_status', got %q", st.Name())
	}
	if !st.ReadOnly() {
		t.Fatal("agent_status should be read-only")
	}
	if st.Schema() == nil {
		t.Fatal("schema should not be nil")
	}
}

func TestAgentStatsToolMetadata(t *testing.T) {
	orc := New(event.Discard)
	st := &agentStatsTool{orc: orc}

	if st.Name() != "agent_stats" {
		t.Fatalf("expected name 'agent_stats', got %q", st.Name())
	}
	if !st.ReadOnly() {
		t.Fatal("agent_stats should be read-only")
	}
	if st.Schema() == nil {
		t.Fatal("schema should not be nil")
	}
}

func TestAgentSpawnToolValidation(t *testing.T) {
	orc := New(event.Discard)
	spawn := &agentSpawnTool{orc: orc}

	tests := []struct {
		name    string
		args    string
		wantErr string
	}{
		{
			name:    "empty name",
			args:    `{"task": "do stuff"}`,
			wantErr: "agent_spawn: name is required",
		},
		{
			name:    "empty task",
			args:    `{"name": "agent-x"}`,
			wantErr: "agent_spawn: task is required",
		},
		{
			name:    "invalid json",
			args:    `not json`,
			wantErr: "agent_spawn: invalid args",
		},
		{
			name:    "missing both",
			args:    `{}`,
			wantErr: "agent_spawn: name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := spawn.Execute(context.Background(), json.RawMessage(tt.args))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestAgentSendToolValidation(t *testing.T) {
	orc := New(event.Discard)
	send := &agentSendTool{orc: orc}

	tests := []struct {
		name    string
		args    string
		wantErr string
	}{
		{
			name:    "empty name",
			args:    `{"message": "hello"}`,
			wantErr: "agent_send: name is required",
		},
		{
			name:    "empty message",
			args:    `{"name": "agent-x"}`,
			wantErr: "agent_send: message is required",
		},
		{
			name:    "invalid json",
			args:    `{bad}`,
			wantErr: "agent_send: invalid args",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := send.Execute(context.Background(), json.RawMessage(tt.args))
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestAgentSpawnToolAgentNotFound(t *testing.T) {
	orc := New(event.Discard)
	spawn := &agentSpawnTool{orc: orc}

	_, err := spawn.Execute(context.Background(), json.RawMessage(`{"name": "nonexistent", "task": "do stuff"}`))
	if err == nil {
		t.Fatal("expected error for non-existent agent")
	}
	if !strings.Contains(err.Error(), "agent \"nonexistent\" not found") {
		t.Fatalf("expected 'not found' error, got %q", err.Error())
	}
}

func TestAgentSendToolAgentNotFound(t *testing.T) {
	orc := New(event.Discard)
	send := &agentSendTool{orc: orc}

	result, err := send.Execute(context.Background(), json.RawMessage(`{"name": "nonexistent", "message": "hello"}`))
	if err != nil {
		t.Fatalf("agent_send should wrap errors in result, got err: %v", err)
	}
	if !strings.Contains(result, "completed with error") {
		t.Fatalf("expected 'completed with error' in result, got %q", result)
	}
	if !strings.Contains(result, "not found") {
		t.Fatalf("expected 'not found' in result, got %q", result)
	}
}

func TestAgentStatusToolNotFound(t *testing.T) {
	orc := New(event.Discard)
	st := &agentStatusTool{orc: orc}

	_, err := st.Execute(context.Background(), json.RawMessage(`{"name": "ghost"}`))
	if err == nil {
		t.Fatal("expected error for non-existent agent")
	}
	if !strings.Contains(err.Error(), "agent \"ghost\" not found") {
		t.Fatalf("expected 'not found' error, got %q", err.Error())
	}
}

func TestAgentStatsToolInvalidArgs(t *testing.T) {
	orc := New(event.Discard)
	st := &agentStatsTool{orc: orc}

	_, err := st.Execute(context.Background(), json.RawMessage(`not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "agent_stats: invalid args") {
		t.Fatalf("expected 'invalid args' error, got %q", err.Error())
	}
}

func TestAgentStatusToolEmptyName(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "helper", []provider.Message{
		{Role: provider.RoleAssistant, Content: "done"},
	})
	orc.AddAgent("helper", ctrl, config.OrchestratorAgentEntry{Name: "helper"})

	if a, ok := orc.Agent("helper"); !ok {
		t.Fatal("expected helper agent to exist")
	} else if a == nil {
		t.Fatal("helper agent should not be nil")
	}

	st := &agentStatusTool{orc: orc}
	result, err := st.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "helper") {
		t.Fatalf("expected result to contain 'helper', got %q", result)
	}
}

func TestAgentStatusToolWithName(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "helper", []provider.Message{
		{Role: provider.RoleAssistant, Content: "done"},
	})
	orc.AddAgent("helper", ctrl, config.OrchestratorAgentEntry{Name: "helper"})

	st := &agentStatusTool{orc: orc}
	result, err := st.Execute(context.Background(), json.RawMessage(`{"name": "helper"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Agent: helper") {
		t.Fatalf("expected result to contain 'Agent: helper', got %q", result)
	}
	if !strings.Contains(result, "Status: ready") {
		t.Fatalf("expected result to contain 'Status: ready', got %q", result)
	}
}

func TestAgentStatsToolWithoutName(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "helper", []provider.Message{
		{Role: provider.RoleAssistant, Content: "done"},
	})
	orc.AddAgent("helper", ctrl, config.OrchestratorAgentEntry{Name: "helper"})

	st := &agentStatsTool{orc: orc}
	result, err := st.Execute(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "helper") {
		t.Fatalf("expected result to contain 'helper', got %q", result)
	}
}

func TestAgentStatsToolWithName(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "helper", []provider.Message{
		{Role: provider.RoleAssistant, Content: "done"},
	})
	orc.AddAgent("helper", ctrl, config.OrchestratorAgentEntry{Name: "helper"})

	st := &agentStatsTool{orc: orc}
	result, err := st.Execute(context.Background(), json.RawMessage(`{"name": "helper"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "helper:") {
		t.Fatalf("expected result to contain 'helper:', got %q", result)
	}
}

func TestAgentSpawnToolSuccess(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "helper", []provider.Message{
		{Role: provider.RoleAssistant, Content: "task completed"},
	})
	mux := NewSinkMultiplexer(event.Discard, "helper")
	orc.AddAgent("helper", ctrl, config.OrchestratorAgentEntry{Name: "helper"})
	// Override the sink so we don't double-wrap
	agent, _ := orc.Agent("helper")
	agent.Sink = mux

	spawn := &agentSpawnTool{orc: orc}
	result, err := spawn.Execute(context.Background(), json.RawMessage(`{"name": "helper", "task": "do the work"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "[Agent \"helper\" completed]") {
		t.Fatalf("expected success prefix, got %q", result)
	}
	if !strings.Contains(result, "task completed") {
		t.Fatalf("expected result to contain 'task completed', got %q", result)
	}
}

func TestAgentSpawnToolRunError(t *testing.T) {
	orc := New(event.Discard)

	errRunner := &mockRunner{
		runFn: func(ctx context.Context, input string) error {
			return errors.New("provider failure")
		},
	}
	sess := agent.NewSession("")
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "partial"})
	exec := agent.New(nil, tool.NewRegistry(), sess, agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{
		Sink:     event.Discard,
		Executor: exec,
		Runner:   errRunner,
	})
	orc.AddAgent("helper", ctrl, config.OrchestratorAgentEntry{Name: "helper"})

	spawn := &agentSpawnTool{orc: orc}
	result, err := spawn.Execute(context.Background(), json.RawMessage(`{"name": "helper", "task": "risky task"}`))
	if err != nil {
		t.Fatalf("agent_spawn should not return error for run errors, got: %v", err)
	}
	if !strings.Contains(result, "completed with error") || !strings.Contains(result, "provider failure") {
		t.Fatalf("expected 'completed with error' with 'provider failure', got %q", result)
	}
}
