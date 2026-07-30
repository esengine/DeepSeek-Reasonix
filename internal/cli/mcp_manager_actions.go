package cli

// mcp_manager_actions.go applies /mcp manager actions: connect, disable, remove,
// mode, auth, and config editing.

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/mcpdiag"
	"reasonix/internal/plugin"
	"reasonix/internal/shellparse"
)

func (m chatTUI) applyMCPAction(v mcpServerView, action mcpAction) (tea.Model, tea.Cmd) {
	switch action {
	case mcpActionViewTools:
		m.mcp.stage = mcpStageTools
	case mcpActionMode:
		m.mcp.stage = mcpStageMode
		m.mcp.mode = mcpModeIndex(v.Tier)
	case mcpActionEdit:
		return m.openMCPConfig(v)
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
	persisted := false
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
	if persisted {
		m.notice("disabled " + v.Name)
	} else {
		m.notice("disabled " + v.Name + " for this session")
	}
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
	workspace := m.mcpWorkspaceRoot()
	cfg, err := config.LoadForRoot(workspace)
	if err != nil {
		m.notice("mcp mode: " + err.Error())
		return m, nil
	}
	found := false
	var selected config.PluginEntry
	for _, entry := range cfg.Plugins {
		if entry.Name == v.Name {
			entry.Tier = normalizeMCPTierForCLI(tier)
			if !entry.ShouldAutoStart() {
				entry.AutoStart = mcpBoolPtr(true)
			}
			selected = entry
			found = true
			break
		}
	}
	if !found {
		m.notice(fmt.Sprintf("mcp mode: no configured MCP server named %q", v.Name))
		return m, nil
	}
	if _, err := config.UpsertPluginInSourceForRoot(workspace, selected); err != nil {
		m.notice("mcp mode: " + err.Error())
		return m, nil
	}
	if m.mcpDisabled != nil {
		delete(m.mcpDisabled, v.Name)
	}
	if m.ctrl != nil && !mcpConnected(m.ctrl, v.Name) {
		if _, err := m.ctrl.ConnectConfiguredMCPServer(v.Name); err != nil {
			recordMCPModePluginFailure(m.ctrl, selected, err)
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

func recordMCPModePluginFailure(ctrl control.Capabilities, e config.PluginEntry, err error) {
	if ctrl == nil || ctrl.Host() == nil || err == nil {
		return
	}
	exp := e.ExpandedPlugin()
	ctrl.Host().RecordFailure(plugin.Spec{
		Name:    exp.Name,
		Type:    exp.Type,
		Command: exp.Command,
		Args:    exp.Args,
		Env:     exp.Env,
		URL:     exp.URL,
		Headers: exp.Headers,
	}, err)
}

func (m chatTUI) openMCPConfig(v mcpServerView) (tea.Model, tea.Cmd) {
	fallback := config.UserConfigPath()
	if m.mcp != nil && strings.TrimSpace(m.mcp.snapshot.configPath) != "" {
		fallback = m.mcp.snapshot.configPath
	}
	path := mcpConfigPathForView(v, fallback)
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
		m.notice("managed MCP servers do not store authentication")
		return m, nil
	}
	_, changed, _, err := config.ClearPluginAuthenticationInSourceForRoot(m.mcpWorkspaceRoot(), v.Name)
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

func mcpModeIndex(tier string) int {
	tier = normalizeMCPTierForCLI(tier)
	for i, choice := range mcpTierChoices {
		if choice == tier {
			return i
		}
	}
	return 0
}

func normalizeMCPTierForCLI(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case "eager":
		return "eager"
	case "background", "lazy":
		return "background"
	case "":
		return "background"
	default:
		return "background"
	}
}

type mcpEditConfigLaunch struct {
	cmd           *exec.Cmd
	editor        string
	systemDefault bool
}

func mcpEditConfigLaunchCommand(path string, lookPath func(string) (string, error)) (mcpEditConfigLaunch, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return mcpEditConfigLaunch{}, fmt.Errorf("no config path available")
	}
	if editor := strings.TrimSpace(os.Getenv("VISUAL")); editor != "" {
		cmd, err := editorLaunchCmd(editor, path)
		if err != nil {
			return mcpEditConfigLaunch{}, err
		}
		return mcpEditConfigLaunch{
			cmd:    cmd,
			editor: mcpEditorDisplayName(editor),
		}, nil
	}
	if editor := strings.TrimSpace(os.Getenv("EDITOR")); editor != "" {
		cmd, err := editorLaunchCmd(editor, path)
		if err != nil {
			return mcpEditConfigLaunch{}, err
		}
		return mcpEditConfigLaunch{
			cmd:    cmd,
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
	fields, err := splitEditorCommand(os.ExpandEnv(editor))
	if err != nil || len(fields) == 0 {
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

func mcpConnected(ctrl control.Capabilities, name string) bool {
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

// editorLaunchCmd builds an exec.Cmd for an editor invocation read from the
// VISUAL/EDITOR environment variable. The editor string may carry arguments
// (e.g. "code --wait", "nvim -p") and shell variable / tilde references
// (e.g. "$HOME/bin/myeditor", "~/bin/myeditor"); these are expanded without
// invoking a shell, and the editor binary is resolved by the OS directly.
// Shell metacharacters in the value cannot be executed: the expanded value must
// parse as one static shell command. Control operators, redirection,
// substitution, globbing, assignments, and other shell-shaping syntax are
// rejected before launch.
//
// This matches the safe pattern already used by the terminal-editor
// fallback (exec.Command(bin, path)) in the same function and avoids the
// previous sh -lc construction that concatenated the raw editor value into
// a shell command string.
//
// Quoting and backslash escaping are honored for word splitting only; shell
// operators, globbing, command substitution, and redirection are rejected.
// Tilde expansion only covers the leading-token forms "~" and "~/..."; "~user"
// is not supported (and was not reliably supported by the prior sh -lc path
// either, since $HOME for another user is not available without getpwuid).
func editorLaunchCmd(editor, path string) (*exec.Cmd, error) {
	expanded := os.ExpandEnv(editor)
	args, err := splitEditorCommand(expanded)
	if err != nil {
		return nil, fmt.Errorf("invalid EDITOR/VISUAL value: %w", err)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("invalid EDITOR/VISUAL value: %q", editor)
	}
	args[0] = expandLeadingTilde(args[0])
	return exec.Command(args[0], append(args[1:], path)...), nil
}

func splitEditorCommand(s string) ([]string, error) {
	args, malformed := shellparse.StaticFields(s)
	if malformed != "" {
		return nil, fmt.Errorf("%s", malformed)
	}
	return args, nil
}

// expandLeadingTilde replaces a leading "~" or "~/" prefix with the current
// user's home directory. Other forms (e.g. "~user") are returned unchanged.
// If the home directory cannot be determined the value is returned as-is so
// the caller surfaces the exec failure rather than panicking.
func expandLeadingTilde(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return home + p[1:]
}

func mcpBoolPtr(v bool) *bool { return &v }
