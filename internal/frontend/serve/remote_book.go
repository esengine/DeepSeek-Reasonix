package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"reasonix/internal/contract/config"
)

// The host book is user-global: a machine is reachable from every project on
// this computer, so an entry written into one repository's file would follow
// that repository to someone who cannot reach it.

// RemoteHostEdit is one row of the book as the settings page sends it. Secrets
// are named, never carried: the two Env fields hold the name of a variable, the
// same shape a provider's key takes.
type RemoteHostEdit struct {
	Name          string   `json:"name"`
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	User          string   `json:"user"`
	IdentityFile  string   `json:"identityFile"`
	ProxyJump     string   `json:"proxyJump"`
	Workspace     string   `json:"workspace"`
	Workspaces    []string `json:"workspaces"`
	ServeInstall  string   `json:"serveInstall"`
	Provider      string   `json:"provider"`
	UseSSHConfig  bool     `json:"useSSHConfig"`
	PassphraseEnv string   `json:"passphraseEnv"`
	PasswordEnv   string   `json:"passwordEnv"`
	Disabled      bool     `json:"disabled"`
}

func (h *Hub) saveRemoteHost(w http.ResponseWriter, r *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	var body RemoteHostEdit
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		refuse(w, http.StatusBadRequest, "remote.name_required", "give this machine a name", nil)
		return
	}
	host := strings.TrimSpace(body.Host)
	if host == "" && !body.UseSSHConfig {
		// An entry that layers ssh_config has its address there; one that does
		// not has nowhere else to get it.
		refuse(w, http.StatusBadRequest, "remote.host_required", "say which address to dial", nil)
		return
	}
	if body.Port < 0 || body.Port > 65535 {
		refuse(w, http.StatusBadRequest, "remote.bad_port", "that is not a port number", nil)
		return
	}
	entry := config.RemoteHostEntry{
		Name:          name,
		Host:          host,
		Port:          body.Port,
		User:          strings.TrimSpace(body.User),
		IdentityFile:  strings.TrimSpace(body.IdentityFile),
		ProxyJump:     strings.TrimSpace(body.ProxyJump),
		Workspace:     strings.TrimSpace(body.Workspace),
		Workspaces:    body.Workspaces,
		ServeInstall:  strings.TrimSpace(body.ServeInstall),
		Provider:      strings.TrimSpace(body.Provider),
		UseSSHConfig:  body.UseSSHConfig,
		PassphraseEnv: strings.TrimSpace(body.PassphraseEnv),
		PasswordEnv:   strings.TrimSpace(body.PasswordEnv),
		Disabled:      body.Disabled,
	}
	if host == "" {
		entry.Host = name
	}
	err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		// Editing an entry must not drop the forwards it carries: they are set
		// from the CLI and have no control on this page to put them back.
		if existing, ok := c.RemoteHost(name); ok {
			entry.Forwards = append([]config.RemoteForwardEntry(nil), existing.Forwards...)
		}
		return nil, c.UpsertRemoteHost(entry)
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Hub) removeRemoteHost(w http.ResponseWriter, r *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	name := strings.TrimSpace(body.Name)
	// Panes on the machine are closed first, so no live link outlives its entry
	// in the book; one mid-turn is refused.
	onHost := func(rt *Runtime) bool { ep, ok := rt.Remote(); return ok && ep.Host == name }
	if !h.releaseOrRefuse(w, r, "remote.running", "a conversation on this machine is running; stop it first", h.panesWhere(onHost)) {
		return
	}
	err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		// Removing what is not there is the state the caller asked for, so it
		// is not an error to report.
		c.RemoveRemoteHost(name)
		return nil, nil
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// The folder list has endpoints of its own because the sidebar writes one field
// of a row where the settings page replaces all of it: a control that knows
// only a folder would blank every field it never displayed.

// addRemoteWorkspace records a folder on a machine in the near book. The far
// machine's own list would be the better home, but it is only readable while
// something is open over there, and cold is the state a folder is picked in.
func (h *Hub) addRemoteWorkspace(w http.ResponseWriter, r *http.Request) {
	host, dir, ok := h.remoteWorkspaceTarget(w, r)
	if !ok {
		return
	}
	h.commitRemoteWorkspace(w, host, func(c *config.Config) { c.AddRemoteWorkspace(host, dir) })
}

// removeRemoteWorkspace drops a folder from a machine's row. Nothing over there
// is touched: the folder stays, and adding it back costs one pick.
func (h *Hub) removeRemoteWorkspace(w http.ResponseWriter, r *http.Request) {
	host, dir, ok := h.remoteWorkspaceTarget(w, r)
	if !ok {
		return
	}
	inFolder := func(rt *Runtime) bool {
		ep, ok := rt.Remote()
		return ok && ep.Host == host && ep.Workspace == dir
	}
	if !h.releaseOrRefuse(w, r, "workspace.running", "a conversation in this folder is running; stop it first", h.panesWhere(inFolder)) {
		return
	}
	h.commitRemoteWorkspace(w, host, func(c *config.Config) { c.RemoveRemoteWorkspace(host, dir) })
}

// remoteWorkspaceTarget reads the machine and folder both writes are about.
func (h *Hub) remoteWorkspaceTarget(w http.ResponseWriter, r *http.Request) (host, dir string, ok bool) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return "", "", false
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return "", "", false
	}
	if dir = strings.TrimSpace(body.Path); dir == "" {
		missingField(w, "path")
		return "", "", false
	}
	return r.PathValue("host"), dir, true
}

