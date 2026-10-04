package tui

import "context"

// Command bodies the in-process kernel test drives through the public client.
func QueueCommand(ctx context.Context, c *Client, args []string) string {
	return queueCommand(ctx, c, args).text
}

func SteerCommand(ctx context.Context, c *Client, text string) string {
	return steerCommand(ctx, c, text).text
}

func CopyParts(msgs []HistoryMessage) []string { return copyParts(msgs) }
