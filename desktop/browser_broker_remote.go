package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"

	"reasonix/internal/remote/bootstrap"
)

// serveCapabilityBrowser is the token a serve advertises on its token
// handshake when it was started with a browser broker (and accepts
// POST /browser/broker rebinds). Older serves omit the header entirely.
const serveCapabilityBrowser = "browser"

// browserBrokerCallback is the bootstrap BrowserBroker hook for one ensure
// round: it mints this connection generation's token and opens the reverse
// tunnel only when the desktop shell can host browser surfaces. A desktop
// without host mode returns nil, launching the serve without a broker.
func (m *desktopRemoteManager) browserBrokerCallback(c desktopSSHClient, hostID string, gen *managedHost) func(context.Context) (*bootstrap.BrowserBrokerOptions, error) {
	return func(ctx context.Context) (*bootstrap.BrowserBrokerOptions, error) {
		app, ok := m.sink.(*App)
		if !ok || app == nil || !app.hostMode() {
			return nil, nil
		}
		if !m.isCurrent(hostID, gen) {
			return nil, fmt.Errorf("browser broker: host %q connection was replaced", hostID)
		}
		token, port, err := app.registerBrowserBrokerRoute(hostID, gen)
		if err != nil {
			return nil, err
		}
		remotePort, err := ensureBrowserBrokerForward(c, hostID, port)
		if err != nil {
			app.revokeBrowserBrokerRoutes(hostID)
			return nil, fmt.Errorf("browser broker: reverse tunnel: %w", err)
		}
		return &bootstrap.BrowserBrokerOptions{
			BaseURL: fmt.Sprintf("http://127.0.0.1:%d", remotePort),
			Token:   token,
		}, nil
	}
}

// rebindBrowserBroker re-points a REUSED serve at the new connection
// generation's broker: the process environment still carries the dead
// generation's endpoint and token, so the desktop rotates the route and posts
// the fresh pair. Serves without the browser capability keep every existing
// feature untouched.
func (m *desktopRemoteManager) rebindBrowserBroker(ctx context.Context, c desktopSSHClient, mh *managedHost, hostID string, view RemoteServerView, token string) error {
	app, ok := m.sink.(*App)
	if !ok || app == nil || !app.hostMode() {
		return nil
	}
	if strings.TrimSpace(view.LocalURL) == "" || token == "" {
		return nil
	}
	if !m.isCurrent(hostID, mh) {
		return fmt.Errorf("browser broker: host %q connection was replaced", hostID)
	}
	client, err := newServeHTTPClient(view.LocalURL)
	if err != nil {
		return err
	}
	caps, err := serveHandshakeCapabilities(ctx, client, view.LocalURL, token)
	if err != nil {
		return fmt.Errorf("browser broker: handshake: %w", err)
	}
	if !slices.Contains(caps, serveCapabilityBrowser) {
		return nil
	}
	brokerToken, port, err := app.registerBrowserBrokerRoute(hostID, mh)
	if err != nil {
		return err
	}
	remotePort, err := ensureBrowserBrokerForward(c, hostID, port)
	if err != nil {
		return fmt.Errorf("browser broker: reverse tunnel: %w", err)
	}
	body, err := json.Marshal(map[string]string{
		"endpoint": fmt.Sprintf("http://127.0.0.1:%d", remotePort),
		"token":    brokerToken,
	})
	if err != nil {
		return err
	}
	if err := servePost(ctx, client, serveURL(view.LocalURL, "/browser/broker"), body); err != nil {
		return fmt.Errorf("browser broker: rebind: %w", err)
	}
	log.Printf("[remote] browser broker rebound host=%s url=%s", hostID, view.LocalURL)
	return nil
}

// rebindBrowserBrokerBestEffort runs the rebind without failing the ensure
// round: browser access is additive, and a serve whose rebind failed reports
// itself unavailable through the broker health check instead.
func (m *desktopRemoteManager) rebindBrowserBrokerBestEffort(ctx context.Context, c desktopSSHClient, mh *managedHost, hostID string, view RemoteServerView, token string) {
	if err := m.rebindBrowserBroker(ctx, c, mh, hostID, view, token); err != nil {
		log.Printf("[remote] browser broker rebind FAILED host=%s err=%v", hostID, err)
	}
}

// revokeBrowserBroker drops the host's broker route and reverse tunnel when
// its serve is stopped or the connection goes away.
func (m *desktopRemoteManager) revokeBrowserBroker(hostID string) {
	if app, ok := m.sink.(*App); ok && app != nil {
		app.revokeBrowserBrokerRoutes(hostID)
	}
}
