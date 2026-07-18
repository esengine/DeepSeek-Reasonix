package history

import (
	"errors"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/eventwire"
	"reasonix/internal/provider"
	"reasonix/internal/remote/protocol"
)

type captureLookups struct {
	metadata     map[int]MessageMetadata
	checkpoints  map[int]protocol.CheckpointID
	supplemental map[int][]protocol.HistoryMessage
}

func projectCapture(capture Capture) ([]projectedEntry, int, error) {
	// Clone the canonical provider history first. Projection then never retains
	// caller-owned slices, even when a provider reused buffers after snapshot.
	messages := cloneProviderMessages(capture.Messages)
	if err := validateProviderMessages(messages); err != nil {
		return nil, 0, err
	}
	lookups, err := buildCaptureLookups(capture, len(messages))
	if err != nil {
		return nil, 0, err
	}
	tools := buildToolProjection(capture.Binding.SnapshotID, messages)
	replayedTodoArgs := historyTodoArgsWithCompleteSteps(messages)

	entries := make([]projectedEntry, 0, len(messages)+len(capture.Supplemental))
	turn := -1
	appendSupplemental := func(after int) {
		for _, message := range lookups.supplemental[after] {
			entries = append(entries, projectedEntry{turn: turn, message: cloneHistoryMessage(message)})
		}
	}
	appendSupplemental(-1)
	for index, message := range messages {
		metadata := lookups.metadata[index]
		visibleUser := isVisibleUser(message, metadata)
		if visibleUser {
			turn++
		}
		if projected, ok := projectProviderMessage(index, message, metadata, lookups.checkpoints[index], tools, replayedTodoArgs); ok {
			entries = append(entries, projectedEntry{turn: turn, message: projected})
		}
		appendSupplemental(index)
	}
	return entries, turn + 1, nil
}

func validateProviderMessages(messages []provider.Message) error {
	for messageIndex, message := range messages {
		switch message.Role {
		case provider.RoleSystem, provider.RoleUser, provider.RoleAssistant, provider.RoleTool:
		default:
			return fmt.Errorf("%w: message %d has unsupported role %q", ErrInvalidCapture, messageIndex, message.Role)
		}
		if message.WorkDurationMs < 0 {
			return fmt.Errorf("%w: message %d has negative workDurationMs", ErrInvalidCapture, messageIndex)
		}
		for callIndex, call := range message.ToolCalls {
			if call.Added < 0 || call.Removed < 0 {
				return fmt.Errorf("%w: message %d tool call %d has negative diff counts", ErrInvalidCapture, messageIndex, callIndex)
			}
		}
	}
	return nil
}

func buildCaptureLookups(capture Capture, messageCount int) (captureLookups, error) {
	lookups := captureLookups{
		metadata:     make(map[int]MessageMetadata, len(capture.Metadata)),
		checkpoints:  make(map[int]protocol.CheckpointID, len(capture.Checkpoints)),
		supplemental: make(map[int][]protocol.HistoryMessage),
	}
	for _, metadata := range capture.Metadata {
		if metadata.MessageIndex < 0 || metadata.MessageIndex >= messageCount {
			return captureLookups{}, fmt.Errorf("%w: metadata message index %d is out of range", ErrInvalidCapture, metadata.MessageIndex)
		}
		if metadata.CreatedAtMs < 0 {
			return captureLookups{}, fmt.Errorf("%w: metadata createdAtMs is negative", ErrInvalidCapture)
		}
		if _, exists := lookups.metadata[metadata.MessageIndex]; exists {
			return captureLookups{}, fmt.Errorf("%w: duplicate metadata for message index %d", ErrInvalidCapture, metadata.MessageIndex)
		}
		lookups.metadata[metadata.MessageIndex] = cloneMessageMetadata(metadata)
	}
	for _, checkpoint := range capture.Checkpoints {
		if checkpoint.MessageIndex < 0 || checkpoint.MessageIndex >= messageCount {
			return captureLookups{}, fmt.Errorf("%w: checkpoint message index %d is out of range", ErrInvalidCapture, checkpoint.MessageIndex)
		}
		if strings.TrimSpace(string(checkpoint.CheckpointID)) == "" {
			return captureLookups{}, fmt.Errorf("%w: checkpointId is empty", ErrInvalidCapture)
		}
		if _, exists := lookups.checkpoints[checkpoint.MessageIndex]; exists {
			return captureLookups{}, fmt.Errorf("%w: duplicate checkpoint for message index %d", ErrInvalidCapture, checkpoint.MessageIndex)
		}
		lookups.checkpoints[checkpoint.MessageIndex] = checkpoint.CheckpointID
	}
	for index, supplemental := range capture.Supplemental {
		if supplemental.AfterMessageIndex < -1 || supplemental.AfterMessageIndex >= messageCount {
			return captureLookups{}, fmt.Errorf("%w: supplemental index %d is out of range", ErrInvalidCapture, supplemental.AfterMessageIndex)
		}
		message, err := validateAndCloneSupplemental(supplemental.Message)
		if err != nil {
			return captureLookups{}, fmt.Errorf("%w: supplemental %d: %v", ErrInvalidCapture, index, err)
		}
		lookups.supplemental[supplemental.AfterMessageIndex] = append(lookups.supplemental[supplemental.AfterMessageIndex], message)
	}
	return lookups, nil
}

