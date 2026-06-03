package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/mcpdiag"
	"reasonix/internal/plugin"
)

const (
	mcpListMaxRows = 10
	mcpToolMaxRows = 14
)

type mcpStage int

const (
	mcpStageList mcpStage = iota
	mcpStageDetail
	mcpStageTools
	mcpStageLogs
	mcpStageMode
	mcpStageConfirmRemove
	mcpStageConfirmClearAuth
)

type mcpManager struct {
	stage    mcpStage
	snapshot mcpSnapshot
	sel      int
	name     string
	action   int
	mode     int
	confirm  int
}

type mcpSnapshot struct {
	servers    []mcpServerView
	configPath string
	err        string
}

type mcpServerView struct {
	Name       string
	Transport  string
	Status     string
	BuiltIn    bool
	Configured bool
	AutoStart  bool
	Tier       string
	Command    string
	Args       []string
	URL        string
	EnvKeys    []string
	Tools      int
	Prompts    int
	Resources  int
	Error      string
	ToolList   []plugin.ToolInfo
	AuthStatus string
	AuthURL    string

	authConfigured bool
}

type mcpAction string

const (
	mcpActionViewTools mcpAction = "view-tools"
	mcpActionMode      mcpAction = "mode"
	mcpActionEdit      mcpAction = "edit"
	mcpActionConnect   mcpAction = "connect"
	mcpActionAuth      mcpAction = "auth"
	mcpActionClearAuth mcpAction = "clear-auth"
	mcpActionLogs      mcpAction = "logs"
	mcpActionDisable   mcpAction = "disable"
	mcpActionRemove    mcpAction = "remove"
)

type mcpActionItem struct {
	kind  mcpAction
	label string
}

type mcpExternalDoneMsg struct {
	label  string
	target string
	err    error
}

var mcpModeChoices = []struct {
	tier  string
	label string
	desc  string
}{
	{"lazy", "Connect when this MCP is used", "Do not pre-connect; connect automatically on first tool use."},
	{"background", "Connect in background after session starts", "New sessions connect automatically without blocking chat."},
	{"eager", "Connect before chat starts", "New sessions wait for this MCP before chat begins."},
}

func (m *chatTUI) openMCPManager(name string) {
	m.mcp = &mcpManager{stage: mcpStageList, snapshot: m.buildMCPSnapshot()}
	if name != "" {
		m.mcp.selectName(name)
		m.mcp.stage = mcpStageDetail
	}
	m.mcp.clamp()
}

func (m *chatTUI) refreshMCPManager() {
	if m.mcp == nil {
		return
	}
	m.mcp.snapshot = m.buildMCPSnapshot()
	m.mcp.clamp()
}

func (m chatTUI) renderMCPManager() string {
	if m.mcp == nil {
		return ""
	}
	return m.mcp.render(m.width)
}

func (m chatTUI) handleMCPManagerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := m.mcp
	if p == nil {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c", "q":
		m.mcp = nil
		return m, nil
	case "esc", "left", "h":
		switch p.stage {
		case mcpStageList:
			m.mcp = nil
			return m, nil
		case mcpStageDetail:
			p.stage = mcpStageList
			p.action = 0
			return m, nil
		default:
			p.stage = mcpStageDetail
			p.action = 0
			if p.name == "" {
				p.stage = mcpStageList
			}
			return m, nil
		}
	}

	switch p.stage {
	case mcpStageList:
		switch msg.String() {
		case "up", "k":
			if p.sel > 0 {
				p.sel--
			}
		case "down", "j":
			if p.sel < len(p.snapshot.servers)-1 {
				p.sel++
			}
		case "enter", "right", "l":
			if len(p.snapshot.servers) > 0 {
				p.name = p.snapshot.servers[p.sel].Name
				p.stage = mcpStageDetail
				p.action = 0
			}
		}
	case mcpStageDetail:
		v, ok := p.selectedServer()
		if !ok {
			p.stage = mcpStageList
			return m, nil
		}
		actions := mcpActionsFor(v, p.snapshot.configPath)
		switch msg.String() {
		case "up", "k":
			if p.action > 0 {
				p.action--
			}
		case "down", "j":
			if p.action < len(actions)-1 {
				p.action++
			}
		case "enter":
			if len(actions) > 0 {
				return m.applyMCPAction(v, actions[p.action].kind)
			}
		default:
			if idx, ok := numberKeyIndex(msg.String(), len(actions)); ok {
				p.action = idx
				return m.applyMCPAction(v, actions[p.action].kind)
			}
		}
	case mcpStageMode:
		switch msg.String() {
		case "up", "k":
			if p.mode > 0 {
				p.mode--
			}
		case "down", "j":
			if p.mode < len(mcpModeChoices)-1 {
				p.mode++
			}
		case "enter":
			return m.applyMCPMode(mcpModeChoices[p.mode].tier)
		default:
			if idx, ok := numberKeyIndex(msg.String(), len(mcpModeChoices)); ok {
				p.mode = idx
				return m.applyMCPMode(mcpModeChoices[p.mode].tier)
			}
		}
	case mcpStageConfirmRemove:
		switch msg.String() {
		case "up", "k", "down", "j":
			if p.confirm == 0 {
				p.confirm = 1
			} else {
				p.confirm = 0
			}
		case "y":
			p.confirm = 0
			return m.removeSelectedMCP()
		case "n":
			p.stage = mcpStageDetail
		case "enter":
			if p.confirm == 0 {
				return m.removeSelectedMCP()
			}
			p.stage = mcpStageDetail
		}
	case mcpStageConfirmClearAuth:
		switch msg.String() {
		case "up", "k", "down", "j":
			if p.confirm == 0 {
				p.confirm = 1
			} else {
				p.confirm = 0
			}
		case "y":
			p.confirm = 0
			return m.clearSelectedMCPAuthentication()
		case "n":
			p.stage = mcpStageDetail
		case "enter":
			if p.confirm == 0 {
				return m.clearSelectedMCPAuthentication()
			}
			p.stage = mcpStageDetail
		}
	}
	return m, nil
}

