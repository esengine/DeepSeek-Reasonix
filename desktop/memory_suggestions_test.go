package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/memory"
	"reasonix/internal/provider"
)

func TestMemorySuggestionsReturnsNonNilArraysBeforeStartup(t *testing.T) {
	isolateDesktopUserDirs(t)

	view := NewApp().MemorySuggestions()
	if view.Memories == nil || view.Skills == nil {
		t.Fatalf("MemorySuggestions() arrays must be non-nil before startup: %+v", view)
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatalf("marshal MemorySuggestions(): %v", err)
	}
	for _, bad := range []string{`"memories":null`, `"skills":null`} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("MemorySuggestions() JSON contains %s; frontend expects []: %s", bad, raw)
		}
	}
}

func TestMemorySuggestionsAcceptMemoryCandidate(t *testing.T) {
	isolateDesktopUserDirs(t)
	userDir := t.TempDir()
	cwd := t.TempDir()
	sessionDir := t.TempDir()
	store := memory.StoreFor(userDir, cwd)
	writeSuggestionSession(t, sessionDir, "pref.jsonl",
		provider.Message{Role: provider.RoleUser, Content: "以后请始终用中文回复，除非我明确要求英文。"},
		provider.Message{Role: provider.RoleAssistant, Content: "好的。"},
	)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{
		Memory:     &memory.Set{Store: store, CWD: cwd, UserDir: userDir},
		SessionDir: sessionDir,
	}), "test-model")
	app.tabs["test"].WorkspaceRoot = cwd

	view := app.MemorySuggestions()
	if len(view.Memories) == 0 {
		t.Fatalf("MemorySuggestions() memories = %+v, want at least one candidate", view.Memories)
	}
	path, err := app.AcceptMemorySuggestion(view.Memories[0])
	if err != nil {
		t.Fatalf("AcceptMemorySuggestion: %v", err)
	}
	if path == "" {
		t.Fatal("AcceptMemorySuggestion returned empty path")
	}
	got := store.List()
	if len(got) != 1 || !strings.Contains(got[0].Body, "中文回复") {
		t.Fatalf("saved memories = %+v, want confirmed candidate body", got)
	}
}

func TestMemorySuggestionsForTabUsesSelectedTab(t *testing.T) {
	isolateDesktopUserDirs(t)
	activeUserDir := t.TempDir()
	selectedUserDir := t.TempDir()
	activeCwd := t.TempDir()
	selectedCwd := t.TempDir()
	activeSessionDir := t.TempDir()
	selectedSessionDir := t.TempDir()
	activeStore := memory.StoreFor(activeUserDir, activeCwd)
	selectedStore := memory.StoreFor(selectedUserDir, selectedCwd)
	writeSuggestionSession(t, selectedSessionDir, "selected.jsonl",
		provider.Message{Role: provider.RoleUser, Content: "以后请始终用中文回复，除非我明确要求英文。"},
		provider.Message{Role: provider.RoleAssistant, Content: "好的。"},
	)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{
		Memory:     &memory.Set{Store: activeStore, CWD: activeCwd, UserDir: activeUserDir},
		SessionDir: activeSessionDir,
	}), "test-model")
	app.tabs["test"].WorkspaceRoot = activeCwd
	app.tabs["selected"] = &WorkspaceTab{
		ID:            "selected",
		Scope:         "project",
		WorkspaceRoot: selectedCwd,
		Ctrl: control.New(control.Options{
			Memory:     &memory.Set{Store: selectedStore, CWD: selectedCwd, UserDir: selectedUserDir},
			SessionDir: selectedSessionDir,
		}),
		Ready:       true,
		disabledMCP: map[string]ServerView{},
	}

	if view := app.MemorySuggestions(); len(view.Memories) != 0 {
		t.Fatalf("active tab suggestions = %+v, want none", view.Memories)
	}
	view := app.MemorySuggestionsForTab("selected")
	if len(view.Memories) == 0 {
		t.Fatalf("MemorySuggestionsForTab(selected) memories = %+v, want at least one candidate", view.Memories)
	}
	path, err := app.AcceptMemorySuggestionForTab("selected", view.Memories[0])
	if err != nil {
		t.Fatalf("AcceptMemorySuggestionForTab: %v", err)
	}
	if !strings.HasPrefix(path, selectedStore.Dir) && !strings.HasPrefix(path, selectedStore.GlobalDir) {
		t.Fatalf("memory path = %q, want selected store under %q or %q", path, selectedStore.Dir, selectedStore.GlobalDir)
	}
	if got := activeStore.List(); len(got) != 0 {
		t.Fatalf("active store should remain untouched, got %+v", got)
	}
	got := selectedStore.List()
	if len(got) != 1 || !strings.Contains(got[0].Body, "中文回复") {
		t.Fatalf("selected store = %+v, want confirmed candidate body", got)
	}

	skillPath, err := app.AcceptSkillSuggestionForTab("selected", SkillSuggestion{
		ID:          "selected-skill",
		Name:        "selected-workflow",
		Description: "Selected workspace workflow",
		Scope:       "project",
		Body:        "Use the selected workspace context before changing files.",
	})
	if err != nil {
		t.Fatalf("AcceptSkillSuggestionForTab: %v", err)
	}
	wantSkillPath := filepath.Join(selectedCwd, ".reasonix", "skills", "selected-workflow", "SKILL.md")
	if skillPath != wantSkillPath {
		t.Fatalf("skill path = %q, want %q", skillPath, wantSkillPath)
	}
	if _, err := os.Stat(filepath.Join(activeCwd, ".reasonix", "skills", "selected-workflow", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("active workspace should not receive selected skill, stat err = %v", err)
	}
	body, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read selected skill: %v", err)
	}
	if !strings.Contains(string(body), "selected workspace context") {
		t.Fatalf("selected skill body missing candidate content:\n%s", body)
	}
}

