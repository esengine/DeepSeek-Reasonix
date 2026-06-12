package orchestrator

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestNewOrchestrator(t *testing.T) {
	orc := New(event.Discard)
	if orc == nil {
		t.Fatal("New returned nil")
	}
	if len(orc.Agents()) != 0 {
		t.Fatalf("expected 0 agents initially, got %d", len(orc.Agents()))
	}
	if orc.MainSink() == nil {
		t.Fatal("MainSink should not be nil")
	}
}

func TestOrchestratorSetMainCtrl(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "main", nil)
	orc.SetMainCtrl(ctrl)

	if orc.MainCtrl() != ctrl {
		t.Fatal("MainCtrl should return the set controller")
	}
}

func TestOrchestratorSessionDir(t *testing.T) {
	orc := New(event.Discard)
	if orc.SessionDir() != "" {
		t.Fatalf("expected empty session dir, got %q", orc.SessionDir())
	}

	orc.SetSessionDir("/tmp/sessions")
	if orc.SessionDir() != "/tmp/sessions" {
		t.Fatalf("expected '/tmp/sessions', got %q", orc.SessionDir())
	}
}

func TestOrchestratorAddAgent(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "worker", []provider.Message{
		{Role: provider.RoleAssistant, Content: "done"},
	})

	orc.AddAgent("worker", ctrl, config.OrchestratorAgentEntry{Name: "worker", Verbose: true})

	agents := orc.Agents()
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "worker" {
		t.Fatalf("expected agent name 'worker', got %q", agents[0].Name)
	}
	if agents[0].Status() != StatusIdle {
		t.Fatalf("expected StatusIdle, got %q", agents[0].Status())
	}
}

func TestOrchestratorAgent(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "alice", nil)
	orc.AddAgent("alice", ctrl, config.OrchestratorAgentEntry{})

	a, ok := orc.Agent("alice")
	if !ok {
		t.Fatal("expected to find agent 'alice'")
	}
	if a.Name != "alice" {
		t.Fatalf("expected name 'alice', got %q", a.Name)
	}

	_, ok = orc.Agent("nonexistent")
	if ok {
		t.Fatal("expected to not find agent 'nonexistent'")
	}
}

func TestOrchestratorAgentNames(t *testing.T) {
	orc := New(event.Discard)
	orc.AddAgent("a", testAgentCtrl(t, "a", nil), config.OrchestratorAgentEntry{})
	orc.AddAgent("b", testAgentCtrl(t, "b", nil), config.OrchestratorAgentEntry{})

	names := orc.AgentNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}

	seen := map[string]bool{}
	for _, n := range names {
		seen[n] = true
	}
	if !seen["a"] || !seen["b"] {
		t.Fatalf("expected names 'a' and 'b', got %v", names)
	}
}

func TestOrchestratorClose(t *testing.T) {
	orc := New(event.Discard)
	ctrl1 := testAgentCtrl(t, "a", nil)
	ctrl2 := testAgentCtrl(t, "b", nil)
	orc.AddAgent("a", ctrl1, config.OrchestratorAgentEntry{})
	orc.AddAgent("b", ctrl2, config.OrchestratorAgentEntry{})

	orc.Close()

	// Close should not panic and agents should still be accessible
	if len(orc.Agents()) != 2 {
		t.Fatalf("expected 2 agents after close, got %d", len(orc.Agents()))
	}
}

func TestOrchestratorCloseEmpty(t *testing.T) {
	orc := New(event.Discard)
	orc.Close() // should not panic
}

func TestOrchestratorStatsNotFound(t *testing.T) {
	orc := New(event.Discard)
	result := orc.Stats("ghost")
	if !strings.Contains(result, "not found") {
		t.Fatalf("expected 'not found' in result, got %q", result)
	}
}

