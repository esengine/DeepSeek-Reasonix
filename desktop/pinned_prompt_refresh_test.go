package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/control"
	"reasonix/internal/provider"
)

type pinnedPromptProvider struct {
	requests chan provider.Request
}

func (p *pinnedPromptProvider) Name() string { return "pinned-prompt" }

func (p *pinnedPromptProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.requests <- req
	chunks := make(chan provider.Chunk, 2)
	chunks <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	chunks <- provider.Chunk{Type: provider.ChunkDone}
	close(chunks)
	return chunks, nil
}

func submitAndCapturePinnedRequest(t *testing.T, ctrl *control.Controller, requests <-chan provider.Request, input string) provider.Request {
	t.Helper()
	if err := ctrl.RunTurn(context.Background(), input); err != nil {
		t.Fatalf("RunTurn(%q): %v", input, err)
	}
	var req provider.Request
	select {
	case req = <-requests:
	default:
		t.Fatalf("provider request for %q was not recorded", input)
	}
	if len(req.Messages) == 0 || req.Messages[0].Role != provider.RoleSystem {
		t.Fatalf("request %q has no leading system message: %+v", input, req.Messages)
	}
	return req
}

func pinnedRevisionBodies(messages []provider.Message) []string {
	var out []string
	for _, message := range messages {
		if message.Role == provider.RoleUser && strings.HasPrefix(strings.TrimSpace(message.Content), "<pinned_context_revision") {
			out = append(out, message.Content)
		}
	}
	return out
}

func assertProviderRequestPrefix(t *testing.T, previous, current provider.Request) {
	t.Helper()
	if len(current.Messages) < len(previous.Messages) || !reflect.DeepEqual(current.Messages[:len(previous.Messages)], previous.Messages) {
		t.Fatalf("previous request is not an exact prefix:\nprevious: %#v\ncurrent: %#v", previous.Messages, current.Messages)
	}
}

func TestDesktopPinAPIAppendsRevisionOnlyWhenFileChanges(t *testing.T) {
	prov := &pinnedPromptProvider{requests: make(chan provider.Request, 4)}
	app, tab, ctrl, _ := pinnedConcurrencyFixture(t, prov)
	path := filepath.Join(tab.WorkspaceRoot, "context.md")
	if err := os.WriteFile(path, []byte("version one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.PinFileForTab(tab.ID, "context.md"); err != nil {
		t.Fatalf("PinFileForTab: %v", err)
	}

	first := submitAndCapturePinnedRequest(t, ctrl, prov.requests, "first")
	second := submitAndCapturePinnedRequest(t, ctrl, prov.requests, "second")
	assertProviderRequestPrefix(t, first, second)
	if first.Messages[0].Content != "BASE" || second.Messages[0].Content != "BASE" {
		t.Fatalf("pinned context changed leading system messages: first=%q second=%q", first.Messages[0].Content, second.Messages[0].Content)
	}
	if revisions := pinnedRevisionBodies(second.Messages); len(revisions) != 1 || !strings.Contains(revisions[0], "version one") {
		t.Fatalf("unchanged pinned file should retain exactly one revision: %v", revisions)
	}
	if err := os.WriteFile(path, []byte("version two"), 0o600); err != nil {
		t.Fatal(err)
	}
	third := submitAndCapturePinnedRequest(t, ctrl, prov.requests, "third")
	assertProviderRequestPrefix(t, second, third)
	if revisions := pinnedRevisionBodies(third.Messages); len(revisions) != 2 || !strings.Contains(revisions[1], "version two") {
		t.Fatalf("changed pinned file did not append a delta revision: %v", revisions)
	}
	fourth := submitAndCapturePinnedRequest(t, ctrl, prov.requests, "fourth")
	assertProviderRequestPrefix(t, third, fourth)
	if revisions := pinnedRevisionBodies(fourth.Messages); len(revisions) != 2 {
		t.Fatalf("stable post-change file appended another revision: %v", revisions)
	}
}
