package main

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

func TestHistoryMessagesRestorePendingFinalReadinessAction(t *testing.T) {
	marker := provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName, LocalOnly: true,
		FinalReadinessRecovery: &provider.FinalReadinessRecovery{Pending: true, Missing: []string{"verification", "review"}},
	}
	got := historyMessages([]provider.Message{
		{Role: provider.RoleUser, Content: "fix it"},
		{Role: provider.RoleAssistant, Content: "partial result"},
		marker,
	}, strings.TrimSpace)
	if len(got) != 3 || got[2].Role != "notice" || got[2].Code != event.NoticeCodeFinalReadiness || !got[2].Pending || got[2].Readiness == nil {
		t.Fatalf("pending readiness history row = %+v", got)
	}
	if missing := got[2].Readiness.Missing; len(missing) != 2 || missing[0] != "verification" || missing[1] != "review" {
		t.Fatalf("pending readiness missing = %v", missing)
	}
	wired, err := json.Marshal(got[2])
	if err != nil {
		t.Fatalf("marshal pending readiness: %v", err)
	}
	if text := string(wired); !strings.Contains(text, `"readiness":{"attempts":1,"missing":["verification","review"]}`) || strings.Contains(text, `"Attempts"`) {
		t.Fatalf("pending readiness JSON = %s, want frontend-compatible camel case", text)
	}

	marker.FinalReadinessRecovery.Pending = false
	consumed := historyMessages([]provider.Message{{Role: provider.RoleUser, Content: "fix it"}, marker}, strings.TrimSpace)
	if len(consumed) != 1 {
		t.Fatalf("consumed readiness marker should be hidden: %+v", consumed)
	}
}
