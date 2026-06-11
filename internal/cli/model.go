package cli

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
	"reasonix/internal/i18n"
)

// runModelSubcommand handles "/model": with no argument it lists the configured
// (provider, model) refs and marks the active one; "/model <ref>" switches the
// session to that model in place, carrying the conversation across. The actual
// controller build runs asynchronously so it cannot block the TUI event loop.
func (m *chatTUI) runModelSubcommand(input string) {
	args := tokenizeArgs(input) // args[0] == "/model"
	if len(args) < 2 {
		m.showModels()
		return
	}
	ref := args[1]
	if m.buildController == nil {
		m.notice(i18n.M.ModelSwitchUnavailable)
		return
	}
	if m.ctrl.Running() {
		m.notice(i18n.M.ModelSwitchBusy)
		return
	}
	if ref == m.modelRef {
		m.notice(fmt.Sprintf(i18n.M.ModelAlreadyOnFmt, ref))
		return
	}
	// Persist the user's choice to ~/.config/reasonix/config.toml so the next
	// session starts on the same model instead of falling back to the global
	// default. Mirrors the pattern used by /theme (persistTheme), /effort, and
	// /language.
	m.persistModel(ref)
	carried := m.ctrl.History()
	prevPath := m.ctrl.SessionPath()
	if err := m.ctrl.Snapshot(); err != nil {
		m.notice("model: snapshot failed: " + err.Error())
	}
	m.notice(fmt.Sprintf(i18n.M.ModelSwitchingFmt, ref))

	// Capture old controller for cleanup after the async build succeeds.
	oldCtrl := m.ctrl
	build := m.buildController

	// Fire the build off the event loop; the result arrives as a tea.Cmd.
	// Both the build AND the old-controller close run in the goroutine so
	// neither blocks the bubbletea event loop. The old controller's Close
	// kills plugin subprocesses (incl. CodeGraph), which can disrupt the
	// terminal's cancelReader if called synchronously inside Update — so it
	// must happen here, before we hand the new controller back.
	m.modelSwitchPending = true
	m.pendingModelSwitch = func() tea.Msg {
		c, err := build(ref, carried, prevPath)
		if err != nil {
			return modelSwitchMsg{ref: ref, err: err}
		}
		// Do NOT close the old controller here. Controller.Close() runs
		// SessionEnd hooks (arbitrary shell commands) and kills plugin
		// subprocesses — operations that corrupt bubbletea's terminal raw
		// mode when executed from a goroutine. Instead, pass the old
		// controller back in the message so the Update handler can defer
		// its cleanup as a tea.Cmd that runs after the next render.
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
}

// showModels lists the configured provider/model refs, marking the active one.
func (m *chatTUI) showModels() {
	cfg, err := config.Load()
	if err != nil {
		m.notice("model: " + err.Error())
		return
	}
	var refs []string
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		for _, model := range p.ChatModelList() {
			refs = append(refs, p.Name+"/"+model)
		}
	}
	m.commitLine(renderModels(m.width, refs, m.modelRef))
}

// persistModel writes ref (a "provider/model" string) to default_model in
// ~/.config/reasonix/config.toml so the next CLI launch starts on the same
// model. The in-memory switch is always allowed to proceed regardless of the
// outcome here, but every step (rejected by validation, save failed, or
// persisted successfully) reports back to the TUI notice channel so the user
// can see whether their /model choice will survive a restart. Run before
// Snapshot/ModelSwitchingFmt so the persistence outcome shows up first in
// the notice area.
func (m *chatTUI) persistModel(ref string) {
	path := config.UserConfigPath()
	if path == "" {
		return
	}
	edit := config.LoadForEdit(path)
	if err := edit.SetDefaultModel(ref); err != nil {
		m.notice(fmt.Sprintf("model: persist refused: %v (ref=%s)", err, ref))
		return
	}
	if err := edit.SaveTo(path); err != nil {
		m.notice(fmt.Sprintf("model: persist save failed: %v (ref=%s, path=%s)", err, ref, path))
		return
	}
	m.notice(fmt.Sprintf("model: persisted (ref=%s, path=%s)", ref, path))
}

// modelRefs returns the configured provider/model refs for slash completion.
func modelRefs() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	var out []string
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		for _, model := range p.ChatModelList() {
			out = append(out, p.Name+"/"+model)
		}
	}
	return out
}

// providerNames returns the names of configured providers for slash completion.
func providerNames() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	var out []string
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		out = append(out, p.Name)
	}
	return out
}
