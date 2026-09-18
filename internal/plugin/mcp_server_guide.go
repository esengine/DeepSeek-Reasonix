package plugin

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

var serverInstructions sync.Map // *Client -> string

func setServerInstructions(c *Client, text string) {
	if c == nil {
		return
	}
	if text == "" {
		serverInstructions.Delete(c)
		return
	}
	serverInstructions.Store(c, text)
}

const maxMCPServerGuideBytes = 32 * 1024

// ServerGuide is the user-turn text naming configured MCP servers for the
// shared resource tools. Empty when no server is connected or failed.
func (h *Host) ServerGuide() string {
	if h == nil {
		return ""
	}
	type row struct {
		name         string
		instructions string
		connected    bool
	}
	byName := map[string]*row{}
	for _, name := range h.ServerNames() {
		byName[name] = &row{name: name, connected: true, instructions: h.serverInstructions(name)}
	}
	for _, failure := range h.Failures() {
		name := strings.TrimSpace(failure.Name)
		if name == "" {
			continue
		}
		if _, ok := byName[name]; !ok {
			byName[name] = &row{name: name}
		}
	}
	if len(byName) == 0 {
		return ""
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	quoted, _ := json.Marshal(names)
	var b strings.Builder
	fmt.Fprintf(&b, "Use list_mcp_resources, list_mcp_resource_templates, or read_mcp_resource with one of these names as the server argument: %s.", quoted)
	for _, name := range names {
		item := byName[name]
		b.WriteString("\n\n")
		b.WriteString(name)
		if !item.connected {
			b.WriteString(": not connected")
			continue
		}
		if item.instructions == "" {
			b.WriteString(": connected")
			continue
		}
		b.WriteString(":\n")
		b.WriteString(item.instructions)
	}
	return clampMCPServerGuide(b.String())
}

func (h *Host) serverInstructions(name string) string {
	c := h.lookupClient(name)
	if c == nil {
		return ""
	}
	if v, ok := serverInstructions.Load(c); ok {
		if text, ok := v.(string); ok {
			return text
		}
	}
	return ""
}

func clampMCPServerGuide(text string) string {
	if len(text) <= maxMCPServerGuideBytes {
		return text
	}
	cut := maxMCPServerGuideBytes
	for cut > 0 && !utf8.ValidString(text[:cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut])
}
