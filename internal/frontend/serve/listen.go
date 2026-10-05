// Listening and shutdown for both hosts: one server, or a hub serving several
// runtimes behind the same listener.
package serve

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Run serves until the process is killed. Interactive approval is enabled so
// "ask" decisions surface as approval_request events answered via POST /approve.
func (s *Server) Run(addr string) error {
	s.ctl().EnableInteractiveApproval()
	s.auth.hosts.ListenOn(addr)
	return http.ListenAndServe(addr, s.Handler())
}

// RunGraceful serves with graceful shutdown. It listens for SIGINT/SIGTERM on
// the provided context and drains active connections for up to 10 seconds
// before returning.
func (s *Server) RunGraceful(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.RunGracefulListener(ctx, ln)
}

// RunGracefulListener is RunGraceful over a caller-supplied listener. Callers
// that need the real bound address (e.g. --addr 127.0.0.1:0 with --port-file)
// listen first, record ln.Addr(), then hand the listener here.
func (s *Server) RunGracefulListener(ctx context.Context, ln net.Listener) error {
	s.ctl().EnableInteractiveApproval()
	s.auth.hosts.ListenOn(ln.Addr().String())
	return runGracefulListener(ctx, ln, s.Handler())
}

// RunGracefulHandler is RunGracefulListener for a host that serves something
// other than a hub's own handler — a boundary wrapped around it, say — and so
// must not reimplement the drain in order to put one there.
func RunGracefulHandler(ctx context.Context, ln net.Listener, handler http.Handler) error {
	return runGracefulListener(ctx, ln, handler)
}

// runGracefulListener serves handler until ctx ends, then drains for up to ten
// seconds. Shared with the hub, which serves several runtimes behind one
// listener and must shut them down the same way.
func runGracefulListener(ctx context.Context, ln net.Listener, handler http.Handler) error {
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(ln)
	}()
	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		slog.Info("serve: shutting down gracefully")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("serve: graceful shutdown failed", "err", err)
		}
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