func validateAndCloneSupplemental(message protocol.HistoryMessage) (protocol.HistoryMessage, error) {
	switch message.Role {
	case "assistant", "phase", "notice", "compaction":
	default:
		return protocol.HistoryMessage{}, fmt.Errorf("role %q cannot be supplemental", message.Role)
	}
	if message.Content == nil {
		return protocol.HistoryMessage{}, errors.New("content must contain the pre-externalization body")
	}
	if message.CreatedAtMs < 0 || message.WorkDurationMs < 0 || message.Messages < 0 {
		return protocol.HistoryMessage{}, errors.New("numeric metadata must be non-negative")
	}
	if message.CheckpointID != "" {
		return protocol.HistoryMessage{}, errors.New("checkpointId must come from checkpoint capture metadata")
	}
	if message.ToolResultArchived {
		return protocol.HistoryMessage{}, errors.New("archived tool results are not complete history bodies")
	}
	for _, call := range message.ToolCalls {
		if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
			return protocol.HistoryMessage{}, errors.New("supplemental tool calls require non-empty id and name")
		}
		if call.Arguments == nil || call.ArgumentsArchived {
			return protocol.HistoryMessage{}, errors.New("supplemental tool calls require complete arguments")
		}
		if call.Added < 0 || call.Removed < 0 {
			return protocol.HistoryMessage{}, errors.New("supplemental tool diff counts must be non-negative")
		}
	}
	return cloneHistoryMessage(message), nil
}

func isVisibleUser(message provider.Message, metadata MessageMetadata) bool {
	if message.Role != provider.RoleUser {
		return false
	}
	if _, steer := agent.SteerText(message.Content); steer {
		return false
	}
	return !control.IsSyntheticUserMessage(displayContent(message, metadata))
}

func projectProviderMessage(
	index int,
	message provider.Message,
	metadata MessageMetadata,
	checkpointID protocol.CheckpointID,
	tools toolProjection,
	replayedTodoArgs map[string]string,
) (protocol.HistoryMessage, bool) {
	if message.Role == provider.RoleUser {
		if steerText, steer := agent.SteerText(message.Content); steer {
			content := "↪ " + steerText
			return protocol.HistoryMessage{Role: "notice", Content: stringPointer(content), CreatedAtMs: metadata.CreatedAtMs}, true
		}
		content := displayContent(message, metadata)
		if control.IsSyntheticUserMessage(content) {
			return protocol.HistoryMessage{}, false
		}
		projected := protocol.HistoryMessage{
			Role: "user", Content: stringPointer(content), CheckpointID: checkpointID,
			CreatedAtMs: metadata.CreatedAtMs,
		}
		projected.SubmitText = userSubmitText(message, metadata, content)
		return projected, true
	}

	content := message.Content
	projected := protocol.HistoryMessage{
		Role: string(message.Role), Content: stringPointer(content), CreatedAtMs: metadata.CreatedAtMs,
		WorkDurationMs: message.WorkDurationMs,
	}
	if projected.Role == "" {
		projected.Role = "assistant"
	}
	if message.Role == provider.RoleAssistant {
		if message.ReasoningContent != "" {
			projected.Reasoning = stringPointer(message.ReasoningContent)
		}
		if len(message.MemoryCitations) > 0 {
			projected.MemoryCitations = eventwire.ToWireMemoryCitations(message.MemoryCitations)
		}
		if len(message.ToolCalls) > 0 {
			projected.ToolCalls = make([]protocol.HistoryToolCall, 0, len(message.ToolCalls))
			for callIndex, call := range message.ToolCalls {
				arguments := call.Arguments
				if call.Name == "todo_write" && call.ID != "" {
					if replayed, ok := replayedTodoArgs[call.ID]; ok {
						arguments = replayed
					}
				}
				identity := tools.call(index, callIndex)
				result := tools.resultForCall(index, callIndex)
				projected.ToolCalls = append(projected.ToolCalls, projectHistoryToolCall(call, identity, arguments, result))
			}
		}
	}
	if message.Role == provider.RoleTool {
		identity := tools.result(index)
		projected.ToolCallID = identity.id
		projected.ToolName = firstNonEmpty(message.Name, identity.name, "tool")
		if historyToolResultFailed(content) {
			projected.ToolResultError = stringPointer(content)
		}
		// Successful and failed tool result bodies both stay complete. The
		// contentRef layer externalizes them after page construction.
		projected.ToolResultArchived = false
	}
	return projected, true
}

