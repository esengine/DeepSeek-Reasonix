//go:build linux

package service

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const serviceTestTimeout = 3 * time.Second

func newTestEndpoint(t *testing.T) (*Endpoint, string, string) {
	t.Helper()
	runtimeDir := t.TempDir()
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	unitPath := filepath.Join(t.TempDir(), UnitName)
	return NewEndpoint(Paths{RuntimeDir: runtimeDir, UnitPath: unitPath}), runtimeDir, unitPath
}

func TestDefaultEndpointRequiresXDGAndUsesOnlyFrozenPaths(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("TMPDIR", t.TempDir())
	if endpoint, err := DefaultEndpoint(); err == nil || endpoint != nil {
		t.Fatal("DefaultEndpoint used a fallback without XDG_RUNTIME_DIR")
	}

	runtimeDir := t.TempDir()
	configDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("XDG_CONFIG_HOME", configDir)
	endpoint, err := DefaultEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	wantSocket := filepath.Join(runtimeDir, SocketDirName, SocketFileName)
	if socket, err := endpoint.SocketPath(); err != nil || socket != wantSocket {
		t.Fatalf("socket path = %q, %v; want %q", socket, err, wantSocket)
	}
	wantUnit := filepath.Join(configDir, "systemd", "user", UnitName)
	if unit, err := endpoint.UnitPath(); err != nil || unit != wantUnit {
		t.Fatalf("unit path = %q, %v; want %q", unit, err, wantUnit)
	}
}

