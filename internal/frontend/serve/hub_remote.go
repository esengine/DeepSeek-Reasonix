package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/contract/config"
)

// RemoteEndpoint is a `reasonix serve` on another machine, already reachable at
// a local address — the near end of an SSH forward. The hub takes the endpoint
// and not the connection: what makes a pane remote is where its HTTP goes, so
// SSH stays above this file and these panes stay testable over httptest.
type RemoteEndpoint struct {
	Host      string // configured host name, shown on the pane
	Workspace string // remote workspace path, as the remote kernel spells it
	Addr      string // local end of the forward, host:port
	Token     string // what the remote serve's token gate expects
	// Where the far hub publishes this pane's runtime. Without it the proxy
	// lands on that hub's default runtime, and two panes on one workspace
	// would drive a single conversation between them.
	Base string
	// The same runtime, for the hub-level call that retires it.
	RemoteID string
	// The transcript this pane was opened onto, when it was opened onto one. A
	// fresh session has none until its first turn, and asking the far kernel
	// costs a round trip the pane list cannot wait on.
	SessionPath string
}

func (ep RemoteEndpoint) validate() error {
	switch {
	case strings.TrimSpace(ep.Host) == "":
		return errors.New("remote endpoint needs a host name")
	case strings.TrimSpace(ep.Addr) == "":
		return errors.New("remote endpoint needs a local address")
	case strings.TrimSpace(ep.Workspace) == "":
		return errors.New("remote endpoint needs a workspace path")
	}
	return nil
}

// remoteBinding is what a proxied pane holds where a local one holds an
// assembly. Grouped rather than spread across Runtime so that "is this pane
// remote" is one question with one answer.
type remoteBinding struct {
	ep RemoteEndpoint
	// release drops this pane's hold on the connection its endpoint rides.
	// Several panes share one, so the last one out closes it, not the first.
	release func()
}

// farCloseTimeout bounds the retire call. A window shutting down closes every
// pane in turn, so one unreachable machine must not hold up the rest.
const farCloseTimeout = 10 * time.Second

// RemoteAttacher is what a host offers the hub for panes on other machines: a
// way to make one reachable, and what state each machine's link is in. The
// book of hosts stays here — configuration is this layer's, and keeping the
// attacher to the link is what keeps SSH out of the hub.
type RemoteAttacher interface {
	Attach(ctx context.Context, host, workspace string) (RemoteEndpoint, func(), error)
	// Browse lists the folders under dir on host, an empty dir meaning the
	// login home. It rides the connection alone — nothing is installed over
	// there — so a folder is pickable on a machine with no kernel to ask.
	Browse(ctx context.Context, host, dir string) (RemoteListing, error)
	// States is the live state per host name. A host with no link is absent
	// rather than reported idle: only the caller knows which hosts exist.
	States() map[string]RemoteLinkState
	// Candidates are ssh_config aliases this machine already knows how to
	// reach. Reading that file is the link layer's job, which is what keeps
	// SSH out of here even for a listing.
	Candidates() []string
	// Probe reads what a first connect would depend on, changing nothing over
	// there. A cold connect stops at the first missing piece, so without this
	// a reader learns them one failed attempt at a time.
	Probe(ctx context.Context, host string) (RemoteProbe, error)
}

// RemoteProbe is one machine's readiness. Ready says a route to a kernel
// exists, never that taking it will succeed — only trying finds that out.
type RemoteProbe struct {
	OS       string             `json:"os"`
	Arch     string             `json:"arch"`
	Home     string             `json:"home"`
	Kernel   string             `json:"kernel,omitempty"`
	Version  string             `json:"version,omitempty"`
	Outdated string             `json:"outdated,omitempty"`
	NPM      string             `json:"npm,omitempty"`
	Ready    bool               `json:"ready"`
	Routes   []RemoteProbeRoute `json:"routes"`
}

