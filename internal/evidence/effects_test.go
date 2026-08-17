package evidence

import (
	"encoding/json"
	"os"
	"testing"
)

func TestEvidenceConsumesSharedCommandEffectMatrix(t *testing.T) {
	raw, err := os.ReadFile("../shellsafe/testdata/command_effects.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Command                      string
		Certainty                          string
		TaskPolicyBlocked, ContentMutation bool
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			args, _ := json.Marshal(map[string]string{"command": tc.Command})
			got := ClassifyToolCall("bash", args, false)
			if got.Known != (tc.Certainty == "known") || got.StateMutation != tc.TaskPolicyBlocked || got.ContentMutation != tc.ContentMutation {
				t.Fatalf("ClassifyToolCall(%q) = %+v, matrix=%+v", tc.Command, got, tc)
			}
		})
	}
}

func TestClassifyToolCallSeparatesMutationDomains(t *testing.T) {
	tests := []struct {
		name, tool, args string
		readOnly         bool
		want             ToolEffects
	}{
		{"branch listing", "bash", `{"command":"git branch -a"}`, false, ToolEffects{Known: true}},
		{"tag creation", "bash", `{"command":"git tag v1.2.3"}`, true, ToolEffects{Known: true, StateMutation: true, WorkspaceMutation: true, RepositoryMutation: true}},
		{"pure commit", "bash", `{"command":"git commit -q -m checkpoint"}`, false, ToolEffects{Known: true, StateMutation: true, WorkspaceMutation: true, RepositoryMutation: true}},
		{"commit all", "bash", `{"command":"git commit -am checkpoint"}`, false, ToolEffects{Known: true, StateMutation: true, WorkspaceMutation: true, ContentMutation: true, RepositoryMutation: true}},
		{"host clock", "bash", `{"command":"date --set tomorrow"}`, false, ToolEffects{Known: true, StateMutation: true}},
		{"audit fix", "bash", `{"command":"npm audit fix"}`, false, ToolEffects{Known: true, StateMutation: true, WorkspaceMutation: true, ContentMutation: true}},
		{"verification", "bash", `{"command":"go test ./..."}`, false, ToolEffects{Known: true}},
		{"env prefixed test", "bash", `{"command":"GOROOT=/x go test ./..."}`, false, ToolEffects{Known: true}},
		{"unknown shell", "bash", `{"command":"custom-tool --run"}`, false, ToolEffects{StateMutation: true, WorkspaceMutation: true, ContentMutation: true}},
		{"trusted reader", "read_file", `{}`, true, ToolEffects{Known: true}},
		{"session title", "set_session_title", `{"title":"Current task"}`, false, ToolEffects{Known: true, StateMutation: true, Reason: "host session metadata write"}},
		{"generic writer", "edit_file", `{}`, false, ToolEffects{Known: true, StateMutation: true, WorkspaceMutation: true, ContentMutation: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyToolCall(tt.tool, json.RawMessage(tt.args), tt.readOnly)
			got.Reason, tt.want.Reason = "", ""
			if got != tt.want {
				t.Fatalf("ClassifyToolCall(%q, %s) = %+v, want %+v", tt.tool, tt.args, got, tt.want)
			}
		})
	}
}

func TestReceiptMutationTracksContentNotRepositoryCement(t *testing.T) {
	pure := ReceiptFromToolCall("bash", json.RawMessage(`{"command":"git commit -m checkpoint"}`), true, false)
	all := ReceiptFromToolCall("bash", json.RawMessage(`{"command":"git commit -am checkpoint"}`), true, false)
	if pure.Mutation || !all.Mutation {
		t.Fatalf("commit receipt domains: pure=%+v all=%+v", pure, all)
	}
}
