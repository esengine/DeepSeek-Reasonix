package bot

import (
	"testing"

	"reasonix/internal/event"
)

func TestApprovalCardCarriesChatType(t *testing.T) {
	card := approvalCard(event.Approval{ID: "approval-1"}, ChatDM, "allowed-user")
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
	if value["user_id"] != "allowed-user" {
		t.Fatalf("user_id = %q, want allowed-user", value["user_id"])
	}
}

func TestApprovalCardActionsAreToolAgnostic(t *testing.T) {
	for _, approval := range []event.Approval{
		{ID: "plan-1", Tool: "exit_plan_mode", Subject: "plan"},
		{ID: "task-1", Tool: "task", Subject: "run subtask"},
	} {
		card := approvalCard(approval, ChatGroup, "allowed-user")
		if len(card.Elements) < 2 {
			t.Fatalf("%s card elements = %d, want actions", approval.Tool, len(card.Elements))
		}
		actions, ok := card.Elements[1].Extra["actions"].([]map[string]any)
		if !ok || len(actions) != 2 {
			t.Fatalf("%s actions missing or wrong type: %#v", approval.Tool, card.Elements[1].Extra["actions"])
		}
		allow, ok := actions[0]["value"].(map[string]string)
		if !ok {
			t.Fatalf("%s allow value has wrong type: %#v", approval.Tool, actions[0]["value"])
		}
		deny, ok := actions[1]["value"].(map[string]string)
		if !ok {
			t.Fatalf("%s deny value has wrong type: %#v", approval.Tool, actions[1]["value"])
		}
		if allow["command"] != "/approve "+approval.ID || deny["command"] != "/deny "+approval.ID {
			t.Fatalf("%s commands = %q/%q, want approve/deny by id", approval.Tool, allow["command"], deny["command"])
		}
	}
}

func TestAskCardAddsAnswerButtonsForSingleChoice(t *testing.T) {
	card := askCard(event.Ask{
		ID: "ask-1",
		Questions: []event.AskQuestion{{
			ID:     "q1",
			Prompt: "Choose one",
			Options: []event.AskOption{
				{Label: "允许一次"},
				{Label: "拒绝"},
			},
		}},
	}, "fallback", ChatDM, "allowed-user")

	if len(card.Elements) != 2 {
		t.Fatalf("ask card elements = %d, want markdown + actions", len(card.Elements))
	}
	actions, ok := card.Elements[1].Extra["actions"].([]map[string]any)
	if !ok || len(actions) != 2 {
		t.Fatalf("ask card actions missing or wrong type: %#v", card.Elements[1].Extra["actions"])
	}
	value, ok := actions[0]["value"].(map[string]string)
	if !ok {
		t.Fatalf("ask action value has wrong type: %#v", actions[0]["value"])
	}
	if value["command"] != "/answer ask-1 1" {
		t.Fatalf("command = %q, want /answer ask-1 1", value["command"])
	}
	if value["chat_type"] != string(ChatDM) {
		t.Fatalf("chat_type = %q, want %q", value["chat_type"], ChatDM)
	}
	if value["user_id"] != "allowed-user" {
		t.Fatalf("user_id = %q, want allowed-user", value["user_id"])
	}
}
