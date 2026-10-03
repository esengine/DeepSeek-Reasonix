package tui

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/frontend/termrender"
)

type MCPDraftServer struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	URL       string            `json:"url,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type MCPDraftRisk struct {
	Server string `json:"server"`
	Kind   string `json:"kind"`
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

type MCPDraft struct {
	Servers []MCPDraftServer `json:"servers"`
	Risks   []MCPDraftRisk   `json:"risks"`
}

func (c *Client) ParseMCP(ctx context.Context, input string) (MCPDraft, error) {
	var out MCPDraft
	err := c.do(ctx, http.MethodPost, "/mcp/parse", map[string]string{"input": input}, &out)
	return out, err
}

func (c *Client) InstallMCP(ctx context.Context, server MCPDraftServer) error {
	return c.do(ctx, http.MethodPost, "/mcp/install", map[string]any{"server": server, "scope": "user"}, nil)
}

type mcpAddState struct {
	input string
	draft *MCPDraft
	busy  bool
}

type mcpParsedMsg struct {
	picker *catalogPicker
	draft  MCPDraft
	err    error
}

func (m *model) mcpAddKey(msg tea.KeyPressMsg) tea.Cmd {
	p, s := m.catalog, m.catalog.add
	if s.busy {
		return nil
	}
	switch msg.String() {
	case "esc", "ctrl+c":
		if s.draft != nil {
			s.draft = nil
		} else {
			m.catalog = nil
		}
	case "enter":
		if s.draft != nil {
			servers := s.draft.Servers
			p.saving = true
			return func() tea.Msg {
				for _, server := range servers {
					if err := m.client.InstallMCP(m.ctx, server); err != nil {
						return catalogSavedMsg{picker: p, err: err}
					}
				}
				return catalogSavedMsg{picker: p}
			}
		}
		if strings.TrimSpace(s.input) == "" {
			return nil
		}
		s.busy = true
		input := s.input
		return func() tea.Msg {
			draft, err := m.client.ParseMCP(m.ctx, input)
			return mcpParsedMsg{picker: p, draft: draft, err: err}
		}
	case "backspace":
		if s.draft == nil && s.input != "" {
			_, n := utf8.DecodeLastRuneInString(s.input)
			s.input = s.input[:len(s.input)-n]
		}
	default:
		if s.draft == nil {
			s.input += typedText(msg)
		}
	}
	return nil
}

func (m *model) onMCPParsed(msg mcpParsedMsg) tea.Cmd {
	if m.catalog != msg.picker {
		return nil
	}
	s := m.catalog.add
	s.busy = false
	if msg.err != nil {
		m.tr.AddNotice("error", "mcp parse: "+msg.err.Error())
		return m.commit()
	}
	s.draft = &msg.draft
	return nil
}

func (m *model) mcpAddPanel() []string {
	s := m.catalog.add
	lines := []string{termrender.Accent("Add MCP server")}
	if s.draft == nil {
		lines = append(lines, "  Paste an MCP declaration", "  "+oneLine(s.input, max(m.width-8, 1)), termrender.Dim("Enter preview · Esc cancel"))
	} else {
		lines = append(lines, "  Review before installing in user config:")
		for _, server := range s.draft.Servers {
			lines = append(lines, "  "+server.Name+" · "+server.Transport)
			lines = append(lines, "    "+oneLine(strings.Join(append([]string{server.Command}, server.Args...), " "), max(m.width-10, 1)))
			if server.URL != "" {
				lines = append(lines, "    "+oneLine(server.URL, max(m.width-10, 1)))
			}
		}
		for _, risk := range s.draft.Risks {
			lines = append(lines, termrender.Yellow("  "+oneLine(risk.Server+": "+risk.Detail, max(m.width-8, 1))))
		}
		lines = append(lines, termrender.Dim("Enter install and connect · Esc back"))
	}
	if s.busy || m.catalog.saving {
		lines = append(lines, termrender.Dim("Working…"))
	}
	return panel(lines, m.width, accentEdge)
}
