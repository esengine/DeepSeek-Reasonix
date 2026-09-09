package provider

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReadPauseRoundTripAndProviderIsolation(t *testing.T) {
	pause := &ReadPause{ID: "turn-receipt", Reads: []PausedRead{{ReadID: "r", Path: "file", Reason: "page_budget"}}}
	msg := Message{Role: RoleTool, ToolCallID: LocalOnlyToolID, Name: LocalOnlyToolName, LocalOnly: true, ReadPause: pause}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var restored Message
	if err := json.Unmarshal(data, &restored); err != nil || restored.ReadPause == nil || restored.ReadPause.Reads[0].Reason != "page_budget" {
		t.Fatal("lost pause on reload")
	}
	input := []Message{{Role: RoleUser, Content: "continue"}, restored, {Role: RoleAssistant, Content: "candidate", ReadPause: pause}}
	visible, _ := json.Marshal(ModelMessages(input))
	if strings.Contains(string(visible), "turn-receipt") || strings.Contains(string(visible), "read_pause") {
		t.Fatal("pause metadata leaked into provider request")
	}
	var old Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","content":"old"}`), &old); err != nil || old.ReadPause != nil {
		t.Fatal("old message compatibility")
	}
}
