package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/mcpsetup"
	"reasonix/internal/plugin"
	"reasonix/internal/skill"
	"reasonix/internal/textutil"
)

// slashEntry is one thing the user can type after "/". Kind separates the two
// sources because they behave differently once invoked: a subagent skill runs
// in its own context and needs an argument, a command expands inline.
type slashEntry struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
	ArgHint     string `json:"argHint,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Plugin      string `json:"plugin,omitempty"`
	Subagent    bool   `json:"subagent,omitempty"`
}

// slash lists the complete slash surface Submit resolves, so a frontend can
// offer completion without restating what the controller accepts. Built-in
// verbs are absent on purpose: most of them are chat-TUI only, and offering
// one the HTTP path drops would be worse than not listing it.
func (s *Server) slash(w http.ResponseWriter, r *http.Request) {
	ctl := s.ctl()
	out := []slashEntry{}
	// Submit resolves a custom command before a skill of the same name; listing
	// them in that order keeps the menu's answer and the kernel's identical.
	seen := map[string]bool{}
	for _, c := range ctl.Commands() {
		if c.Hidden || seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		out = append(out, slashEntry{
			Name: c.Name, Kind: "command", Description: c.Description,
			ArgHint: c.ArgHint, Plugin: c.Plugin,
		})
	}
	for _, sk := range ctl.SlashSkills() {
		name := sk.SlashName()
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, slashEntry{
			Name: name, Kind: "skill", Description: sk.Description,
			Scope: string(sk.Scope), Plugin: sk.Plugin,
			Subagent: sk.RunAs == skill.RunSubagent,
		})
	}
	writeJSONCached(w, r, out)
}

// skillEntry is one discoverable skill as a management surface needs it. The
// slash list answers "what can I type"; this answers "what may run", which is
// the larger set: a skill with no slash name still fires on model discovery.
type skillEntry struct {
	Name        string   `json:"name"`
	SlashName   string   `json:"slashName,omitempty"`
	Description string   `json:"description,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	Plugin      string   `json:"plugin,omitempty"`
	Path        string   `json:"path,omitempty"`
	Subagent    bool     `json:"subagent,omitempty"`
	ReadOnly    bool     `json:"readOnly,omitempty"`
	Model       string   `json:"model,omitempty"`
	Effort      string   `json:"effort,omitempty"`
	AllowedURI  []string `json:"allowedTools,omitempty"`
	// Manual means the model never discovers it; only an explicit call runs it.
	Manual  bool `json:"manual,omitempty"`
	Enabled bool `json:"enabled"`
	// SwitchScope is where the decision governing Enabled lives — "project" when
	// this project set it for itself, "global" when it applies everywhere, and
	// empty when nothing overrides what the skill itself declares.
	SwitchScope string `json:"switchScope,omitempty"`
}

// skills lists every discoverable skill, disabled ones included, so the
// management surface can re-enable what it hid. Implicit reports whether
// model-initiated discovery is on at all — a global off makes every "auto"
// skill manual in practice, and hiding that would misreport all of them.
func (s *Server) skills(w http.ResponseWriter, r *http.Request) {
	root, other, ok := s.requestedRoot(r)
	if !ok {
		unknownProject(w, root)
		return
	}
	if other {
		s.skillsForProject(w, root)
		return
	}
	ctl := s.ctl()
	raw := ctl.AllSkills()
	entries := make([]skillEntry, 0, len(raw))
	for _, sk := range raw {
		switchScope := ""
		if scope, found := ctl.SkillOverrideScope(sk.Name); found {
			switchScope = string(scope)
		}
		entries = append(entries, skillEntry{
			SwitchScope: switchScope,
			Name:        sk.Name,
			SlashName:   sk.SlashName(),
			Description: sk.Description,
			Scope:       string(sk.Scope),
			Plugin:      sk.Plugin,
			Path:        sk.Path,
			Subagent:    sk.RunAs == skill.RunSubagent,
			ReadOnly:    sk.ReadOnly,
			Model:       sk.Model,
			Effort:      sk.Effort,
			AllowedURI:  sk.AllowedTools,
			Manual:      strings.EqualFold(strings.TrimSpace(sk.Invocation), "manual"),
			Enabled:     ctl.SkillEnabled(sk.Name),
		})
	}
	writeJSON(w, map[string]any{
		"implicit": ctl.ImplicitSkillInvocationEnabled(),
		"skills":   entries,
		"scope":    scopeView(ctl),
	})
}

