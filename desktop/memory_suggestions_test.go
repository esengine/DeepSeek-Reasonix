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
