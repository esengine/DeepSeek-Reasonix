package control

import (
	"slices"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Bind stable local message IDs before publishing a result. Provider call IDs
// can be reused in later turns; an ambiguous source stays unavailable.
func bindCompletionLogSources(receipt *event.CompletionReceipt, messages []provider.Message) *event.CompletionReceipt {
	if receipt == nil {
		return nil
	}
	out := *receipt
	out.Verifications = append([]event.ReceiptVerification(nil), receipt.Verifications...)
	sources := make(map[string]string)
	for _, message := range messages {
		if message.Role != provider.RoleTool || message.ToolCallID == "" {
			continue
		}
		if _, exists := sources[message.ToolCallID]; exists {
			sources[message.ToolCallID] = ""
		} else {
			sources[message.ToolCallID] = message.ID
		}
	}
	for i := range out.Verifications {
		out.Verifications[i].ToolResultID = sources[out.Verifications[i].ToolCallID]
	}
	return &out
}

// ToolResultData holds the full arguments and output for one tool call, loaded
// on demand when a frontend expands a collapsed tool card.
type ToolResultData struct {
	Args      string                  `json:"args"`
	Output    string                  `json:"output"`
	Execution *provider.ToolExecution `json:"execution,omitempty"`
	// MCPApp is the optional Apps presentation for inline rendering.
	MCPApp *provider.MCPAppPresentation `json:"mcpApp,omitempty"`
}

// ToolResult looks up a tool call by its ID in the session history and returns
// the full arguments + output that were elided from the frontend's items[].
// Returns nil when the tool ID isn't found (e.g. a sub-agent's tool call that
// lives in a different session).
func (c *Controller) ToolResult(toolID string) *ToolResultData {
	if c.executor == nil {
		return nil
	}
	return lookupToolResult(c.executor.Session().Snapshot(), toolID)
}

func lookupToolResult(msgs []provider.Message, toolID string) *ToolResultData {
	if toolID == "" {
		return nil
	}
	// Search backwards: tool result first (most recent), then find the args
	// from the preceding assistant turn.
	for i, msg := range slices.Backward(msgs) {
		if msg.Role != provider.RoleTool || msg.ToolCallID != toolID {
			continue
		}
		out := &ToolResultData{
			Args:      "",
			Output:    msg.Content,
			Execution: msg.ToolExecution,
			MCPApp:    msg.MCPApp,
		}
		// Walk back to find the assistant turn that issued this call.
		for j := i; j >= 0; j-- {
			if msgs[j].Role != provider.RoleAssistant {
				continue
			}
			for _, tc := range msgs[j].ToolCalls {
				if tc.ID == toolID {
					out.Args = tc.Arguments
					return out
				}
			}
		}
		return out
	}
	for _, msg := range slices.Backward(msgs) {
		if msg.Role != provider.RoleAssistant {
			continue
		}
		for _, search := range msg.ServerSearch {
			if search.ID != toolID {
				continue
			}
			return &ToolResultData{
				Args:   provider.FormatServerSearchArgs(search.Query),
				Output: provider.ServerSearchDisplayOutput(search),
			}
		}
	}
	return nil
}
