package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestSessionPath(t *testing.T) {
	o := New(event.Discard)
	path := o.SessionPath("/tmp/sessions", "agent-x")
	expected := filepath.Join("/tmp/sessions", "orchestrator_agent-x.jsonl")
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}
}

func TestSaveSessionsEmptyDir(t *testing.T) {
	o := New(event.Discard)
	err := o.SaveSessions("")
	if err != nil {
		t.Fatalf("expected nil for empty dir, got %v", err)
	}
}

func TestSaveSessionsNoopForNonPersist(t *testing.T) {
	dir := t.TempDir()
	o := New(event.Discard)

	ctrl := testAgentCtrl(t, "temp", []provider.Message{
		{Role: provider.RoleAssistant, Content: "result"},
	})
	o.AddAgent("temp", ctrl, config.OrchestratorAgentEntry{
		Name:    "temp",
		Persist: false,
	})

	err := o.SaveSessions(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveSessionsPersistsAgent(t *testing.T) {
	dir := t.TempDir()
	o := New(event.Discard)

	ctrl := testAgentCtrl(t, "persist-agent", []provider.Message{
		{Role: provider.RoleAssistant, Content: "data"},
	})
	ctrl.SetSessionPath(filepath.Join(dir, "session.jsonl"))
	o.AddAgent("persist-agent", ctrl, config.OrchestratorAgentEntry{
		Name:    "persist-agent",
		Persist: true,
	})

	err := o.SaveSessions(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadSessionsEmptyDir(t *testing.T) {
	o := New(event.Discard)
	err := o.LoadSessions("")
	if err != nil {
		t.Fatalf("expected nil for empty dir, got %v", err)
	}
}

func TestLoadSessionsNonExistingFile(t *testing.T) {
	dir := t.TempDir()
	o := New(event.Discard)

	ctrl := testAgentCtrl(t, "fresh", []provider.Message{
		{Role: provider.RoleAssistant, Content: "old data"},
	})
	o.AddAgent("fresh", ctrl, config.OrchestratorAgentEntry{
		Name:    "fresh",
		Persist: true,
	})

	// No session file exists on disk yet
	err := o.LoadSessions(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveAndLoadSessionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	o := New(event.Discard)

	ctrl := testAgentCtrl(t, "persistent", []provider.Message{
		{Role: provider.RoleAssistant, Content: "saved result"},
	})
	path := filepath.Join(dir, "session.jsonl")
	ctrl.SetSessionPath(path)

	o.AddAgent("persistent", ctrl, config.OrchestratorAgentEntry{
		Name:    "persistent",
		Persist: true,
	})

	// First save the session via the agent's run to have it snapshot
	err := o.SaveSessions(dir)
	if err != nil {
		t.Fatalf("SaveSessions error: %v", err)
	}

	// Load should succeed
	o2 := New(event.Discard)
	ctrl2 := testAgentCtrl(t, "persistent", nil)
	o2.AddAgent("persistent", ctrl2, config.OrchestratorAgentEntry{
		Name:    "persistent",
		Persist: true,
	})

	err = o2.LoadSessions(dir)
	if err != nil {
		t.Fatalf("LoadSessions error: %v", err)
	}
}

func TestSaveSessionsCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "sessions")
	o := New(event.Discard)

	ctrl := testAgentCtrl(t, "agent", []provider.Message{
		{Role: provider.RoleAssistant, Content: "data"},
	})
	ctrl.SetSessionPath(filepath.Join(dir, "session.jsonl"))
	o.AddAgent("agent", ctrl, config.OrchestratorAgentEntry{
		Name:    "agent",
		Persist: true,
	})

	err := o.SaveSessions(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected session dir to be created")
	}
}
