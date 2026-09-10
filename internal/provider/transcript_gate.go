package provider

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateTranscript validates a request view without changing history or
// inventing tool results. IDs are scoped to one assistant batch.
func ValidateTranscript(msgs []Message) error {
	pending := map[string]string{}
	for i, m := range msgs {
		if m.Role != RoleTool && len(pending) != 0 {
			return fmt.Errorf("transcript gate: unanswered tool calls before message %d", i)
		}
		for _, c := range m.ToolCalls {
			if m.Role != RoleAssistant || strings.TrimSpace(c.ID) == "" || strings.TrimSpace(c.Name) == "" {
				return fmt.Errorf("transcript gate: invalid tool identity at message %d", i)
			}
			if _, exists := pending[c.ID]; exists {
				return fmt.Errorf("transcript gate: duplicate call ID at message %d", i)
			}
			var args map[string]json.RawMessage
			if json.Unmarshal([]byte(c.Arguments), &args) != nil || args == nil {
				return fmt.Errorf("transcript gate: tool arguments must be a JSON object at message %d", i)
			}
			pending[c.ID] = c.Name
		}
		if m.Role == RoleTool {
			name, found := pending[m.ToolCallID]
			if !found || (m.Name != "" && m.Name != name) {
				return fmt.Errorf("transcript gate: orphan or mismatched result at message %d", i)
			}
			delete(pending, m.ToolCallID)
		}
	}
	if len(pending) > 0 {
		return fmt.Errorf("transcript gate: unanswered tool calls")
	}
	return nil
}

// ValidateModelTranscript uses the adapters' pairing-normalized view. Invalid
// arguments are rejected before normalizers can silently replace them.
func ValidateModelTranscript(msgs []Message) error {
	view := ModelMessages(msgs)
	for i, m := range view {
		for _, c := range m.ToolCalls {
			var args map[string]json.RawMessage
			if json.Unmarshal([]byte(c.Arguments), &args) != nil || args == nil {
				return fmt.Errorf("transcript gate: tool arguments must be a JSON object at message %d", i)
			}
		}
	}
	view = append([]Message(nil), SanitizeToolPairing(view)...)
	// Some compatible gateways stream by index and omit IDs. Validate using
	// request-local positional identities, retaining their existing wire path.
	var empty []ToolCall
	for i := range view {
		m := &view[i]
		if len(m.ToolCalls) > 0 {
			empty = nil
			m.ToolCalls = append([]ToolCall(nil), m.ToolCalls...)
			for j := range m.ToolCalls {
				if m.ToolCalls[j].ID == "" {
					m.ToolCalls[j].ID = fmt.Sprintf("legacy-%d-%d", i, j)
					empty = append(empty, m.ToolCalls[j])
				}
			}
		} else if m.Role == RoleTool && m.ToolCallID == "" {
			for j, c := range empty {
				if c.Name == m.Name {
					m.ToolCallID = c.ID
					empty = append(empty[:j], empty[j+1:]...)
					break
				}
			}
		}
	}
	return ValidateTranscript(view)
}

// RepairRejectedArguments changes only the outbound copy of calls the host
// proved never ran. Their validation-error result remains available for the
// model to correct its next proposal; stored arguments remain inspectable.
func RepairRejectedArguments(msgs []Message) []Message {
	out := append([]Message(nil), msgs...)
	for i, m := range out {
		if m.Role != RoleAssistant {
			continue
		}
		for j, c := range m.ToolCalls {
			var args map[string]json.RawMessage
			if json.Unmarshal([]byte(c.Arguments), &args) == nil && args != nil {
				continue
			}
			for k := i + 1; k < len(out) && out[k].Role == RoleTool; k++ {
				r := out[k]
				if r.ToolCallID == c.ID && r.Name == c.Name && (r.ToolRunState == ToolRunNotStarted || r.ToolRunState == ToolRunCancelled) {
					out[i].ToolCalls = append([]ToolCall(nil), out[i].ToolCalls...)
					out[i].ToolCalls[j].Arguments = "{}"
					break
				}
			}
		}
	}
	return out
}
