package anthropic

import (
	"slices"

	"reasonix/internal/sessioncontext"
)

func sessionContextTextBlocks(content string) []contentBlock {
	parts := sessioncontext.SplitBlocks(content)
	blocks := make([]contentBlock, 0, len(parts))
	for _, part := range parts {
		blocks = append(blocks, contentBlock{Type: "text", Text: part.Text})
	}
	return blocks
}

// markPromptCacheBreakpoints marks stable system/tools, the latest valid
// session context, and the request tail. Native Anthropic permits four cache
// breakpoints; Reasonix uses at most three and keeps the default five-minute TTL.
func markPromptCacheBreakpoints(system []textBlock, tools []anthTool, messages []anthMessage) {
	if n := len(system); n > 0 {
		system[n-1].CacheControl = ephemeral()
	} else if n := len(tools); n > 0 {
		tools[n-1].CacheControl = ephemeral()
	}

	contextMessage, contextBlock := -1, -1
	for i := len(messages) - 1; i >= 0 && contextMessage < 0; i-- {
		for j := range slices.Backward(messages[i].Content) {
			block := messages[i].Content[j]
			if block.Type == "text" && sessioncontext.IsContent(block.Text) {
				contextMessage, contextBlock = i, j
				break
			}
		}
	}
	if contextMessage >= 0 {
		messages[contextMessage].Content[contextBlock].CacheControl = ephemeral()
	}
	if n := len(messages); n > 0 {
		if k := len(messages[n-1].Content); k > 0 {
			messages[n-1].Content[k-1].CacheControl = ephemeral()
		}
	}
}
