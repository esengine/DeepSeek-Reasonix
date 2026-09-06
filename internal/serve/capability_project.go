package serve

import (
	"net/http"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/skill"
)

// Only the session pointed at a folder knows whether its servers came up, so a
// project other than the running one reports what is declared and switched on
// rather than a status nobody measured.

// mcpProjectState separates pending from disabled: one is waiting for the user
// and the other is their answer, and a panel showing both as off reads as the
// project's MCP having quietly gone missing.
func mcpProjectState(st control.MCPServerState) string {
	switch {
	case st.Pending:
		return "pending"
	case !st.Enabled:
		return "disabled"
	}
	return "idle"
}

func (s *Server) mcpForProject(w http.ResponseWriter, root string) {
	view := control.InspectProject(root)
	out := make([]mcpEntry, 0, len(view.Servers))
	for _, st := range view.Servers {
		state := mcpProjectState(st)
		out = append(out, remembered(st, mcpEntry{
			Name: st.Entry.Name, State: state, Enabled: st.Enabled, LocalOverride: st.LocalOverride,
			Transport: st.Entry.Type, Source: string(st.Entry.Source),
		}))
	}
	writeJSON(w, map[string]any{"servers": out, "scope": view.Scope, "live": false})
}

func (s *Server) skillsForProject(w http.ResponseWriter, root string) {
	view := control.InspectProject(root)
	entries := make([]skillEntry, 0, len(view.Skills))
	for _, row := range view.Skills {
		switchScope := ""
		if row.HasOverride {
			switchScope = string(row.SwitchScope)
		}
		entries = append(entries, skillEntry{
			SwitchScope: switchScope,
			Name:        row.Skill.Name,
			SlashName:   row.Skill.SlashName(),
			Description: row.Skill.Description,
			Scope:       string(row.Skill.Scope),
			Plugin:      row.Skill.Plugin,
			Path:        row.Skill.Path,
			Subagent:    row.Skill.RunAs == skill.RunSubagent,
			ReadOnly:    row.Skill.ReadOnly,
			Model:       row.Skill.Model,
			Effort:      row.Skill.Effort,
			AllowedURI:  row.Skill.AllowedTools,
			Manual:      strings.EqualFold(strings.TrimSpace(row.Skill.Invocation), "manual"),
			Enabled:     row.Enabled,
		})
	}
	writeJSON(w, map[string]any{
		"implicit": s.ctl().ImplicitSkillInvocationEnabled(),
		"skills":   entries,
		"scope":    view.Scope,
		"live":     false,
	})
}

// switchForProject writes one capability's switch under another project's
// identity. It touches storage only: the running session is pointed elsewhere,
// so there is no registry to move and nothing to roll back.
func (s *Server) switchForProject(root, kind, name string, scope config.ActivationScope, enabled, clear bool) error {
	store := config.DefaultActivationStore()
	if kind == string(config.CapabilitySkill) {
		if clear {
			return store.ClearSkill(name, root, scope)
		}
		return store.SetSkillEnabled(name, root, scope, enabled)
	}
	entry, found := projectServerEntry(root, name)
	if !found {
		return &config.ServerNotFoundError{Name: name}
	}
	if clear {
		return store.ClearServer(entry, root, scope)
	}
	return store.SetServerEnabled(entry, root, scope, enabled)
}

func projectServerEntry(root, name string) (config.PluginEntry, bool) {
	for _, st := range control.InspectProject(root).Servers {
		if st.Entry.Name == name {
			return st.Entry, true
		}
	}
	return config.PluginEntry{}, false
}
