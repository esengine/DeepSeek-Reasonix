package anthropic

import (
	"encoding/json"

	"reasonix/internal/provider"
)

type messageAssembler struct {
	client        *client
	messages      []anthMessage
	pendingImages []contentBlock
}

func (b *messageAssembler) appendBlocks(role string, blocks ...contentBlock) {
	if len(blocks) == 0 {
		return
	}
	if n := len(b.messages); n > 0 && b.messages[n-1].Role == role {
		b.messages[n-1].Content = mergeThinkingFirst(b.messages[n-1].Content, blocks)
		return
	}
	b.messages = append(b.messages, anthMessage{Role: role, Content: blocks})
}
func (b *messageAssembler) flushImages() {
	b.appendBlocks("user", b.pendingImages...)
	b.pendingImages = nil
}
func (c *client) buildMessages(messages []provider.Message) ([]textBlock, []anthMessage) {
	var system []textBlock
	b := messageAssembler{client: c}
	for _, m := range provider.SanitizeToolPairing(c.replayMessages(messages)) {
		if m.Role != provider.RoleTool {
			b.flushImages()
		}
		switch m.Role {
		case provider.RoleSystem:
			if m.Content != "" {
				system = append(system, textBlock{Type: "text", Text: m.Content})
			}
		case provider.RoleUser:
			b.addUser(m)
		case provider.RoleTool:
			b.addTool(m)
		case provider.RoleAssistant:
			b.addAssistant(m)
		}
	}
	b.flushImages()
	return system, b.messages
}

func (b *messageAssembler) addUser(m provider.Message) {
	if m.Content != "" {
		b.appendBlocks("user", sessionContextTextBlocks(m.Content)...)
	}
	if b.client.vision {
		for _, ref := range m.Images {
			if src := imageSourceFromRef(ref); src != nil {
				b.appendBlocks("user", contentBlock{Type: "image", Source: src})
			}
		}
	}
}

func (b *messageAssembler) addTool(m provider.Message) {
	content := m.Content
	if content == "" {
		content = "(no output)" // tool_result content must be non-empty
	}
	block := contentBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: content}
	if b.client.vision && !b.client.deepseek {
		if blocks := toolResultBlocks(content, m.Images); blocks != nil {
			block.Content = blocks
		}
	}
	b.appendBlocks("user", block)
	if b.client.vision && b.client.deepseek {
		for _, ref := range m.Images {
			if src := imageSourceFromRef(ref); src != nil {
				b.pendingImages = append(b.pendingImages, contentBlock{Type: "image", Source: src})
			}
		}
	}
}

func (b *messageAssembler) addAssistant(m provider.Message) {
	var blocks []contentBlock
	// Replay reasoning before content: DeepSeek needs historical thinking;
	// Anthropic requires a valid signature. replayReasoningBlocks owns both rules.
	blocks = append(blocks, b.client.replayReasoningBlocks(m)...)
	blocks = appendServerSearchBlocks(blocks, m.ServerSearch)
	if m.Content != "" {
		blocks = append(blocks, contentBlock{Type: "text", Text: m.Content})
	}
	for _, tc := range m.ToolCalls {
		input := json.RawMessage(tc.Arguments)
		if len(input) == 0 {
			input = json.RawMessage("{}") // input is required, even when empty
		}
		blocks = append(blocks, contentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Name, Input: input})
	}
	b.appendBlocks("assistant", blocks...)
}
