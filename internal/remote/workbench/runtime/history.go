package runtime

import (
	"fmt"
	"strconv"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
)

func historyCursorTurn(cursor protocol.Cursor) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	raw, ok := strings.CutPrefix(string(cursor), "turn:")
	if !ok {
		return 0, fmt.Errorf("invalid history cursor")
	}
	turn, err := strconv.Atoi(raw)
	if err != nil || turn < 0 {
		return 0, fmt.Errorf("invalid history cursor")
	}
	return turn, nil
}

func historyPage(sess *session, snapshotID protocol.SnapshotID, beforeTurn, pageTurns int) protocol.HistoryPage {
	if pageTurns <= 0 || pageTurns > protocol.HistoryMaxTurns {
		pageTurns = protocol.HistoryMaxTurns
	}
	history := sess.ctrl.History()
	totalTurns := 0
	for _, message := range history {
		if visibleHistoryUser(message) {
			totalTurns++
		}
	}
	if beforeTurn <= 0 || beforeTurn > totalTurns {
		beforeTurn = totalTurns
	}
	startTurn := beforeTurn - pageTurns
	if startTurn < 0 {
		startTurn = 0
	}
	selected := make([]provider.Message, 0, len(history))
	turn := -1
	for _, message := range history {
		if visibleHistoryUser(message) {
			turn++
		}
		if turn < 0 {
			if startTurn == 0 {
				selected = append(selected, message)
			}
			continue
		}
		if turn >= startTurn && turn < beforeTurn {
			selected = append(selected, message)
		}
	}
	messages := make([]protocol.HistoryMessage, 0, len(selected))
	for _, message := range selected {
		content := message.Content
		item := protocol.HistoryMessage{
			Role: string(message.Role), Content: &content, CreatedAtMs: message.CreatedAt,
			WorkDurationMs: message.WorkDurationMs, ToolCallID: message.ToolCallID,
			ToolName: message.Name,
		}
		if message.ReasoningContent != "" {
			reasoning := message.ReasoningContent
			item.Reasoning = &reasoning
		}
		for _, call := range message.ToolCalls {
			args := call.Arguments
			item.ToolCalls = append(item.ToolCalls, protocol.HistoryToolCall{
				ID: call.ID, Name: call.Name, Arguments: &args,
				Diff: stringPtrOrNil(call.Diff), Added: call.Added, Removed: call.Removed,
			})
		}
		messages = append(messages, item)
	}
	page := protocol.HistoryPage{
		SnapshotID: snapshotID, Messages: messages, StartTurn: startTurn, EndTurn: beforeTurn,
		TotalTurns: totalTurns, ActualTurns: beforeTurn - startTurn, HasOlder: startTurn > 0,
		Externalized: []protocol.ExternalizedField{},
	}
	if page.HasOlder {
		page.NextCursor = protocol.Cursor(fmt.Sprintf("turn:%d", startTurn))
	}
	return page
}

func visibleHistoryUser(message provider.Message) bool {
	if message.Role != provider.RoleUser {
		return false
	}
	if _, steer := agent.SteerText(message.Content); steer {
		return false
	}
	return !control.IsSyntheticUserMessage(message.Content)
}
