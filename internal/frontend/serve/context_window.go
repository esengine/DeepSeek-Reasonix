// context_window.go — declaring a window for a source that never reports one.
package serve

import (
	"net/http"
	"strings"

	"reasonix/internal/contract/config"
)

// contextView is the gauge and what fills it. Named rather than anonymous
// because two handlers answer with it: reading the gauge, and declaring the
// window it is drawn against.
type contextView struct {
	Used   int `json:"used"`
	Window int `json:"window"`
	System int `json:"system"`
	Tools  int `json:"tools"`
	User   int `json:"user"`
	Reply  int `json:"reply"`
	Output int `json:"output"`
	// Where maintenance actually happens, which is the lower of the two bounds
	// and not the window: drawn against the window, a 1M session reads 16% full
	// at the moment it folds.
	CompactAt int `json:"compact_at"`
	// Which bound that is, and where the other one sits. Without the pair the
	// panel shows a fold point nothing on the screen explains.
	Boundary   string `json:"boundary"`
	CapacityAt int    `json:"capacity_at"`
}

func (s *Server) contextView() contextView {
	used, window := s.ctl().ContextSnapshot()
	b := s.ctl().ContextBreakdown()
	return contextView{
		Used: used, Window: window,
		System: b.System, Tools: b.Tools, User: b.User, Reply: b.Reply, Output: b.Output,
		CompactAt: b.CompactAt, Boundary: b.Boundary, CapacityAt: b.CapacityAt,
	}
}

// setContextWindow declares the window for the model this session runs on. A
// relay forwards somebody else's model under its own name, so no probe and no
// catalogue can answer what that model holds — only whoever bought it can, and
// until they say, the gauge has no denominator and compaction stays off.
func (s *Server) setContextWindow(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "provider.editing_disabled", "provider editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Window int `json:"window"`
	}
	if !decodeBody(w, r, &body) {
		return
	}
	if body.Window < 0 {
		refuse(w, http.StatusBadRequest, "provider.bad_context_window", "context window cannot be negative", nil)
		return
	}
	name, model, ok := strings.Cut(currentModelRef(s.ctl()), "/")
	if !ok {
		refuse(w, http.StatusConflict, "provider.no_current_model", "no current model to declare a window for", nil)
		return
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	entry, found := edit.Provider(name)
	if !found {
		notFound(w, "provider", name)
		return
	}
	setModelContextWindow(entry, model, body.Window)
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// The window is bound into the agent at assembly, so a live session keeps
	// the one it was built with until it is replaced — the rule the shell and
	// the permission lists are already saved under.
	if err := s.rebuildInPlace(r.Context()); err != nil {
		// The window is on disk by now, so a runtime that is mid-turn is not a
		// failed save: it is a window that starts counting one turn later, and
		// rebuild_failed would send the reader looking for a malfunction.
		if codedRefusal(err) == codeSwitchModel {
			busy(w, "context.window_after_this_turn", "the window was saved; it applies once the running work finishes", nil)
			return
		}
		rebuildFailed(w, err)
		return
	}
	writeJSON(w, s.contextView())
}

// setModelContextWindow writes the window against the one model rather than the
// endpoint. A relay serves several under one base_url, and a window declared for
// all of them is wrong for every one but the one it was typed against. Zero is
// the stored "inherit", which is why clearing the field goes back to whatever
// the entry or the catalogue says rather than to no window at all.
func setModelContextWindow(entry *config.ProviderEntry, model string, window int) {
	if entry.ModelOverrides == nil {
		entry.ModelOverrides = map[string]config.ProviderModelOverride{}
	}
	// The lookup that reads these back matches case-insensitively, so writing
	// the ref's spelling would leave a second entry the reader never reaches.
	key := model
	for k := range entry.ModelOverrides {
		if strings.EqualFold(strings.TrimSpace(k), model) {
			key = k
			break
		}
	}
	override := entry.ModelOverrides[key]
	override.ContextWindow = window
	entry.ModelOverrides[key] = override
}
