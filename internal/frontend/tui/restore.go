package tui

import "reasonix/internal/contract/eventwire"

// Restore replaces the transcript with the session as /history records it.
// The record carries no error field for a call beyond toolFailed and nothing
// about how its turns ended, so a restored call has an outcome only where the
// record says it failed, and the last turn reads as neither open nor finished.
func (t *Transcript) Restore(msgs []HistoryMessage) {
	t.Items, t.awaiting = t.Items[:0], nil
	t.Running, t.Terminal, t.EndReason = false, TurnOpen, ""
	calls := map[string]int{}
	for _, m := range msgs {
		switch m.Role {
		case "user":
			// A line the host composed is not something the person said.
			if m.HostAuthored || m.Content == "" {
				continue
			}
			t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemUser, Text: m.Content, Steer: m.Steer})
		case "assistant":
			if m.Content != "" || m.Reasoning != "" {
				t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemSay, Text: m.Content, Reasoning: m.Reasoning, Done: true, Framed: true, ThoughtMs: m.ThoughtMs})
			}
			for _, c := range m.ToolCalls {
				if c.ID != "" {
					calls[c.ID] = len(t.Items)
				}
				t.Items = append(t.Items, Item{ID: t.id(), Kind: ItemTool, Tool: &eventwire.Tool{ID: c.ID, Name: c.Name, Args: c.Arguments}})
			}
		case "tool":
			at, ok := calls[m.ToolCallID]
			if !ok {
				continue
			}
			call := *t.Items[at].Tool
			call.Output = m.Content
			call.OutputDiff = m.OutputDiff
			if m.ToolFailed {
				call.Err = m.Content
			}
			t.Items[at].Tool = &call
		}
	}
}
