package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"reasonix/internal/contract/tool"
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

// captureToolResult retains a rich result whole and tells the model where it
// is; a text-only result is read exactly as it always was and never stored.
func captureToolResult(ctx context.Context, raw json.RawMessage) (string, []string, error) {
	text, images, parseErr := parseToolResult(raw)
	var result struct {
		Structured json.RawMessage `json:"structuredContent"`
		Content    []toolContent   `json:"content"`
	}
	_ = json.Unmarshal(raw, &result)
	rich := hasStructured(result.Structured)
	for _, item := range result.Content {
		rich = rich || item.Type != "text"
	}
	if !rich || !tool.CanRecordResult(ctx) {
		return text, images, parseErr
	}
	if len(raw) > tool.ResultRetainLimit {
		text += fmt.Sprintf("\n[MCP result not retained: %d bytes is over the %d-byte limit]", len(raw), tool.ResultRetainLimit)
		return text, images, parseErr
	}
	ref, err := tool.RecordResult(ctx, raw)
	if err != nil {
		return text, images, fmt.Errorf("retain MCP result: %w", err)
	}
	text += "\n[MCP result artifact: " + ref + "]"
	if fields := structuredFields(result.Structured); fields != "" {
		text += "\n[structuredContent fields: " + fields + "]"
	}
	text += "\nUse read_file with this reference to inspect the full result."
	return text, images, parseErr
}

// structuredFields names an object's top-level keys, so a JSON pointer can be
// chosen without the values entering the conversation.
func structuredFields(raw json.RawMessage) string {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) == 0 {
		return ""
	}
	keys := slices.Sorted(maps.Keys(object))
	if len(keys) > 24 {
		keys = append(keys[:24], "…")
	}
	for i, k := range keys {
		if len(k) > 64 {
			keys[i] = k[:64] + "…"
		}
	}
	return strings.Join(keys, ", ")
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
