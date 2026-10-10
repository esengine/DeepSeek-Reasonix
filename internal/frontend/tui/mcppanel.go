package tui

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/base/i18n"
	"reasonix/internal/base/textutil"
	"reasonix/internal/frontend/termrender"
)

// MCPTool is one tool a server offers.
type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReadOnly    bool   `json:"readOnly"`
	Destructive bool   `json:"destructive"`
}

// MCPServer is one configured or connected MCP server as the kernel reports it.
type MCPServer struct {
	Name        string    `json:"name"`
	State       string    `json:"state"`
	Enabled     bool      `json:"enabled"`
	Transport   string    `json:"transport"`
	Source      string    `json:"source"`
	Description string    `json:"description"`
	Tools       int       `json:"tools"`
	ToolList    []MCPTool `json:"toolList"`
	Error       string    `json:"error"`
	Launch      string    `json:"launch"`
	// PendingReason is the kernel's code for why a pending server waits.
	PendingReason string `json:"pendingReason"`
}

func (c *Client) MCPServers(ctx context.Context) ([]MCPServer, error) {
	var out struct {
		Servers []MCPServer `json:"servers"`
	}
	err := c.do(ctx, http.MethodGet, "/mcp", nil, &out)
	return out.Servers, err
}

func (c *Client) SetMCPEnabled(ctx context.Context, name string, enabled bool) error {
	return c.do(ctx, http.MethodPost, "/mcp/enabled",
		map[string]any{"name": name, "enabled": enabled, "scope": "project"}, nil)
}

func (c *Client) ReconnectMCP(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodPost, "/mcp/reconnect", map[string]string{"name": name}, nil)
}

// mcpPanel lists the MCP servers; Enter on one shows its tools.
type mcpPanel struct {
	servers []MCPServer
	sel     int
	detail  bool
	busy    bool
	confirm string
}

type mcpMsg struct {
	servers []MCPServer
	err     error
	keep    string
	open    bool
}

func (m *model) openMCP() tea.Cmd { return m.fetchMCP("", false) }

func (m *model) fetchMCP(keep string, open bool) tea.Cmd {
	return func() tea.Msg {
		list, err := m.client.MCPServers(m.ctx)
		return mcpMsg{servers: list, err: err, keep: keep, open: open}
	}
}

func (m *model) onMCP(msg mcpMsg) tea.Cmd {
	if msg.err != nil {
		m.tr.AddNotice("error", fmt.Sprintf(i18n.M.McpPanelErrFmt, msg.err))
		if m.mcp != nil {
			m.mcp.busy = false
		}
		return m.commit()
	}
	if len(msg.servers) == 0 {
		m.mcp = nil
		m.tr.AddNotice("info", i18n.M.SlashMCPNone)
		return m.commit()
	}
	p := m.mcp
	if p == nil {
		p = &mcpPanel{}
		m.mcp = p
	}
	p.servers, p.busy = msg.servers, false
	for i, s := range p.servers {
		if s.Name == msg.keep {
			p.sel = i
		}
	}
	p.sel = min(p.sel, len(p.servers)-1)
	return nil
}

func (m *model) mcpKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := m.mcp
	if p == nil {
		return nil, false
	}
	if p.busy {
		return nil, true
	}
	cur := p.servers[p.sel]
	if p.confirm != "" {
		if msg.String() == "y" && p.confirm == cur.Name {
			p.confirm = ""
			return m.mcpToggle(cur), true
		}
		p.confirm = ""
		return nil, true
	}
	switch msg.String() {
	case "q":
		m.mcp = nil
	case "esc", "ctrl+c", "left", "h":
		if p.detail {
			p.detail = false
		} else {
			m.mcp = nil
		}
	case "up", "k":
		p.sel = max(p.sel-1, 0)
	case "down", "j":
		p.sel = min(p.sel+1, len(p.servers)-1)
	case "enter", "right", "l":
		p.detail = true
	case "space":
		if !cur.Enabled && repoDeclared(cur) {
			p.confirm = cur.Name
			return nil, true
		}
		return m.mcpToggle(cur), true
	case "r":
		if !cur.Enabled || cur.State != "failed" {
			return nil, true
		}
		p.busy = true
		return m.mcpAction(cur.Name, func(ctx context.Context) error {
			return m.client.ReconnectMCP(ctx, cur.Name)
		}), true
	}
	return nil, true
}

// repoDeclared reports a server a repository's own files declare: starting
// it runs what a stranger wrote.
func repoDeclared(s MCPServer) bool {
	return s.Source == "project_mcp_json" || s.Source == "project_config"
}

func (m *model) mcpToggle(s MCPServer) tea.Cmd {
	m.mcp.busy = true
	return m.mcpAction(s.Name, func(ctx context.Context) error {
		return m.client.SetMCPEnabled(ctx, s.Name, !s.Enabled)
	})
}

