package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/agent/testutil"
	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

type persistedLargeOutputTool struct {
	result string
}

func (persistedLargeOutputTool) Name() string { return "large_output" }

func (persistedLargeOutputTool) Description() string {
	return "returns a deterministic large result for persistence tests"
}

func (persistedLargeOutputTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }

func (t persistedLargeOutputTool) Execute(context.Context, json.RawMessage) (string, error) {
	return t.result, nil
}

func (persistedLargeOutputTool) ReadOnly() bool { return true }

func TestRunPersistsBoundedLargeToolOutputThroughSessionSave(t *testing.T) {
	const (
		head = "HEAD-MARKER\n"
		tail = "\nTAIL-MARKER"
	)
	largeResult := head + strings.Repeat("x", maxPersistedToolOutputBytes) + tail
	prov := testutil.NewMock("large-output", testutil.Turn{
		ToolCalls: []provider.ToolCall{{ID: "large-1", Name: "large_output", Arguments: `{}`}},
	}, testutil.Turn{Text: "large output handled"})
	registry := tool.NewRegistry()
	registry.Add(persistedLargeOutputTool{result: largeResult})
	session := NewSession("system")
	agent := New(prov, registry, session, Options{}, event.Discard)

	if err := agent.Run(context.Background(), "persist the large result safely"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var toolMessage provider.Message
	for _, message := range session.Snapshot() {
		if message.Role == provider.RoleTool && message.ToolCallID == "large-1" {
			toolMessage = message
			break
		}
	}
	if toolMessage.ToolCallID == "" {
		t.Fatal("session did not contain the executed large tool result")
	}
	if len(toolMessage.Content) > maxToolOutputBytes {
		t.Fatalf("model-visible tool content = %d bytes, want <= %d", len(toolMessage.Content), maxToolOutputBytes)
	}
	if !strings.Contains(toolMessage.Content, "original_bytes=") {
		t.Fatalf("model-visible tool content lacks truncation provenance: %q", toolMessage.Content[:min(len(toolMessage.Content), 300)])
	}
	if len(toolMessage.RawContent) > maxPersistedToolOutputBytes {
		t.Fatalf("persisted RawContent = %d bytes, want <= %d", len(toolMessage.RawContent), maxPersistedToolOutputBytes)
	}
	if !strings.Contains(toolMessage.RawContent, "HEAD-MARKER") || !strings.Contains(toolMessage.RawContent, "TAIL-MARKER") {
		t.Fatal("persisted RawContent lost head or tail evidence")
	}
	if !strings.Contains(toolMessage.RawContent, "persisted tool output bounded") {
		t.Fatalf("persisted RawContent lacks bounded-output provenance: %q", toolMessage.RawContent[:min(len(toolMessage.RawContent), 300)])
	}

	path := filepath.Join(t.TempDir(), "large-tool-output.jsonl")
	if err := session.SaveSnapshot(path); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	loaded, err := LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	var loadedToolMessage provider.Message
	for _, message := range loaded.Messages {
		if message.Role == provider.RoleTool && message.ToolCallID == "large-1" {
			loadedToolMessage = message
			break
		}
	}
	if loadedToolMessage.RawContent != toolMessage.RawContent {
		t.Fatalf("reloaded RawContent changed: got %d bytes, want %d", len(loadedToolMessage.RawContent), len(toolMessage.RawContent))
	}
}
