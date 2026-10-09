package bootstrap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"reasonix/internal/platform/remote/sftpfs"
)

// adoptPublished finds a live serve that published this workspace's port and
// pid files without leaving a record — started by hand or by a client that
// never wrote one — and records it, so it is attached rather than raced. found
// is false when nothing live is published. A live serve that cannot be driven
// from here is an error: its files are not this connect's to clear.
func adoptPublished(ctx context.Context, conn Conn, target remoteOS, fs *sftpfs.FS, paths StatePaths, workspace, minVersion string, held bool, clock func() time.Time) (ServeState, bool, error) {
	pid, err := readPIDFile(ctx, fs, paths.PidFile)
	if err != nil || !pidIsServe(ctx, conn, target, pid, paths) {
		return ServeState{}, false, nil
	}
	if !held {
		lock, err := acquireServeLock(ctx, fs, paths, clock)
		if err != nil {
			return ServeState{}, false, nil
		}
		defer lock.release()
		if st, err := readState(ctx, fs, paths.StateJSON); err == nil && recordIsLive(ctx, conn, target, paths, st) {
			return st, true, nil
		}
	}
	data, _, _, err := fs.ReadFile(ctx, paths.PortFile, 128)
	addr := strings.TrimSpace(string(data))
	if err != nil || !validServeAddr(addr) {
		return ServeState{}, false, fmt.Errorf("%w: pid %d published no usable address", ErrServeNotAttachable, pid)
	}
	if _, err := readToken(ctx, fs, paths.TokenFile); err != nil {
		return ServeState{}, false, fmt.Errorf("%w: pid %d has no readable token file", ErrServeNotAttachable, pid)
	}
	// Asked of the process itself, before the token goes anywhere near it: the
	// token is a bearer, and this serve was not started by this connect.
	version := probeServeVersion(ctx, conn, target, pid)
	if !meetsMinVersion(version, minVersion) {
		return ServeState{}, false, &KernelTooOldError{Found: version, Need: minVersion}
	}
	st := ServeState{
		PID: pid, Addr: addr, Workspace: workspace, Version: version,
		TokenFile: paths.TokenFile, StartedAt: nowUnix(clock),
	}
	if data, err := MarshalState(st); err == nil {
		_ = fs.WriteFileAtomic(ctx, paths.StateJSON, data, 0o600)
	}
	return st, true, nil
}

// clearDeadEndpoint removes the port and pid files a serve that no longer runs
// left behind, so the next launch's port file is its own. A live holder is
// never cleared: whoever started it published those files, and deleting them
// is how two serves came to share one workspace.
func clearDeadEndpoint(ctx context.Context, conn Conn, target remoteOS, fs *sftpfs.FS, paths StatePaths) error {
	if pid, err := readPIDFile(ctx, fs, paths.PidFile); err == nil && pidIsServe(ctx, conn, target, pid, paths) {
		return fmt.Errorf("%w: pid %d is still running", ErrServeNotAttachable, pid)
	}
	_ = fs.Remove(ctx, paths.PortFile, false)
	_ = fs.Remove(ctx, paths.PidFile, false)
	return nil
}

func recordIsLive(ctx context.Context, conn Conn, target remoteOS, paths StatePaths, st ServeState) bool {
	return st.PID > 0 && validServeAddr(st.Addr) && pidIsServe(ctx, conn, target, st.PID, paths)
}

// probeServeVersion is empty when the binary cannot be named or run, which
// meetsMinVersion treats as unknown rather than old.
func probeServeVersion(ctx context.Context, conn Conn, target remoteOS, pid int) string {
	res, err := conn.Exec(ctx, target.ServeVersion(pid))
	if err != nil {
		return ""
	}
	version, _ := ParseVersion(string(res.Stdout))
	return version
}
