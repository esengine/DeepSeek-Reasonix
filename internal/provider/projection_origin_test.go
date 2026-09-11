package provider

import (
	"encoding/json"
	"testing"
)

func TestModelMessagesStripsOriginWithoutChangingProviderBytes(t *testing.T) {
	stored := []Message{{Role: RoleUser, Origin: MessageOriginUser, Content: "fix the bug", RawContent: "fix the bug"}}
	legacy := []Message{{Role: RoleUser, Content: "fix the bug"}}

	model := ModelMessages(stored)
	want := ModelMessages(legacy)
	gotJSON, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("origin changed provider bytes:\n got %s\nwant %s", gotJSON, wantJSON)
	}
	if stored[0].Origin != MessageOriginUser {
		t.Fatal("provider projection mutated stored origin")
	}
}
