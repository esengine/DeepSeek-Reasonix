package plugin

import (
	"context"
	"encoding/json"
	"fmt"
)

// maxListPages bounds one catalog read: a server that keeps handing out fresh
// cursors would otherwise hold startup open for as long as it likes.
const maxListPages = 64

// listAllPages reads every page of an MCP list method, following nextCursor
// until the server stops returning one. A cursor seen twice is a server loop,
// and an incomplete catalog is reported rather than served as whole.
func listAllPages[T any](ctx context.Context, c *Client, method, field string) ([]T, error) {
	var all []T
	seen := map[string]bool{}
	cursor := ""
	for range maxListPages {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		res, err := c.call(ctx, method, params)
		if err != nil {
			return nil, err
		}
		var page map[string]json.RawMessage
		if err := json.Unmarshal(res, &page); err != nil {
			return nil, fmt.Errorf("plugin %q: decode %s: %w", c.name, method, err)
		}
		if raw, ok := page[field]; ok && string(raw) != "null" {
			var items []T
			if err := json.Unmarshal(raw, &items); err != nil {
				return nil, fmt.Errorf("plugin %q: decode %s: %w", c.name, method, err)
			}
			all = append(all, items...)
		}
		next := ""
		if raw, ok := page["nextCursor"]; ok && string(raw) != "null" {
			if err := json.Unmarshal(raw, &next); err != nil {
				return nil, fmt.Errorf("plugin %q: decode %s nextCursor: %w", c.name, method, err)
			}
		}
		if next == "" {
			return all, nil
		}
		if seen[next] {
			return nil, &catalogPageError{}
		}
		seen[next] = true
		cursor = next
	}
	return nil, &catalogPageError{limit: maxListPages}
}
