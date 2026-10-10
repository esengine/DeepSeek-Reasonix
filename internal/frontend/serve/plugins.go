// plugins.go — installed plugin packages: what each one contributes, and the
// two-phase install that puts one there.
package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reasonix/internal/base/textutil"
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
	"reasonix/internal/ext/installsource"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/ext/skill"
	"reasonix/internal/ext/theme"
)

// A package's capabilities are assembled at boot, so every write here ends
// with a runtime reload. A reload refused mid-turn is reported next to the
// result instead of as a failure: the state on disk did change.
func (s *Server) registerPluginRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /plugins", s.plugins)
	mux.HandleFunc("POST /plugins/plan", s.planPlugin)
	mux.HandleFunc("POST /plugins/install", s.installPlugin)
	mux.HandleFunc("POST /plugins/enabled", s.pluginEnabled)
	mux.HandleFunc("DELETE /plugins/{name}", s.removePlugin)
	mux.HandleFunc("GET /plugins/{name}/export", s.exportPlugin)
}

type pluginView struct {
	Name          string                         `json:"name"`
	Version       string                         `json:"version,omitempty"`
	Description   string                         `json:"description,omitempty"`
	Source        string                         `json:"source,omitempty"`
	Root          string                         `json:"root"`
	ManifestKind  string                         `json:"manifestKind,omitempty"`
	Enabled       bool                           `json:"enabled"`
	Status        string                         `json:"status,omitempty"`
	StatusReason  string                         `json:"statusReason,omitempty"`
	Compatibility string                         `json:"compatibility,omitempty"`
	Skipped       []pluginpkg.CompatibilityIssue `json:"skipped,omitempty"`
	Skills        []pluginItem                   `json:"skills,omitempty"`
	Agents        []pluginItem                   `json:"agents,omitempty"`
	Commands      []pluginItem                   `json:"commands,omitempty"`
	Prompts       []pluginItem                   `json:"prompts,omitempty"`
	Themes        []pluginItem                   `json:"themes,omitempty"`
	// Hooks, servers and a runtime execute code the package brought with it.
	// They stay out of the contribution list a reader skims, because "one more
	// skill" and "one more process" are not the same size of decision.
	Hooks      []pluginHook   `json:"hooks,omitempty"`
	MCPServers []pluginServer `json:"mcpServers,omitempty"`
	Runtime    *pluginRuntime `json:"runtime,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
	Error      string         `json:"error,omitempty"`
}

type pluginItem struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Invocation  string `json:"invocation,omitempty"`
}

type pluginHook struct {
	Event       string `json:"event"`
	Match       string `json:"match,omitempty"`
	Command     string `json:"command,omitempty"`
	ContextFile string `json:"contextFile,omitempty"`
	Description string `json:"description,omitempty"`
}

type pluginServer struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
	Transport   string `json:"transport,omitempty"`
	Command     string `json:"command,omitempty"`
	URL         string `json:"url,omitempty"`
	AutoStart   bool   `json:"autoStart,omitempty"`
}

// A runtime process runs inside Reasonix with the user's full trust, so what
// it intercepts and replaces is part of the row rather than a detail behind it.
type pluginRuntime struct {
	Command      string   `json:"command"`
	Args         []string `json:"args,omitempty"`
	Intercepts   []string `json:"intercepts,omitempty"`
	Replaces     []string `json:"replaces,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	Tools        []string `json:"tools,omitempty"`
}

func (s *Server) plugins(w http.ResponseWriter, r *http.Request) {
	home := config.ReasonixHomeDir()
	st, err := pluginpkg.LoadState(home)
	if err != nil {
		saveFailed(w, http.StatusInternalServerError, "plugin.state_unreadable", err)
		return
	}
	root := s.ctl().WorkspaceRoot()
	out := make([]pluginView, 0, len(st.Plugins))
	for _, p := range st.Plugins {
		out = append(out, pluginViewFor(home, root, p))
	}
	writeJSON(w, out)
}