func (p *mcpManager) clamp() {
	if p.sel < 0 {
		p.sel = 0
	}
	if n := len(p.snapshot.servers); n > 0 && p.sel >= n {
		p.sel = n - 1
	}
	if p.name != "" {
		p.selectName(p.name)
	}
	if p.action < 0 {
		p.action = 0
	}
	if p.mode < 0 {
		p.mode = 0
	}
	if p.mode >= len(mcpModeChoices) {
		p.mode = len(mcpModeChoices) - 1
	}
	if p.confirm < 0 || p.confirm > 1 {
		p.confirm = 0
	}
}

func (p *mcpManager) selectName(name string) bool {
	for i, s := range p.snapshot.servers {
		if s.Name == name {
			p.sel = i
			p.name = name
			return true
		}
	}
	return false
}

func (p *mcpManager) selectedServer() (mcpServerView, bool) {
	if p.name != "" {
		for _, s := range p.snapshot.servers {
			if s.Name == p.name {
				return s, true
			}
		}
	}
	if p.sel >= 0 && p.sel < len(p.snapshot.servers) {
		return p.snapshot.servers[p.sel], true
	}
	return mcpServerView{}, false
}

func (p *mcpManager) render(width int) string {
	w := max(viewWidth(width), 40)
	switch p.stage {
	case mcpStageDetail:
		return choicePanelStyle.Width(w).Render(p.renderDetail(w))
	case mcpStageTools:
		return choicePanelStyle.Width(w).Render(p.renderTools(w))
	case mcpStageLogs:
		return choicePanelStyle.Width(w).Render(p.renderLogs(w))
	case mcpStageMode:
		return choicePanelStyle.Width(w).Render(p.renderMode(w))
	case mcpStageConfirmRemove:
		return choicePanelStyle.Width(w).Render(p.renderConfirmRemove(w))
	case mcpStageConfirmClearAuth:
		return choicePanelStyle.Width(w).Render(p.renderConfirmClearAuth(w))
	default:
		return choicePanelStyle.Width(w).Render(p.renderList(w))
	}
}

func (p *mcpManager) renderList(width int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", viewHeader("Manage MCP servers"))
	fmt.Fprintf(&b, "%s\n\n", viewMeta(fmt.Sprintf("%d servers", len(p.snapshot.servers))))
	if p.snapshot.err != "" {
		fmt.Fprintf(&b, "%s\n\n", yellow("config: "+p.snapshot.err))
	}
	if len(p.snapshot.servers) == 0 {
		b.WriteString(viewMeta("No MCP servers configured. Use /mcp add <name> ... to add one.") + "\n\n")
		b.WriteString(dim("↑/↓ navigate · Enter to confirm · Esc to cancel"))
		return b.String()
	}
	start, end := visibleRange(len(p.snapshot.servers), p.sel, mcpListMaxRows)
	if start > 0 {
		fmt.Fprintf(&b, "%s\n", viewMeta(fmt.Sprintf("↑ %d more above", start)))
	}
	lastGroup := ""
	for i := start; i < end; i++ {
		s := p.snapshot.servers[i]
		group := "User MCPs"
		if s.BuiltIn {
			group = "Built-in MCPs"
		}
		if group != lastGroup {
			if lastGroup != "" {
				b.WriteByte('\n')
			}
			header := group
			if group == "User MCPs" && p.snapshot.configPath != "" {
				header += " (" + p.snapshot.configPath + ")"
			}
			fmt.Fprintf(&b, "  %s\n", bold(header))
			lastGroup = group
		}
		b.WriteString(p.renderListRow(i, s, width) + "\n")
	}
	if end < len(p.snapshot.servers) {
		fmt.Fprintf(&b, "%s\n", viewMeta(fmt.Sprintf("↓ %d more below", len(p.snapshot.servers)-end)))
	}
	b.WriteString("\n" + dim("↑/↓ navigate · Enter for details · Esc to close"))
	return strings.TrimRight(b.String(), "\n")
}

