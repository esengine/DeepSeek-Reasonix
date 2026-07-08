package main

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/memory"
	"reasonix/internal/memorycompiler"
)

func seedCompilerFailures(t *testing.T, workspaceRoot string, turns int, errText string) {
	t.Helper()
	rt := memorycompiler.New(config.MemoryCompilerDir(workspaceRoot))
	if rt == nil {
		t.Fatal("memory compiler dir did not resolve under isolated dirs")
	}
	for i := 0; i < turns; i++ {
		_, turn := rt.StartTurn(context.Background(), "fix a bug", nil)
		turn.RecordToolResults([]memorycompiler.ToolRecord{
			{Name: "bash", Error: errText},
			{Name: "bash", Error: errText},
		})
		turn.Finish(nil)
	}
}

func TestMemorySuggestionsIncludeCompilerCandidates(t *testing.T) {
	isolateDesktopUserDirs(t)
	userDir := t.TempDir()
	cwd := t.TempDir()
	sessionDir := t.TempDir()
	store := memory.StoreFor(userDir, cwd)
	seedCompilerFailures(t, cwd, 2, "cannot find module providing package foo")

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{
		Memory:     &memory.Set{Store: store, CWD: cwd, UserDir: userDir},
		SessionDir: sessionDir,
	}), "test-model")
	app.tabs["test"].WorkspaceRoot = cwd

	view := app.MemorySuggestions()
	var candidate MemorySuggestion
	for _, item := range view.Memories {
		if strings.HasPrefix(item.Name, "memory-v5-") {
			candidate = item
			break
		}
	}
	if candidate.ID == "" {
		t.Fatalf("memories = %+v, want a memory-v5 candidate", view.Memories)
	}
	if candidate.Type != string(memory.TypeProject) {
		t.Fatalf("candidate type = %q, want project", candidate.Type)
	}
	if !strings.Contains(candidate.Description, "cannot find module") {
		t.Fatalf("candidate description missing pattern: %q", candidate.Description)
	}
	if !strings.Contains(candidate.Reason, "Memory v5") {
		t.Fatalf("candidate reason missing provenance: %q", candidate.Reason)
	}

	path, err := app.AcceptMemorySuggestion(candidate)
	if err != nil {
		t.Fatalf("AcceptMemorySuggestion: %v", err)
	}
	if path == "" {
		t.Fatal("AcceptMemorySuggestion returned empty path")
	}
	saved := store.List()
	if len(saved) != 1 || !strings.Contains(saved[0].Body, "cannot find module") {
		t.Fatalf("saved memories = %+v, want compiler candidate body", saved)
	}

	// Once accepted, the same pattern is covered by an existing memory and
	// must not be suggested again.
	again := app.MemorySuggestions()
	for _, item := range again.Memories {
		if strings.HasPrefix(item.Name, "memory-v5-") {
			t.Fatalf("accepted pattern suggested again: %+v", item)
		}
	}
}

func TestCompilerCandidatesRequireStablePattern(t *testing.T) {
	isolateDesktopUserDirs(t)
	userDir := t.TempDir()
	cwd := t.TempDir()
	sessionDir := t.TempDir()
	store := memory.StoreFor(userDir, cwd)
	// A pattern seen in only one turn is not stable enough to suggest.
	seedCompilerFailures(t, cwd, 1, "cannot find module providing package foo")

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{
		Memory:     &memory.Set{Store: store, CWD: cwd, UserDir: userDir},
		SessionDir: sessionDir,
	}), "test-model")
	app.tabs["test"].WorkspaceRoot = cwd

	view := app.MemorySuggestions()
	for _, item := range view.Memories {
		if strings.HasPrefix(item.Name, "memory-v5-") {
			t.Fatalf("unstable pattern was suggested: %+v", item)
		}
	}
}

