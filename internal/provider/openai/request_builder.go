package openai

import "reasonix/internal/provider"

type toolImageAccumulator struct {
	pending   []trackedImage
	messages  *[]chatMessage
	locations *[]wireImageLocation
	detail    string
}

func (a *toolImageAccumulator) add(message provider.Message) {
	for ordinal, image := range message.Images {
		a.pending = append(a.pending, trackedImage{
			Ref:      image,
			Identity: provider.ImageIdentity{MessageID: message.ID, ImageOrdinal: ordinal, ContentDigest: provider.ImageContentDigest(image)},
		})
	}
}

func (a *toolImageAccumulator) flush() {
	if len(a.pending) == 0 {
		return
	}
	refs := make([]string, len(a.pending))
	for i, image := range a.pending {
		refs[i] = image.Ref
	}
	wireIndex := len(*a.messages)
	*a.messages = append(*a.messages, chatMessage{
		Role:    "user",
		Content: imageContentParts("Images returned by the preceding tool call(s):", refs, a.detail),
	})
	for i, image := range a.pending {
		*a.locations = append(*a.locations, wireImageLocation{
			WireMessageIndex: wireIndex, WireContentIndex: i + 1, Identity: image.Identity,
		})
	}
	a.pending = nil
}

func (c *client) buildRequest(req provider.Request) chatRequest {
	wire, _ := c.buildRequestWithImageMap(req)
	return wire
}

func (c *client) buildRequestWithImageMap(req provider.Request) (chatRequest, []wireImageLocation) {
	// Repair interrupted histories before serialization so every assistant tool
	// call still has the result sequence required by strict providers.
	source := provider.SanitizeToolPairing(req.Messages)
	messages := make([]chatMessage, 0, len(source))
	var locations []wireImageLocation
	toolImages := toolImageAccumulator{messages: &messages, locations: &locations, detail: c.visionDetail}

	for _, message := range source {
		if message.Role != provider.RoleTool {
			toolImages.flush()
		}
		wire := c.buildChatMessage(message)
		c.applyMessageContent(&wire, message, len(messages), &locations)
		messages = append(messages, wire)
		if c.vision && message.Role == provider.RoleTool {
			toolImages.add(message)
		}
	}
	toolImages.flush()
	return c.finishChatRequest(req, messages), locations
}

func (c *client) buildChatMessage(message provider.Message) chatMessage {
	wire := chatMessage{Role: string(message.Role), ToolCallID: message.ToolCallID}
	if message.Role == provider.RoleTool {
		// Strict backends reject a tool result when the name key is absent.
		name := message.Name
		wire.Name = &name
	}
	if message.Role == provider.RoleAssistant {
		switch {
		case c.kimiK3 && (message.ReasoningContent != "" || len(message.ToolCalls) > 0):
			wire.ReasoningContent = &message.ReasoningContent
		case (c.deepseek || c.RequiresToolCallReasoning()) && hasReasoningOrToolCall(message):
			if c.RequiresToolCallReasoning() || message.ReasoningContent != "" {
				wire.ReasoningContent = &message.ReasoningContent
			}
		case c.zhipu && (message.ReasoningContent != "" || (c.glmThinkingEnabled() && len(message.ToolCalls) > 0)):
			wire.ReasoningContent = &message.ReasoningContent
		}
	}
	for _, call := range message.ToolCalls {
		toolCall := chatToolCall{ID: call.ID, Type: "function"}
		toolCall.Function.Name = call.Name
		toolCall.Function.Arguments = call.Arguments
		if call.ThoughtSignature != "" && usesGeminiThoughtSignatures(c.baseURL, c.model) {
			toolCall.ExtraContent = &chatToolCallExtraContent{}
			toolCall.ExtraContent.Google.ThoughtSignature = call.ThoughtSignature
		}
		wire.ToolCalls = append(wire.ToolCalls, toolCall)
	}
	return wire
}

func (c *client) applyMessageContent(wire *chatMessage, message provider.Message, wireIndex int, locations *[]wireImageLocation) {
	if c.vision && message.Role == provider.RoleUser && len(message.Images) > 0 {
		wire.Content = imageContentParts(message.Content, message.Images, c.visionDetail)
		contentOffset := 0
		if message.Content != "" {
			contentOffset = 1
		}
		for ordinal, image := range message.Images {
			*locations = append(*locations, wireImageLocation{
				WireMessageIndex: wireIndex, WireContentIndex: ordinal + contentOffset,
				Identity: provider.ImageIdentity{MessageID: message.ID, ImageOrdinal: ordinal, ContentDigest: provider.ImageContentDigest(image)},
			})
		}
		return
	}
	if message.Role != provider.RoleAssistant || len(wire.ToolCalls) == 0 || message.Content != "" {
		wire.Content = message.Content
	}
}

func (c *client) finishChatRequest(req provider.Request, messages []chatMessage) chatRequest {
	maxOutputTokens := req.MaxTokens
	if maxOutputTokens == 0 {
		maxOutputTokens = c.maxOutputTokens
	}
	if maxOutputTokens < 0 {
		maxOutputTokens = 0
	}
	out := chatRequest{
		Model: c.model, Messages: messages, Tools: encodeChatTools(req, c.mimo), Stream: true,
		StreamOptions: &streamOptions{IncludeUsage: true}, Temperature: req.Temperature,
		MaxTokens: maxOutputTokens, ReasoningEffort: kimiK3ReasoningEffort(c.kimiK3, c.requestEffort(req)), ExtraBody: c.extraBody,
	}
	c.applyReasoning(&out, req)
	return out
}

func (c *client) buildPrefixRequest(req provider.Request, content, reasoning string) chatRequest {
	out := c.buildRequest(req)
	prefix := chatMessage{Role: "assistant", Content: content, Prefix: true}
	if c.deepseek && c.thinkingType != "disabled" {
		prefix.ReasoningContent = &reasoning
	}
	out.Messages = append(out.Messages, prefix)
	return out
}