func (h *Hub) commitRemoteWorkspace(w http.ResponseWriter, host string, apply func(*config.Config)) {
	err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		if _, ok := c.RemoteHost(host); !ok {
			return nil, fmt.Errorf("no machine named %q in the book", host)
		}
		apply(c)
		return nil, nil
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rememberRemoteWorkspace records a folder that was just opened, so the row it
// was reached through is still there next launch. Best effort: a pane already
// driving the far kernel must not be undone by a book that would not take a
// note about it.
func rememberRemoteWorkspace(host, dir string) {
	if strings.TrimSpace(host) == "" || strings.TrimSpace(dir) == "" {
		return
	}
	// Read before write. Every pane opened on a machine comes through here, and
	// nearly all of them land on a folder the book already has — rewriting the
	// user's config file for each one is a file touched for nothing.
	if cfg, err := config.Load(); err == nil {
		if entry, ok := cfg.RemoteHost(host); !ok || slices.Contains(entry.WorkspaceList(), dir) {
			return
		}
	}
	err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		c.AddRemoteWorkspace(host, dir)
		return nil, nil
	})
	if err != nil {
		slog.Warn("serve: remember remote workspace", "host", host, "dir", dir, "err", err)
	}
}

// remoteCandidates are aliases in the user's ~/.ssh/config that the book does
// not have yet — the cheapest way to fill it on a machine already set up.
func (h *Hub) remoteCandidates(w http.ResponseWriter, _ *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	known := map[string]bool{}
	if cfg, err := config.Load(); err == nil {
		for _, entry := range cfg.Remote.Hosts {
			known[entry.Name] = true
		}
	}
	out := make([]string, 0)
	for _, alias := range h.opts.Remote.Candidates() {
		if !known[alias] {
			out = append(out, alias)
		}
	}
	writeJSON(w, out)
}

// remoteTree is the far machine's own workspace list, read through a pane
// already open on it. It needs no second connection: any of that host's panes
// reaches the same hub, and that hub answers for the whole machine.
func (h *Hub) remoteTree(w http.ResponseWriter, r *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	ep, release, err := h.remoteLink(r, r.PathValue("host"))
	if errors.Is(err, errRemoteDisabled) {
		refuse(w, http.StatusConflict, "remote.disabled", "this machine is turned off in the host book", nil)
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	defer release()
	var tree json.RawMessage
	if err := farRequest(r.Context(), ep, http.MethodGet, "/tree", nil, &tree); err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(tree)
}

// removeRemoteSession erases a conversation on the far machine over the same
// link its list is read through. That kernel decides whether it may, so its
// refusal reaches the window under its own code.
func (h *Hub) removeRemoteSession(w http.ResponseWriter, r *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		badBody(w)
		return
	}
	path := strings.TrimSpace(body.Path)
	if path == "" {
		missingField(w, "path")
		return
	}
	ep, release, err := h.remoteLink(r, r.PathValue("host"))
	if errors.Is(err, errRemoteDisabled) {
		refuse(w, http.StatusConflict, "remote.disabled", "this machine is turned off in the host book", nil)
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	defer release()
	if err := farRequest(r.Context(), ep, http.MethodPost, "/tree/sessions/remove", map[string]string{"path": path}, nil); err != nil {
		writeErr(w, http.StatusBadGateway, relayFarReason(err))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// remoteLink is an endpoint on host: a pane's link when one is open, or one
// taken for this request and handed back by release.
func (h *Hub) remoteLink(r *http.Request, host string) (RemoteEndpoint, func(), error) {
	if ep, ok := h.anyRemotePane(host); ok {
		return ep, func() {}, nil
	}
	// The far kernel outlives the link, and its book is on its own disk. A
	// machine the book has turned off is not reached for at all, so a question
	// about it fails the same way opening it would.
	if err := h.remoteHostDialable(host); err != nil {
		return RemoteEndpoint{}, nil, err
	}
	return h.opts.Remote.Attach(operationContext(r), host, "")
}

// errRemoteDisabled is the typed refusal a machine the book has turned off gets,
// so a read answers the same code an open does.
var errRemoteDisabled = errors.New("this machine is turned off in the host book")

// remoteHostDialable reports why the book will not dial name: nil when it will,
// errRemoteDisabled when the machine is off, and the read error when the book
// cannot be read at all — a switch that stops a machine has to keep stopping it
// then too.
func (h *Hub) remoteHostDialable(name string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	entry, ok := cfg.RemoteHost(name)
	if !ok {
		return nil
	}
	if !entry.IsEnabled() {
		return errRemoteDisabled
	}
	return nil
}

// anyRemotePane returns an endpoint on host, for a question about the machine
// rather than about one pane.
func (h *Hub) anyRemotePane(host string) (RemoteEndpoint, bool) {
	for _, rt := range h.Runtimes() {
		if ep, ok := rt.Remote(); ok && ep.Host == host {
			return ep, true
		}
	}
	return RemoteEndpoint{}, false
}

func refuseNoRemote(w http.ResponseWriter) {
	refuse(w, http.StatusNotImplemented, "remote.not_available",
		"this kernel does not open panes on other machines", nil)
}
