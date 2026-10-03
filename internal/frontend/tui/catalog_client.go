package tui

import (
	"context"
	"net/http"
)

type ModelChoice struct {
	Ref      string `json:"ref"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Answers  string `json:"answers,omitempty"`
	Active   bool   `json:"active,omitempty"`
}

func (c *Client) Models(ctx context.Context) ([]ModelChoice, error) {
	var out struct {
		Models []ModelChoice `json:"models"`
	}
	err := c.do(ctx, http.MethodGet, "/models", nil, &out)
	return out.Models, err
}

func (c *Client) SelectModel(ctx context.Context, ref string) error {
	return c.do(ctx, http.MethodPost, "/model", map[string]string{"ref": ref}, nil)
}

type CapabilityChoice struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	State       string `json:"state,omitempty"`
	Enabled     bool   `json:"enabled"`
}

func (c *Client) Capabilities(ctx context.Context, kind string) ([]CapabilityChoice, error) {
	var out struct {
		Skills  []CapabilityChoice `json:"skills"`
		Servers []CapabilityChoice `json:"servers"`
	}
	err := c.do(ctx, http.MethodGet, "/"+kind, nil, &out)
	if kind == "mcp" {
		return out.Servers, err
	}
	return out.Skills, err
}

func (c *Client) SetCapabilityEnabled(ctx context.Context, kind, name string, enabled bool) error {
	return c.do(ctx, http.MethodPost, "/"+kind+"/enabled", map[string]any{"name": name, "enabled": enabled, "scope": "project"}, nil)
}
