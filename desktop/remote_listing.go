package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const (
	serveSnapshotMaxBytes = 32 << 20
	serveSessionsMaxBytes = 8 << 20
	serveEventMaxBytes    = 8 << 20
)

// This listing-only bridge lets project groups show sessions before the full
// remote-tab attach and event-pump surface lands.

// serveSessionEntry mirrors one GET /sessions row from the Serve.
type serveSessionEntry struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Title      string `json:"title"`
	Turns      int    `json:"turns"`
	Current    bool   `json:"current"`
	Running    bool   `json:"running"`
	MtimeMilli int64  `json:"mtimeMilli"`
}

type serveHTTPStatusError struct {
	url        string
	statusCode int
	message    string
}

func (e *serveHTTPStatusError) Error() string {
	if e.message != "" {
		return fmt.Sprintf("%s: status %d: %s", e.url, e.statusCode, e.message)
	}
	return fmt.Sprintf("%s: status %d", e.url, e.statusCode)
}

// RemoteSessionView mirrors one serve /sessions entry on the frontend side.
type RemoteSessionView struct {
	Name           string `json:"name"`
	Path           string `json:"path,omitempty"`
	Title          string `json:"title,omitempty"`
	Turns          int    `json:"turns,omitempty"`
	Current        bool   `json:"current,omitempty"`
	Running        bool   `json:"running,omitempty"`
	LastActivityAt int64  `json:"lastActivityAt,omitempty"`
	Pinned         bool   `json:"pinned,omitempty"`
}

