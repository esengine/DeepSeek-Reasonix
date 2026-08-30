package responses

import (
	"encoding/json"

	"reasonix/internal/provider"
)

func encodeResponsesTools(c *client, req provider.Request) []map[string]any {
	tools := make([]map[string]any, 0, len(req.Tools)+1)
	if c.webSearch {
		tools = append(tools, map[string]any{"type": "web_search"})
	}
	for _, tool := range req.Tools {
		parameters := tool.Parameters
		if len(parameters) == 0 {
			parameters = provider.CanonicalizeSchema(nil)
		}
		item := map[string]any{
			"type": "function", "name": tool.Name, "description": tool.Description,
			"parameters": json.RawMessage(parameters),
		}
		if tool.Strict && provider.IsFirstPartyOpenAI(c.baseURL) {
			item["strict"] = true
		}
		if tool.Deferred && provider.IsFirstPartyOpenAI(c.baseURL) {
			item["defer_loading"] = true
		}
		tools = append(tools, item)
	}
	if req.ToolSearch != nil && req.ToolSearch.Enabled && provider.IsFirstPartyOpenAI(c.baseURL) {
		tools = append([]map[string]any{{"type": "tool_search"}}, tools...)
	}
	return tools
}