func TestInstalledRequiresOwnedRegularSafeUnit(t *testing.T) {
	endpoint, _, unitPath := newTestEndpoint(t)
	if installed, err := endpoint.Installed(context.Background()); err != nil || installed {
		t.Fatalf("missing unit installed=%v err=%v", installed, err)
	}
	if err := os.WriteFile(unitPath, []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if installed, err := endpoint.Installed(context.Background()); err != nil || !installed {
		t.Fatalf("regular unit installed=%v err=%v", installed, err)
	}
	if err := os.Chmod(unitPath, 0o666); err != nil {
		t.Fatal(err)
	}
	if installed, err := endpoint.Installed(context.Background()); err == nil || installed {
		t.Fatal("group/world-writable unit was accepted")
	}
	if err := os.Remove(unitPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(unitPath), "target.service")
	if err := os.WriteFile(target, []byte("target"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, unitPath); err != nil {
		t.Fatal(err)
	}
	if installed, err := endpoint.Installed(context.Background()); err == nil || installed {
		t.Fatal("symlink unit was accepted")
	}
}

func TestListenAndDialEnforceSecureCurrentUserUnixEndpoint(t *testing.T) {
	endpoint, _, _ := newTestEndpoint(t)
	listener, err := endpoint.Listen()
	if err != nil {
		t.Fatal(err)
	}
	socketPath, _ := endpoint.SocketPath()
	parentInfo, err := os.Lstat(filepath.Dir(socketPath))
	if err != nil {
		t.Fatal(err)
	}
	if !parentInfo.IsDir() || parentInfo.Mode().Perm() != 0o700 {
		t.Fatalf("socket parent mode = %v", parentInfo.Mode())
	}
	socketInfo, err := validateSocket(socketPath, uint32(os.Geteuid()), true)
	if err != nil {
		t.Fatal(err)
	}
	if socketInfo.Mode().Perm() != 0o600 || listener.Addr().Network() != "unix" {
		t.Fatalf("listener socket mode/network = %04o/%q", socketInfo.Mode().Perm(), listener.Addr().Network())
	}
	if second, err := endpoint.Listen(); !errors.Is(err, ErrAlreadyRunning) || second != nil {
		if second != nil {
			_ = second.Close()
		}
		t.Fatalf("second listener = %v, %v", second, err)
	}

	serverDone := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer connection.Close()
		buffer := make([]byte, 4)
		if _, err := io.ReadFull(connection, buffer); err != nil {
			serverDone <- err
			return
		}
		if string(buffer) != "ping" {
			serverDone <- errors.New("unexpected request: " + string(buffer))
			return
		}
		_, err = io.WriteString(connection, "pong")
		serverDone <- err
	}()
	ctx, cancel := context.WithTimeout(context.Background(), serviceTestTimeout)
	defer cancel()
	connection, err := endpoint.Dial(ctx)
	if err != nil {
		t.Fatal(err)
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		t.Fatalf("Dial returned %T", connection)
	}
	if peerUID, err := unixPeerUID(unixConnection); err != nil || peerUID != uint32(os.Geteuid()) {
		t.Fatalf("peer uid = %d, %v; want %d", peerUID, err, os.Geteuid())
	}
	if _, err := io.WriteString(connection, "ping"); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(connection, reply); err != nil || string(reply) != "pong" {
		t.Fatalf("reply = %q, %v", reply, err)
	}
	_ = connection.Close()
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(socketPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned socket remained after Close: %v", err)
	}
}

func TestListenSafelyReplacesOnlyStaleOwnedSocket(t *testing.T) {
	t.Run("stale", func(t *testing.T) {
		endpoint, _, _ := newTestEndpoint(t)
		path, _ := endpoint.SocketPath()
		if err := ensureListenerParent(filepath.Dir(path), uint32(os.Geteuid())); err != nil {
			t.Fatal(err)
		}
		stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		stale.SetUnlinkOnClose(false)
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := stale.Close(); err != nil {
			t.Fatal(err)
		}
		if connection, err := stale.AcceptUnix(); err == nil || connection != nil {
			if connection != nil {
				_ = connection.Close()
			}
			t.Fatal("closed stale listener still accepted a connection")
		}
		listener, err := endpoint.Listen()
		if err != nil {
			t.Fatal(err)
		}
		info, err := validateSocket(path, uint32(os.Geteuid()), true)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("replacement socket = %v, %v", info, err)
		}
		accepted := make(chan error, 1)
		go func() {
			connection, acceptErr := listener.Accept()
			if connection != nil {
				_ = connection.Close()
			}
			accepted <- acceptErr
		}()
		connection, err := net.DialTimeout("unix", path, serviceTestTimeout)
		if err != nil {
			t.Fatal(err)
		}
		_ = connection.Close()
		if err := <-accepted; err != nil {
			t.Fatal(err)
		}
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("replacement socket remained after Close: %v", err)
		}
	})

	t.Run("active", func(t *testing.T) {
		endpoint, _, _ := newTestEndpoint(t)
		path, _ := endpoint.SocketPath()
		if err := ensureListenerParent(filepath.Dir(path), uint32(os.Geteuid())); err != nil {
			t.Fatal(err)
		}
		active, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		active.SetUnlinkOnClose(false)
		defer func() {
			_ = active.Close()
			_ = os.Remove(path)
		}()
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
		before, _ := os.Lstat(path)
		listener, err := endpoint.Listen()
		if listener != nil || !errors.Is(err, ErrAlreadyRunning) {
			if listener != nil {
				_ = listener.Close()
			}
			t.Fatalf("active endpoint replacement = %v, %v", listener, err)
		}
		after, statErr := os.Lstat(path)
		if statErr != nil || !os.SameFile(before, after) {
			t.Fatalf("active socket was removed/replaced: %v", statErr)
		}
	})
}

func TestListenerCloseNeverRemovesAReplacementPath(t *testing.T) {
	endpoint, _, _ := newTestEndpoint(t)
	listener, err := endpoint.Listen()
	if err != nil {
		t.Fatal(err)
	}
	path, err := endpoint.SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	displaced := path + ".displaced"
	if err := os.Rename(path, displaced); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement must survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(path)
		_ = os.Remove(displaced)
	})

	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	value, err := os.ReadFile(path)
	if err != nil || string(value) != "replacement must survive" {
		t.Fatalf("listener Close removed or changed replacement: %q, %v", value, err)
	}
}

