package history

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
)

type toolKey struct {
	message int
	call    int
}

type toolIdentity struct {
	id   string
	name string
}

type toolProjection struct {
	calls       map[toolKey]toolIdentity
	results     map[int]toolIdentity
	resultIndex map[toolKey]int
	messages    []provider.Message
}

func buildToolProjection(snapshotID protocol.SnapshotID, messages []provider.Message) toolProjection {
	projection := toolProjection{
		calls: make(map[toolKey]toolIdentity), results: make(map[int]toolIdentity),
		resultIndex: make(map[toolKey]int), messages: messages,
	}
	used := make(map[string]struct{})
	resultByID := make(map[string]int)
	for index, message := range messages {
		if message.Role == provider.RoleTool && message.ToolCallID != "" {
			// Match Desktop history: the latest retained result wins summary
			// lookup for duplicate legacy IDs.
			resultByID[message.ToolCallID] = index
			used[message.ToolCallID] = struct{}{}
		}
		for _, call := range message.ToolCalls {
			if call.ID != "" {
				used[call.ID] = struct{}{}
			}
		}
	}
	legacySequence := 0
	for messageIndex, message := range messages {
		if message.Role != provider.RoleAssistant || len(message.ToolCalls) == 0 {
			continue
		}
		resultCursor := messageIndex + 1
		for callIndex, call := range message.ToolCalls {
			key := toolKey{message: messageIndex, call: callIndex}
			identity := toolIdentity{id: call.ID, name: call.Name}
			if identity.id == "" {
				for {
					legacySequence++
					candidate := fmt.Sprintf("history_%s_positional_%d", snapshotID, legacySequence)
					if _, collision := used[candidate]; collision {
						continue
					}
					identity.id = candidate
					used[candidate] = struct{}{}
					break
				}
				// Preserve the current positional fallback for old sessions: only
				// contiguous, empty-ID tool messages following the assistant can
				// satisfy an empty-ID call.
				for resultCursor < len(messages) {
					candidate := messages[resultCursor]
					if candidate.Role != provider.RoleTool {
						break
					}
					candidateIndex := resultCursor
					resultCursor++
					if candidate.ToolCallID != "" {
						continue
					}
					projection.results[candidateIndex] = identity
					projection.resultIndex[key] = candidateIndex
					if identity.name == "" {
						identity.name = candidate.Name
					}
					break
				}
			} else if resultIndex, ok := resultByID[identity.id]; ok {
				projection.resultIndex[key] = resultIndex
				projection.results[resultIndex] = toolIdentity{id: identity.id, name: firstNonEmpty(messages[resultIndex].Name, identity.name)}
				if identity.name == "" {
					identity.name = messages[resultIndex].Name
				}
			}
			if identity.name == "" {
				identity.name = "tool"
			}
			projection.calls[key] = identity
		}
	}
	return projection
}

func (p toolProjection) call(messageIndex, callIndex int) toolIdentity {
	identity := p.calls[toolKey{message: messageIndex, call: callIndex}]
	if identity.name == "" {
		identity.name = "tool"
	}
	return identity
}

func (p toolProjection) result(messageIndex int) toolIdentity {
	identity := p.results[messageIndex]
	if identity.id == "" {
		identity.id = p.messages[messageIndex].ToolCallID
	}
	if identity.name == "" {
		identity.name = p.messages[messageIndex].Name
	}
	return identity
}

func (p toolProjection) resultForCall(messageIndex, callIndex int) provider.Message {
	if resultIndex, ok := p.resultIndex[toolKey{message: messageIndex, call: callIndex}]; ok {
		return p.messages[resultIndex]
	}
	return provider.Message{}
}

func projectHistoryToolCall(call provider.ToolCall, identity toolIdentity, arguments string, result provider.Message) protocol.HistoryToolCall {
	name := firstNonEmpty(identity.name, call.Name, "tool")
	projected := protocol.HistoryToolCall{
		ID: identity.id, Name: name,
		Arguments: stringPointer(arguments), Subject: historyToolSubject(name, arguments),
		Added: call.Added, Removed: call.Removed,
	}
	if summary := historyToolSummary(name, arguments, result.Content); summary != "" {
		projected.Summary = stringPointer(summary)
	}
	if call.Diff != "" {
		projected.Diff = stringPointer(call.Diff)
	}
	// Full arguments are retained until contentref externalization.
	projected.ArgumentsArchived = false
	return projected
}

func historyToolSubject(name, arguments string) string {
	args := parseHistoryToolArgs(arguments)
	var subject string
	switch name {
	case "bash":
		subject = historyArgString(args, "command")
	case "grep", "glob":
		subject = firstNonEmpty(historyArgString(args, "pattern"), historyArgString(args, "path"))
	case "web_fetch":
		subject = historyArgString(args, "url")
	case "task":
		subject = firstNonEmpty(historyArgString(args, "description"), historyArgString(args, "prompt"))
	case "run_skill":
		subject = historyArgString(args, "name")
	case "move_file":
		source := historyArgString(args, "source_path")
		destination := historyArgString(args, "destination_path")
		if source != "" && destination != "" {
			subject = source + " -> " + destination
		} else {
			subject = firstNonEmpty(source, destination)
		}
	case "remember":
		subject = firstNonEmpty(historyArgString(args, "name"), historyArgString(args, "description"))
	case "todo_write", "exit_plan_mode":
		subject = ""
	default:
		subject = firstNonEmpty(historyArgString(args, "path"), historyArgString(args, "file_path"))
	}
	return clipSingleLine(subject, 240)
}

