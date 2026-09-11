package anthropic

import (
	"encoding/json"

	"reasonix/internal/provider"
)

func encodeAnthTools(c *client, req provider.Request) []anthTool {
	var tools []anthTool
	if c.search.NativeEnabled && !c.search.ClientEnabled {
		tools = append(tools, anthTool{Type: "web_search_20250305", Name: "web_search"})
	}
	for _, t := range req.Tools {
		schema := t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		if c.mimo {
			schema = provider.NormalizeLegacyTupleItemsForDraft202012(schema)
		}
		item := anthTool{Name: t.Name, Description: t.Description, InputSchema: schema}
		if !c.deepseek && provider.IsFirstPartyAnthropic(c.baseURL) {
			item.Strict = t.Strict
			item.DeferLoading = t.Deferred
		}
		tools = append(tools, item)
	}
	return tools
}
