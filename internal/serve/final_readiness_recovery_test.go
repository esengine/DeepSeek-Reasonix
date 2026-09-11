package serve

import (
	"testing"

	"reasonix/internal/provider"
)

func TestHistoryMessagesExposeOnlyPendingFinalReadinessRecovery(t *testing.T) {
	marker := provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName, LocalOnly: true,
		FinalReadinessRecovery: &provider.FinalReadinessRecovery{Pending: true, Missing: []string{"verification"}},
	}
	got := historyMessages([]provider.Message{{Role: provider.RoleUser, Content: "fix it"}, marker})
	if len(got) != 2 || got[1].Role != "final_readiness" || len(got[1].Missing) != 1 || got[1].Missing[0] != "verification" {
		t.Fatalf("pending readiness history = %+v", got)
	}
	marker.FinalReadinessRecovery.Pending = false
	got = historyMessages([]provider.Message{{Role: provider.RoleUser, Content: "fix it"}, marker})
	if len(got) != 1 {
		t.Fatalf("consumed readiness marker should be hidden: %+v", got)
	}
}
