// Package service adapts the platform-neutral Remote daemon and attach paths to
// a user-owned local IPC endpoint. Remote Host core never depends on systemd or
// Unix sockets directly.
package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sync"

	"reasonix/internal/nilutil"
)

const (
	UnitName       = "reasonix-remote.service"
	SocketDirName  = "reasonix"
	SocketFileName = "remote.sock"
)

var (
	ErrUnsupportedPlatform = errors.New("Reasonix Remote Host is unsupported on this platform")
	ErrAlreadyRunning      = errors.New("Reasonix Remote daemon is already running")
)

type Paths struct {
	RuntimeDir string
	UnitPath   string
}

// Endpoint fixes Remote V1 to one user service and one local IPC path. Tests
// may inject roots, but callers cannot select a TCP address or alternate socket.
type Endpoint struct{ paths Paths }

func NewEndpoint(paths Paths) *Endpoint {
	paths.RuntimeDir = filepath.Clean(paths.RuntimeDir)
	paths.UnitPath = filepath.Clean(paths.UnitPath)
	return &Endpoint{paths: paths}
}

func (e *Endpoint) SocketPath() (string, error) {
	if e == nil || e.paths.RuntimeDir == "" || e.paths.RuntimeDir == "." || !filepath.IsAbs(e.paths.RuntimeDir) {
		return "", errors.New("XDG_RUNTIME_DIR must be a non-empty absolute path")
	}
	return filepath.Join(e.paths.RuntimeDir, SocketDirName, SocketFileName), nil
}

func (e *Endpoint) UnitPath() (string, error) {
	if e == nil || e.paths.UnitPath == "" || e.paths.UnitPath == "." || !filepath.IsAbs(e.paths.UnitPath) {
		return "", errors.New("Remote systemd user unit path must be absolute")
	}
	return e.paths.UnitPath, nil
}

type HostServer interface {
	ServeListener(net.Listener) error
	Close()
}

// RunServer binds the platform endpoint and drives the real daemon Server. It
// contains no ControllerFactory fallback: callers must provide a fully
// assembled production HostServer.
func RunServer(ctx context.Context, endpoint *Endpoint, server HostServer) error {
	if endpoint == nil || nilutil.IsNil(server) {
		return errors.New("Remote service endpoint and Host server are required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	listener, err := endpoint.Listen()
	if err != nil {
		return err
	}
	defer listener.Close()

	watchDone := make(chan struct{})
	watcherExited := make(chan struct{})
	var closeServerOnce sync.Once
	closeServer := func() { closeServerOnce.Do(server.Close) }
	go func() {
		defer close(watcherExited)
		select {
		case <-ctx.Done():
			// Closing the listener first guarantees that even a HostServer whose
			// Close waits for ServeListener can make progress. The production
			// daemon also closes registered listeners itself, so both calls are
			// deliberately idempotent.
			_ = listener.Close()
			closeServer()
		case <-watchDone:
		}
	}()
	err = server.ServeListener(listener)
	close(watchDone)
	closeServer()
	<-watcherExited
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return fmt.Errorf("serve Remote endpoint: %w", err)
	}
	return nil
}