func TestMemorySuggestionsAcceptSkillCandidate(t *testing.T) {
	isolateDesktopUserDirs(t)
	userDir := t.TempDir()
	cwd := t.TempDir()
	sessionDir := t.TempDir()
	store := memory.StoreFor(userDir, cwd)
	writeSuggestionSession(t, sessionDir, "pr-a.jsonl",
		provider.Message{Role: provider.RoleUser, Content: "把这个 PR 合并到本地并说明主要做了什么。"},
		provider.Message{Role: provider.RoleAssistant, Content: "已检查。"},
	)
	writeSuggestionSession(t, sessionDir, "pr-b.jsonl",
		provider.Message{Role: provider.RoleUser, Content: "解决该 pr 下机器人提出来的问题，合理的问题进行修复。"},
		provider.Message{Role: provider.RoleAssistant, Content: "已处理。"},
	)

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{
		Memory:     &memory.Set{Store: store, CWD: cwd, UserDir: userDir},
		SessionDir: sessionDir,
	}), "test-model")
	app.tabs["test"].WorkspaceRoot = cwd

	view := app.MemorySuggestions()
	var candidate SkillSuggestion
	for _, item := range view.Skills {
		if item.Name == "reasonix-pr-followup" {
			candidate = item
			break
		}
	}
	if candidate.Name == "" {
		t.Fatalf("MemorySuggestions() skills = %+v, want reasonix-pr-followup", view.Skills)
	}
	path, err := app.AcceptSkillSuggestion(candidate)
	if err != nil {
		t.Fatalf("AcceptSkillSuggestion: %v", err)
	}
	wantSuffix := filepath.Join(".reasonix", "skills", "reasonix-pr-followup", "SKILL.md")
	if !strings.HasSuffix(path, wantSuffix) {
		t.Fatalf("skill path = %q, want suffix %q", path, wantSuffix)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read skill: %v", err)
	}
	if !strings.Contains(string(body), "Review or update a Reasonix GitHub PR") {
		t.Fatalf("skill body missing description: %s", body)
	}
}

func writeSuggestionSession(t *testing.T, dir, name string, messages ...provider.Message) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	sess := agent.NewSession("")
	for _, msg := range messages {
		sess.Add(msg)
	}
	if err := sess.Save(filepath.Join(dir, name)); err != nil {
		t.Fatalf("save session %s: %v", name, err)
	}
}
