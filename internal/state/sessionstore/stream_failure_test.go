package sessionstore

import (
	"path/filepath"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestInterruptedStreamFailureSurvivesSaveLoad(t *testing.T) {
	for _, cause := range []provider.StreamFailureCause{"", provider.StreamFailureInvalidFunctionCall, provider.StreamFailureUnfinishedFunctionCall} {
		t.Run(string(cause), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			s := NewSession("system")
			s.Add(provider.Message{Role: provider.RoleUser, Content: "run"})
			s.Add(provider.Message{Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName, LocalOnly: true, InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true, StreamFailure: cause}})
			if err := s.Save(path); err != nil {
				t.Fatal(err)
			}
			restored, err := LoadSession(path)
			if err != nil {
				t.Fatal(err)
			}
			msgs := restored.Snapshot()
			last := msgs[len(msgs)-1]
			if !last.LocalOnly || last.InterruptedTurn == nil || last.InterruptedTurn.StreamFailure != cause || !last.InterruptedTurn.Pending {
				t.Fatalf("recovery=%+v", last)
			}
			for _, m := range provider.ModelMessages(msgs) {
				if m.InterruptedTurn != nil {
					t.Error("local recovery leaked directly into model history")
				}
			}
		})
	}
}
