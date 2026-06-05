package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Prompt is an MCP prompt exposed by a server. It surfaces in the chat TUI as a
// slash command "/mcp__<server>__<prompt>"; running it fetches the rendered
// prompt and sends it to the model as a turn.
type Prompt struct {
	Name        string      // "mcp__<server>__<prompt>" — the slash-command body
	Server      string      // owning server name
	Raw         string      // original prompt name for prompts/get
	Description string      // human-readable summary
	Args        []PromptArg // declared arguments, in order
	client      *Client
}

// PromptArg is one declared prompt argument. Reasonix maps space-separated
// positional command arguments onto these in order, matching Claude Code.
type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// Get fetches the prompt with the given arguments and flattens its returned
// messages into a single text block to send to the model.
func (p Prompt) Get(ctx context.Context, args map[string]string) (string, error) {
	return p.client.getPrompt(ctx, p.Raw, args)
}

func (c *Client) listPrompts(ctx context.Context) ([]Prompt, error) {
	res, err := c.call(ctx, "prompts/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out struct {
		Prompts []struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Arguments   []PromptArg `json:"arguments"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("plugin %q: decode prompts/list: %w", c.name, err)
	}
	prompts := make([]Prompt, 0, len(out.Prompts))
	for _, p := range out.Prompts {
		prompts = append(prompts, Prompt{
			Name:        "mcp__" + normalizeName(c.name) + "__" + normalizeName(p.Name),
			Server:      c.name,
			Raw:         p.Name,
			Description: p.Description,
			Args:        p.Arguments,
			client:      c,
		})
	}
	return prompts, nil
}

func (c *Client) getPrompt(ctx context.Context, name string, args map[string]string) (string, error) {
	params := map[string]any{"name": name}
	if len(args) > 0 {
		params["arguments"] = args
	}
	res, err := c.call(ctx, "prompts/get", params)
	if err != nil {
		return "", err
	}
	var out struct {
		Messages []struct {
			Role    string `json:"role"`
			Content struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return "", fmt.Errorf("plugin %q: decode prompts/get: %w", c.name, err)
	}
	var sb strings.Builder
	for _, m := range out.Messages {
		if m.Content.Type == "text" && m.Content.Text != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(m.Content.Text)
		}
	}
	return sb.String(), nil
}
