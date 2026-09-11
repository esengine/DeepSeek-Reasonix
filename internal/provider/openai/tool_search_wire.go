package openai

import "reasonix/internal/provider"

func encodeChatTools(req provider.Request, mimo bool) []chatTool {
	var tools []chatTool
	for _, t := range req.Tools {
		parameters := t.Parameters
		if len(parameters) == 0 {
			parameters = provider.CanonicalizeSchema(nil)
		}
		if mimo {
			parameters = provider.NormalizeLegacyTupleItemsForDraft202012(parameters)
		}
		item := chatTool{Type: "function", Function: chatFunction{Name: t.Name, Description: t.Description, Parameters: parameters, Strict: t.Strict}}
		item.DeferLoading = t.Deferred
		tools = append(tools, item)
	}
	return tools
}
