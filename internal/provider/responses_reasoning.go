package provider

import "encoding/json"

func IsReplayableResponsesReasoning(raw json.RawMessage) bool {
	var item struct {
		Type      string          `json:"type"`
		Status    string          `json:"status"`
		Content   json.RawMessage `json:"content"`
		Summary   json.RawMessage `json:"summary"`
		Encrypted string          `json:"encrypted_content"`
	}
	if json.Unmarshal(raw, &item) != nil || item.Type != "reasoning" || item.Status == "in_progress" || item.Status == "incomplete" {
		return false
	}
	return item.Encrypted != "" || len(item.Content) > 0 && string(item.Content) != "null" || len(item.Summary) > 0 && string(item.Summary) != "null"
}

// UpsertResponsesItem lets a completed response replace an earlier item snapshot
// without replaying two copies of the same provider-issued reasoning ID.
func UpsertResponsesItem(items []json.RawMessage, raw json.RawMessage) []json.RawMessage {
	var item struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(raw, &item)
	if item.ID != "" {
		for i, old := range items {
			var previous struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(old, &previous)
			if previous.ID == item.ID {
				items[i] = append(json.RawMessage(nil), raw...)
				return items
			}
		}
	}
	return append(items, append(json.RawMessage(nil), raw...))
}

// WithoutResponsesReasoning detaches opaque proofs after an extension replaces
// the reasoning they authenticated; server-search replay items remain intact.
func WithoutResponsesReasoning(items []json.RawMessage) []json.RawMessage {
	var out []json.RawMessage
	for _, item := range items {
		var header struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(item, &header)
		if header.Type != "reasoning" {
			out = append(out, item)
		}
	}
	return out
}
