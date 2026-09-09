package control

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestPinnedContextNeverChangesBasePrompt(t *testing.T) {
	dir := t.TempDir()
	exec := agent.New(nil, nil, agent.NewSession("legacy composed system"), agent.Options{}, event.Discard)
	ctrl := New(Options{
		Runner:       exec,
		Executor:     exec,
		SystemPrompt: "BASE",
		SessionDir:   dir,
		SessionPath:  filepath.Join(dir, "session.jsonl"),
		Sink:         event.Discard,
	})
	if got := controlSystemMessage(ctrl.History()); got != "BASE" {
		t.Fatalf("migrated system prompt = %q", got)
	}
	if reasons := exec.Session().DrainContentRewriteReasons(); !slices.Contains(reasons, "legacy_pinned_system_migration") {
		t.Fatalf("migration reasons = %v", reasons)
	}

	ctrl.ApplyExtensionSystemPrompt("EXTENSION")
	if got := ctrl.SystemPrompt(); got != "EXTENSION" {
		t.Fatalf("SystemPrompt = %q", got)
	}
	if got := controlSystemMessage(ctrl.History()); got != "EXTENSION" {
		t.Fatalf("extension system prompt = %q", got)
	}

	if err := ctrl.NewSession(); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if got := controlSystemMessage(ctrl.History()); got != "EXTENSION" {
		t.Fatalf("new session system prompt = %q", got)
	}
}

func TestPinnedContextLoaderAppendsAtAdmittedTurns(t *testing.T) {
	prov := &recordingProvider{streams: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "one"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "two"}, {Type: provider.ChunkDone}},
		{{Type: provider.ChunkText, Text: "three"}, {Type: provider.ChunkDone}},
	}}
	exec := agent.New(prov, nil, agent.NewSession("BASE"), agent.Options{}, event.Discard)
	content := "A"
	loads := 0
	sessionPath := filepath.Join(t.TempDir(), "session.jsonl")
	ctrl := New(Options{
		Runner:       exec,
		Executor:     exec,
		SystemPrompt: "BASE",
		SessionPath:  sessionPath,
		PinnedContextLoader: func(_ context.Context, path string) (agent.PinnedContextSnapshot, error) {
			loads++
			if path != sessionPath {
				t.Fatalf("loader path = %q", path)
			}
			return agent.PinnedContextSnapshot{Files: []agent.PinnedContextFile{{Path: "a.md", Content: content}}}, nil
		},
		Sink: event.Discard,
	})
	if err := ctrl.Run(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Run(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}
	content = "B"
	if err := ctrl.Run(context.Background(), "third"); err != nil {
		t.Fatal(err)
	}
	if loads != 3 {
		t.Fatalf("loader calls = %d", loads)
	}
	if got := controlSystemMessage(ctrl.History()); got != "BASE" {
		t.Fatalf("system prompt changed: %q", got)
	}
	revisions := 0
	for _, message := range ctrl.History() {
		if agent.IsPinnedContextRevision(message) {
			revisions++
		}
	}
	if revisions != 2 {
		t.Fatalf("revision messages = %d, want 2", revisions)
	}
	if len(prov.requests) != 3 {
		t.Fatalf("provider requests = %d", len(prov.requests))
	}
	for i := 1; i < len(prov.requests); i++ {
		previous := prov.requests[i-1].Messages
		current := prov.requests[i].Messages
		if len(current) < len(previous) || !reflect.DeepEqual(current[:len(previous)], previous) {
			t.Fatalf("request %d is not prefixed by request %d", i, i-1)
		}
	}
}