// writeCatalogError projects a capability-switch failure onto the class its
// producer gave it. Every one of these went out as 400, so a name nobody
// declared and a disk that would not write said the same thing: your request
// was bad. The dependency arm answers 409 — the request was fine and the world
// does not satisfy it — pending the MCP failure taxonomy.
func writeCatalogError(w http.ResponseWriter, err error) {
	var unknownSkill *skill.NotFoundError
	var unknownServer *config.ServerNotFoundError
	switch {
	case errors.As(err, &unknownSkill):
		notFound(w, "skill", unknownSkill.Name)
	case errors.As(err, &unknownServer):
		notFound(w, "MCP server", unknownServer.Name)
	// Checked before the failure it accompanies: the switch is persisted and the
	// runtime disagrees, which is the one outcome here worth interrupting over.
	case errors.Is(err, control.ErrSwitchNotUndone):
		refuse(w, http.StatusConflict, "mcp.switch_not_undone", err.Error(), nil)
	case errors.Is(err, control.ErrMCPUnavailable):
		refuse(w, http.StatusConflict, "mcp.unavailable", err.Error(), nil)
	case errors.Is(err, config.ErrActivationUnavailable):
		refuse(w, http.StatusInternalServerError, "activation.unavailable", err.Error(), nil)
	default:
		// Not a condition this package can name. Internal is the safe direction:
		// a failure nobody classified is not evidence the request was wrong.
		// writeErr still renders a malformed store as the file it is.
		writeErr(w, http.StatusInternalServerError, err)
	}
}