func historyToolSummary(name, arguments, output string) string {
	if historyToolResultFailed(output) {
		return ""
	}
	args := parseHistoryToolArgs(arguments)
	switch name {
	case "write_file":
		if content := historyArgString(args, "content"); content != "" {
			return fmt.Sprintf("%d lines", historyLineCount(content))
		}
	case "edit_file":
		oldText := historyArgString(args, "old_string")
		newText := historyArgString(args, "new_string")
		if oldText != "" || newText != "" {
			return fmt.Sprintf("%d -> %d lines", historyLineCount(oldText), historyLineCount(newText))
		}
	case "multi_edit":
		if edits, ok := args["edits"].([]any); ok && len(edits) > 0 {
			return fmt.Sprintf("%d edits", len(edits))
		}
	}
	if output == "" {
		return ""
	}
	switch name {
	case "read_file":
		if strings.HasPrefix(output, "(empty file)") {
			return "empty file"
		}
		if arrows := strings.Count(output, "→"); arrows > 0 {
			return fmt.Sprintf("%d lines", arrows)
		}
		return fmt.Sprintf("%d lines", historyLineCount(output))
	case "grep":
		return fmt.Sprintf("%d matches", historyNonEmptyLineCount(output))
	case "glob":
		return fmt.Sprintf("%d files", historyNonEmptyLineCount(output))
	case "ls":
		return fmt.Sprintf("%d entries", historyNonEmptyLineCount(output))
	case "web_fetch":
		return clipSingleLine(strings.SplitN(output, "\n", 2)[0], 80)
	case "bash":
		if strings.TrimSpace(output) == "" {
			return "no output"
		}
		return fmt.Sprintf("%d lines", historyLineCount(output))
	default:
		return ""
	}
}

func parseHistoryToolArgs(arguments string) map[string]any {
	if arguments == "" {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return map[string]any{}
	}
	return result
}

func historyArgString(arguments map[string]any, key string) string {
	value, _ := arguments[key].(string)
	return value
}

func historyLineCount(value string) int {
	if value == "" {
		return 0
	}
	value = strings.TrimSuffix(value, "\n")
	if value == "" {
		return 0
	}
	return strings.Count(value, "\n") + 1
}

func historyNonEmptyLineCount(value string) int {
	count := 0
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func clipSingleLine(value string, maximum int) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len(value) <= maximum {
		return value
	}
	if maximum <= 3 {
		return clipStringBytes(value, maximum)
	}
	return clipStringBytes(value, maximum-3) + "..."
}

func clipStringBytes(value string, maximum int) string {
	if maximum <= 0 {
		return ""
	}
	if len(value) <= maximum {
		return value
	}
	for maximum > 0 && !utf8.RuneStart(value[maximum]) {
		maximum--
	}
	return value[:maximum]
}

func historyTodoArgsWithCompleteSteps(messages []provider.Message) map[string]string {
	successful := successfulHistoryToolCallIDs(messages)
	result := map[string]string{}
	var todos []evidence.TodoItem
	latestTodoID := ""
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if call.ID == "" || !successful[call.ID] {
				continue
			}
			switch call.Name {
			case "todo_write":
				receipt := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), true, true)
				if len(receipt.Todos) == 0 {
					continue
				}
				todos = evidence.NormalizeSerialTodos(receipt.Todos)
				latestTodoID = call.ID
				if arguments, ok := todoArgsJSON(todos); ok {
					result[latestTodoID] = arguments
				}
			case "complete_step":
				if latestTodoID == "" || len(todos) == 0 {
					continue
				}
				receipt := evidence.ReceiptFromToolCall(call.Name, json.RawMessage(call.Arguments), true, true)
				match, ok := evidence.MatchStep(receipt.Step, todos)
				if !ok || !evidence.AdvanceSerialTodo(todos, match.Index-1) {
					continue
				}
				if arguments, ok := todoArgsJSON(todos); ok {
					result[latestTodoID] = arguments
				}
			}
		}
	}
	return result
}

func successfulHistoryToolCallIDs(messages []provider.Message) map[string]bool {
	successful := make(map[string]bool)
	for _, message := range messages {
		if message.Role == provider.RoleTool && message.ToolCallID != "" && !historyToolResultFailed(message.Content) {
			successful[message.ToolCallID] = true
		}
	}
	return successful
}

func historyToolResultFailed(content string) bool {
	content = strings.TrimSpace(content)
	return strings.HasPrefix(content, "error:") ||
		strings.HasPrefix(content, "blocked:") ||
		strings.HasPrefix(content, "Error:") ||
		strings.HasPrefix(content, "[error")
}

func todoArgsJSON(todos []evidence.TodoItem) (string, bool) {
	encoded, err := json.Marshal(map[string]any{"todos": todos})
	if err != nil {
		return "", false
	}
	return string(encoded), true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
