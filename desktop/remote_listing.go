package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	MtimeMilli int64  `json:"mtimeMilli"`
}

// RemoteSessionView mirrors one serve /sessions entry on the frontend side.
type RemoteSessionView struct {
	Name           string `json:"name"`
	Title          string `json:"title,omitempty"`
	Turns          int    `json:"turns,omitempty"`
	Current        bool   `json:"current,omitempty"`
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
	if body == nil {
		body = []byte("{}")
	}
	resp, err := serveDo(ctx, client, http.MethodPost, url, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	if message := strings.TrimSpace(string(data)); message != "" {
		return fmt.Errorf("%s: status %d: %s", url, resp.StatusCode, message)
	}
	return fmt.Errorf("%s: status %d", url, resp.StatusCode)
}

// serveDo issues a JSON request; the csrf guard rejects non-JSON POSTs.
func serveDo(ctx context.Context, client *http.Client, method, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
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

// serveClientForRef resolves an HTTP client for a host+workspace WITHOUT
// waking anything: a one-shot handshake against an already-ready serve
// registration. A serve that is not running reports an error — query paths
// must never cold-start one.
func (a *App) serveClientForRef(hostID, workspace string) (*http.Client, string, func(), error) {
	a.remoteTabMu.Lock()
	for _, tab := range a.remoteTabs {
		if tab.ref.HostID == hostID && tab.ref.Workspace == workspace && tab.client != nil {
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
	entries, err := serveSessions(ctx, client, base)
	if err != nil {
		return nil, err
	}
	out := make([]RemoteSessionView, 0, len(entries))
	pinned := make([]RemoteSessionView, 0, len(entries))
	hasCurrent := false
	for _, e := range entries {
		title := strings.TrimSpace(e.Title)
		if override := remoteSessionTitleOverride(hostID, workspace, e.Name); override != "" {
			title = override
		}
		view := RemoteSessionView{
			Name:           e.Name,
			Title:          title,
			Turns:          e.Turns,
			Current:        e.Current,
			LastActivityAt: e.MtimeMilli,
			Pinned:         remoteSessionPinned(hostID, workspace, e.Name),
		}
		hasCurrent = hasCurrent || e.Current
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
				blank = &RemoteSessionView{Name: "", Title: tab.topicTitle, Current: true, LastActivityAt: time.Now().UnixMilli()}
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
