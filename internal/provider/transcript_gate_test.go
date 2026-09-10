package provider

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func transcriptGateCall(id, args string) Message {
	return Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: id, Name: "read_file", Arguments: args}}}
}

func transcriptGateResult(id string) Message {
	return Message{Role: RoleTool, ToolCallID: id, Name: "read_file", Content: "observed result"}
}

func TestValidateTranscriptValidPairingPreservesBytes(t *testing.T) {
	cases := map[string][]Message{
		"empty":        nil,
		"conversation": {{Role: RoleSystem, Content: "system"}, {Role: RoleUser, Content: "question"}},
		"parallel results reversed": {
			{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a", Name: "read_file", Arguments: ` { "path": "one" } `}, {ID: "b", Name: "read_file", Arguments: `{}`}}},
			transcriptGateResult("b"), transcriptGateResult("a"), {Role: RoleAssistant, Content: "done"},
		},
		"id reused in next round": {
			transcriptGateCall("same", `{}`), transcriptGateResult("same"),
			{Role: RoleUser, Content: "again"},
			transcriptGateCall("same", `{"path":"another"}`), transcriptGateResult("same"),
		},
	}
	for name, msgs := range cases {
		t.Run(name, func(t *testing.T) {
			before, err := json.Marshal(msgs)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateTranscript(msgs); err != nil {
				t.Fatalf("valid transcript rejected: %v", err)
			}
			after, err := json.Marshal(msgs)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("validation changed healthy provider bytes")
			}
		})
	}
}

func TestValidateTranscriptRejectsMalformedToolHistory(t *testing.T) {
	cases := map[string][]Message{
		"missing result":             {transcriptGateCall("a", `{}`)},
		"result after user boundary": {transcriptGateCall("a", `{}`), {Role: RoleUser, Content: "next"}, transcriptGateResult("a")},
		"orphan result":              {transcriptGateResult("a")},
		"duplicate result":           {transcriptGateCall("a", `{}`), transcriptGateResult("a"), transcriptGateResult("a")},
		"wrong result id":            {transcriptGateCall("a", `{}`), transcriptGateResult("b")},
		"duplicate parallel id":      {{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "a", Name: "read_file", Arguments: `{}`}, {ID: "a", Name: "read_file", Arguments: `{}`}}}, transcriptGateResult("a")},
		"empty call id":              {transcriptGateCall("", `{}`), transcriptGateResult("")},
	}
	for _, args := range []string{"", `{"path":`, `[]`, `null`, `"text"`, `true`, `42`, `{} {}`} {
		cases["arguments "+args] = []Message{transcriptGateCall("a", args), transcriptGateResult("a")}
	}
	for name, msgs := range cases {
		t.Run(name, func(t *testing.T) {
			before, _ := json.Marshal(msgs)
			if err := ValidateTranscript(msgs); err == nil {
				t.Fatal("malformed transcript accepted")
			}
			after, _ := json.Marshal(msgs)
			if !bytes.Equal(before, after) {
				t.Fatal("validation repaired/mutated canonical history")
			}
		})
	}
}

func TestTranscriptGateModelProjectionHidesRecoveryArguments(t *testing.T) {
	const secret = "RECOVERY-RAW-ARGUMENT-ONLY"
	recovery := &InterruptedTurnRecovery{Pending: true, ToolCalls: []ToolCallRecord{{
		Identity:  ActionIdentity{CallID: "interrupted", CanonicalTool: "write_file"},
		Arguments: json.RawMessage(`{"content":"` + secret + `"}`), State: ToolRunUnknown,
	}}}
	for _, localOnly := range []bool{false, true} {
		name := "visible message metadata"
		if localOnly {
			name = "local recovery sentinel"
		}
		t.Run(name, func(t *testing.T) {
			stored := []Message{{Role: RoleUser, Content: "continue", LocalOnly: localOnly, InterruptedTurn: recovery}}
			before, _ := json.Marshal(stored)
			model := ModelMessages(stored)
			wire, err := json.Marshal(model)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), secret) || strings.Contains(string(wire), "interrupted_turn") {
				t.Fatalf("provider projection leaked recovery metadata: %s", wire)
			}
			if err := ValidateTranscript(model); err != nil {
				t.Fatalf("projected transcript rejected: %v", err)
			}
			after, _ := json.Marshal(stored)
			if !bytes.Equal(before, after) {
				t.Fatal("projection deleted local recovery evidence")
			}
		})
	}
}