// TestCompilerCandidateNamesUniqueForSimilarPatterns covers the review
// finding: asciiSlug drops non-ASCII runes and truncates to 56 chars, so
// patterns differing only in CJK text (or sharing a long English prefix) used
// to collide on Name/ID — colliding IDs cross-wire the frontend's accepted
// state and colliding Names make Store.Save overwrite the earlier memory.
func TestCompilerCandidateNamesUniqueForSimilarPatterns(t *testing.T) {
	isolateDesktopUserDirs(t)
	userDir := t.TempDir()
	cwd := t.TempDir()
	sessionDir := t.TempDir()
	store := memory.StoreFor(userDir, cwd)
	// Same tool, errors differ only in CJK text: identical after asciiSlug.
	seedCompilerFailures(t, cwd, 2, "编译失败：找不到模块甲")
	seedCompilerFailures(t, cwd, 2, "编译失败：找不到模块乙")

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{
		Memory:     &memory.Set{Store: store, CWD: cwd, UserDir: userDir},
		SessionDir: sessionDir,
	}), "test-model")
	app.tabs["test"].WorkspaceRoot = cwd

	view := app.MemorySuggestions()
	var candidates []MemorySuggestion
	for _, item := range view.Memories {
		if strings.HasPrefix(item.Name, "memory-v5-") {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %+v, want both CJK-only-diff patterns", candidates)
	}
	if candidates[0].Name == candidates[1].Name || candidates[0].ID == candidates[1].ID {
		t.Fatalf("similar patterns collided on Name/ID: %q vs %q", candidates[0].Name, candidates[1].Name)
	}

	// Accepting both must persist two distinct memories, not overwrite one.
	for _, c := range candidates {
		if _, err := app.AcceptMemorySuggestion(c); err != nil {
			t.Fatalf("AcceptMemorySuggestion(%s): %v", c.Name, err)
		}
	}
	saved := store.List()
	if len(saved) != 2 {
		t.Fatalf("saved memories = %d (%+v), want 2 distinct files", len(saved), saved)
	}
}

// TestCompilerCandidateNamesStableAcrossRefreshes: the hash suffix must be
// derived from the pattern, not from ordering or randomness, so a refresh
// keeps the same ID and the frontend's accepted-state map stays valid.
func TestCompilerCandidateNamesStableAcrossRefreshes(t *testing.T) {
	isolateDesktopUserDirs(t)
	userDir := t.TempDir()
	cwd := t.TempDir()
	sessionDir := t.TempDir()
	store := memory.StoreFor(userDir, cwd)
	seedCompilerFailures(t, cwd, 2, "cannot find module providing package foo")

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{
		Memory:     &memory.Set{Store: store, CWD: cwd, UserDir: userDir},
		SessionDir: sessionDir,
	}), "test-model")
	app.tabs["test"].WorkspaceRoot = cwd

	first := app.MemorySuggestions()
	second := app.MemorySuggestions()
	firstIDs := compilerCandidateIDs(first.Memories)
	secondIDs := compilerCandidateIDs(second.Memories)
	if len(firstIDs) == 0 || strings.Join(firstIDs, ",") != strings.Join(secondIDs, ",") {
		t.Fatalf("candidate IDs changed across refreshes: %v vs %v", firstIDs, secondIDs)
	}
}

func compilerCandidateIDs(memories []MemorySuggestion) []string {
	var out []string
	for _, item := range memories {
		if strings.HasPrefix(item.Name, "memory-v5-") {
			out = append(out, item.ID)
		}
	}
	return out
}

// TestCompilerCandidateLongPrefixNamesPersistDistinctly reproduces the review
// finding: two patterns sharing a >56-char ASCII prefix get distinct generated
// Name/ID (hash suffix), but the accept path used to re-run asciiSlug, which
// truncates to 56 chars and strips the hash — so both saved to one file.
func TestCompilerCandidateLongPrefixNamesPersistDistinctly(t *testing.T) {
	isolateDesktopUserDirs(t)
	userDir := t.TempDir()
	cwd := t.TempDir()
	sessionDir := t.TempDir()
	store := memory.StoreFor(userDir, cwd)
	prefix := "cannot find module providing package example dot com slash very long prefix "
	seedCompilerFailures(t, cwd, 2, prefix+"alpha")
	seedCompilerFailures(t, cwd, 2, prefix+"beta")

	app := NewApp()
	app.setTestCtrl(control.New(control.Options{
		Memory:     &memory.Set{Store: store, CWD: cwd, UserDir: userDir},
		SessionDir: sessionDir,
	}), "test-model")
	app.tabs["test"].WorkspaceRoot = cwd

	view := app.MemorySuggestions()
	var candidates []MemorySuggestion
	for _, item := range view.Memories {
		if strings.HasPrefix(item.Name, "memory-v5-") {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %+v, want both long-prefix patterns", candidates)
	}
	for _, c := range candidates {
		if _, err := app.AcceptMemorySuggestion(c); err != nil {
			t.Fatalf("AcceptMemorySuggestion(%s): %v", c.Name, err)
		}
	}
	saved := store.List()
	if len(saved) != 2 {
		t.Fatalf("saved memories = %d, want 2 distinct files (accept path truncated the hash suffix)", len(saved))
	}
	if saved[0].Name == saved[1].Name {
		t.Fatalf("saved names collided: %q", saved[0].Name)
	}
}