func displayContent(message provider.Message, metadata MessageMetadata) string {
	content := control.StripComposePrefixes(message.Content)
	if metadata.DisplayContent != nil {
		content = *metadata.DisplayContent
	}
	if agent.ContainsMemoryCompilerExecution(content) {
		content = control.StripComposePrefixes(content)
	}
	return content
}

func userSubmitText(message provider.Message, metadata MessageMetadata, display string) *string {
	if metadata.SubmitText != nil {
		return safeSubmitText(*metadata.SubmitText, display)
	}
	if message.Edited && strings.TrimSpace(message.Original) != "" {
		return safeSubmitText(message.Original, display)
	}
	if display == message.Content {
		return nil
	}
	if agent.ContainsMemoryCompilerExecution(message.Content) {
		replay := control.StripComposePrefixes(message.Content)
		if strings.HasPrefix(strings.TrimSpace(replay), "/") && replay != display {
			return stringPointer(replay)
		}
		return nil
	}
	return stringPointer(message.Content)
}

func safeSubmitText(candidate, display string) *string {
	if agent.ContainsMemoryCompilerExecution(candidate) {
		candidate = control.StripComposePrefixes(candidate)
	}
	if candidate == display {
		return nil
	}
	return stringPointer(candidate)
}

func cloneProviderMessages(messages []provider.Message) []provider.Message {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]provider.Message, len(messages))
	copy(cloned, messages)
	for index := range messages {
		cloned[index].Images = append([]string(nil), messages[index].Images...)
		cloned[index].ToolCalls = append([]provider.ToolCall(nil), messages[index].ToolCalls...)
		cloned[index].MemoryCitations = append([]provider.MemoryCitation(nil), messages[index].MemoryCitations...)
	}
	return cloned
}

func cloneMessageMetadata(metadata MessageMetadata) MessageMetadata {
	metadata.DisplayContent = cloneStringPointer(metadata.DisplayContent)
	metadata.SubmitText = cloneStringPointer(metadata.SubmitText)
	return metadata
}

func cloneHistoryMessage(message protocol.HistoryMessage) protocol.HistoryMessage {
	message.Content = cloneStringPointer(message.Content)
	message.Detail = cloneStringPointer(message.Detail)
	message.SubmitText = cloneStringPointer(message.SubmitText)
	message.Reasoning = cloneStringPointer(message.Reasoning)
	message.ToolResultError = cloneStringPointer(message.ToolResultError)
	message.Summary = cloneStringPointer(message.Summary)
	message.Archive = cloneStringPointer(message.Archive)
	message.MemoryCitations = append([]eventwire.MemoryCitation(nil), message.MemoryCitations...)
	if len(message.ToolCalls) > 0 {
		calls := make([]protocol.HistoryToolCall, len(message.ToolCalls))
		copy(calls, message.ToolCalls)
		for index := range calls {
			calls[index].Arguments = cloneStringPointer(calls[index].Arguments)
			calls[index].Summary = cloneStringPointer(calls[index].Summary)
			calls[index].Diff = cloneStringPointer(calls[index].Diff)
		}
		message.ToolCalls = calls
	}
	return message
}

func stringPointer(value string) *string {
	copy := value
	return &copy
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	return stringPointer(*value)
}
