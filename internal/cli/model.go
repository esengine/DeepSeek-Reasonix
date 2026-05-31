package cli

import (
	"fmt"
	"log/slog"
	"strings"

	tea "charm.land/bubbletea/v2"

	"reasonix/internal/config"
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
		m.notice("model switching is unavailable in this session")
		return
	}
	if m.ctrl.Running() {
		m.notice("finish or cancel the current turn before switching models")
		return
	}
	if ref == m.modelRef {
		m.notice("already on " + ref)
		return
	}
	carried := m.ctrl.History()
	if err := m.ctrl.Snapshot(); err != nil {
		slog.Warn("model switch: snapshot failed", "err", err)
	}
	m.notice(fmt.Sprintf("switching to %s…", ref))

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
		c, err := build(ref, carried)
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
	var b strings.Builder
	b.WriteString(dim("  · models (/model <provider/model> to switch)\n"))
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		for _, model := range p.ModelList() {
			ref := p.Name + "/" + model
			marker := "  "
			if ref == m.modelRef {
				marker = accent("› ")
			}
			fmt.Fprintf(&b, "%s%s\n", marker, ref)
		}
	}
	m.notice(strings.TrimRight(b.String(), "\n"))
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
		for _, model := range p.ModelList() {
			out = append(out, p.Name+"/"+model)
		}
	}
	return out
}
