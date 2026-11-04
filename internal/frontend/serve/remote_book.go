package serve

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/platform/wsl"
)

// The host book is user-global: a machine is reachable from every project on
// this computer, so an entry written into one repository's file would follow
// that repository to someone who cannot reach it.

// RemoteHostEdit is one row of the book as the settings page sends it. Secrets
// are named, never carried: the two Env fields hold the name of a variable, the
// same shape a provider's key takes.
type RemoteHostEdit struct {
	Name          string   `json:"name"`
	Kind          string   `json:"kind"`
	Distro        string   `json:"distro"`
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
	if !decodeRemoteBody(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		refuse(w, http.StatusBadRequest, "remote.name_required", "give this machine a name", nil)
		return
	}
	if (config.RemoteHostEntry{Kind: body.Kind}).HostKind() == config.RemoteKindWSL {
		h.saveWSLHost(w, name, body)
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
			if existing.HostKind() != config.RemoteKindSSH || strings.TrimSpace(body.Distro) != "" {
				return nil, errRemoteKind
			}
			entry.Forwards = append([]config.RemoteForwardEntry(nil), existing.Forwards...)
		}
		return nil, c.UpsertRemoteHost(entry)
	})
	if errors.Is(err, errRemoteKind) {
		refuse(w, http.StatusConflict, "remote.kind_unsupported", "this machine is not an SSH host, so this form cannot change it", nil)
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// errRemoteKind stops the SSH form from rewriting a machine of another kind,
// which would drop the fields it has no control for.
var errRemoteKind = errors.New("remote host is not an ssh host")

func (h *Hub) removeRemoteHost(w http.ResponseWriter, r *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if !decodeRemoteBody(w, r, &body) {
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

// placeRemoteHost puts a machine before another in the book, or last. The
// sidebar lists machines in the book's order, so this is the one place that
// order is set; naming the neighbour keeps it right when the sidebar hides some.
func (h *Hub) placeRemoteHost(w http.ResponseWriter, r *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	var body struct {
		Before string `json:"before"`
	}
	if !decodeRemoteBody(w, r, &body) {
		return
	}
	host, before := r.PathValue("host"), strings.TrimSpace(body.Before)
	err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		if _, ok := c.RemoteHost(host); !ok {
			return nil, unknownRemoteHost(host)
		}
		if _, ok := c.RemoteHost(before); before != "" && !ok {
			return nil, unknownRemoteHost(before)
		}
		c.PlaceRemoteHost(host, before)
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
	host, dirs, ok := h.remoteWorkspaceTarget(w, r)
	if !ok {
		return
	}
	h.commitRemoteWorkspace(w, host, func(c *config.Config) {
		for _, dir := range dirs {
			c.AddRemoteWorkspace(host, dir)
		}
	})
}

// removeRemoteWorkspace drops a folder from a machine's row. Nothing over there
// is touched: the folder stays, and adding it back costs one pick.
func (h *Hub) removeRemoteWorkspace(w http.ResponseWriter, r *http.Request) {
	host, dirs, ok := h.remoteWorkspaceTarget(w, r)
	if !ok {
		return
	}
	inFolder := func(rt *Runtime) bool {
		ep, ok := rt.Remote()
		return ok && ep.Host == host && slices.Contains(dirs, ep.Workspace)
	}
	if !h.releaseOrRefuse(w, r, "workspace.running", "a conversation in this folder is running; stop it first", h.panesWhere(inFolder)) {
		return
	}
	h.commitRemoteWorkspace(w, host, func(c *config.Config) {
		for _, dir := range dirs {
			c.RemoveRemoteWorkspace(host, dir)
		}
	})
}

// remoteWorkspaceTarget reads the machine and folder both writes are about.
func (h *Hub) remoteWorkspaceTarget(w http.ResponseWriter, r *http.Request) (host string, dirs []string, ok bool) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return "", nil, false
	}
	var body struct {
		Path  string   `json:"path"`
		Paths []string `json:"paths,omitempty"`
	}
	if !decodeRemoteBody(w, r, &body) {
		return "", nil, false
	}
	if strings.TrimSpace(body.Path) == "" {
		missingField(w, "path")
		return "", nil, false
	}
	return r.PathValue("host"), (config.RemoteHostEntry{Workspace: body.Path, Workspaces: body.Paths}).WorkspaceList(), true
}

func (h *Hub) commitRemoteWorkspace(w http.ResponseWriter, host string, apply func(*config.Config)) {
	err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		if _, ok := c.RemoteHost(host); !ok {
			return nil, unknownRemoteHost(host)
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
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	defer release()
	var tree json.RawMessage
	path := "/tree"
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	if err := farRequest(r.Context(), ep, http.MethodGet, path, nil, &tree); err != nil {
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
	if !decodeRemoteBody(w, r, &body) {
		return
	}
	path := strings.TrimSpace(body.Path)
	if path == "" {
		missingField(w, "path")
		return
	}
	ep, release, err := h.remoteLink(r, r.PathValue("host"))
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

// remoteLinkLinger keeps a link taken for one request past its answer. The
// tree, a delete and a flag change arrive in quick succession, and a link
// dropped between them is re-dialled — on a stopped WSL distribution, booted.
var remoteLinkLinger = 45 * time.Second

// remoteLink is an endpoint on host: a pane's link when one is open, or one
// taken for this request and handed back by release after remoteLinkLinger.
func (h *Hub) remoteLink(r *http.Request, host string) (RemoteEndpoint, func(), error) {
	// Disabling a host must also refuse reads that could reuse an existing pane.
	workspace, err := h.remoteHostWorkspace(host)
	if err != nil {
		return RemoteEndpoint{}, nil, err
	}
	if ep, ok := h.remotePane(host, workspace); ok {
		return ep, func() {}, nil
	}
	ep, release, err := h.opts.Remote.Attach(operationContext(r), host, workspace)
	if err != nil {
		return RemoteEndpoint{}, nil, err
	}
	return ep, func() {
		if remoteLinkLinger <= 0 {
			release()
			return
		}
		time.AfterFunc(remoteLinkLinger, release)
	}, nil
}

// remoteHostWorkspace resolves the user-held default before a link can be taken.
func (h *Hub) remoteHostWorkspace(name string) (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	entry, ok := cfg.RemoteHost(name)
	if !ok {
		return "", unknownRemoteHost(name)
	}
	if !entry.IsEnabled() {
		return "", refusal(http.StatusConflict, "remote.disabled", fmt.Errorf("this machine is turned off in the host book"), map[string]any{"host": name})
	}
	return entry.DefaultWorkspace(), nil
}

func (h *Hub) remotePane(host, workspace string) (RemoteEndpoint, bool) {
	for _, rt := range h.Runtimes() {
		if ep, ok := rt.Remote(); ok && ep.Host == host && ep.Workspace == workspace {
			return ep, true
		}
	}
	return RemoteEndpoint{}, false
}

func refuseNoRemote(w http.ResponseWriter) {
	refuse(w, http.StatusNotImplemented, "remote.not_available",
		"this kernel does not open panes on other machines", nil)
}

// saveWSLHost writes a WSL machine. Only its folder and install fields are
// read from the body: the SSH fields mean nothing for it, and the config layer
// refuses a row that carries them.
func (h *Hub) saveWSLHost(w http.ResponseWriter, name string, body RemoteHostEdit) {
	distro := strings.TrimSpace(body.Distro)
	if !config.ValidWSLDistroName(distro) {
		refuse(w, http.StatusBadRequest, "remote.distro_invalid", "that is not the name of a WSL distribution", nil)
		return
	}
	entry := config.RemoteHostEntry{
		Name:         name,
		Kind:         config.RemoteKindWSL,
		Distro:       distro,
		Workspace:    strings.TrimSpace(body.Workspace),
		Workspaces:   body.Workspaces,
		ServeInstall: strings.TrimSpace(body.ServeInstall),
		Disabled:     body.Disabled,
	}
	err := config.EditUserConfigWithCredentials(func(c *config.Config) ([]config.CredentialChange, error) {
		if existing, ok := c.RemoteHost(name); ok && existing.HostKind() != config.RemoteKindWSL {
			return nil, errRemoteKind
		}
		return nil, c.UpsertRemoteHost(entry)
	})
	if errors.Is(err, errRemoteKind) {
		refuse(w, http.StatusConflict, "remote.kind_unsupported", "this machine is not a WSL distribution, so it cannot be saved as one", nil)
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// WSLDistro is one installed distribution as the page shows it.
type WSLDistro struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Default bool   `json:"default,omitempty"`
}

// WSLDistros is what this machine has. Available is false where there is no
// WSL to ask, which is not a failure and draws no WSL choice.
type WSLDistros struct {
	Available bool        `json:"available"`
	Distros   []WSLDistro `json:"distros"`
}

func (h *Hub) remoteWSLDistros(w http.ResponseWriter, r *http.Request) {
	run := h.opts.WSL
	if run == nil {
		run = wsl.System()
	}
	list, err := wsl.List(r.Context(), run)
	switch {
	case errors.Is(err, wsl.ErrUnavailable):
		writeJSON(w, WSLDistros{Distros: []WSLDistro{}})
		return
	case err != nil:
		refuse(w, http.StatusBadGateway, "remote.wsl_unreadable", "WSL did not say which distributions are installed", nil)
		return
	}
	out := WSLDistros{Available: true, Distros: make([]WSLDistro, 0, len(list))}
	for _, d := range list {
		out.Distros = append(out.Distros, WSLDistro{Name: d.Name, Version: d.Version, Default: d.Default})
	}
	writeJSON(w, out)
}