// serveURL joins a serve base URL and an API path.
func serveURL(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

func newServeHTTPClient(base string) (*http.Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return nil, fmt.Errorf("invalid remote serve URL: %w", err)
	}
	ip := net.ParseIP(parsed.Hostname())
	if parsed.Scheme != "http" || ip == nil || !ip.IsLoopback() || parsed.User != nil {
		return nil, fmt.Errorf("remote serve URL must use loopback HTTP")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Jar:       jar,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// servePost keeps the bounded response text in failures so remote lease and
// busy-state hints reach the desktop surface.
func servePost(ctx context.Context, client *http.Client, url string, body []byte) error {
	_, err := servePostSessionPath(ctx, client, url, body)
	return err
}

const expectedSessionPathHeader = "X-Reasonix-Expected-Session-Path"

// servePostForSession fences a foreground mutation to the session the Desktop
// tab displayed when the command was issued. Older Serve binaries ignore the
// optional header and retain their single-session behavior.
func servePostForSession(ctx context.Context, client *http.Client, url string, body []byte, expectedPath string) error {
	if body == nil {
		body = []byte("{}")
	}
	resp, err := serveDoForSession(ctx, client, http.MethodPost, url, body, expectedPath)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return &serveHTTPStatusError{
		url: url, statusCode: resp.StatusCode, message: strings.TrimSpace(string(data)),
	}
}

// servePostSessionPath preserves the ordinary 2xx contract while reading the
// optional path header returned by session-rotation endpoints. Older Serve
// binaries omit it and keep their legacy untagged single-session behavior.
func servePostSessionPath(ctx context.Context, client *http.Client, url string, body []byte) (string, error) {
	return servePostSessionPathForSession(ctx, client, url, body, "")
}

func servePostSessionPathForSession(ctx context.Context, client *http.Client, url string, body []byte, expectedPath string) (string, error) {
	if body == nil {
		body = []byte("{}")
	}
	resp, err := serveDoForSession(ctx, client, http.MethodPost, url, body, expectedPath)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return strings.TrimSpace(resp.Header.Get("X-Reasonix-Session-Path")), nil
	}
	return "", &serveHTTPStatusError{
		url: url, statusCode: resp.StatusCode, message: strings.TrimSpace(string(data)),
	}
}

// serveDo issues a JSON request; the csrf guard rejects non-JSON POSTs.
func serveDo(ctx context.Context, client *http.Client, method, url string, body []byte) (*http.Response, error) {
	return serveDoForSession(ctx, client, method, url, body, "")
}

func serveDoForSession(ctx context.Context, client *http.Client, method, url string, body []byte, expectedPath string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if expectedPath = strings.TrimSpace(expectedPath); expectedPath != "" {
		req.Header.Set(expectedSessionPathHeader, expectedPath)
	}
	return client.Do(req)
}

// serveHandshake exchanges the pre-shared token for the session cookie.
// Serve replies 204 on success; the cookie lands in client's jar.
func serveHandshake(ctx context.Context, client *http.Client, base, token string) error {
	body, err := json.Marshal(map[string]string{"token": token})
	if err != nil {
		return err
	}
	resp, err := serveDo(ctx, client, http.MethodPost, serveURL(base, "/auth/token"), body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return fmt.Errorf("serve auth handshake: status %d", resp.StatusCode)
}

// serveSessions lists the serve's sessions.
func serveSessions(ctx context.Context, client *http.Client, base string) ([]serveSessionEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serveURL(base, "/sessions"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serve /sessions: status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, serveSessionsMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > serveSessionsMaxBytes {
		return nil, fmt.Errorf("serve /sessions response exceeds %d bytes", serveSessionsMaxBytes)
	}
	var out []serveSessionEntry
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func singleCurrentServeSession(entries []serveSessionEntry) *serveSessionEntry {
	var current *serveSessionEntry
	for i := range entries {
		if !entries[i].Current {
			continue
		}
		if current != nil {
			return nil
		}
		current = &entries[i]
	}
	return current
}

// serveClientForRef resolves an HTTP client for a host+workspace WITHOUT
// waking anything: a one-shot handshake against an already-ready serve
// registration. A serve that is not running reports an error — query paths
// must never cold-start one.
func (a *App) serveClientForRef(hostID, workspace string) (*http.Client, string, func(), error) {
	a.remoteTabMu.Lock()
	for _, tab := range a.remoteTabs {
		if tab.ref.HostID == hostID && tab.ref.Workspace == workspace && tab.state == "ready" && tab.client != nil {
			client, base := tab.client, tab.base
			a.remoteTabMu.Unlock()
			return client, base, func() {}, nil
		}
	}
	a.remoteTabMu.Unlock()

	rt, err := a.remoteRT()
	if err != nil {
		return nil, "", nil, err
	}
	view, token, ok := rt.ServeSnapshot(hostID, workspace)
	if !ok {
		return nil, "", nil, fmt.Errorf("remote serve for %s:%s is not running", hostID, workspace)
	}
	ctx := a.bootContext()
	if ctx == nil {
		ctx = context.Background()
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	client, clientErr := newServeHTTPClient(view.LocalURL)
	if clientErr != nil {
		cancel()
		return nil, "", nil, clientErr
	}
	if err := serveHandshake(callCtx, client, view.LocalURL, token); err != nil {
		cancel()
		return nil, "", nil, err
	}
	return client, view.LocalURL, cancel, nil
}

// serveClientEnsured may connect the host and start Serve, so only explicit
// user-intent paths should use it. Passive listings remain read-only.
func (a *App) serveClientEnsured(hostID, workspace string) (*http.Client, string, func(), error) {
	if client, base, done, err := a.serveClientForRef(hostID, workspace); err == nil {
		return client, base, done, nil
	}
	rt, err := a.remoteRT()
	if err != nil {
		return nil, "", nil, err
	}
	if err := rt.Connect(hostID); err != nil {
		return nil, "", nil, err
	}
	if err := waitForRemoteHost(rt, hostID, 60*time.Second); err != nil {
		return nil, "", nil, err
	}
	bootCtx := a.bootContext()
	if bootCtx == nil {
		bootCtx = context.Background()
	}
	view, token, err := rt.EnsureServer(bootCtx, hostID, workspace)
	if err != nil {
		return nil, "", nil, err
	}
	callCtx, cancel := context.WithTimeout(bootCtx, 30*time.Second)
	client, err := newServeHTTPClient(view.LocalURL)
	if err != nil {
		cancel()
		return nil, "", nil, err
	}
	if err := serveHandshake(callCtx, client, view.LocalURL, token); err != nil {
		cancel()
		return nil, "", nil, err
	}
	return client, view.LocalURL, cancel, nil
}

// RemoteProjectSessions lists a remote project's serve sessions for the
// project tree. Live-tab fast paths, desktop title overrides and pinned
// synthesis arrive with the remote sessions PR.
func (a *App) RemoteProjectSessions(hostID, workspace string) ([]RemoteSessionView, error) {
	client, base, done, err := a.serveClientForRef(hostID, workspace)
	if err != nil {
		return nil, err
	}
	defer done()
	ctx, cancel := commandContext(a)
	defer cancel()
	return a.remoteProjectSessions(ctx, client, base, hostID, workspace)
}

// EnsureRemoteProjectSessions is the explicit group-open listing path. It can
// wake the SSH host and Serve before returning sessions.
func (a *App) EnsureRemoteProjectSessions(hostID, workspace string) ([]RemoteSessionView, error) {
	client, base, done, err := a.serveClientEnsured(hostID, workspace)
	if err != nil {
		return nil, err
	}
	defer done()
	ctx, cancel := commandContext(a)
	defer cancel()
	return a.remoteProjectSessions(ctx, client, base, hostID, workspace)
}

func (a *App) remoteProjectSessions(ctx context.Context, client *http.Client, base, hostID, workspace string) ([]RemoteSessionView, error) {
	listing, err := a.fetchRemoteSessionListing(ctx, client, base, hostID, workspace)
	if err != nil {
		return nil, err
	}
	entries := listing.entries
	liveRunning := listing.liveRunning
	liveCurrentPath := listing.liveCurrentPath
	preferLiveCurrent := listing.preferLive
	out := make([]RemoteSessionView, 0, len(entries))
	pinned := make([]RemoteSessionView, 0, len(entries))
	prefs := remotePrefsSnapshot()
	hasCurrent := false
	for _, e := range entries {
		title := strings.TrimSpace(e.Title)
		prefKey := remoteSessionPrefKey(hostID, workspace, e.Name)
		if override := prefs.SessionTitles[prefKey]; override != "" {
			title = override
		}
		current := e.Current
		if preferLiveCurrent {
			current = liveCurrentPath != "" && e.Path == liveCurrentPath
		}
		view := RemoteSessionView{
			Name: e.Name, Path: e.Path, Title: title, Turns: e.Turns, Current: current,
			Running:        remoteSessionRunning(e.Running, liveRunning, e.Path, preferLiveCurrent),
			LastActivityAt: e.MtimeMilli,
			Pinned:         remoteSessionPinnedLocked(prefs, prefKey),
		}
		hasCurrent = hasCurrent || view.Current
		if view.Pinned {
			pinned = append(pinned, view)
		} else {
			out = append(out, view)
		}
	}
	if !hasCurrent {
		a.remoteTabMu.Lock()
		var blank *RemoteSessionView
		for _, tab := range a.remoteTabs {
			if tab.ref.HostID == hostID && tab.ref.Workspace == workspace && tab.session.reset {
				blank = &RemoteSessionView{Name: "", Path: tab.routing.currentPath, Title: tab.topicTitle, Current: true, Running: tab.runtime.running, LastActivityAt: time.Now().UnixMilli()}
				break
			}
		}
		a.remoteTabMu.Unlock()
		if blank != nil {
			return append([]RemoteSessionView{*blank}, append(pinned, out...)...), nil
		}
	}
	return append(pinned, out...), nil
}

type remoteSessionListing struct {
	entries         []serveSessionEntry
	liveRunning     map[string]bool
	liveCurrentPath string
	preferLive      bool
}

func (a *App) fetchRemoteSessionListing(ctx context.Context, client *http.Client, base, hostID, workspace string) (remoteSessionListing, error) {
	const maxRaceRetries = 2

listingAttempt:
	for attempt := 0; ; attempt++ {
		a.remoteTabMu.Lock()
		var observedTab *remoteTab
		var observedRevision uint64
		for _, tab := range a.remoteTabs {
			if tab.ref.HostID == hostID && tab.ref.Workspace == workspace {
				observedTab, observedRevision = tab, tab.routing.revision
				break
			}
		}
		a.remoteTabMu.Unlock()
		entries, err := serveSessions(ctx, client, base)
		if err != nil {
			return remoteSessionListing{}, err
		}
		authoritativeCurrent := singleCurrentServeSession(entries)
		authoritativeTitle := remoteAuthoritativeSessionTitle(hostID, workspace, authoritativeCurrent)
		unlockRoute := lockRemoteTabRoute(observedTab)
		a.remoteTabMu.Lock()
		liveRunning := map[string]bool{}
		liveCurrentPath := ""
		preferLiveCurrent := false
		var routeUpdate *TabMeta
		routeReadyBarrier := false
		for _, tab := range a.remoteTabs {
			if tab.ref.HostID != hostID || tab.ref.Workspace != workspace {
				continue
			}
			// Without a newer SSE/status revision, /sessions replaces the running
			// cache and current route. A raced revision preserves the newer live route
			// instead of marking both its row and the stale server row current.
			authoritativeListing := tab == observedTab && tab.routing.revision == observedRevision
			if !authoritativeListing && attempt < maxRaceRetries && remoteSessionRunningConflict(entries, tab.routing.running) {
				a.remoteTabMu.Unlock()
				unlockRoute()
				continue listingAttempt
			}
			if authoritativeListing {
				authoritative := make(map[string]bool, len(entries))
				for _, entry := range entries {
					authoritative[entry.Path] = entry.Running
				}
				tab.routing.running = authoritative
				if authoritativeCurrent != nil {
					path := strings.TrimSpace(authoritativeCurrent.Path)
					pathChanged := adoptRemoteTabSessionPathLocked(tab, path)
					tab.session.name = strings.TrimSpace(authoritativeCurrent.Name)
					if pathChanged {
						tab.topicTitle = authoritativeTitle
						meta := remoteTabMetaLocked(tab)
						routeUpdate = &meta
						routeReadyBarrier = remoteTabReadyBarrier(tab, true)
					}
				}
			} else {
				preferLiveCurrent = true
			}
			liveCurrentPath = tab.routing.currentPath
			maps.Copy(liveRunning, tab.routing.running)
			break
		}
		a.remoteTabMu.Unlock()
		if routeUpdate != nil {
			a.emitRemoteEvent("remote-tab:updated", *routeUpdate)
			if routeReadyBarrier {
				a.emitRemoteEvent(fmt.Sprintf("remote-tab:%s:state", routeUpdate.ID), RemoteTabStateView{State: "ready"})
			}
		}
		unlockRoute()
		return remoteSessionListing{
			entries: entries, liveRunning: liveRunning,
			liveCurrentPath: liveCurrentPath, preferLive: preferLiveCurrent,
		}, nil
	}
}

func remoteSessionRunningConflict(entries []serveSessionEntry, live map[string]bool) bool {
	listedPaths := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		listedPaths[entry.Path] = struct{}{}
		if running, ok := live[entry.Path]; entry.Running && ok && !running {
			return true
		}
	}
	for path, running := range live {
		if _, listed := listedPaths[path]; !running && !listed {
			return true
		}
	}
	return false
}

func remoteSessionRunning(listed bool, live map[string]bool, path string, preferLive bool) bool {
	if preferLive {
		if running, ok := live[path]; ok {
			// A raced false can mean a completed turn or remaining background jobs.
			// The bounded refresh resolves the ordinary case; after repeated races,
			// retain the conservative row rather than hiding active background work.
			if listed && !running {
				return true
			}
			return running
		}
	}
	return listed || live[path]
}

func remoteAuthoritativeSessionTitle(hostID, workspace string, current *serveSessionEntry) string {
	if current == nil {
		return ""
	}
	title := strings.TrimSpace(current.Title)
	if override := remoteSessionTitleOverride(hostID, workspace, current.Name); override != "" {
		title = override
	}
	if title == "" {
		title = remoteWorkspaceName(workspace)
	}
	return title
}