// RemoteProbeRoute is one way a kernel could get there. A closed one carries
// the same dotted code a failed connect would have refused with, so the window
// says the same sentence either way.
type RemoteProbeRoute struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	Code string `json:"code,omitempty"`
}

// RemoteLinkState is one machine's link as the attacher sees it.
type RemoteLinkState struct {
	Status  string // idle|connecting|connected|reconnecting|degraded|stopped
	Attempt int    // reconnect counter, while reconnecting
	Step    string // bootstrap step, while a first connect runs
	Detail  string
	Err     string
	Panes   int
}

// RemoteHostView is one row of the host book, with whatever its link is doing.
// The stored fields come back in full, not just the ones a row displays: saving
// replaces an entry, so a page that edited one field while holding a partial
// copy would blank whatever it never saw.
type RemoteHostView struct {
	Name   string `json:"name"`
	Target string `json:"target"`
	Status string `json:"status"`

	Host         string `json:"host,omitempty"`
	Port         int    `json:"port,omitempty"`
	User         string `json:"user,omitempty"`
	IdentityFile string `json:"identityFile,omitempty"`
	ProxyJump    string `json:"proxyJump,omitempty"`
	// Workspace is the default; Workspaces is every folder this machine is
	// worked in, default first — the sidebar lists them before a link exists,
	// which is the only moment the far kernel cannot answer for itself.
	Workspace     string   `json:"workspace,omitempty"`
	Workspaces    []string `json:"workspaces,omitempty"`
	ServeInstall  string   `json:"serveInstall,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	UseSSHConfig  bool     `json:"useSSHConfig,omitempty"`
	PassphraseEnv string   `json:"passphraseEnv,omitempty"`
	PasswordEnv   string   `json:"passwordEnv,omitempty"`
	// Forwards are set from the CLI and have no control here; the count is
	// shown so an edit does not look like it silently dropped them.
	Forwards int `json:"forwards,omitempty"`
	// Disabled is a row kept in the book but not dialed. It is reported so the
	// settings page can draw the switch, and so the sidebar can leave it out.
	Disabled bool `json:"disabled,omitempty"`

	Attempt int    `json:"attempt,omitempty"`
	Step    string `json:"step,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
	Panes   int    `json:"panes,omitempty"`
}

func (h *Hub) listRemoteHosts(w http.ResponseWriter, _ *http.Request) {
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	live := h.opts.Remote.States()
	out := make([]RemoteHostView, 0, len(cfg.Remote.Hosts))
	for _, entry := range cfg.Remote.Hosts {
		view := RemoteHostView{
			Name:          entry.Name,
			Target:        remoteTarget(entry),
			Status:        "idle",
			Disabled:      entry.Disabled,
			Host:          entry.Host,
			Port:          entry.Port,
			User:          entry.User,
			IdentityFile:  entry.IdentityFile,
			ProxyJump:     entry.ProxyJump,
			Workspace:     entry.Workspace,
			Workspaces:    entry.WorkspaceList(),
			ServeInstall:  entry.ServeInstall,
			Provider:      entry.ProviderMode(),
			UseSSHConfig:  entry.UseSSHConfig,
			PassphraseEnv: entry.PassphraseEnv,
			PasswordEnv:   entry.PasswordEnv,
			Forwards:      len(entry.Forwards),
		}
		if state, ok := live[entry.Name]; ok {
			view.Status, view.Attempt = state.Status, state.Attempt
			view.Step, view.Detail, view.Error, view.Panes = state.Step, state.Detail, state.Err, state.Panes
		}
		out = append(out, view)
	}
	writeJSON(w, out)
}

// remoteTarget spells a host the way the user would type it to ssh. The port
// is shown only when it is not the one ssh would have assumed.
func remoteTarget(entry config.RemoteHostEntry) string {
	target := entry.Host
	if entry.User != "" {
		target = entry.User + "@" + target
	}
	if entry.Port > 0 && entry.Port != 22 {
		target += ":" + strconv.Itoa(entry.Port)
	}
	return target
}

