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
	if m.runtimeSwitchBusy() {
		m.notice("finish or cancel active work and stop background jobs before changing effort")
		return nil
	}
	if m.modelSwitchPending {
		m.notice("wait for the current runtime switch to finish")
		return nil
	}

	path := config.UserConfigPath()
	if path == "" {
		m.notice("effort: cannot resolve user config directory")
		return nil
	}
	// Lock only the load-modify-save cycle; the snapshot and controller
	// rebuild below run off-lock.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		edit := config.LoadForEdit(path)
		if _, ok := edit.Provider(entry.Name); !ok {
			if err := edit.UpsertProvider(*entry); err != nil {
				return err
			}
		}
		if entry.Kind == "anthropic" && effort != "" && entry.Thinking == "" {
			if err := edit.SetProviderThinking(entry.Name, "adaptive"); err != nil {
				return err
			}
		}
		if err := edit.SetProviderEffort(entry.Name, effort); err != nil {
			return err
		}
		return edit.SaveTo(path)
	}(); err != nil {
		m.notice("effort: " + err.Error())
		return nil
	}

	display := effort
	if display == "" {
		display = "auto"
	}
	m.notice(fmt.Sprintf("setting effort for %s to %s…", entry.Name, display))
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("effort: snapshot: " + err.Error())
	}
	// Capture the resume path and history only after Snapshot: a snapshot
	// conflict can retarget the controller to a recovery branch (or adopt the
	// newer disk transcript), and a pre-snapshot capture would bind the rebuilt
	// controller back to the original file, re-conflicting on every later save.
	carried := m.ctrl.History()
	prevPath := m.ctrl.SessionPath()
	// Move the lease before the rebuilt controller binds prevPath for writing
	// (AdoptHistory resumes there): after a snapshot retarget the lease still
	// guards the old path, and the async build must not open an unguarded
	// writer on the recovery branch.
	if err := m.rebindSessionLease(prevPath); err != nil {
		m.notice("effort: " + sessionLeaseHeldNotice(err))
		return nil
	}
	oldCtrl := m.ctrl
	build := m.buildController
	m.modelSwitchPending = true
	m.pendingModelSwitch = func() tea.Msg {
		c, err := build(controllerBuildSpec{
			ModelRef:         ref,
			RuntimeProfile:   m.runtimeProfile,
			ToolApprovalMode: oldCtrl.ToolApprovalMode(),
			PlanMode:         oldCtrl.PlanMode(),
			EffortOverride:   &effort,
		}, carried, prevPath, oldCtrl)
		if err != nil {
			return modelSwitchMsg{ref: ref, err: err}
		}
		return modelSwitchMsg{
			ref:      ref,
			ctrl:     c,
			oldCtrl:  oldCtrl,
			label:    c.Label(),
			commands: c.Commands(),
			skills:   c.SlashSkills(),
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
