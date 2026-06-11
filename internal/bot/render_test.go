package bot

import (
	"testing"

	"reasonix/internal/event"
)

func TestApprovalCardCarriesChatType(t *testing.T) {
	card := approvalCard(event.Approval{ID: "approval-1"}, ChatDM)
	if len(card.Elements) < 2 {
		t.Fatalf("approval card elements = %d, want at least 2", len(card.Elements))
	}
	actions, ok := card.Elements[1].Extra["actions"].([]map[string]any)
	if !ok || len(actions) == 0 {
		t.Fatalf("approval card actions missing or wrong type: %#v", card.Elements[1].Extra["actions"])
	}
	value, ok := actions[0]["value"].(map[string]string)
	if !ok {
		t.Fatalf("approval action value has wrong type: %#v", actions[0]["value"])
	}
	if value["command"] != "/approve approval-1" {
		t.Fatalf("command = %q, want /approve approval-1", value["command"])
	}
	if value["chat_type"] != string(ChatDM) {
		t.Fatalf("chat_type = %q, want %q", value["chat_type"], ChatDM)
	}
}
