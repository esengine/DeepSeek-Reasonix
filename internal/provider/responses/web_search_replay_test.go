package responses

import (
	"encoding/json"
	"testing"

	"reasonix/internal/provider"
)

func TestConversationDigestIncludesEnabledWebSearchReplayItems(t *testing.T) {
	messages := []provider.Message{
		{Role: provider.RoleUser, Content: "search"},
		{Role: provider.RoleAssistant, Content: "found", ResponsesItems: []json.RawMessage{
			json.RawMessage(`{"id":"ws_1","type":"web_search_call","status":"completed"}`),
		}},
	}
	searchOff := New(Config{Name: "compatible", BaseURL: "https://gateway.example", Model: "m", Mode: "stateful"}).(*client)
	searchOn := New(Config{Name: "compatible", BaseURL: "https://gateway.example", Model: "m", Mode: "stateful", WebSearch: true}).(*client)
	if searchOff.conversationDigest(messages) == searchOn.conversationDigest(messages) {
		t.Fatal("web search replay items must change the digest (wire sends completed search calls)")
	}
}
