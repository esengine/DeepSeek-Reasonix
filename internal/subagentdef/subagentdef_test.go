package subagentdef

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubagentDefinition_Normalize(t *testing.T) {
	def := &SubagentDefinition{
		Name:            "  test-agent  ",
		Description:     "  Test description  ",
		Tools:           []string{" read_file ", " grep ", ""},
		DisallowedTools: []string{" bash ", ""},
	}
	def.Normalize()

	if def.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got %q", def.Name)
	}
	if def.Description != "Test description" {
		t.Errorf("expected description 'Test description', got %q", def.Description)
	}
	if len(def.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(def.Tools))
	}
	if def.Tools[0] != "read_file" {
		t.Errorf("expected first tool 'read_file', got %q", def.Tools[0])
	}
	if len(def.DisallowedTools) != 1 {
		t.Errorf("expected 1 disallowed tool, got %d", len(def.DisallowedTools))
	}
}

func TestSubagentDefinition_ToolAllowed(t *testing.T) {
	def := &SubagentDefinition{
		Tools:           []string{"read_file", "grep", "glob"},
		DisallowedTools: []string{"bash"},
	}
	def.Normalize()

	if !def.ToolAllowed("read_file") {
		t.Error("read_file should be allowed")
	}
	if !def.ToolAllowed("grep") {
		t.Error("grep should be allowed")
	}
	if def.ToolAllowed("bash") {
		t.Error("bash should be disallowed")
	}
	if def.ToolAllowed("write_file") {
		t.Error("write_file should be disallowed (not in tools list)")
	}
}

func TestSubagentDefinition_Valid(t *testing.T) {
	def := &SubagentDefinition{Name: "test"}
	if !def.Valid() {
		t.Error("definition with name should be valid")
	}

	def2 := &SubagentDefinition{Name: ""}
	if def2.Valid() {
		t.Error("definition without name should not be valid")
	}
}

func TestRegistry(t *testing.T) {
	reg := NewRegistry()

	def1 := &SubagentDefinition{Name: "agent-one", SourceScope: ScopeUser}
	def2 := &SubagentDefinition{Name: "agent-two", SourceScope: ScopeProject}
	def3 := &SubagentDefinition{Name: "Agent-One", SourceScope: ScopeBuiltin}

	reg.Add(def1)
	reg.Add(def2)
	reg.Add(def3)

	if reg.Len() != 2 {
		t.Errorf("expected 2 definitions (case-insensitive dedup), got %d", reg.Len())
	}

	got, ok := reg.Get("agent-one")
	if !ok {
		t.Error("should find agent-one")
	}
	if got.SourceScope != ScopeUser {
		t.Errorf("expected user scope (higher priority), got %s", got.SourceScope)
	}

	names := reg.Names()
	if len(names) != 2 {
		t.Errorf("expected 2 names, got %d", len(names))
	}
}

func TestLoadFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test-agent.md")
	content := `---
name: test-agent
description: A test agent
tools:
  - read_file
  - grep
model: sonnet
color: "#4CAF50"
---

You are a test agent.
Do good work.
`
	if err := os.WriteFile(testFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := LoadFromFile(testFile, LoadOptions{Scope: ScopeUser})
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if def.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got %q", def.Name)
	}
	if def.Description != "A test agent" {
		t.Errorf("expected description 'A test agent', got %q", def.Description)
	}
	if len(def.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(def.Tools))
	}
	if def.Model != "sonnet" {
		t.Errorf("expected model 'sonnet', got %q", def.Model)
	}
	if def.Color != "#4CAF50" {
		t.Errorf("expected color '#4CAF50', got %q", def.Color)
	}
	if def.SourceScope != ScopeUser {
		t.Errorf("expected scope 'user', got %q", def.SourceScope)
	}
	if !containsString(def.Prompt, "You are a test agent.") {
		t.Errorf("prompt should contain body text, got %q", def.Prompt)
	}
}

func TestBuiltinDefinitions(t *testing.T) {
	defs := BuiltinDefinitions()
	if len(defs) == 0 {
		t.Error("expected builtin definitions")
	}

	found := false
	for _, def := range defs {
		if def.Name == "explore" {
			found = true
			if def.SourceScope != ScopeBuiltin {
				t.Errorf("explore should have builtin scope, got %s", def.SourceScope)
			}
			break
		}
	}
	if !found {
		t.Error("expected 'explore' builtin agent")
	}
}

func TestRegistry_FindByDescription(t *testing.T) {
	reg := NewRegistry()
	reg.Add(&SubagentDefinition{Name: "code-reviewer", Description: "Reviews code for quality and security"})
	reg.Add(&SubagentDefinition{Name: "debugger", Description: "Debugs errors and test failures"})

	results := reg.FindByDescription("code")
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'code', got %d", len(results))
	}
	if results[0].Name != "code-reviewer" {
		t.Errorf("expected code-reviewer, got %s", results[0].Name)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
