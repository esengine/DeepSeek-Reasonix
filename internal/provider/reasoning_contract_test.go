package provider

import (
	"encoding/json"
	"reflect"
	"testing"
)

type contractProvider struct {
	replayProjectionProvider
	caps ReasoningReplayCapabilities
}

func (p contractProvider) ReasoningReplayCapabilities() ReasoningReplayCapabilities { return p.caps }

func TestReasoningContractsDistinguishMissingEmptyAndUnsafe(t *testing.T) {
	call := []ToolCall{{ID: "c", Name: "write_file"}}
	for _, tc := range []struct {
		name     string
		caps     ReasoningReplayCapabilities
		fallback bool
		msg      Message
		want     bool
	}{
		{"signed-empty", ReasoningReplayCapabilities{RequireSignature: true}, false, Message{ReasoningSignature: "proof", ReasoningState: ReasoningEmpty}, true},
		{"native-missing-signature", ReasoningReplayCapabilities{RequireSignature: true}, false, Message{ReasoningContent: "thought"}, false},
		{"deepseek-anthropic-empty", ReasoningReplayCapabilities{Format: "anthropic-thinking"}, false, Message{ReasoningState: ReasoningEmpty}, false},
		{"chat-empty", ReasoningReplayCapabilities{Format: "chat-completions"}, true, Message{ReasoningState: ReasoningEmpty}, true},
		{"chat-truncated", ReasoningReplayCapabilities{Format: "chat-completions"}, true, Message{ReasoningContent: "cut", ReasoningState: ReasoningTruncated}, false},
		{"responses-incomplete", ReasoningReplayCapabilities{Format: "responses-items"}, true, Message{ReasoningState: ReasoningIncomplete}, false},
		{"legacy-reasoning", ReasoningReplayCapabilities{}, false, Message{ReasoningContent: "complete"}, true},
		{"unknown-missing", ReasoningReplayCapabilities{}, false, Message{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := contractProvider{replayProjectionProvider{allowEmpty: tc.fallback}, tc.caps}
			tc.msg.Role = RoleAssistant
			tc.msg.ToolCalls = call
			if got := CanReplayAssistantMessage(p, tc.msg); got != tc.want {
				t.Fatalf("replay=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestReplayMetadataLegacyAndProjection(t *testing.T) {
	var old Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"old"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.ReasoningState != "" || len(old.ThinkingBlocks) > 0 {
		t.Fatal("legacy defaults changed")
	}
	original := Message{Role: RoleAssistant, ReasoningState: ReasoningComplete, ThinkingBlocks: []ThinkingBlock{{Type: "thinking", Signature: "one"}, {Type: "redacted_thinking", Data: "opaque"}}}
	raw, _ := json.Marshal(original)
	var reloaded Message
	if err := json.Unmarshal(raw, &reloaded); err != nil || !reflect.DeepEqual(original, reloaded) {
		t.Fatalf("roundtrip: %s %v", raw, err)
	}
	p := contractProvider{caps: ReasoningReplayCapabilities{RequireSignature: true}}
	original.ToolCalls = []ToolCall{{ID: "c", Name: "read_file"}}
	msgs := []Message{original}
	if got, changed := ProjectReplaySafeMessages(p, msgs); changed || &got[0] != &msgs[0] {
		t.Fatal("healthy signed blocks changed")
	}
	tool := Message{Role: RoleTool, Content: "done", ToolRunState: ToolRunCompleted}
	if ModelMessages([]Message{tool})[0].ToolRunState != "" || ProjectionMessages([]Message{tool})[0].ToolRunState != ToolRunCompleted {
		t.Fatal("execution state projection wrong")
	}
}

func TestLegacyInterruptedResultIsUnknown(t *testing.T) {
	if ToolResultRunState(Message{Content: interruptedToolResult}) != ToolRunUnknown {
		t.Fatal("synthetic missing result must not count completed")
	}
}