func (p *mcpManager) renderListRow(i int, s mcpServerView, width int) string {
	prefix := "    "
	if i == p.sel {
		prefix = accent("  › ")
	}
	nameWidth := min(28, max(12, width/3))
	name := compactMiddle(s.Name, nameWidth)
	status := mcpStatusLabel(s)
	meta := fmt.Sprintf("%s · %s", status, countText(s.Tools, "tool"))
	if s.Prompts > 0 {
		meta += " · " + countText(s.Prompts, "prompt")
	}
	if s.Resources > 0 {
		meta += " · " + countText(s.Resources, "resource")
	}
	if s.Transport != "" {
		meta = s.Transport + " · " + meta
	}
	used := visibleWidth(prefix) + nameWidth + 3
	meta = viewCompactText(meta, viewBudget(width, used))
	name = padRight(name, nameWidth)
	if i == p.sel {
		name = bold(name)
	}
	return fmt.Sprintf("%s%s · %s", prefix, name, viewMeta(meta))
}

func (p *mcpManager) renderDetail(width int) string {
	v, ok := p.selectedServer()
	if !ok {
		return "MCP server not found\n\n" + dim("Esc to back")
	}
	actions := mcpActionsFor(v, p.snapshot.configPath)
	if p.action >= len(actions) {
		p.action = max(0, len(actions)-1)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s MCP Server\n\n", bold(titleText(v.Name)))
	writeMCPDetailField(&b, "Status", mcpStatusLabel(v))
	if auth := mcpAuthLabel(v); auth != "" {
		writeMCPDetailField(&b, "Auth", auth)
	}
	writeMCPDetailField(&b, "Transport", fallbackText(v.Transport, "unknown"))
	if v.BuiltIn {
		writeMCPDetailField(&b, "Config location", "built-in")
	} else {
		loc := fallbackText(p.snapshot.configPath, "not saved")
		if loc != "not saved" {
			loc = viewCompactPath(loc, viewBudget(width, 18))
		}
		writeMCPDetailField(&b, "Config location", loc)
	}
	if v.Configured {
		writeMCPDetailField(&b, "Connection mode", mcpModeLabel(v.Tier))
	}
	writeMCPDetailField(&b, "Capabilities", mcpCapabilitiesText(v))
	writeMCPDetailField(&b, "Tools", countText(v.Tools, "tool"))
	if line := mcpCommandLine(v); line != "" {
		writeMCPDetailField(&b, mcpCommandLabel(v), viewCompactText(line, viewBudget(width, 18)))
	}
	if len(v.EnvKeys) > 0 {
		writeMCPDetailField(&b, "Env", strings.Join(v.EnvKeys, ", "))
	}
	if v.Error != "" {
		writeMCPDetailField(&b, "Error", viewCompactText(v.Error, viewBudget(width, 18)))
	}
	b.WriteByte('\n')
	if len(actions) == 0 {
		b.WriteString(viewMeta("No actions available.") + "\n")
	} else {
		for i, a := range actions {
			b.WriteString(rowLine(i == p.action, i+1, "", a.label, false) + "\n")
		}
	}
	b.WriteString(dim("↑/↓ navigate · Enter to select · Esc to back"))
	return strings.TrimRight(b.String(), "\n")
}

func (p *mcpManager) renderTools(width int) string {
	v, ok := p.selectedServer()
	if !ok {
		return "MCP server not found\n\n" + dim("Esc to back")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s tools\n\n", bold(v.Name))
	if len(v.ToolList) == 0 {
		b.WriteString(viewMeta("Current connection did not return tool details.") + "\n")
	} else {
		limit := len(v.ToolList)
		if limit > mcpToolMaxRows {
			limit = mcpToolMaxRows
		}
		for _, t := range v.ToolList[:limit] {
			desc := viewCompactText(t.Description, viewBudget(width, 24))
			fmt.Fprintf(&b, "  %-20s %s\n", t.Name, viewMeta(desc))
		}
		if extra := len(v.ToolList) - limit; extra > 0 {
			fmt.Fprintf(&b, "%s\n", viewMore(extra, "tools"))
		}
	}
	b.WriteString("\n" + dim("Esc to back"))
	return strings.TrimRight(b.String(), "\n")
}

func (p *mcpManager) renderLogs(width int) string {
	v, ok := p.selectedServer()
	if !ok {
		return "MCP server not found\n\n" + dim("Esc to back")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s logs\n\n", bold(v.Name))
	if strings.TrimSpace(v.Error) == "" {
		b.WriteString(viewMeta("No failure log recorded for this MCP.") + "\n")
	} else {
		b.WriteString(viewProtectLines(v.Error, viewBudget(width, 2)) + "\n")
	}
	b.WriteString("\n" + dim("Esc to back"))
	return strings.TrimRight(b.String(), "\n")
}

func (p *mcpManager) renderMode(width int) string {
	v, ok := p.selectedServer()
	if !ok {
		return "MCP server not found\n\n" + dim("Esc to back")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Connection mode for %s\n\n", bold(v.Name))
	for i, choice := range mcpModeChoices {
		active := choice.tier == v.Tier
		line := rowLine(i == p.mode, i+1, "", choice.label, active)
		b.WriteString(line + "\n")
		b.WriteString(dim("       "+viewCompactText(choice.desc, viewBudget(width, 7))) + "\n")
	}
	b.WriteString(dim("Enter to apply · Esc to back"))
	return strings.TrimRight(b.String(), "\n")
}

func (p *mcpManager) renderConfirmRemove(width int) string {
	v, ok := p.selectedServer()
	if !ok {
		return "MCP server not found\n\n" + dim("Esc to back")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Remove MCP server %q?\n", v.Name)
	b.WriteString(viewMeta("This removes it from Reasonix config. It cannot be undone from this panel.") + "\n\n")
	b.WriteString(rowLine(p.confirm == 0, 1, "", "Confirm remove", false) + "\n")
	b.WriteString(rowLine(p.confirm == 1, 2, "", "Cancel", false) + "\n")
	b.WriteString(dim("Enter to select · y confirm · n cancel · Esc to back"))
	return strings.TrimRight(b.String(), "\n")
}

func (p *mcpManager) renderConfirmClearAuth(width int) string {
	v, ok := p.selectedServer()
	if !ok {
		return "MCP server not found\n\n" + dim("Esc to back")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Clear authentication for MCP server %q?\n", v.Name)
	hint := "This removes local auth-like headers, environment values, and URL tokens; the server stays in config."
	b.WriteString(viewMeta(viewCompactText(hint, viewBudget(width, 0))) + "\n\n")
	b.WriteString(rowLine(p.confirm == 0, 1, "", "Confirm clear authentication", false) + "\n")
	b.WriteString(rowLine(p.confirm == 1, 2, "", "Cancel", false) + "\n")
	b.WriteString(dim("Enter to select · y confirm · n cancel · Esc to back"))
	return strings.TrimRight(b.String(), "\n")
}

func (m chatTUI) buildMCPSnapshot() mcpSnapshot {
	snap := mcpSnapshot{configPath: mcpConfigLocation()}
	cfg, err := config.Load()
	if err != nil {
		snap.err = err.Error()
	}
	configured := map[string]config.PluginEntry{}
	var configuredEntries []config.PluginEntry
	codegraphEnabled := false
	if cfg != nil {
		codegraphEnabled = cfg.Codegraph.Enabled
		configuredEntries = append(configuredEntries, cfg.Plugins...)
		for _, p := range configuredEntries {
			configured[p.Name] = p
		}
	}
	seen := map[string]bool{}
	if m.host != nil {
		for _, s := range m.host.Servers() {
			v := mcpServerView{
				Name: s.Name, Transport: fallbackText(s.Transport, "stdio"), Status: "connected",
				BuiltIn: s.Name == "codegraph",
				Tools:   s.Tools, Prompts: s.Prompts, Resources: s.Resources,
				ToolList: append([]plugin.ToolInfo(nil), s.ToolList...),
			}
			if p, ok := configured[s.Name]; ok {
				v = withMCPPluginConfig(v, p)
			}
			snap.servers = append(snap.servers, v)
			seen[s.Name] = true
		}
		for _, f := range m.host.Failures() {
			v := mcpServerView{
				Name: f.Name, Transport: fallbackText(f.Transport, "stdio"), Status: "failed",
				BuiltIn: f.Name == "codegraph",
				Error:   f.Error,
			}
			if p, ok := configured[f.Name]; ok {
				v = withMCPPluginConfig(v, p)
			}
			snap.servers = append(snap.servers, v)
			seen[f.Name] = true
		}
	}
	for _, p := range configuredEntries {
		if seen[p.Name] {
			continue
		}
		v := mcpServerView{Name: p.Name}
		switch {
		case m.mcpDisabled[p.Name] || !p.ShouldAutoStart():
			v.Status = "disabled"
		case p.ResolvedTier() == "background" || p.ResolvedTier() == "eager":
			v.Status = "initializing"
		default:
			v.Status = "deferred"
		}
		v = withMCPPluginConfig(v, p)
		snap.servers = append(snap.servers, v)
		seen[p.Name] = true
	}
	if codegraphEnabled && !seen["codegraph"] {
		status := "initializing"
		if m.mcpDisabled["codegraph"] {
			status = "disabled"
		}
		snap.servers = append(snap.servers, mcpServerView{
			Name: "codegraph", Transport: "stdio", Status: status, BuiltIn: true,
		})
	}
	return snap
}

func withMCPPluginConfig(v mcpServerView, p config.PluginEntry) mcpServerView {
	transport := strings.ToLower(strings.TrimSpace(p.Type))
	if transport == "" {
		transport = "stdio"
	}
	v.Transport = transport
	v.Configured = true
	v.AutoStart = p.ShouldAutoStart()
	v.Tier = p.ResolvedTier()
	v.Command = p.Command
	v.Args = append([]string(nil), p.Args...)
	v.URL = p.URL
	v.authConfigured = mcpdiag.HasAuthConfig(p.Headers, p.Env, p.URL)
	if len(p.Env) > 0 {
		v.EnvKeys = make([]string, 0, len(p.Env))
		for k := range p.Env {
			v.EnvKeys = append(v.EnvKeys, k)
		}
		sort.Strings(v.EnvKeys)
	}
	auth := mcpdiag.DiagnoseAuth(v.Transport, v.Status, v.Error, v.URL, v.authConfigured)
	v.AuthStatus = auth.Status
	v.AuthURL = auth.URL
	return v
}

func (m chatTUI) applyMCPAction(v mcpServerView, action mcpAction) (tea.Model, tea.Cmd) {
	switch action {
	case mcpActionViewTools:
		m.mcp.stage = mcpStageTools
	case mcpActionMode:
		m.mcp.stage = mcpStageMode
		m.mcp.mode = mcpModeIndex(v.Tier)
	case mcpActionEdit:
		return m.openMCPConfig()
	case mcpActionAuth:
		return m.authenticateMCP(v)
	case mcpActionClearAuth:
		m.mcp.stage = mcpStageConfirmClearAuth
		m.mcp.confirm = 1
	case mcpActionConnect:
		return m.connectSelectedMCP(v)
	case mcpActionLogs:
		m.mcp.stage = mcpStageLogs
	case mcpActionDisable:
		return m.disableSelectedMCP(v)
	case mcpActionRemove:
		m.mcp.stage = mcpStageConfirmRemove
		m.mcp.confirm = 1
	}
	return m, nil
}

func (m chatTUI) connectSelectedMCP(v mcpServerView) (tea.Model, tea.Cmd) {
	if m.ctrl == nil {
		m.notice("mcp: no active session")
		return m, nil
	}
	if v.Status == "connected" {
		m.ctrl.DisconnectMCPServer(v.Name)
	}
	n, err := m.ctrl.ConnectConfiguredMCPServer(v.Name)
	if err != nil {
		m.notice("mcp connect: " + err.Error())
		return m, nil
	}
	if m.mcpDisabled != nil {
		delete(m.mcpDisabled, v.Name)
	}
	m.host = m.ctrl.Host()
	m.refreshMCPManager()
	if m.mcp != nil {
		m.mcp.stage = mcpStageDetail
		m.mcp.selectName(v.Name)
	}
	m.notice(fmt.Sprintf("connected %s — %d tools (available next message)", v.Name, n))
	return m, nil
}

func (m chatTUI) disableSelectedMCP(v mcpServerView) (tea.Model, tea.Cmd) {
	if m.ctrl == nil {
		m.notice("mcp: no active session")
		return m, nil
	}
	if m.mcpDisabled == nil {
		m.mcpDisabled = map[string]bool{}
	}
	m.mcpDisabled[v.Name] = true
	m.ctrl.DisconnectMCPServer(v.Name)
	m.host = m.ctrl.Host()
	m.refreshMCPManager()
	if m.mcp != nil {
		m.mcp.stage = mcpStageDetail
		m.mcp.selectName(v.Name)
	}
	m.notice("disabled " + v.Name + " for this session")
	return m, nil
}

func (m chatTUI) removeSelectedMCP() (tea.Model, tea.Cmd) {
	v, ok := m.mcp.selectedServer()
	if !ok {
		m.mcp.stage = mcpStageList
		return m, nil
	}
	if m.ctrl == nil {
		m.notice("mcp: no active session")
		return m, nil
	}
	disconnected, err := m.ctrl.RemoveMCPServer(v.Name)
	if err != nil {
		m.notice("mcp remove: " + err.Error())
		m.mcp.stage = mcpStageDetail
		return m, nil
	}
	if m.mcpDisabled != nil {
		delete(m.mcpDisabled, v.Name)
	}
	m.host = m.ctrl.Host()
	m.refreshMCPManager()
	if m.mcp != nil {
		m.mcp.stage = mcpStageList
		m.mcp.name = ""
	}
	if disconnected {
		m.notice("disconnected " + v.Name + " and removed it from config")
	} else {
		m.notice("removed " + v.Name + " from config")
	}
	return m, nil
}

func (m chatTUI) applyMCPMode(tier string) (tea.Model, tea.Cmd) {
	v, ok := m.mcp.selectedServer()
	if !ok {
		return m, nil
	}
	if v.BuiltIn {
		m.notice("codegraph is built in; configure it with [codegraph]")
		return m, nil
	}
	cfg, err := config.Load()
	if err != nil {
		m.notice("mcp mode: " + err.Error())
		return m, nil
	}
	found := false
	for i := range cfg.Plugins {
		if cfg.Plugins[i].Name == v.Name {
			cfg.Plugins[i].Tier = normalizeMCPTierForCLI(tier)
			if !cfg.Plugins[i].ShouldAutoStart() {
				cfg.Plugins[i].AutoStart = mcpBoolPtr(true)
			}
			found = true
			break
		}
	}
	if !found {
		m.notice(fmt.Sprintf("mcp mode: no configured MCP server named %q", v.Name))
		return m, nil
	}
	if err := cfg.Save(); err != nil {
		m.notice("mcp mode: " + err.Error())
		return m, nil
	}
	if m.mcpDisabled != nil {
		delete(m.mcpDisabled, v.Name)
	}
	if tier != "lazy" && m.ctrl != nil && !mcpConnected(m.ctrl, v.Name) {
		if _, err := m.ctrl.ConnectConfiguredMCPServer(v.Name); err != nil {
			m.notice("saved connection mode, but connect failed: " + err.Error())
		}
		m.host = m.ctrl.Host()
	}
	m.refreshMCPManager()
	if m.mcp != nil {
		m.mcp.stage = mcpStageDetail
		m.mcp.selectName(v.Name)
	}
	m.notice("updated connection mode for " + v.Name)
	return m, nil
}

func (m chatTUI) openMCPConfig() (tea.Model, tea.Cmd) {
	path := ""
	if m.mcp != nil {
		path = m.mcp.snapshot.configPath
	}
	if strings.TrimSpace(path) == "" {
		path = mcpConfigLocation()
	}
	launch, err := mcpEditConfigLaunchCommand(path, exec.LookPath)
	if err != nil {
		m.notice("edit config: " + err.Error())
		return m, nil
	}
	if launch.systemDefault {
		m.notice("no terminal editor found; opened config with the system default app. Set EDITOR=vim to edit in terminal.")
	} else if launch.editor != "" {
		m.notice("opening config with " + launch.editor)
	}
	return m, tea.ExecProcess(launch.cmd, func(err error) tea.Msg {
		return mcpExternalDoneMsg{label: "edit config", target: path, err: err}
	})
}

func (m chatTUI) authenticateMCP(v mcpServerView) (tea.Model, tea.Cmd) {
	u := mcpAuthURL(v)
	if u == "" {
		m.notice("mcp auth: no authorization URL was returned; view logs for details")
		return m, nil
	}
	cmd, err := mcpOpenCommand(u)
	if err != nil {
		m.notice("mcp auth: " + err.Error())
		return m, nil
	}
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return mcpExternalDoneMsg{label: "authorization page", target: u, err: err}
	})
}

func (m chatTUI) clearSelectedMCPAuthentication() (tea.Model, tea.Cmd) {
	if m.mcp == nil {
		return m, nil
	}
	v, ok := m.mcp.selectedServer()
	if !ok {
		m.mcp.stage = mcpStageList
		return m, nil
	}
	return m.clearMCPAuthentication(v)
}

func (m chatTUI) clearMCPAuthentication(v mcpServerView) (tea.Model, tea.Cmd) {
	if v.BuiltIn {
		m.notice("codegraph is built in; it has no stored MCP authentication")
		return m, nil
	}
	_, changed, _, err := config.ClearPluginAuthenticationInSource(v.Name)
	if err != nil {
		m.notice("clear authentication: " + err.Error())
		return m, nil
	}
	if m.ctrl != nil {
		m.ctrl.DisconnectMCPServer(v.Name)
		if h := m.ctrl.Host(); h != nil {
			h.ClearFailure(v.Name)
		}
		m.host = m.ctrl.Host()
	}
	m.refreshMCPManager()
	if m.mcp != nil {
		m.mcp.stage = mcpStageDetail
		m.mcp.selectName(v.Name)
	}
	if changed {
		m.notice("cleared authentication for " + v.Name + "; reconnect to authorize again")
	} else {
		m.notice("cleared local authentication state for " + v.Name)
	}
	return m, nil
}

func mcpActionsFor(v mcpServerView, configPath string) []mcpActionItem {
	var out []mcpActionItem
	if v.Tools > 0 || len(v.ToolList) > 0 {
		out = append(out, mcpActionItem{mcpActionViewTools, "View tools"})
	}
	if v.Status == "failed" {
		if mcpAuthStatus(v) == mcpdiag.AuthRequired {
			out = append(out, mcpActionItem{mcpActionAuth, "Authenticate"})
		} else {
			out = append(out, mcpActionItem{mcpActionConnect, "Retry"})
		}
		if mcpCanClearAuth(v) {
			out = append(out, mcpActionItem{mcpActionClearAuth, "Clear authentication"})
		}
		out = appendMCPFailureSecondaryActions(out, v, configPath)
		return out
	}
	switch v.Status {
	case "connected":
		out = append(out, mcpActionItem{mcpActionConnect, "Reconnect"})
	case "disabled":
		out = append(out, mcpActionItem{mcpActionConnect, "Enable and connect"})
	default:
		out = append(out, mcpActionItem{mcpActionConnect, "Connect now"})
	}
	out = appendMCPConfigActions(out, v, configPath)
	if mcpCanClearAuth(v) {
		out = append(out, mcpActionItem{mcpActionClearAuth, "Clear authentication"})
	}
	if v.Status != "disabled" {
		out = append(out, mcpActionItem{mcpActionDisable, "Disable for this session"})
	}
	if !v.BuiltIn {
		out = append(out, mcpActionItem{mcpActionRemove, "Remove server"})
	}
	return out
}

func appendMCPFailureSecondaryActions(out []mcpActionItem, v mcpServerView, configPath string) []mcpActionItem {
	if strings.TrimSpace(v.Error) != "" {
		out = append(out, mcpActionItem{mcpActionLogs, "View logs"})
	}
	out = appendMCPConfigActions(out, v, configPath)
	if v.Status != "disabled" {
		out = append(out, mcpActionItem{mcpActionDisable, "Disable for this session"})
	}
	if !v.BuiltIn {
		out = append(out, mcpActionItem{mcpActionRemove, "Remove server"})
	}
	return out
}

func appendMCPConfigActions(out []mcpActionItem, v mcpServerView, configPath string) []mcpActionItem {
	if v.Configured && !v.BuiltIn {
		out = append(out, mcpActionItem{mcpActionMode, "Change connection mode"})
		if configPath != "" {
			out = append(out, mcpActionItem{mcpActionEdit, "Edit config"})
		}
	}
	return out
}

func writeMCPDetailField(b *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	fmt.Fprintf(b, "%-16s %s\n", label+":", value)
}

func mcpStatusLabel(v mcpServerView) string {
	switch {
	case v.Status == "connected":
		return green("✓ connected")
	case v.Status == "failed" && mcpAuthStatus(v) == mcpdiag.AuthRequired:
		return yellow("⚠ needs authentication")
	case v.Status == "failed":
		return red("✕ failed")
	case v.Status == "deferred":
		return "○ connect on use"
	case v.Status == "initializing":
		return "◌ connecting..."
	case v.Status == "disabled":
		return "○ disabled"
	default:
		return viewMeta("unknown")
	}
}

func mcpAuthLabel(v mcpServerView) string {
	switch {
	case v.Status == "connected":
		return green("✓ authenticated")
	case mcpAuthStatus(v) == mcpdiag.AuthRequired:
		return red("✕ not authenticated")
	case mcpAuthStatus(v) == mcpdiag.AuthPossible:
		return yellow("may need authorization")
	default:
		return ""
	}
}

func mcpCapabilitiesText(v mcpServerView) string {
	var caps []string
	if v.Tools > 0 {
		caps = append(caps, "tools")
	}
	if v.Prompts > 0 {
		caps = append(caps, "prompts")
	}
	if v.Resources > 0 {
		caps = append(caps, "resources")
	}
	if len(caps) == 0 {
		return "none"
	}
	return strings.Join(caps, ", ")
}

func mcpCommandLabel(v mcpServerView) string {
	if v.Transport == "http" || v.Transport == "sse" {
		return "URL"
	}
	return "Command"
}

func mcpCommandLine(v mcpServerView) string {
	if v.Transport == "http" || v.Transport == "sse" {
		return strings.TrimSpace(v.URL)
	}
	return strings.TrimSpace(v.Command + " " + strings.Join(v.Args, " "))
}

func mcpModeLabel(tier string) string {
	for _, choice := range mcpModeChoices {
		if choice.tier == normalizeMCPTierForCLI(tier) {
			return choice.label
		}
	}
	return mcpModeChoices[0].label
}

func mcpModeIndex(tier string) int {
	tier = normalizeMCPTierForCLI(tier)
	for i, choice := range mcpModeChoices {
		if choice.tier == tier {
			return i
		}
	}
	return 0
}

func normalizeMCPTierForCLI(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background":
		return "background"
	default:
		return "lazy"
	}
}

func mcpConfigLocation() string {
	if path := config.SourcePath(); path != "" {
		return path
	}
	if _, err := os.Stat(".mcp.json"); err == nil {
		return ".mcp.json"
	}
	if path := config.UserConfigPath(); path != "" {
		return path
	}
	return "reasonix.toml"
}

type mcpEditConfigLaunch struct {
	cmd           *exec.Cmd
	editor        string
	systemDefault bool
}

func mcpEditConfigCommand(path string) (*exec.Cmd, error) {
	launch, err := mcpEditConfigLaunchCommand(path, exec.LookPath)
	if err != nil {
		return nil, err
	}
	return launch.cmd, nil
}

func mcpEditConfigLaunchCommand(path string, lookPath func(string) (string, error)) (mcpEditConfigLaunch, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return mcpEditConfigLaunch{}, fmt.Errorf("no config path available")
	}
	if editor := strings.TrimSpace(os.Getenv("VISUAL")); editor != "" {
		return mcpEditConfigLaunch{
			cmd:    exec.Command("sh", "-lc", editor+" "+shellQuote(path)),
			editor: mcpEditorDisplayName(editor),
		}, nil
	}
	if editor := strings.TrimSpace(os.Getenv("EDITOR")); editor != "" {
		return mcpEditConfigLaunch{
			cmd:    exec.Command("sh", "-lc", editor+" "+shellQuote(path)),
			editor: mcpEditorDisplayName(editor),
		}, nil
	}
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	for _, editor := range []string{"vim", "vi", "nano"} {
		if bin, err := lookPath(editor); err == nil && strings.TrimSpace(bin) != "" {
			return mcpEditConfigLaunch{
				cmd:    exec.Command(bin, path),
				editor: editor,
			}, nil
		}
	}
	cmd, err := mcpOpenCommand(path)
	if err != nil {
		return mcpEditConfigLaunch{}, err
	}
	return mcpEditConfigLaunch{cmd: cmd, systemDefault: true}, nil
}

func mcpEditorDisplayName(editor string) string {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func mcpOpenCommand(target string) (*exec.Cmd, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return nil, fmt.Errorf("empty target")
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target), nil
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target), nil
	default:
		return exec.Command("xdg-open", target), nil
	}
}

