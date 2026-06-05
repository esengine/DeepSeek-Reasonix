package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
)

func (m *chatTUI) runEffortCommand(input string) tea.Cmd {
	entry, ref, err := m.currentConfigProvider()
	if err != nil {
		m.notice("effort: " + err.Error())
		return nil
	}
	cap := config.EffortCapabilityForEntry(entry)
	if !cap.Supported {
		m.notice(fmt.Sprintf("effort is not configurable for %s", entry.Name))
		return nil
	}

	args := tokenizeArgs(input)
	if len(args) < 2 {
		current := config.EffortDisplay(entry)
		options := strings.Join(cap.Levels, "|")
		m.notice(fmt.Sprintf("effort for %s: %s (default: %s; options: %s)", entry.Name, current, cap.Default, options))
		return nil
	}
	if len(args) > 2 {
		m.notice("usage: /effort " + strings.Join(cap.Levels, "|"))
		return nil
	}
	effort, err := config.NormalizeEffort(entry, args[1])
	if err != nil {
		m.notice(err.Error())
		return nil
	}
	if m.buildController == nil {
		m.notice("model switching is unavailable in this session")
		return nil
	}
	if m.ctrl.Running() {
		m.notice("finish or cancel the current turn before changing effort")
		return nil
	}

	path := config.UserConfigPath()
	if path == "" {
		m.notice("effort: cannot resolve user config directory")
		return nil
	}
	edit := config.LoadForEdit(path)
	if _, ok := edit.Provider(entry.Name); !ok {
		if err := edit.UpsertProvider(*entry); err != nil {
			m.notice("effort: " + err.Error())
			return nil
		}
	}
	if entry.Kind == "anthropic" && effort != "" && entry.Thinking == "" {
		if err := edit.SetProviderThinking(entry.Name, "adaptive"); err != nil {
			m.notice("effort: " + err.Error())
			return nil
		}
	}
	if err := edit.SetProviderEffort(entry.Name, effort); err != nil {
		m.notice("effort: " + err.Error())
		return nil
	}
	if err := edit.SaveTo(path); err != nil {
		m.notice("effort: " + err.Error())
		return nil
	}

	display := effort
	if display == "" {
		display = "auto"
	}
	m.notice(fmt.Sprintf("setting effort for %s to %s…", entry.Name, display))
	carried := m.ctrl.History()
	prevPath := m.ctrl.SessionPath()
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("effort: snapshot: " + err.Error())
	}
	oldCtrl := m.ctrl
	build := m.buildController
	m.modelSwitchPending = true
	m.pendingModelSwitch = func() tea.Msg {
		c, err := build(ref, carried, prevPath)
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
	m.notice(fmt.Sprintf("effort for %s set to %s", entry.Name, display))
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

func (m *chatTUI) refreshEffortStatus() {
	m.effortLevel = ""
	entry, _, err := m.currentConfigProvider()
	if err != nil {
		return
	}
	if !config.EffortCapabilityForEntry(entry).Supported {
		return
	}
	m.effortLevel = config.EffortDisplay(entry)
}