func TestListenRejectsSymlinkAndNonSocketWithoutRemovingThem(t *testing.T) {
	tests := []struct {
		name   string
		create func(t *testing.T, path string)
		check  func(os.FileInfo) bool
	}{
		{
			name: "regular file",
			create: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("do not remove"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			check: func(info os.FileInfo) bool { return info.Mode().IsRegular() },
		},
		{
			name: "symlink",
			create: func(t *testing.T, path string) {
				target := filepath.Join(filepath.Dir(path), "target")
				if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			},
			check: func(info os.FileInfo) bool { return info.Mode()&os.ModeSymlink != 0 },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			endpoint, _, _ := newTestEndpoint(t)
			path, _ := endpoint.SocketPath()
			if err := ensureListenerParent(filepath.Dir(path), uint32(os.Geteuid())); err != nil {
				t.Fatal(err)
			}
			test.create(t, path)
			if listener, err := endpoint.Listen(); err == nil || listener != nil {
				if listener != nil {
					_ = listener.Close()
				}
				t.Fatal("unsafe endpoint was accepted")
			}
			info, err := os.Lstat(path)
			if err != nil || !test.check(info) {
				t.Fatalf("unsafe endpoint was removed or changed: %v, %v", info, err)
			}
		})
	}
}

func TestDialRejectsUnsafePermissionsAndCancelledContext(t *testing.T) {
	endpoint, runtimeDir, _ := newTestEndpoint(t)
	if connection, err := endpoint.Dial(nil); err == nil || connection != nil {
		if connection != nil {
			_ = connection.Close()
		}
		t.Fatal("Dial with a nil context unexpectedly found a daemon")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if connection, err := endpoint.Dial(cancelled); !errors.Is(err, context.Canceled) || connection != nil {
		t.Fatalf("cancelled Dial = %v, %v", connection, err)
	}

	if err := os.Chmod(runtimeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if listener, err := endpoint.Listen(); err == nil || listener != nil {
		if listener != nil {
			_ = listener.Close()
		}
		t.Fatal("unsafe XDG_RUNTIME_DIR permissions were accepted")
	}
	if err := os.Chmod(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	listener, err := endpoint.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	path, _ := endpoint.SocketPath()
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if connection, err := endpoint.Dial(context.Background()); err == nil || connection != nil {
		if connection != nil {
			_ = connection.Close()
		}
		t.Fatal("unsafe socket parent permissions were accepted")
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o660); err != nil {
		t.Fatal(err)
	}
	if connection, err := endpoint.Dial(context.Background()); err == nil || connection != nil {
		if connection != nil {
			_ = connection.Close()
		}
		t.Fatal("unsafe socket permissions were accepted")
	}
}

type fakeOwnerInfo struct{ uid uint32 }

func (f fakeOwnerInfo) Name() string       { return "fake" }
func (f fakeOwnerInfo) Size() int64        { return 0 }
func (f fakeOwnerInfo) Mode() os.FileMode  { return 0o600 }
func (f fakeOwnerInfo) ModTime() time.Time { return time.Time{} }
func (f fakeOwnerInfo) IsDir() bool        { return false }
func (f fakeOwnerInfo) Sys() any           { return &syscall.Stat_t{Uid: f.uid} }

func TestOwnerValidationRejectsDifferentUID(t *testing.T) {
	current := uint32(os.Geteuid())
	other := current + 1
	if other == current {
		other++
	}
	if err := validateOwner(fakeOwnerInfo{uid: other}, current, "test path"); err == nil {
		t.Fatal("wrong owner was accepted")
	}
}

type fakeHostServer struct {
	serveErr   error
	started    chan struct{}
	closeCalls atomic.Int32
}

func (s *fakeHostServer) ServeListener(listener net.Listener) error {
	close(s.started)
	if s.serveErr != nil {
		return s.serveErr
	}
	_, err := listener.Accept()
	return err
}

func (s *fakeHostServer) Close() { s.closeCalls.Add(1) }

type closeWaitsForServeServer struct {
	started     chan struct{}
	serveExited chan struct{}
	closeCalls  atomic.Int32
}

func (s *closeWaitsForServeServer) ServeListener(listener net.Listener) error {
	close(s.started)
	_, err := listener.Accept()
	close(s.serveExited)
	return err
}

func (s *closeWaitsForServeServer) Close() {
	s.closeCalls.Add(1)
	<-s.serveExited
}

func TestRunServerCancelAndServeErrorCleanUpEndpoint(t *testing.T) {
	t.Run("cancel", func(t *testing.T) {
		endpoint, _, _ := newTestEndpoint(t)
		server := &fakeHostServer{started: make(chan struct{})}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- RunServer(ctx, endpoint, server) }()
		select {
		case <-server.started:
		case <-time.After(serviceTestTimeout):
			t.Fatal("server did not start")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(serviceTestTimeout):
			t.Fatal("RunServer did not stop after cancellation")
		}
		if server.closeCalls.Load() != 1 {
			t.Fatalf("server Close calls = %d", server.closeCalls.Load())
		}
		path, _ := endpoint.SocketPath()
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("socket remained after cancel: %v", err)
		}
	})

	t.Run("serve error", func(t *testing.T) {
		endpoint, _, _ := newTestEndpoint(t)
		serveErr := errors.New("accept failed")
		server := &fakeHostServer{serveErr: serveErr, started: make(chan struct{})}
		err := RunServer(context.Background(), endpoint, server)
		if !errors.Is(err, serveErr) {
			t.Fatalf("RunServer error = %v", err)
		}
		if server.closeCalls.Load() != 1 {
			t.Fatalf("server Close calls = %d", server.closeCalls.Load())
		}
		path, _ := endpoint.SocketPath()
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("socket remained after serve error: %v", err)
		}
	})

	t.Run("listener closes before server", func(t *testing.T) {
		endpoint, _, _ := newTestEndpoint(t)
		server := &closeWaitsForServeServer{
			started: make(chan struct{}), serveExited: make(chan struct{}),
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- RunServer(ctx, endpoint, server) }()
		select {
		case <-server.started:
		case <-time.After(serviceTestTimeout):
			t.Fatal("server did not start")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(serviceTestTimeout):
			t.Fatal("RunServer deadlocked when HostServer.Close waited for ServeListener")
		}
		if server.closeCalls.Load() != 1 {
			t.Fatalf("server Close calls = %d", server.closeCalls.Load())
		}
	})
}

func TestRunServerRejectsCancelledContextAndTypedNilServerBeforeBinding(t *testing.T) {
	endpoint, _, _ := newTestEndpoint(t)
	path, err := endpoint.SocketPath()
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	server := &fakeHostServer{started: make(chan struct{})}
	if err := RunServer(cancelled, endpoint, server); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-cancelled RunServer error = %v", err)
	}
	if _, err := os.Lstat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pre-cancelled RunServer bound endpoint: %v", err)
	}

	var nilServer *fakeHostServer
	if err := RunServer(context.Background(), endpoint, nilServer); err == nil {
		t.Fatal("typed-nil HostServer was accepted")
	}
	if _, err := os.Lstat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("typed-nil RunServer bound endpoint: %v", err)
	}
}

func TestSocketPathNeverAcceptsRelativeRuntimeDir(t *testing.T) {
	endpoint := NewEndpoint(Paths{RuntimeDir: "relative", UnitPath: "/unit"})
	if path, err := endpoint.SocketPath(); err == nil || strings.Contains(path, "/tmp") {
		t.Fatalf("relative runtime path = %q, %v", path, err)
	}
}