func mcpAuthURL(v mcpServerView) string {
	auth := mcpAuthDiagnosis(v)
	if auth.Status != mcpdiag.AuthRequired {
		return ""
	}
	return strings.TrimSpace(auth.URL)
}

func mcpAuthStatus(v mcpServerView) string {
	return mcpAuthDiagnosis(v).Status
}

func mcpAuthDiagnosis(v mcpServerView) mcpdiag.AuthDiagnosis {
	if v.AuthStatus != "" {
		return mcpdiag.AuthDiagnosis{Status: v.AuthStatus, URL: v.AuthURL}
	}
	return mcpdiag.DiagnoseAuth(v.Transport, v.Status, v.Error, v.URL, v.authConfigured)
}

func mcpCanClearAuth(v mcpServerView) bool {
	if !v.Configured || v.BuiltIn {
		return false
	}
	if v.authConfigured || mcpAuthStatus(v) != mcpdiag.AuthNone {
		return true
	}
	return mcpdiag.IsRemoteTransport(v.Transport)
}

func mcpConnected(ctrl *control.Controller, name string) bool {
	if ctrl == nil || ctrl.Host() == nil {
		return false
	}
	for _, s := range ctrl.Host().Servers() {
		if s.Name == name {
			return true
		}
	}
	return false
}

func visibleRange(total, sel, limit int) (int, int) {
	if limit <= 0 || total <= limit {
		return 0, total
	}
	if sel < 0 {
		sel = 0
	}
	if sel >= total {
		sel = total - 1
	}
	start := sel - limit/2
	if start < 0 {
		start = 0
	}
	if start+limit > total {
		start = total - limit
	}
	return start, start + limit
}

func numberKeyIndex(s string, limit int) (int, bool) {
	if len(s) != 1 || s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	idx := int(s[0] - '1')
	return idx, idx < limit
}

func fallbackText(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func titleText(s string) string {
	if s == "" {
		return "MCP"
	}
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && size == 0 {
		return s
	}
	return strings.ToUpper(string(r)) + s[size:]
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func mcpBoolPtr(v bool) *bool { return &v }