// skillEnabled persists one skill's switch at the requested scope, or clears it
// so the skill inherits again. Live in the session that flips it: the next
// eligible turn owes the model a catalogue without the skill, and a slash
// invocation stops resolving at once.
func (s *Server) skillEnabled(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Scope   string `json:"scope"`
		Clear   bool   `json:"clear"`
		Root    string `json:"root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		missingField(w, "name")
		return
	}
	name, scope := strings.TrimSpace(body.Name), activationScope(body.Scope)
	root, other, ok := s.resolveRoot(body.Root)
	if !ok {
		unknownProject(w, root)
		return
	}
	if other {
		if err := s.switchForProject(root, string(config.CapabilitySkill), name, scope, body.Enabled, body.Clear); err != nil {
			writeCatalogError(w, err)
			return
		}
		writeJSON(w, map[string]any{"enabled": body.Enabled, "scope": string(scope), "root": root})
		return
	}
	if body.Clear {
		if err := s.ctl().ClearSkillOverride(name, scope); err != nil {
			writeCatalogError(w, err)
			return
		}
		writeJSON(w, map[string]any{
			"enabled": s.ctl().SkillEnabled(name), "cleared": true, "restartRequired": false,
		})
		return
	}
	if err := s.ctl().SetSkillEnabled(name, scope, body.Enabled); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, map[string]any{
		"enabled": body.Enabled, "scope": string(scope), "restartRequired": false,
	})
}

// activationScope reads the scope a mutation asks for. Project is the default:
// a switch flipped while looking at one project answers for that project, and
// applying it everywhere is the deliberate choice.
func activationScope(raw string) config.ActivationScope {
	if strings.EqualFold(strings.TrimSpace(raw), string(config.ActivationGlobal)) {
		return config.ActivationGlobal
	}
	return config.ActivationProject
}

// mcpEntry is one external tool provider. State is what the user can act on —
// running, still connecting, failed, disabled, or configured but not connected.
type mcpEntry struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Enabled   bool   `json:"enabled"`
	Transport string `json:"transport,omitempty"`
	Source    string `json:"source,omitempty"`
	// Description is the server's own account of itself. Nothing local can
	// stand in for it: a server that never said what it is for has none.
	Description string    `json:"description,omitempty"`
	Tools       int       `json:"tools"`
	Prompts     int       `json:"prompts,omitempty"`
	Resources   int       `json:"resources,omitempty"`
	ToolList    []mcpTool `json:"toolList,omitempty"`
	// Remembered marks an answer recovered from the schema cache rather than a
	// live connection, and Stale that the declaration changed since. A row that
	// hides the difference claims a disconnected server is reporting for itself.
	Remembered bool   `json:"remembered,omitempty"`
	Stale      bool   `json:"stale,omitempty"`
	Error      string `json:"error,omitempty"`
	// HTTPStatus is what the endpoint answered, or absent when the failure was
	// not one. It travels as the number the host held: a row that had to read it
	// back out of Error was reading text the server itself wrote.
	HTTPStatus int `json:"httpStatus,omitempty"`
	// LocalOverride marks a switch this project set for itself instead of
	// inheriting the global one, which the surface shows as a local exception.
	LocalOverride bool `json:"localOverride,omitempty"`
}

// mcpTool is one tool as the server describes it, plus the two hints that
// decide how a call is classified. Error carries a schema the host rejected:
// the tool is listed but not callable, which is a different thing from absent.
type mcpTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ReadOnly    bool   `json:"readOnly,omitempty"`
	Destructive bool   `json:"destructive,omitempty"`
	Error       string `json:"error,omitempty"`
}

// Server-written text lands in a settings row, not a document. The cap is
// generous enough for a paragraph and small enough that one verbose server
// cannot make the page it is listed on expensive to load.
const (
	mcpServerTextLimit = 400
	mcpToolTextLimit   = 240
)

func mcpToolViews(tools []plugin.ToolInfo) []mcpTool {
	out := make([]mcpTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, mcpTool{
			Name:        textutil.SanitizeDisplay(t.Name),
			Description: displayText(t.Description, mcpToolTextLimit),
			ReadOnly:    t.ReadOnlyHint,
			Destructive: t.DestructiveHint,
			Error:       displayText(t.SchemaError, mcpToolTextLimit),
		})
	}
	return out
}

// displayText prepares one piece of server-written text for a UI row.
func displayText(s string, limit int) string {
	return textutil.TruncateGraphemes(textutil.SanitizeDisplay(s), limit, "…")
}

// mcp lists the session's external tool providers. The activation switch is
// resolved for every configured name, because "off" and "never needed yet" look
// identical from the live host and mean opposite things to the user.
func (s *Server) mcp(w http.ResponseWriter, r *http.Request) {
	root, other, ok := s.requestedRoot(r)
	if !ok {
		unknownProject(w, root)
		return
	}
	if other {
		s.mcpForProject(w, root)
		return
	}
	ctl := s.ctl()
	out := []mcpEntry{}
	configured := ctl.ConfiguredMCPServers()
	declared := make(map[string]control.MCPServerState, len(configured))
	for _, st := range configured {
		declared[st.Entry.Name] = st
	}
	// A runtime-only server has no configured declaration; it is live, so it is
	// on by definition.
	on := func(name string) bool {
		st, ok := declared[name]
		return st.Enabled || !ok
	}
	host := ctl.Host()
	seen := map[string]bool{}
	if host != nil {
		for _, srv := range host.Servers() {
			seen[srv.Name] = true
			out = append(out, mcpEntry{
				Name: srv.Name, State: "ready", Enabled: on(srv.Name), LocalOverride: declared[srv.Name].LocalOverride,
				Transport: srv.Transport, Source: srv.ConfigSource,
				Description: displayText(srv.Description, mcpServerTextLimit),
				Tools:       srv.Tools, Prompts: srv.Prompts, Resources: srv.Resources,
				ToolList: mcpToolViews(srv.ToolList),
			})
		}
		for _, name := range host.ConnectingServers() {
			if !seen[name] {
				seen[name] = true
				out = append(out, remembered(declared[name], mcpEntry{
					Name: name, State: "connecting", Enabled: on(name),
					Source: string(declared[name].Entry.Source), LocalOverride: declared[name].LocalOverride,
				}))
			}
		}
		for _, f := range host.Failures() {
			if seen[f.Name] {
				continue
			}
			seen[f.Name] = true
			out = append(out, remembered(declared[f.Name], mcpEntry{
				Name: f.Name, State: "failed", Enabled: on(f.Name), LocalOverride: declared[f.Name].LocalOverride,
				Transport: f.Transport, Source: string(declared[f.Name].Entry.Source), Error: f.Error,
				HTTPStatus: f.HTTPStatus,
			}))
		}
	}
	// Configured with no process running, which is three different things —
	// the catalog tells the last two apart, not the host. configuredState below
	// carries the reasoning.
	inCatalog := ctl.MCPCatalogTools()
	for _, st := range configured {
		if seen[st.Entry.Name] {
			continue
		}
		out = append(out, remembered(st, mcpEntry{
			Name: st.Entry.Name, State: configuredState(st, inCatalog[st.Entry.Name]),
			Enabled: st.Enabled, LocalOverride: st.LocalOverride,
			Transport: st.Entry.Type, Source: string(st.Entry.Source),
		}))
	}
	writeJSON(w, map[string]any{"servers": out, "scope": scopeView(ctl)})
}

// configuredState is what a server with no process running is. Three answers,
// not two: switched off, standing by with its tools already in the catalog and
// the process due to start on the first call, or holding nothing to offer yet.
// The middle one is the steady state of every working cache-hit server, and
// reporting it as a failed connection is what sent people looking for a fault.
func configuredState(st control.MCPServerState, inCatalog int) string {
	switch {
	case !st.Enabled:
		return "disabled"
	case inCatalog == 0:
		return "idle"
	default:
		return "standby"
	}
}

// remembered fills a row that has no live connection to ask with what the last
// successful handshake recorded. It is marked as remembered rather than merged
// silently: a row that cannot tell the two apart claims a server that is off or
// broken is reporting for itself.
func remembered(st control.MCPServerState, e mcpEntry) mcpEntry {
	if st.Description == "" && len(st.Tools) == 0 {
		return e
	}
	e.Description = displayText(st.Description, mcpServerTextLimit)
	if len(st.Tools) > 0 {
		e.ToolList = mcpToolViews(st.Tools)
		e.Tools = len(st.Tools)
	}
	e.Remembered = true
	e.Stale = st.Stale
	return e
}

// mcpReconnect retries one server. It answers with the refreshed row rather than
// a bare 204: the outcome the user is waiting for is the new state, and a
// follow-up GET would race the connect that just finished.
func (s *Server) mcpReconnect(w http.ResponseWriter, r *http.Request) {
	name, ok := decodeMCPName(w, r)
	if !ok {
		return
	}
	tools, err := s.ctl().ReconnectMCPServer(name)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]any{"name": name, "state": "failed", "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"name": name, "state": "ready", "tools": tools})
}

// mcpEnabled flips the durable activation switch for one server at the
// requested scope, or clears this project's exception so it inherits again.
func (s *Server) mcpEnabled(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		Scope   string `json:"scope"`
		Clear   bool   `json:"clear"`
		Root    string `json:"root"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
		missingField(w, "name")
		return
	}
	name, scope := strings.TrimSpace(body.Name), activationScope(body.Scope)
	root, other, ok := s.resolveRoot(body.Root)
	if !ok {
		unknownProject(w, root)
		return
	}
	if other {
		if err := s.switchForProject(root, string(config.CapabilityMCP), name, scope, body.Enabled, body.Clear); err != nil {
			writeCatalogError(w, err)
			return
		}
		writeJSON(w, map[string]any{"name": name, "enabled": body.Enabled, "scope": string(scope), "root": root})
		return
	}
	if body.Clear {
		if err := s.ctl().ClearMCPServerOverride(name, scope); err != nil {
			writeCatalogError(w, err)
			return
		}
		enabled, err := s.ctl().MCPServerEnabled(name)
		if err != nil {
			writeCatalogError(w, err)
			return
		}
		writeJSON(w, map[string]any{"name": name, "enabled": enabled, "cleared": true})
		return
	}
	if err := s.ctl().SetMCPServerEnabled(name, scope, body.Enabled); err != nil {
		writeCatalogError(w, err)
		return
	}
	writeJSON(w, map[string]any{"name": name, "enabled": body.Enabled, "scope": string(scope)})
}