func (m *model) mcpAction(name string, do func(context.Context) error) tea.Cmd {
	return func() tea.Msg {
		if err := do(m.ctx); err != nil {
			return mcpActionErrMsg{name: name, err: err}
		}
		list, err := m.client.MCPServers(m.ctx)
		return mcpMsg{servers: list, err: err, keep: name}
	}
}

type mcpActionErrMsg struct {
	name string
	err  error
}

func (m *model) onMCPActionErr(msg mcpActionErrMsg) tea.Cmd {
	m.tr.AddNotice("error", fmt.Sprintf(i18n.M.McpActionErrFmt, msg.name, msg.err))
	return tea.Batch(m.commit(), m.fetchMCP(msg.name, false))
}

func (m *model) mcpPanelLines() []string {
	p := m.mcp
	cur := p.servers[p.sel]
	if p.detail {
		return m.mcpDetail(cur)
	}
	on := 0
	for _, s := range p.servers {
		if s.Enabled {
			on++
		}
	}
	lines := []string{termrender.Accent(i18n.M.McpPanelTitle),
		termrender.Dim("  " + fmt.Sprintf(i18n.M.McpPanelSummaryFmt, len(p.servers), on))}
	start := max(min(p.sel-pickerRows/2, len(p.servers)-pickerRows), 0)
	end := min(start+pickerRows, len(p.servers))
	if start > 0 {
		lines = append(lines, termrender.Dim("  "+i18n.M.ListMoreAbove))
	}
	width := max(m.width-8, 12)
	for i := start; i < end; i++ {
		s := p.servers[i]
		state := s.State
		switch {
		case s.State == "pending" && s.PendingReason == "changed_since_enabled":
			state = i18n.M.McpPanelChanged
		case s.State == "pending":
			state = i18n.M.McpPanelPending
		case !s.Enabled:
			state = i18n.M.McpPanelOff
		}
		meta := []string{state}
		if s.Tools > 0 {
			meta = append(meta, fmt.Sprintf(i18n.M.McpPanelToolsFmt, s.Tools))
		}
		if s.Transport != "" {
			meta = append(meta, s.Transport)
		}
		if s.Source != "" {
			meta = append(meta, s.Source)
		}
		lines = append(lines, rowLine(i == p.sel, i+1, "", clipVisible(textutil.SanitizeLaunch(s.Name)+" · "+strings.Join(meta, " · "), width), s.Enabled))
	}
	if end < len(p.servers) {
		lines = append(lines, termrender.Dim("  "+i18n.M.ListMoreBelow))
	}
	if p.confirm != "" {
		lines = append(lines, termrender.Yellow("  "+fmt.Sprintf(i18n.M.McpPanelConfirmFmt, textutil.SanitizeLaunch(p.confirm), clipVisible(textutil.SanitizeLaunch(cur.Launch), width-8))))
	} else {
		if cur.State == "pending" && cur.Launch != "" {
			lines = append(lines, termrender.Yellow("  "+fmt.Sprintf(i18n.M.McpPanelLaunchFmt, clipVisible(textutil.SanitizeLaunch(cur.Launch), width-8))))
		}
		lines = append(lines, termrender.Dim(i18n.M.McpPanelHint))
	}
	return panel(lines, m.width, accentEdge)
}

func (m *model) mcpDetail(s MCPServer) []string {
	width := max(m.width-8, 12)
	lines := []string{termrender.Accent(textutil.SanitizeLaunch(s.Name)), termrender.Dim("  " + textutil.SanitizeLaunch(s.State+" · "+s.Transport+" · "+s.Source))}
	if s.Description != "" {
		lines = append(lines, "  "+clipVisible(strings.Join(strings.Fields(s.Description), " "), width))
	}
	if s.Error != "" {
		lines = append(lines, "  "+termrender.Red(clipVisible(strings.Join(strings.Fields(s.Error), " "), width)))
	}
	if len(s.ToolList) == 0 {
		lines = append(lines, termrender.Dim("  "+i18n.M.McpPanelNoTools))
	}
	for i, t := range s.ToolList {
		if i >= pickerRows*2 {
			lines = append(lines, termrender.Dim("  "+fmt.Sprintf(i18n.M.ListMoreFmt, len(s.ToolList)-i)))
			break
		}
		tag := ""
		switch {
		case t.Destructive:
			tag = " · " + i18n.M.McpToolDestructive
		case t.ReadOnly:
			tag = " · " + i18n.M.McpToolReadOnly
		}
		lines = append(lines, "  "+clipVisible(t.Name+tag, width))
	}
	lines = append(lines, termrender.Dim(i18n.M.McpPanelDetailHint))
	return panel(lines, m.width, accentEdge)
}
