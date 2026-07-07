package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

type promptResumeCtrl struct {
	history []provider.Message
	resumed *agent.Session
	path    string
}

func (c *promptResumeCtrl) History() []provider.Message {
	return append([]provider.Message(nil), c.history...)
}

func (c *promptResumeCtrl) Resume(s *agent.Session, path string) {
	c.resumed = s
	c.path = path
}

func (c *promptResumeCtrl) SetSessionPath(path string) {
	c.path = path
}

func TestSessionWithFreshSystemPromptPreservesLoadedRewriteBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := agent.NewSession("old sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "tool-1", Name: "read_file", Arguments: "{}"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "tool-1", Name: "read_file", Content: strings.Repeat("detail ", 100)})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	resumed := sessionWithFreshSystemPrompt(loaded, "new sys")
	msgs := resumed.Snapshot()
	msgs[3].Content = "[elided tool result]"
	resumed.Replace(msgs)
	if err := resumed.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite fresh-system resume: %v", err)
	}

	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession rewritten: %v", err)
	}
	if got := reloaded.Messages[0].Content; got != "new sys" {
		t.Fatalf("system prompt after rewrite = %q, want new sys", got)
	}
	if got := reloaded.Messages[3].Content; got != "[elided tool result]" {
		t.Fatalf("tool result after rewrite = %q, want elided", got)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after owned resume rewrite = %v err=%v, want none", matches, err)
	}
}

func TestResumeWithFreshSystemPromptPreservesLoadedRewriteBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	s := agent.NewSession("old sys")
	s.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	s.Add(provider.Message{Role: provider.RoleAssistant, ToolCalls: []provider.ToolCall{{ID: "tool-1", Name: "read_file", Arguments: "{}"}}})
	s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: "tool-1", Name: "read_file", Content: strings.Repeat("detail ", 100)})
	s.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := s.Save(path); err != nil {
		t.Fatalf("Save base: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	ctrl := &promptResumeCtrl{history: []provider.Message{{Role: provider.RoleSystem, Content: "new sys"}}}
	resumeWithFreshSystemPrompt(ctrl, loaded.Snapshot(), path)
	if ctrl.resumed == nil {
		t.Fatalf("Resume was not called")
	}

	msgs := ctrl.resumed.Snapshot()
	msgs[3].Content = "[elided tool result]"
	ctrl.resumed.Replace(msgs)
	if err := ctrl.resumed.SaveRewrite(path); err != nil {
		t.Fatalf("SaveRewrite resumed history: %v", err)
	}

	if got := ctrl.path; got != path {
		t.Fatalf("resume path = %q, want %q", got, path)
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession rewritten: %v", err)
	}
	if got := reloaded.Messages[0].Content; got != "new sys" {
		t.Fatalf("system prompt after rewrite = %q, want new sys", got)
	}
	if got := reloaded.Messages[3].Content; got != "[elided tool result]" {
		t.Fatalf("tool result after rewrite = %q, want elided", got)
	}
	if matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*-recovery-*.jsonl")); err != nil || len(matches) != 0 {
		t.Fatalf("recovery branches after resume rewrite = %v err=%v, want none", matches, err)
	}
}

func TestResumeWithFreshSystemPromptRejectsStaleCarriedHistoryBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	current := agent.NewSession("old sys")
	current.Add(provider.Message{Role: provider.RoleUser, Content: "first"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "one"})
	current.Add(provider.Message{Role: provider.RoleUser, Content: "disk second"})
	current.Add(provider.Message{Role: provider.RoleAssistant, Content: "disk two"})
	if err := current.Save(path); err != nil {
		t.Fatalf("Save current: %v", err)
	}

	stale := []provider.Message{
		{Role: provider.RoleSystem, Content: "old sys"},
		{Role: provider.RoleUser, Content: "first"},
		{Role: provider.RoleAssistant, Content: "one"},
	}
	ctrl := &promptResumeCtrl{history: []provider.Message{{Role: provider.RoleSystem, Content: "new sys"}}}
	resumeWithFreshSystemPrompt(ctrl, stale, path)
	if ctrl.resumed == nil {
		t.Fatalf("Resume was not called")
	}
	if err := ctrl.resumed.SaveRewrite(path); !errors.Is(err, agent.ErrSessionSnapshotConflict) {
		t.Fatalf("SaveRewrite stale carried history err = %v, want ErrSessionSnapshotConflict", err)
	}

	reloaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession current: %v", err)
	}
	if got := reloaded.Messages[len(reloaded.Messages)-1].Content; got != "disk two" {
		t.Fatalf("original tail after stale resume rewrite = %q, want disk two", got)
	}
}
