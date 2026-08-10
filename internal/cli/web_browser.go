package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reasonix/internal/serve"
)

// runServeListenerAfterReady opens the browser only after HTTP responds.
func runServeListenerAfterReady(ctx context.Context, srv *serve.Server, ln net.Listener, addr string, onReady func()) error {
	serveCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.RunGracefulListener(serveCtx, ln) }()
	if err := waitForServeHTTPReady(serveCtx, addr); err != nil {
		cancel()
		if serveErr := <-done; serveErr != nil {
			return fmt.Errorf("wait for Web server readiness: %w (server: %w)", err, serveErr)
		}
		return fmt.Errorf("wait for Web server readiness: %w", err)
	}
	if onReady != nil {
		onReady()
	}
	return <-done
}

func waitForServeHTTPReady(ctx context.Context, addr string) error {
	readyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(readyCtx, http.MethodGet, "http://"+browserHostPort(addr)+"/", nil)
	if err != nil {
		return err
	}
	req.Close = true
	client := &http.Client{
		Transport:     &http.Transport{DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

// webBrowserURL maps wildcard binds to loopback and keeps tokens in fragments.
func webBrowserURL(srv *serve.Server, addr, sessionID string) string {
	base := "http://" + browserHostPort(addr)
	entryPath := "/"
	if sessionID != "" {
		entryPath = "/sessions/" + url.PathEscape(sessionID)
	}
	switch srv.AuthMode() {
	case "token":
		return base + entryPath + "#token=" + url.QueryEscape(srv.AuthToken())
	case "password":
		if sessionID != "" {
			return base + entryPath
		}
		return base + "/login"
	default:
		return base + entryPath
	}
}

func launchWebBrowser(srv *serve.Server, addr, sessionID string) (string, error) {
	browserURL := webBrowserURL(srv, addr, sessionID)
	return browserURL, openBrowserURL(browserURL)
}

func browserHostPort(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}
