package cli

import (
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/config"
)

// runModelSubcommand handles "/model": with no argument it lists the configured
// (provider, model) refs and marks the active one; "/model <ref>" switches the
// session to that model in place, carrying the conversation across. The swap
// happens here, on the running model copy, so it actually takes effect.
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
	c, err := m.buildController(ref, carried)
	if err != nil {
		m.notice("model: " + err.Error())
		return
	}
	m.ctrl.Close()
	m.ctrl = c
	m.label = c.Label()
	m.commands = c.Commands()
	m.skills = c.Skills()
	m.host = c.Host()
	m.modelRef = ref
	m.notice(fmt.Sprintf("switched to %s (conversation carried over; prompt cache resets)", m.label))
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
