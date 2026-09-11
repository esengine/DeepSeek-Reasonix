package agent

import (
	"encoding/json"
	"os"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestBatchBarrierConsumesSharedCommandEffectMatrix(t *testing.T) {
	raw, err := os.ReadFile("../shellsafe/testdata/command_effects.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, Command string
		BatchBarrier  bool
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	reg := tool.NewRegistry()
	reg.Add(fakeTool{name: "bash", readOnly: false})
	a := New(nil, reg, NewSession(""), Options{}, event.Discard)
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"command": tc.Command})
			if err != nil {
				t.Fatal(err)
			}
			call := provider.ToolCall{ID: "failed", Name: "bash", Arguments: string(args)}
			got := batchCallMutationFailureCause(a, call, toolOutcome{errMsg: "failed"}) != nil
			if got != tc.BatchBarrier {
				t.Fatalf("batch barrier for %q = %t, want %t", tc.Command, got, tc.BatchBarrier)
			}
		})
	}
}
