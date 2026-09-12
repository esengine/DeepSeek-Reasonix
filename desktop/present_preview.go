package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

// workspacePreviewOrigin owns the unprivileged loopback resource origin used
// by deliverables opened in the built-in browser. Access is limited by the
// random, short-lived media capability embedded in each URL.
type workspacePreviewOrigin struct {
	mu       sync.Mutex
	origin   string
	listener net.Listener
	server   *http.Server
}

func newWorkspacePreviewOrigin() *workspacePreviewOrigin { return &workspacePreviewOrigin{} }

func (a *App) ensureWorkspacePreviewOrigin() (string, error) {
	a.mu.Lock()
	if a.presentPreview == nil {
		a.presentPreview = newWorkspacePreviewOrigin()
	}
	p := a.presentPreview
	a.mu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.server != nil {
		return p.origin, nil
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	handler := a.workspaceMediaMiddleware()(http.NotFoundHandler())
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	p.listener = listener
	p.server = server
	p.origin = "http://" + listener.Addr().String()
	go func() {
		if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			slog.Warn("desktop: workspace preview origin stopped", "err", serveErr)
		}
	}()
	return p.origin, nil
}

func (a *App) stopWorkspacePreviewOrigin() {
	if a == nil {
		return
	}
	a.mu.Lock()
	p := a.presentPreview
	a.mu.Unlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	server := p.server
	p.server = nil
	p.listener = nil
	p.origin = ""
	p.mu.Unlock()
	if server == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}