// draftServer is one server a paste resolved to, in the shape the confirmation
// card reads: what will run, what it will read, and what is risky about it.
type draftServer struct {
	Name      string            `json:"name"`
	Transport string            `json:"transport"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	URL       string            `json:"url,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
}

type draftRisk struct {
	Server string `json:"server"`
	Kind   string `json:"kind"`
	Field  string `json:"field"`
	Detail string `json:"detail"`
}

// mcpParse resolves a pasted block without installing anything. Parsing is
// separate from installing because the user has to see what a stranger's config
// would run on their machine before agreeing to it.
func (s *Server) mcpParse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	draft, err := mcpsetup.Parse(body.Input)
	if err != nil {
		saveFailed(w, http.StatusBadRequest, "mcp.bad_declaration", err)
		return
	}
	servers := make([]draftServer, 0, len(draft.Entries))
	for _, e := range draft.Entries {
		servers = append(servers, draftServer{
			Name: e.Name, Transport: transportOf(e), Command: e.Command, Args: e.Args,
			URL: e.URL, Env: e.Env, Headers: e.Headers,
		})
	}
	risks := make([]draftRisk, 0, len(draft.Risks))
	for _, k := range draft.Risks {
		risks = append(risks, draftRisk{Server: k.Server, Kind: k.Kind, Field: k.Field, Detail: k.Detail})
	}
	writeJSON(w, map[string]any{"servers": servers, "risks": risks})
}

