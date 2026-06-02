package cli

import (
	"fmt"
	"net/url"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

func (m *chatTUI) runThinkingCommand(input string) tea.Cmd {
	entry, ref, err := m.currentConfigProvider()
	if err != nil {
		m.notice("thinking: " + err.Error())
		return nil
	}
	if !isDeepSeekProviderEntry(entry) {
		m.notice(i18n.M.ThinkingUnsupported)
		return nil
	}

	args := tokenizeArgs(input)
	if len(args) < 2 {
		effort := entry.Effort
		if effort == "" {
			effort = "high"
		}
		m.notice(fmt.Sprintf(i18n.M.ThinkingCurrentFmt, entry.Name, effort))
		return nil
	}
	if len(args) > 2 {
		m.notice(i18n.M.ThinkingUsage)
		return nil
	}
	effort := strings.ToLower(strings.TrimSpace(args[1]))
	switch effort {
	case "high", "max", "off":
	default:
		m.notice(i18n.M.ThinkingUsage)
		return nil
	}
	if m.buildController == nil {
		m.notice(i18n.M.ModelSwitchUnavailable)
		return nil
	}
	if m.ctrl.Running() {
		m.notice(i18n.M.ModelSwitchBusy)
		return nil
	}

	path := config.UserConfigPath()
	if path == "" {
		m.notice("thinking: cannot resolve user config directory")
		return nil
	}
	edit := config.LoadForEdit(path)
	if _, ok := edit.Provider(entry.Name); !ok {
		if err := edit.UpsertProvider(*entry); err != nil {
			m.notice("thinking: " + err.Error())
			return nil
		}
	}
	if err := edit.SetProviderEffort(entry.Name, effort); err != nil {
		m.notice("thinking: " + err.Error())
		return nil
	}
	if err := edit.SaveTo(path); err != nil {
		m.notice("thinking: " + err.Error())
		return nil
	}

	m.notice(fmt.Sprintf(i18n.M.ThinkingSettingFmt, entry.Name, effort))
	carried := m.ctrl.History()
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("thinking: snapshot: " + err.Error())
	}
	oldCtrl := m.ctrl
	build := m.buildController
	m.modelSwitchPending = true
	m.pendingModelSwitch = func() tea.Msg {
		c, err := build(ref, carried)
		if err != nil {
			return modelSwitchMsg{ref: ref, err: err}
		}
		return modelSwitchMsg{
			ref:      ref,
			ctrl:     c,
			oldCtrl:  oldCtrl,
			label:    c.Label(),
			commands: c.Commands(),
			skills:   c.Skills(),
			host:     c.Host(),
		}
	}
	m.notice(fmt.Sprintf(i18n.M.ThinkingSetDoneFmt, entry.Name, effort))
	return m.pendingModelSwitch
}

func (m *chatTUI) currentConfigProvider() (*config.ProviderEntry, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", err
	}
	ref := m.modelRef
	if strings.TrimSpace(ref) == "" {
		ref = cfg.DefaultModel
	}
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return nil, "", fmt.Errorf("unknown model %q", ref)
	}
	if ref == entry.Name || !strings.Contains(ref, "/") {
		ref = entry.Name + "/" + entry.Model
	}
	return entry, ref, nil
}

func isDeepSeekProviderEntry(e *config.ProviderEntry) bool {
	if e == nil || e.Kind != "openai" {
		return false
	}
	u, err := url.Parse(e.BaseURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "api.deepseek.com" || strings.HasSuffix(host, ".deepseek.com")
}