// A package whose directory no longer parses still gets a row: it is installed,
// it is what the user came here to fix, and dropping it would make the list
// disagree with the state file.
func pluginViewFor(home, workspaceRoot string, p pluginpkg.InstalledPlugin) pluginView {
	root := pluginpkg.ResolveRoot(home, p.Root)
	p = p.Display()
	view := pluginView{
		Name: p.Name, Version: p.Version, Description: p.Description,
		Source: p.Source, Root: root,
		ManifestKind: p.ManifestKind, Enabled: p.Enabled,
		Status: p.Status, StatusReason: p.StatusReason,
	}
	pkg, warnings, err := pluginpkg.ParseDir(root)
	if err != nil {
		view.Error = textutil.ShownProse(err.Error())
		return view
	}
	all := append(warnings, theme.PluginWarnings(pkg)...)
	all = append(all, hook.PackageWarnings(pkg)...)
	for _, warning := range skill.PluginWarnings(pkg) {
		all = append(all, warning.Error())
	}
	view.Warnings = all
	view.Compatibility = textutil.ShownIdentity(pkg.Compatibility.Status)
	view.Skipped = pluginpkg.DisplayIssues(pkg.Compatibility.Skipped)

	inv := pkg.InventoryForDisplay()
	for _, sk := range inv.Skills {
		view.Skills = append(view.Skills, pluginItem{
			Name: sk.Name, Description: sk.Description, Invocation: "/" + p.Name + ":" + sk.Name,
		})
	}
	for _, a := range inv.Agents {
		view.Agents = append(view.Agents, pluginItem{
			Name: a.Name, Description: a.Description, Invocation: "/" + p.Name + ":agent:" + a.Name,
		})
	}
	for _, c := range inv.Commands {
		view.Commands = append(view.Commands, pluginItem{
			Name: c.Name, Description: c.Description, Invocation: "/" + p.Name + ":" + c.Name,
		})
	}
	for _, pr := range inv.Prompts {
		view.Prompts = append(view.Prompts, pluginItem{
			Name: pr.Name, Description: pr.Description, Invocation: "/" + p.Name + ":" + pr.Name,
		})
	}
	for _, th := range inv.Themes {
		view.Themes = append(view.Themes, pluginItem{Name: th.Name})
	}
	for _, h := range inv.Hooks {
		view.Hooks = append(view.Hooks, pluginHook{
			Event: h.Event, Match: h.Match, Command: h.Command,
			ContextFile: h.ContextFile, Description: h.Description,
		})
	}
	for _, srv := range inv.MCPServers {
		view.MCPServers = append(view.MCPServers, pluginServer{
			Name: srv.Name, DisplayName: srv.DisplayName, Description: srv.Description,
			Transport: srv.Transport, Command: srv.Command, URL: srv.URL, AutoStart: srv.AutoStart,
		})
	}
	if rt := pkg.Manifest.Runtime.Display(); rt != nil {
		view.Runtime = &pluginRuntime{
			Command: rt.Command, Args: rt.Args, Intercepts: rt.Intercepts,
			Replaces: rt.Replaces, Capabilities: rt.Capabilities, Tools: pluginpkg.DisplayIdentities(rt.ToolNames()),
		}
	}
	view.Warnings = pluginpkg.DisplayLines(append(view.Warnings, hookRuntimeWarnings(pkg, workspaceRoot)...))
	return view
}

// hookRuntimeWarnings reports the package's hooks that cannot run on this
// machine — a bash hook on a Windows box without Git Bash is the usual one.
// It rides on the listing rather than behind a "diagnose" button: a hook that
// silently never fires is exactly what the user came to this page to find out.
func hookRuntimeWarnings(pkg pluginpkg.Package, workspaceRoot string) []string {
	if len(pkg.Manifest.Hooks) == 0 {
		return nil
	}
	options := hook.RuntimeOptions{}
	if cfg, _ := config.LoadForRootReadOnly(workspaceRoot); cfg != nil {
		options = hook.RuntimeOptionsForShell(cfg.Tools.Shell.Prefer, cfg.Tools.Shell.Path)
	}
	var out []string
	for _, issue := range hook.CheckPackageRuntime(pkg, options) {
		out = append(out, fmt.Sprintf("%s 钩子跑不起来：%v", issue.Event, issue.Err))
	}
	return out
}

type pluginInstallRequest struct {
	Source  string `json:"source"`
	Name    string `json:"name,omitempty"`
	Link    bool   `json:"link,omitempty"`
	Replace bool   `json:"replace,omitempty"`
	// PlanID echoes the plan the user approved. install_source refuses an apply
	// whose plan hashes differently, which is what stops a source from
	// describing one thing and installing another.
	PlanID string `json:"planId,omitempty"`
}

func (s *Server) planPlugin(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePluginInstall(w, r)
	if !ok {
		return
	}
	s.answerInstallSource(r.Context(), w, pluginInstallBody(req, false), false)
}

func (s *Server) installPlugin(w http.ResponseWriter, r *http.Request) {
	req, ok := decodePluginInstall(w, r)
	if !ok {
		return
	}
	s.answerInstallSource(r.Context(), w, pluginInstallBody(req, true), true)
}

