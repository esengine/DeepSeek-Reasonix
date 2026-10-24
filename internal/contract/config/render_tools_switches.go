package config

import (
	"fmt"
	"strings"
)

// mcpResourcesLine is the documented line in a full render of [tools].
func mcpResourcesLine(t ToolsConfig) string {
	line := fmt.Sprintf("mcp_resources = %v   # a tool that reads resources a connected MCP server declares; user file only\n", t.MCPResources)
	line += fmt.Sprintf("present_files = %v   # let the model declare clickable file-delivery cards; user file only, reopen session to apply\n", t.PresentFiles)
	if len(t.WebSearchBackends) > 0 {
		line += fmt.Sprintf("web_search_backends = %s   # ordered chain web_search tries, free first; user file only\n", renderStringArray(t.WebSearchBackends))
	}
	if t.SearxngURL != "" {
		line += fmt.Sprintf("searxng_url = %q   # your own SearXNG instance with the json format enabled; user file only\n", t.SearxngURL)
	}
	if len(t.WebSearchPaidAllowed) > 0 {
		line += fmt.Sprintf("web_search_paid_allowed = %s   # paid search backends you agreed to be charged for; user file only\n", renderStringArray(t.WebSearchPaidAllowed))
	}
	if len(t.WebFetchBackends) > 0 {
		line += fmt.Sprintf("web_fetch_backends = %s   # ordered chain web_fetch tries; local is free, deepseek_api is paid; user file only\n", renderStringArray(t.WebFetchBackends))
	}
	if len(t.WebFetchPaidAllowed) > 0 {
		line += fmt.Sprintf("web_fetch_paid_allowed = %s   # paid fetch backends you agreed to be charged for; user file only\n", renderStringArray(t.WebFetchPaidAllowed))
	}
	if t.VideoURLPassthrough {
		line += "video_url_passthrough = true   # the model may hand a public video address you gave to the vendor, which then visits it; user file only\n"
	}
	if t.VideoNativeMaxBytes > 0 {
		line += fmt.Sprintf("video_native_max_bytes = %d   # a lower ceiling, in bytes, on a video file read_video sends natively; user file only\n", t.VideoNativeMaxBytes)
	}
	return line
}

// renderToolsSwitches writes the [tools] switches a delta render carries when
// they are set.
func renderToolsSwitches(b *strings.Builder, t ToolsConfig) {
	if t.BrowserTools != nil {
		fmt.Fprintf(b, "browser_tools = %v\n", *t.BrowserTools)
	}
	if t.MCPResources {
		b.WriteString("mcp_resources = true\n")
	}
	if !t.PresentFiles {
		b.WriteString("present_files = false\n")
	}
	if len(t.WebSearchBackends) > 0 {
		fmt.Fprintf(b, "web_search_backends = %s\n", renderStringArray(t.WebSearchBackends))
	}
	if t.SearxngURL != "" {
		fmt.Fprintf(b, "searxng_url = %q\n", t.SearxngURL)
	}
	if len(t.WebSearchPaidAllowed) > 0 {
		fmt.Fprintf(b, "web_search_paid_allowed = %s\n", renderStringArray(t.WebSearchPaidAllowed))
	}
	if len(t.WebFetchBackends) > 0 {
		fmt.Fprintf(b, "web_fetch_backends = %s\n", renderStringArray(t.WebFetchBackends))
	}
	if len(t.WebFetchPaidAllowed) > 0 {
		fmt.Fprintf(b, "web_fetch_paid_allowed = %s\n", renderStringArray(t.WebFetchPaidAllowed))
	}
	if t.VideoURLPassthrough {
		b.WriteString("video_url_passthrough = true\n")
	}
	if t.VideoNativeMaxBytes > 0 {
		fmt.Fprintf(b, "video_native_max_bytes = %d\n", t.VideoNativeMaxBytes)
	}
}
