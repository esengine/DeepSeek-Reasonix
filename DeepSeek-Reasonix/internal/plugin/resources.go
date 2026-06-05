package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Resource is an MCP resource a server exposes. In chat it is referenced as
// "@<server>:<uri>" (e.g. "@docs:file://README.md"); the referenced content is
// fetched and prepended to the message sent to the model.
type Resource struct {
	Server      string // owning server name
	URI         string // canonical resource uri
	Name        string // human-readable label
	Description string
	MimeType    string
}

func (c *Client) listResources(ctx context.Context) ([]Resource, error) {
	res, err := c.call(ctx, "resources/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Resources []struct {
			URI         string `json:"uri"`
			Name        string `json:"name"`
			Description string `json:"description"`
			MimeType    string `json:"mimeType"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("plugin %q: decode resources/list: %w", c.name, err)
	}
	resources := make([]Resource, 0, len(out.Resources))
	for _, r := range out.Resources {
		resources = append(resources, Resource{
			Server:      c.name,
			URI:         r.URI,
			Name:        r.Name,
			Description: r.Description,
			MimeType:    r.MimeType,
		})
	}
	return resources, nil
}

// readResource fetches a resource by uri and flattens its text contents. Binary
// (blob) contents are noted but not decoded — a coding agent consumes text.
func (c *Client) readResource(ctx context.Context, uri string) (string, error) {
	res, err := c.call(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return "", err
	}
	var out struct {
		Contents []struct {
			URI      string `json:"uri"`
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Blob     string `json:"blob"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", fmt.Errorf("plugin %q: decode resources/read: %w", c.name, err)
	}
	var sb strings.Builder
	for _, ct := range out.Contents {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		switch {
		case ct.Text != "":
			sb.WriteString(ct.Text)
		case ct.Blob != "":
			fmt.Fprintf(&sb, "[binary resource %s, %s — %d base64 bytes omitted]", ct.URI, ct.MimeType, len(ct.Blob))
		}
	}
	return sb.String(), nil
}
