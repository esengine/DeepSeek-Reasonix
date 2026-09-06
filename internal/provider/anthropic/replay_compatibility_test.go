package anthropic

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestAnthropicCompatibilityPrecedesRecovery(t *testing.T) {
	native := &client{nativeAnthropic: true, thinking: "adaptive"}
	gateway := &client{model: "deepseek-v4-flash", thinking: "adaptive"}
	deepseek := &client{deepseek: true, thinking: "enabled"}
	plain := provider.Message{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "observed thought", ReasoningState: provider.ReasoningComplete}
	toolTurn := plain
	toolTurn.ToolCalls = []provider.ToolCall{{ID: "one", Name: "read_file", Arguments: `{}`}}
	missing := toolTurn
	missing.ReasoningContent = ""
	missing.ReasoningState = provider.ReasoningEmpty
	incomplete := plain
	incomplete.ReasoningState = provider.ReasoningIncomplete
	truncated := plain
	truncated.ReasoningState = provider.ReasoningTruncated
	for _, tc := range []struct {
		name     string
		c        *client
		m        provider.Message
		complete bool
		want     provider.ReplayDecision
	}{
		{"native plain unsigned", native, plain, true, provider.ReplayCompatible},
		{"native unsigned tool", native, toolTurn, true, provider.ReplayRecover},
		{"native missing tool", native, missing, true, provider.ReplayRecover},
		{"gateway unsigned tool", gateway, toolTurn, true, provider.ReplayDirect},
		{"gateway missing tool", gateway, missing, true, provider.ReplayDirect},
		{"deepseek unsigned tool", deepseek, toolTurn, true, provider.ReplayDirect},
		{"deepseek missing tool", deepseek, missing, true, provider.ReplayRecover},
		{"native incomplete", native, incomplete, true, provider.ReplayReject},
		{"native truncated", native, truncated, true, provider.ReplayReject},
		{"native unfinished response", native, plain, false, provider.ReplayReject},
		{"gateway incomplete", gateway, incomplete, true, provider.ReplayReject},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := provider.DecideReasoningReplay(tc.c, tc.m, tc.complete); got != tc.want {
				t.Fatalf("decision=%s want=%s", got, tc.want)
			}
		})
	}
}

func TestNativeUnsignedTextConversionPreservesCanonicalHistory(t *testing.T) {
	c := &client{nativeAnthropic: true, thinking: "adaptive"}
	raw := []provider.Message{{Role: provider.RoleAssistant, Content: "answer", ReasoningContent: "one two", ReasoningState: provider.ReasoningComplete, ThinkingBlocks: []provider.ThinkingBlock{{Type: "thinking", Thinking: "one"}, {Type: "thinking", Thinking: "two"}}}}
	before, _ := json.Marshal(raw)
	projected, changed := provider.ProjectReplaySafeMessages(c, raw)
	if !changed || len(projected) != 1 || projected[0].Content != "one\n\ntwo\n\nanswer" || projected[0].ReasoningContent != "" || len(projected[0].ThinkingBlocks) != 0 {
		t.Fatalf("projection=%+v changed=%v", projected, changed)
	}
	after, _ := json.Marshal(raw)
	if string(before) != string(after) {
		t.Fatal("conversion rewrote canonical history")
	}
	again, changed := provider.ProjectReplaySafeMessages(c, projected)
	if changed || &again[0] != &projected[0] {
		t.Fatal("conversion is not idempotent")
	}
	req := c.buildRequest(context.Background(), provider.Request{Messages: raw})
	encoded, _ := json.Marshal(req.Messages)
	if strings.Contains(string(encoded), `"type":"thinking"`) || !strings.Contains(string(encoded), `one\n\ntwo\n\nanswer`) {
		t.Fatalf("wire=%s", encoded)
	}
}

func TestGatewayReplayPreservesActualBlocksWithoutFabricatingSignature(t *testing.T) {
	for _, mode := range []string{"adaptive", "enabled"} {
		c := &client{thinking: mode}
		raw := []provider.Message{{Role: provider.RoleAssistant, ReasoningContent: "received", ThinkingBlocks: []provider.ThinkingBlock{{Type: "thinking", Thinking: "received"}}, ToolCalls: []provider.ToolCall{{ID: "c", Name: "read_file", Arguments: `{}`}}}}
		if c.ReasoningReplayCapabilities().RequireSignature {
			t.Fatal("gateway inferred Claude signature contract")
		}
		projected, changed := provider.ProjectReplaySafeMessages(c, raw)
		if changed || &projected[0] != &raw[0] {
			t.Fatal("healthy gateway history changed")
		}
		req := c.buildRequest(context.Background(), provider.Request{Messages: raw})
		encoded, _ := json.Marshal(req.Messages)
		if !strings.Contains(string(encoded), `"thinking":"received"`) || strings.Contains(string(encoded), `"signature"`) {
			t.Fatalf("wire=%s", encoded)
		}
	}
}

func TestSignedAndRedactedHistoryNeverUsesTextConversion(t *testing.T) {
	c := &client{nativeAnthropic: true, thinking: "adaptive"}
	blocks := []provider.ThinkingBlock{{Type: "thinking", Signature: "proof"}, {Type: "redacted_thinking", Data: "opaque"}}
	raw := []provider.Message{{Role: provider.RoleAssistant, ThinkingBlocks: blocks}}
	projected, changed := provider.ProjectReplaySafeMessages(c, raw)
	if changed || &projected[0] != &raw[0] {
		t.Fatal("healthy signed prefix changed")
	}
	replayed := c.replayReasoningBlocks(raw[0])
	if len(replayed) != 2 || replayed[0].Signature != "proof" || replayed[1].Data != "opaque" {
		t.Fatalf("replay=%+v", replayed)
	}
	mixed := raw[0]
	mixed.ThinkingBlocks = append(append([]provider.ThinkingBlock(nil), blocks...), provider.ThinkingBlock{Type: "thinking", Thinking: "unsigned"})
	if _, ok := c.ConvertReasoningReplay(mixed); ok {
		t.Fatal("mixed proof was flattened")
	}
	if !reflect.DeepEqual(raw[0].ThinkingBlocks, blocks) {
		t.Fatal("raw blocks changed")
	}
}

func TestNilCompatibilityAdapterKeepsHistory(t *testing.T) {
	var c *client
	raw := []provider.Message{{Role: provider.RoleAssistant, ReasoningContent: "legacy"}}
	projected, changed := provider.ProjectReplaySafeMessages(c, raw)
	if changed || &projected[0] != &raw[0] {
		t.Fatal("nil adapter changed history")
	}
}