// OpenRemoteRequest asks for a pane on another machine. An empty Workspace
// takes the host entry's own default, which the attach layer resolves.
type OpenRemoteRequest struct {
	Host        string `json:"host"`
	Workspace   string `json:"workspace"`
	SessionPath string `json:"sessionPath"`
}

// farCall reaches the hub surface above a pane — opening the runtime a pane
// will drive, and retiring it afterwards. The pane's own traffic never comes
// through here; it goes through the proxy, which is mounted below this.
func farCall(ctx context.Context, ep RemoteEndpoint, path string, body, out any) error {
	return farRequest(ctx, ep, http.MethodPost, path, body, out)
}

func farRequest(ctx context.Context, ep RemoteEndpoint, method, path string, body, out any) error {
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+ep.Addr+path, payload)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", cookieToken+"="+ep.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// The same condition the proxy below reports, reached through a
		// different door: the link died before an answer came back.
		return refusal(http.StatusBadGateway, "remote.unreachable", err, map[string]any{"host": ep.Host})
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<10))
		return farRefusal(ep.Host, path, resp.StatusCode, resp.Status, body)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// farRefusal is a kernel on the other machine answering, and refusing. It is
// not the link being down, which is why it carries its own code: one is fixed
// by looking at the network and the other by reading what that kernel said.
// Without a code the whole account reaches the window as a status line, and a
// bare 502 there reads as a request that never arrived.
func farRefusal(host, path string, code int, status string, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if detail == "" {
		detail = status
	}
	err := fmt.Errorf("remote %s: %s: %s", path, status, detail)
	// Every path called here is registered over there for the method it is
	// called with, so "not that method" means the path belongs to that kernel's
	// page — where a kernel with no pane hub routes everything.
	if code == http.StatusMethodNotAllowed {
		return refusal(http.StatusBadGateway, "remote.kernel_too_old", err, map[string]any{"host": host})
	}
	params := map[string]any{"host": host, "detail": detail}
	// The far kernel's own reason travels with it, so a caller that relays
	// that kernel's decision can say which one it was.
	var far Reason
	if json.Unmarshal(body, &far) == nil && far.Code != "" {
		params["farCode"], params["farStatus"], params["farParams"] = far.Code, code, far.Params
	}
	return refusal(http.StatusBadGateway, "remote.kernel_refused", err, params)
}

// relayFarReason re-raises the far kernel's refusal under its own code and
// status. Used where that kernel owns the decision, so the window can say why
// rather than only that it was refused.
func relayFarReason(err error) error {
	var c *coded
	if !errors.As(err, &c) {
		return err
	}
	code, _ := c.reason.Params["farCode"].(string)
	status, _ := c.reason.Params["farStatus"].(int)
	if code == "" || status == 0 {
		return err
	}
	params, _ := c.reason.Params["farParams"].(map[string]any)
	return refusal(status, code, err, params)
}

func (h *Hub) openRemoteRuntime(w http.ResponseWriter, r *http.Request) {
	var req OpenRemoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		badBody(w)
		return
	}
	if h.opts.Remote == nil {
		refuseNoRemote(w)
		return
	}
	if err := h.remoteHostDialable(req.Host); errors.Is(err, errRemoteDisabled) {
		refuse(w, http.StatusConflict, "remote.disabled", "this machine is turned off in the host book", map[string]any{"host": req.Host})
		return
	} else if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	ep, release, err := h.opts.Remote.Attach(operationContext(r), req.Host, req.Workspace)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	// The pane drives a runtime of its own over there. The one the far kernel
	// started with belongs to whoever opens that port in a browser.
	var far RuntimeView
	if err := farCall(r.Context(), ep, "/runtimes", OpenRequest{
		Root:        ep.Workspace,
		SessionPath: strings.TrimSpace(req.SessionPath),
	}, &far); err != nil {
		release()
		writeErr(w, http.StatusBadGateway, err)
		return
	}
	ep.Base, ep.RemoteID, ep.SessionPath = far.Base, far.ID, far.SessionPath
	rt, err := h.OpenRemote(ep, release)
	if err != nil {
		release()
		writeErr(w, http.StatusConflict, err)
		return
	}
	// What the far side resolved, not what was asked for: ~ is expanded over
	// there, and a Windows host answers in its own spelling — record the other
	// one and the book would list a second row for the folder already open.
	rememberRemoteWorkspace(req.Host, ep.Workspace)
	writeJSON(w, rt.view())
}