func (s *Server) removePlugin(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if !pluginpkg.IsValidName(name) {
		refuse(w, http.StatusBadRequest, "plugin.bad_name", "that is not a plugin name", nil)
		return
	}
	body := map[string]any{"op": "uninstall", "kind": "plugin", "name": name, "scope": "global"}
	s.answerInstallSource(r.Context(), w, body, true)
}

func (s *Server) pluginEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		missingField(w, "name")
		return
	}
	if err := pluginpkg.SetEnabled(config.ReasonixHomeDir(), name, req.Enabled); err != nil {
		saveFailed(w, http.StatusUnprocessableEntity, "plugin.toggle_failed", err)
		return
	}
	out := map[string]any{"name": name, "enabled": req.Enabled}
	if err := s.reloadExtensions(r.Context()); err != nil {
		out["reloadError"] = err.Error()
	}
	writeJSON(w, out)
}

// exportPlugin answers with the package as a zip. What it strips on the way
// out — the literal values of its MCP and runtime configuration — is named in
// a header, because a download body has nowhere to say it and the person
// receiving the archive has to be told what they will need to fill in.
func (s *Server) exportPlugin(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if !pluginpkg.IsValidName(name) {
		refuse(w, http.StatusBadRequest, "plugin.bad_name", "that is not a plugin name", nil)
		return
	}
	home := config.ReasonixHomeDir()
	st, err := pluginpkg.LoadState(home)
	if err != nil {
		saveFailed(w, http.StatusInternalServerError, "plugin.state_unreadable", err)
		return
	}
	root := ""
	for _, p := range st.Plugins {
		if p.Name == name {
			root = pluginpkg.ResolveRoot(home, p.Root)
			break
		}
	}
	if root == "" {
		refuse(w, http.StatusNotFound, "plugin.not_installed", "that plugin is not installed", nil)
		return
	}
	archive, required, err := pluginpkg.Export(name, root)
	if err != nil {
		saveFailed(w, http.StatusUnprocessableEntity, "plugin.export_failed", err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`.zip"`)
	if len(required) > 0 {
		w.Header().Set("X-Reasonix-Required-Env", strings.Join(required, ","))
	}
	_, _ = w.Write(archive)
}

func decodePluginInstall(w http.ResponseWriter, r *http.Request) (pluginInstallRequest, bool) {
	var req pluginInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return req, false
	}
	req.Source = strings.TrimSpace(req.Source)
	if req.Source == "" {
		missingField(w, "source")
		return req, false
	}
	return req, true
}

func pluginInstallBody(req pluginInstallRequest, apply bool) map[string]any {
	mode := "copy"
	if req.Link {
		mode = "link"
	}
	body := map[string]any{
		"source":  req.Source,
		"kind":    "plugin",
		"mode":    mode,
		"replace": req.Replace,
		"apply":   apply,
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		body["name"] = name
	}
	if planID := strings.TrimSpace(req.PlanID); apply && planID != "" {
		body["planId"] = planID
	}
	return body
}

// answerInstallSource forwards the tool's own JSON. Its plan is already the
// structured answer a confirmation page renders, so re-shaping it here would
// only add a second place for the two to disagree. A tool error is a request
// the server could not carry out at all; everything the plan itself reports —
// blocked, denied, partial — rides in the body with its reasons intact.
func (s *Server) answerInstallSource(ctx context.Context, w http.ResponseWriter, body map[string]any, mayWrite bool) {
	raw, err := json.Marshal(body)
	if err != nil {
		saveFailed(w, http.StatusInternalServerError, "install.request_unreadable", err)
		return
	}
	tool := installsource.NewTool(installsource.Options{
		ProjectRoot: s.ctl().WorkspaceRoot(),
		// Uninstall drops the package's MCP servers out of the live session
		// too; without this they keep answering until something rebuilds.
		OnDisconnect: s.ctl().DisconnectMCPServer,
	})
	out, err := tool.Execute(ctx, raw)
	if err != nil {
		saveFailed(w, http.StatusUnprocessableEntity, "install.failed", err)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &fields); err != nil {
		saveFailed(w, http.StatusInternalServerError, "install.bad_answer", err)
		return
	}
	// A plan that wrote nothing has nothing to reload for, and a refused reload
	// would read as the install itself having failed.
	if mayWrite && jsonTrue(fields["applied"]) {
		if err := s.reloadExtensions(ctx); err != nil {
			fields["reloadError"], _ = json.Marshal(err.Error())
		}
	}
	writeJSON(w, fields)
}

func jsonTrue(raw json.RawMessage) bool {
	var v bool
	return json.Unmarshal(raw, &v) == nil && v
}