// transportOf names the wire an entry uses; an empty Type with a command is the
// stdio default, which the confirmation card should still say out loud.
func transportOf(e config.PluginEntry) string {
	if t := strings.TrimSpace(e.Type); t != "" {
		return t
	}
	if strings.TrimSpace(e.URL) != "" {
		return "http"
	}
	return "stdio"
}

// mcpInstall connects the candidate and persists it only if that works. The
// response is the install result, not a bare 204: "saved" and "actually usable"
// are different outcomes and the user is waiting to hear which one happened.
func (s *Server) mcpInstall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Server draftServer `json:"server"`
		Scope  string      `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	entry := config.PluginEntry{
		Name:    strings.TrimSpace(body.Server.Name),
		Type:    strings.TrimSpace(body.Server.Transport),
		Command: strings.TrimSpace(body.Server.Command),
		Args:    body.Server.Args,
		URL:     strings.TrimSpace(body.Server.URL),
		Env:     body.Server.Env,
		Headers: body.Server.Headers,
	}
	if entry.Type == "stdio" {
		entry.Type = ""
	}
	scope := control.MCPScopeUser
	switch strings.ToLower(strings.TrimSpace(body.Scope)) {
	case string(control.MCPScopeProject):
		scope = control.MCPScopeProject
	case string(control.MCPScopeLocal):
		scope = control.MCPScopeLocal
	}
	result, err := s.ctl().InstallMCPServer(entry, scope)
	if err != nil {
		saveFailed(w, http.StatusInternalServerError, "mcp.install_failed", err)
		return
	}
	writeJSON(w, result)
}

// mcpRemove drops a server from config and disconnects it. A same-named
// declaration at a lower precedence may become effective again, so the reply
// reports what is live afterwards rather than implying the name is gone.
func (s *Server) mcpRemove(w http.ResponseWriter, r *http.Request) {
	name, ok := decodeMCPName(w, r)
	if !ok {
		return
	}
	disconnected, err := s.ctl().RemoveMCPServer(name)
	if err != nil {
		saveFailed(w, http.StatusBadRequest, "mcp.remove_failed", err)
		return
	}
	remaining := false
	for _, st := range s.ctl().ConfiguredMCPServers() {
		if st.Entry.Name == name {
			remaining = true
			break
		}
	}
	writeJSON(w, map[string]any{"name": name, "disconnected": disconnected, "stillConfigured": remaining})
}

func decodeMCPName(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &body) {
		return "", false
	}
	if strings.TrimSpace(body.Name) == "" {
		missingField(w, "name")
		return "", false
	}
	return strings.TrimSpace(body.Name), true
}
