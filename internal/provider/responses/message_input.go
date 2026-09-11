package responses

import (
	"reasonix/internal/provider"
)

func messagesToInput(messages []provider.Message, vision, replayWebSearchItems, summary bool) []map[string]any {
	input := make([]map[string]any, 0, len(messages)*2)
	// Keep function outputs together before appending a vision user message,
	// matching the Chat adapter while retaining provider-specific exclusions.
	var pendingImages []map[string]string
	flushImages := func() {
		if len(pendingImages) > 0 {
			parts := []map[string]string{{"type": "input_text", "text": "Images returned by the preceding tool call(s):"}}
			parts = append(parts, pendingImages...)
			input = append(input, map[string]any{"role": "user", "content": parts})
			pendingImages = nil
		}
	}
	for _, message := range messages {
		if message.Role != provider.RoleTool {
			flushImages()
		}
		switch message.Role {
		case provider.RoleSystem, provider.RoleUser:
			// User images use input_text/input_image parts; text-only and system
			// messages keep the string form.
			if vision && message.Role == provider.RoleUser && len(message.Images) > 0 {
				parts := make([]map[string]string, 0, len(message.Images)+1)
				if message.Content != "" {
					parts = append(parts, map[string]string{"type": "input_text", "text": message.Content})
				}
				for _, ref := range message.Images {
					if part := inputImagePart(ref); part != nil {
						parts = append(parts, part)
					}
				}
				if len(parts) == 0 || (len(parts) == 1 && parts[0]["type"] == "input_text") {
					input = append(input, map[string]any{"role": "user", "content": message.Content})
				} else {
					input = append(input, map[string]any{"role": "user", "content": parts})
				}
			} else {
				input = append(input, map[string]any{"role": string(message.Role), "content": message.Content})
			}
		case provider.RoleAssistant:
			var rawReasoning bool
			input, rawReasoning = appendReasoningItems(input, message.ResponsesItems)
			if !rawReasoning && message.ReasoningContent != "" {
				// Only vendors requiring summary receive the extra reasoning copy;
				// otherwise an echoed summary could duplicate reasoning each turn.
				item := map[string]any{
					"type":    "reasoning",
					"content": []map[string]string{{"type": "reasoning_text", "text": message.ReasoningContent}},
				}
				if message.ReasoningID != "" {
					// OpenAI Responses schema marks Reasoning.id required;
					// round-trip the provider-issued id when we captured one.
					item["id"] = message.ReasoningID
				}
				if message.ReasoningStatus != "" {
					item["status"] = message.ReasoningStatus
				}
				if summary {
					item["summary"] = []map[string]string{{"type": "summary_text", "text": message.ReasoningContent}}
				}
				input = append(input, item)
			}
			if replayWebSearchItems {
				for _, raw := range message.ResponsesItems {
					if item, ok := decodeReplayableWebSearchItem(raw); ok {
						input = append(input, item)
					}
				}
			}
			if message.Content != "" || len(message.ToolCalls) == 0 {
				input = append(input, map[string]any{"role": "assistant", "content": message.Content})
			}
			for _, call := range message.ToolCalls {
				input = append(input, map[string]any{
					"type": "function_call", "call_id": call.ID,
					"name": call.Name, "arguments": call.Arguments,
				})
			}
		case provider.RoleTool:
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": message.ToolCallID, "output": message.Content,
			})
			if vision {
				for _, ref := range message.Images {
					if part := inputImagePart(ref); part != nil {
						pendingImages = append(pendingImages, part)
					}
				}
			}
		}
	}
	flushImages()
	return input
}