// OpenRemote publishes a pane driven by a kernel on another machine. release is
// called once when the pane closes.
func (h *Hub) OpenRemote(ep RemoteEndpoint, release func()) (*Runtime, error) {
	if err := ep.validate(); err != nil {
		return nil, err
	}
	rt := &Runtime{
		ID:     h.nextID(),
		Root:   ep.Workspace,
		remote: &remoteBinding{ep: ep, release: release},
	}
	h.publish(rt)
	return rt, nil
}

// Remote reports the endpoint a proxied pane is bound to, so a host that has
// to reach the far kernel itself — a shell pumping its stream onto a bus the
// page can hear — can do so without a second way to address it.
func (rt *Runtime) Remote() (RemoteEndpoint, bool) {
	if rt.remote == nil {
		return RemoteEndpoint{}, false
	}
	return rt.remote.ep, true
}

// closeFarRuntime retires the runtime this pane was driving on the other
// machine. Best effort: the link may already be down, and a pane that cannot
// be closed locally is worse than one that leaked a runtime remotely.
func (rt *Runtime) closeFarRuntime() {
	id := rt.remote.ep.RemoteID
	if id == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), farCloseTimeout)
	defer cancel()
	if err := farCall(ctx, rt.remote.ep, "/runtimes/"+url.PathEscape(id)+"/close", nil, nil); err != nil {
		slog.Warn("serve: retire remote runtime", "host", rt.remote.ep.Host, "id", id, "err", err)
	}
}

// remoteView labels a proxied pane. The workspace belongs to another machine,
// so neither this one's filepath rules nor a single separator can cut its name
// — the remote spells it, and both separators reach us.
func (rt *Runtime) remoteView() RuntimeView {
	ep := rt.remote.ep
	return RuntimeView{
		ID:          rt.ID,
		Base:        runtimePrefix + rt.ID,
		Root:        ep.Workspace,
		Name:        remoteBaseName(ep.Workspace),
		Host:        ep.Host,
		SessionPath: ep.SessionPath,
	}
}

// remoteBaseName is the last segment of a path spelled by whichever machine
// owns it. Either separator may appear, and this machine's rules are not the
// authority on a path it does not hold.
func remoteBaseName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// remoteProxy forwards a pane's requests to the remote kernel.
func remoteProxy(ep RemoteEndpoint) http.Handler {
	target := &url.URL{Scheme: "http", Host: ep.Addr, Path: ep.Base}
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// SetURL appends the inbound path to the target's, so the pane's
			// own runtime prefix on the far side rides here.
			pr.SetURL(target)
			pr.Out.Host = target.Host
			// The window's own cookies mean nothing to the remote gate, and the
			// token belongs in a header rather than the URL for the reason the
			// gate itself prefers one: request lines end up in logs.
			pr.Out.Header.Set("Cookie", cookieToken+"="+ep.Token)
		},
		// Go already flushes text/event-stream, but a stream that carries a
		// length would sit in the buffer — and a turn's output is worth more
		// than the syscalls saved on the occasional download.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			// A dotted code, not a proxy's default 502 page: the frontend has to
			// tell "the link is down" from "the kernel refused" to say anything
			// useful, and a bare status cannot carry which host went quiet.
			refuse(w, http.StatusBadGateway, "remote.unreachable",
				"the remote kernel is not answering", map[string]any{"host": ep.Host})
		},
	}
}
