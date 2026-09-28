package serve

import (
	"encoding/json"
	"net/http"
	"strings"

	"reasonix/internal/ext/hook"
)

// hookEntry is one configured rule. Blocking and issues are computed here rather
// than in the client: whether exit 2 stops the agent is a property of the event,
// and the client must not have its own opinion about it.
type hookEntry struct {
	Event       string   `json:"event"`
	Match       string   `json:"match,omitempty"`
	Command     string   `json:"command"`
	Description string   `json:"description,omitempty"`
	Timeout     int      `json:"timeout,omitempty"`
	Cwd         string   `json:"cwd,omitempty"`
	Scope       string   `json:"scope"`
	Source      string   `json:"source,omitempty"`
	Blocking    bool     `json:"blocking,omitempty"`
	UsesMatch   bool     `json:"usesMatch,omitempty"`
	ReadOnly    bool     `json:"readOnly,omitempty"`
	Issues      []string `json:"issues,omitempty"`
}

type hookSource struct {
	Scope  string `json:"scope"`
	Path   string `json:"path"`
	Status string `json:"status"`
	Count  int    `json:"hookCount"`
	Error  string `json:"parseError,omitempty"`
}

type hookEventInfo struct {
	Name      string `json:"name"`
	Blocking  bool   `json:"blocking"`
	UsesMatch bool   `json:"usesMatch"`
}

// hooks lists every configured rule with the diagnostics Inspect produces, plus
// the two files they can be written to. The paths are part of the answer: a
// project rule travels with the repository to everyone who clones it.
func (s *Server) hooks(w http.ResponseWriter, r *http.Request) {
	ctl := s.ctl()
	insp := ctl.InspectHooks()
	entries := make([]hookEntry, 0, len(insp.Entries))
	for _, e := range insp.Entries {
		issues := make([]string, 0, len(e.Issues))
		for _, issue := range e.Issues {
			if issue == hook.IssueAwaitingApproval {
				issue = "这个项目的钩子还没有被批准，不会运行；核对内容后点「保存」即批准当前内容"
			}
			issues = append(issues, issue)
		}
		if msg := hook.ValidateMatcher(e.Match); msg != "" && hook.UsesToolMatcher(e.Event) {
			issues = append(issues, msg)
		}
		if !hook.IsKnownEvent(string(e.Event)) {
			issues = append(issues, "这个事件名不存在，它永远不会触发")
		}
		entries = append(entries, hookEntry{
			Event: string(e.Event), Match: e.Match, Command: e.Command,
			Description: e.Description, Timeout: e.Timeout, Cwd: e.Cwd,
			Scope: string(e.Scope), Source: e.Source,
			Blocking: hook.IsBlocking(e.Event), UsesMatch: hook.UsesToolMatcher(e.Event),
			ReadOnly: e.Scope == hook.ScopePlugin, Issues: issues,
		})
	}
	sources := make([]hookSource, 0, len(insp.Sources))
	for _, src := range insp.Sources {
		sources = append(sources, hookSource{
			Scope: string(src.Scope), Path: src.Path, Status: src.Status,
			Count: src.HookCount, Error: src.ParseError,
		})
	}
	events := make([]hookEventInfo, 0, len(hook.Events))
	for _, e := range hook.Events {
		events = append(events, hookEventInfo{
			Name: string(e), Blocking: hook.IsBlocking(e), UsesMatch: hook.UsesToolMatcher(e),
		})
	}
	writeJSON(w, map[string]any{
		"hooks":       entries,
		"sources":     sources,
		"events":      events,
		"projectPath": ctl.HookSettingsPath(hook.ScopeProject),
		"globalPath":  ctl.HookSettingsPath(hook.ScopeGlobal),
	})
}

// saveHooks replaces one scope's rules wholesale. Partial edits would need the
// client to merge, and a client that merges wrong silently drops someone's hook.
func (s *Server) saveHooks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Scope string      `json:"scope"`
		Hooks []hookEntry `json:"hooks"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	scope := hook.ScopeGlobal
	if strings.EqualFold(strings.TrimSpace(body.Scope), string(hook.ScopeProject)) {
		scope = hook.ScopeProject
	}
	settings := hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{}}
	for _, h := range body.Hooks {
		event := hook.Event(strings.TrimSpace(h.Event))
		settings.Hooks[event] = append(settings.Hooks[event], hook.HookConfig{
			Match: h.Match, Command: h.Command, Description: h.Description,
			Timeout: h.Timeout, Cwd: h.Cwd,
		})
	}
	if err := s.ctl().SaveHooks(scope, settings); err != nil {
		saveFailed(w, http.StatusBadRequest, "hooks.rejected", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// dryRunHook runs one rule for real against a sample payload. Real, because a
// simulated pass proves nothing about a command that does not exist.
func (s *Server) dryRunHook(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Event   string `json:"event"`
		Match   string `json:"match"`
		Command string `json:"command"`
		Timeout int    `json:"timeout"`
		Cwd     string `json:"cwd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	cfg := hook.HookConfig{Match: body.Match, Command: body.Command, Timeout: body.Timeout, Cwd: body.Cwd}
	res, err := s.ctl().DryRunHook(r.Context(), cfg, hook.Event(strings.TrimSpace(body.Event)))
	if err != nil {
		saveFailed(w, http.StatusBadRequest, "hooks.dry_run_failed", err)
		return
	}
	writeJSON(w, res)
}