func TestOrchestratorStats(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "worker", []provider.Message{
		{Role: provider.RoleAssistant, Content: "task done"},
	})
	orc.AddAgent("worker", ctrl, config.OrchestratorAgentEntry{Name: "worker"})

	result := orc.Stats("worker")
	if !strings.Contains(result, "worker:") {
		t.Fatalf("expected 'worker:' in stats, got %q", result)
	}
	if !strings.Contains(result, "ready") {
		t.Fatalf("expected 'ready' status in stats, got %q", result)
	}
	if !strings.Contains(result, "turns: 0") {
		t.Fatalf("expected 'turns: 0' in stats, got %q", result)
	}
}

func TestOrchestratorStatsAll(t *testing.T) {
	orc := New(event.Discard)
	orc.AddAgent("alpha", testAgentCtrl(t, "alpha", nil), config.OrchestratorAgentEntry{Name: "alpha"})
	orc.AddAgent("beta", testAgentCtrl(t, "beta", nil), config.OrchestratorAgentEntry{Name: "beta"})

	result := orc.StatsAll()
	if !strings.Contains(result, "alpha") {
		t.Fatalf("expected 'alpha' in StatsAll, got %q", result)
	}
	if !strings.Contains(result, "beta") {
		t.Fatalf("expected 'beta' in StatsAll, got %q", result)
	}
	if !strings.Contains(result, "── total ──") {
		t.Fatalf("expected total in StatsAll with 2 agents, got %q", result)
	}
}

func TestOrchestratorStatsAllSingle(t *testing.T) {
	orc := New(event.Discard)
	orc.AddAgent("solo", testAgentCtrl(t, "solo", nil), config.OrchestratorAgentEntry{Name: "solo"})

	result := orc.StatsAll()
	if !strings.Contains(result, "solo") {
		t.Fatalf("expected 'solo' in StatsAll, got %q", result)
	}
	if strings.Contains(result, "── total ──") {
		t.Fatal("expected no total line for single agent")
	}
}

func TestOrchestratorStatsAllEmpty(t *testing.T) {
	orc := New(event.Discard)
	result := orc.StatsAll()
	if result != "" {
		t.Fatalf("expected empty StatsAll for no agents, got %q", result)
	}
}

func TestOrchestratorSendMessage(t *testing.T) {
	orc := New(event.Discard)
	ctrl := testAgentCtrl(t, "helper", []provider.Message{
		{Role: provider.RoleAssistant, Content: "response text"},
	})
	mux := NewSinkMultiplexer(event.Discard, "helper")
	orc.AddAgent("helper", ctrl, config.OrchestratorAgentEntry{Name: "helper"})
	if a, ok := orc.Agent("helper"); ok {
		a.Sink = mux
	}

	result, err := orc.SendMessage(context.Background(), "helper", "hello")
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}
	if result != "response text" {
		t.Fatalf("expected 'response text', got %q", result)
	}
}

func TestOrchestratorSendMessageNotFound(t *testing.T) {
	orc := New(event.Discard)
	_, err := orc.SendMessage(context.Background(), "ghost", "hello")
	if err == nil {
		t.Fatal("expected error for non-existent agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got %q", err.Error())
	}
}

func TestOrchestratorAgentsList(t *testing.T) {
	orc := New(event.Discard)
	orc.AddAgent("x", testAgentCtrl(t, "x", nil), config.OrchestratorAgentEntry{})
	orc.AddAgent("y", testAgentCtrl(t, "y", nil), config.OrchestratorAgentEntry{})
	orc.AddAgent("z", testAgentCtrl(t, "z", nil), config.OrchestratorAgentEntry{})

	agents := orc.Agents()
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	names := orc.AgentNames()
	if len(names) != 3 {
		t.Fatalf("expected 3 agent names, got %d", len(names))
	}
}

func TestOrchestratorConcurrency(t *testing.T) {
	orc := New(event.Discard)

	add := func(name string) {
		orc.AddAgent(name, testAgentCtrl(t, name, nil), config.OrchestratorAgentEntry{Name: name})
	}

	add("alpha")
	add("beta")
	add("gamma")

	// Concurrent reads should not race
	done := make(chan struct{}, 2)
	go func() {
		orc.AgentNames()
		done <- struct{}{}
	}()
	go func() {
		orc.StatsAll()
		done <- struct{}{}
	}()

	<-done
	<-done
}
