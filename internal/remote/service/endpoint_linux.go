//go:build linux

package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const listenerLockName = ".remote-listener.lock"

func DefaultEndpoint() (*Endpoint, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return nil, errors.New("XDG_RUNTIME_DIR is required for Reasonix Remote")
	}
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return nil, errors.New("cannot resolve the systemd user unit directory")
		}
		configDir = filepath.Join(home, ".config")
	}
	endpoint := NewEndpoint(Paths{
		RuntimeDir: runtimeDir,
		UnitPath:   filepath.Join(configDir, "systemd", "user", UnitName),
	})
	if _, err := endpoint.SocketPath(); err != nil {
		return nil, err
	}
	if _, err := endpoint.UnitPath(); err != nil {
		return nil, err
	}
	return endpoint, nil
}

func (e *Endpoint) Installed(ctx context.Context) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return false, err
	}
	path, err := e.UnitPath()
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return false, errors.New("Remote systemd user unit is not a regular file")
	}
	if err := validateOwner(info, uint32(os.Geteuid()), "Remote systemd user unit"); err != nil {
		return false, err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return false, errors.New("Remote systemd user unit is group/world writable")
	}
	return true, nil
}

func (e *Endpoint) Dial(ctx context.Context) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	path, err := e.SocketPath()
	if err != nil {
		return nil, err
	}
	uid := uint32(os.Geteuid())
	if err := validateRuntimeRoot(e.paths.RuntimeDir, uid); err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	if err := validateOwnedDirectory(parent, uid, 0o700); err != nil {
		return nil, err
	}
	before, err := validateSocket(path, uid, true)
	if err != nil {
		return nil, err
	}

	dialer := net.Dialer{}
	connection, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (net.Conn, error) {
		_ = connection.Close()
		return nil, err
	}
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return fail(errors.New("Remote endpoint did not return a Unix connection"))
	}
	peerUID, err := unixPeerUID(unixConnection)
	if err != nil {
		return fail(err)
	}
	if peerUID != uid {
		return fail(fmt.Errorf("Remote daemon peer uid %d does not match current uid %d", peerUID, uid))
	}
	after, err := validateSocket(path, uid, true)
	if err != nil {
		return fail(err)
	}
	if !os.SameFile(before, after) {
		return fail(errors.New("Remote socket changed while connecting"))
	}
	return connection, nil
}

func (e *Endpoint) Listen() (net.Listener, error) {
	path, err := e.SocketPath()
	if err != nil {
		return nil, err
	}
	uid := uint32(os.Geteuid())
	if err := validateRuntimeRoot(e.paths.RuntimeDir, uid); err != nil {
		return nil, err
	}
	parent := filepath.Dir(path)
	if err := ensureListenerParent(parent, uid); err != nil {
		return nil, err
	}
	lockFile, err := acquireListenerLock(filepath.Join(parent, listenerLockName), uid)
	if err != nil {
		return nil, err
	}
	releaseLock := true
	defer func() {
		if releaseLock {
			releaseListenerLock(lockFile)
		}
	}()

	if err := removeStaleSocket(path, uid); err != nil {
		return nil, err
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	listener.SetUnlinkOnClose(false)
	cleanup := true
	defer func() {
		if cleanup {
			_ = listener.Close()
			_ = os.Remove(path)
		}
	}()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	ownedInfo, err := validateSocket(path, uid, true)
	if err != nil {
		return nil, err
	}

	releaseLock = false
	cleanup = false
	return &ownedUnixListener{UnixListener: listener, path: path, ownedInfo: ownedInfo, lockFile: lockFile}, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func validateRuntimeRoot(path string, uid uint32) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("XDG_RUNTIME_DIR: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("XDG_RUNTIME_DIR is not a real directory")
	}
	if err := validateOwner(info, uid, "XDG_RUNTIME_DIR"); err != nil {
		return err
	}
	if info.Mode().Perm() != 0o700 {
		return fmt.Errorf("XDG_RUNTIME_DIR permissions are %04o, want 0700", info.Mode().Perm())
	}
	return nil
}

func ensureListenerParent(path string, uid uint32) error {
	err := os.Mkdir(path, 0o700)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Remote socket parent is not a real directory")
	}
	if err := validateOwner(info, uid, "Remote socket parent"); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return err
	}
	return validateOwnedDirectory(path, uid, 0o700)
}

func validateOwnedDirectory(path string, uid uint32, permission os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Remote socket parent is not a real directory")
	}
	if err := validateOwner(info, uid, "Remote socket parent"); err != nil {
		return err
	}
	if info.Mode().Perm() != permission {
		return fmt.Errorf("Remote socket parent permissions are %04o, want %04o", info.Mode().Perm(), permission)
	}
	return nil
}

func validateSocket(path string, uid uint32, requirePermission bool) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Type() != os.ModeSocket {
		return nil, errors.New("Remote endpoint is not a Unix socket")
	}
	if err := validateOwner(info, uid, "Remote socket"); err != nil {
		return nil, err
	}
	if requirePermission && info.Mode().Perm() != 0o600 {
		return nil, fmt.Errorf("Remote socket permissions are %04o, want 0600", info.Mode().Perm())
	}
	return info, nil
}

func validateOwner(info os.FileInfo, uid uint32, label string) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%s owner is unavailable", label)
	}
	if stat.Uid != uid {
		return fmt.Errorf("%s uid %d does not match current uid %d", label, stat.Uid, uid)
	}
	return nil
}

func acquireListenerLock(path string, uid uint32) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	fail := func(err error) (*os.File, error) {
		_ = file.Close()
		return nil, err
	}
	if err := unix.Fchmod(fd, 0o600); err != nil {
		return fail(err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fail(err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uid {
		return fail(errors.New("Remote listener lock is not a current-user regular file"))
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return fail(ErrAlreadyRunning)
		}
		return fail(err)
	}
	return file, nil
}

func releaseListenerLock(file *os.File) {
	if file == nil {
		return
	}
	_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
	_ = file.Close()
}

func removeStaleSocket(path string, uid uint32) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := validateSocket(path, uid, false); err != nil {
		return err
	}
	connection, dialErr := net.DialTimeout("unix", path, 100*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return ErrAlreadyRunning
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, syscall.ENOENT) {
		return fmt.Errorf("probe existing Remote socket: %w", dialErr)
	}
	current, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !os.SameFile(info, current) {
		return errors.New("Remote socket changed during stale-socket check")
	}
	return os.Remove(path)
}

func unixPeerUID(connection *net.UnixConn) (uint32, error) {
	raw, err := connection.SyscallConn()
	if err != nil {
		return 0, err
	}
	var credential *unix.Ucred
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		credential, controlErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if controlErr != nil {
		return 0, controlErr
	}
	if credential == nil {
		return 0, errors.New("Remote daemon peer credentials are unavailable")
	}
	return credential.Uid, nil
}

type ownedUnixListener struct {
	*net.UnixListener
	path      string
	ownedInfo os.FileInfo
	lockFile  *os.File
	closeOnce sync.Once
	closeErr  error
}

func (l *ownedUnixListener) Close() error {
	l.closeOnce.Do(func() {
		l.closeErr = l.UnixListener.Close()
		if current, err := os.Lstat(l.path); err == nil && os.SameFile(l.ownedInfo, current) {
			if removeErr := os.Remove(l.path); l.closeErr == nil && removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				l.closeErr = removeErr
			}
		}
		releaseListenerLock(l.lockFile)
	})
	return l.closeErr
}
