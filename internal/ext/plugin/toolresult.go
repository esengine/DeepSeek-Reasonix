package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// toolContent is one item of a tools/call result, across every content type
// the protocol defines up to 2025-11-25.
type toolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Title    string `json:"title"`
	Resource *struct {
		URI      string `json:"uri"`
		MimeType string `json:"mimeType"`
		Text     string `json:"text"`
	} `json:"resource"`
}

// parseToolResult flattens an MCP tools/call result into plain text plus the
// image content items as data URLs. Every item the model cannot take directly
// leaves a short marker at its position, so a text-only reader still learns it
// was returned; a result carrying only structuredContent is given as its JSON,
// which is what the spec has a server put in a text block anyway.
func parseToolResult(res json.RawMessage) (string, []string, error) {
	var out struct {
		Content           []toolContent   `json:"content"`
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", nil, fmt.Errorf("decode tool result: %w", err)
	}
	var sb strings.Builder
	var images []string
	for _, c := range out.Content {
		switch c.Type {
		case "text":
			sb.WriteString(c.Text)
		case "image":
			placeholder, url := toolResultImage(c.MimeType, c.Data, len(images))
			sb.WriteString(placeholder)
			if url != "" {
				images = append(images, url)
			}
		case "audio":
			fmt.Fprintf(&sb, "[audio: %s, not shown]", orDefault(c.MimeType, "audio"))
		case "resource_link":
			fmt.Fprintf(&sb, "[resource: %s <%s>]", orDefault(c.Title, orDefault(c.Name, c.URI)), c.URI)
		case "resource":
			if c.Resource == nil {
				continue
			}
			if c.Resource.Text != "" {
				fmt.Fprintf(&sb, "[resource <%s>]\n%s", c.Resource.URI, c.Resource.Text)
			} else {
				fmt.Fprintf(&sb, "[resource: <%s> %s, binary, not shown]", c.Resource.URI, orDefault(c.Resource.MimeType, ""))
			}
		}
	}
	text := sb.String()
	if strings.TrimSpace(text) == "" && hasStructured(out.StructuredContent) {
		var compact bytes.Buffer
		if json.Compact(&compact, out.StructuredContent) == nil {
			text = compact.String()
		}
	}
	if out.IsError {
		return text, images, fmt.Errorf("plugin tool reported error: %s", text)
	}
	return text, images, nil
}

func hasStructured(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func orDefault(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}
