package tui

import (
	"context"
	"net/http"
)

func (c *Client) IsolatedCommand(ctx context.Context, args string) (string, error) {
	var reply struct {
		Text string `json:"text"`
	}
	err := c.do(ctx, http.MethodPost, "/isolated-results/command", map[string]string{"args": args}, &reply)
	return reply.Text, err
}

func isolatedCommand(ctx context.Context, client *Client, args string) slashDoneMsg {
	text, err := client.IsolatedCommand(ctx, args)
	if err != nil {
		return slashDoneMsg{level: "warn", text: err.Error()}
	}
	return slashDoneMsg{level: "info", text: text}
}
