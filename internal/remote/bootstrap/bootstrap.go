// Package bootstrap starts and manages a detached `reasonix serve` process on
// a remote host over an established SSH connection. It detects the remote
// OS/arch, locates or installs reasonix, launches serve bound to a random
// loopback port with a file-based token (never in argv), and records the
// result under the remote ~/.reasonix/remote so a later reconnect can reuse
// it. V1 targets Linux and macOS remotes.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"reasonix/internal/remote"
	"reasonix/internal/remote/sftpfs"
)

// Conn is the subset of *remote.Client bootstrap needs. *remote.Client
// satisfies it directly; tests inject a fake. bootstrap depends on remote
// (never the reverse), so using remote.ExecResult here introduces no cycle.
type Conn interface {
	Exec(ctx context.Context, cmd string) (remote.ExecResult, error)
	SFTP() (*sftpfs.FS, error)
}

// Install strategies.
const (
	InstallAuto   = "auto"
	InstallNPM    = "npm"
	InstallUpload = "upload"
	InstallNever  = "never"
)

// MinServeVersion is retained for display/informational use only. Usability is
// decided by probing `serve --help` for the --port-file flag (see locate), not
// by a version number: --port-file/--token-file ship in this change, so no
// released version satisfies a numeric gate, and the release number this change
// lands in is unknown at authoring time.
const MinServeVersion = "flag:port-file"

// Options configures EnsureServe.
type Options struct {
	Workspace   string                    // remote workspace path (may start with ~)
	Install     string                    // auto|npm|upload|never
	LocalBinary string                    // path to the running reasonix binary, for same-platform upload
	LocalGOOS   string                    // GOOS of LocalBinary
	LocalGOARCH string                    // GOARCH of LocalBinary
	MinVersion  string                    // minimum acceptable remote version
	Progress    func(step, detail string) // optional progress callback
	Clock       func() time.Time          // nil => time.Now
}

func (o Options) progress(step, detail string) {
	if o.Progress != nil {
		o.Progress(step, detail)
	}
}

func (o Options) clock() func() time.Time {
	if o.Clock != nil {
		return o.Clock
	}
	return time.Now
}

// Result is the outcome of EnsureServe.
type Result struct {
	State  ServeState
	Token  string // the pre-shared auth token (read from or written to TokenFile)
	Reused bool   // true when an already-running serve was reused
}

// EnsureServe returns a running serve for (host, workspace), starting one if
// needed. It is also the reconnect path: an existing live process is reused.
func EnsureServe(ctx context.Context, conn Conn, opts Options) (Result, error) {
	fs, err := conn.SFTP()
	if err != nil {
		return Result{}, err
	}
	home, err := fs.RealPath(ctx, "~")
	if err != nil {
		return Result{}, fmt.Errorf("bootstrap: resolve remote home: %w", err)
	}
	workspace, err := resolveWorkspace(ctx, fs, opts.Workspace, home)
	if err != nil {
		return Result{}, err
	}
	paths := pathsFor(home, workspace)

	// 1. Reuse a live process if the recorded pid is still running.
	if st, tok, ok := tryReuse(ctx, conn, fs, paths, workspace); ok {
		opts.progress("reuse", st.Addr)
		return Result{State: st, Token: tok, Reused: true}, nil
	}

	// 2. Detect remote platform.
	opts.progress("detect", "")
	unameRes, err := conn.Exec(ctx, "uname -sm")
	if err != nil {
		return Result{}, fmt.Errorf("bootstrap: uname: %w", err)
	}
	goos, goarch, err := ParseUname(string(unameRes.Stdout))
	if err != nil {
		return Result{}, err
	}

	// 3. Locate or install a usable reasonix.
	bin, version, err := ensureBinary(ctx, conn, fs, opts, home, goos, goarch, paths)
	if err != nil {
		return Result{}, err
	}

	// 4. Serialize only the short launch/publish section across every client.
	// Another caller may have completed while this one was locating/installing,
	// so re-check state after acquiring the remote lock.
	opts.progress("waiting_lock", "")
	lock, err := acquireServeLock(ctx, fs, paths, opts.clock())
	if err != nil {
		return Result{}, err
	}
	defer lock.release()
	if st, tok, ok := tryReuse(ctx, conn, fs, paths, workspace); ok {
		opts.progress("reuse", st.Addr)
		return Result{State: st, Token: tok, Reused: true}, nil
	}

	// 5. Generate token, write it 0600, and launch detached serve.
	token, err := generateToken()
	if err != nil {
		return Result{}, err
	}
	if err := fs.MkdirAll(ctx, paths.Dir); err != nil {
		return Result{}, err
	}
	if err := fs.WriteFileAtomic(ctx, paths.TokenFile, []byte(token+"\n"), 0o600); err != nil {
		return Result{}, fmt.Errorf("bootstrap: write token: %w", err)
	}
	opts.progress("launch", "")
	launchRes, err := conn.Exec(ctx, LaunchCommand(bin, workspace, paths))
	if err != nil {
		cleanupFailedLaunch(conn, fs, paths, 0)
		return Result{}, fmt.Errorf("bootstrap: launch: %w", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(launchRes.Stdout)))

	// 6. Poll the newly-created port file for the real bound address. The launch
	// command removes stale port/pid files before forking.
	opts.progress("health_check", "")
	addr, err := pollPortFile(ctx, fs, paths.PortFile, opts.clock())
	if err != nil {
		cleanupFailedLaunch(conn, fs, paths, pid)
		return Result{}, err
	}
	if filePID, perr := readPIDFile(ctx, fs, paths.PidFile); perr == nil {
		pid = filePID // --pid-file is authoritative when available.
	}
	if pid <= 0 || !pidIsServe(ctx, conn, pid, paths) {
		cleanupFailedLaunch(conn, fs, paths, pid)
		return Result{}, errors.New("bootstrap: launched process did not become the expected reasonix serve")
	}

	st := ServeState{
		PID:       pid,
		Addr:      addr,
		Workspace: workspace,
		Version:   version,
		TokenFile: paths.TokenFile,
		LogFile:   paths.LogFile,
		StartedAt: nowUnix(opts.clock()),
	}
	data, err := MarshalState(st)
	if err != nil {
		cleanupFailedLaunch(conn, fs, paths, pid)
		return Result{}, err
	}
	if err := fs.WriteFileAtomic(ctx, paths.StateJSON, data, 0o600); err != nil {
		cleanupFailedLaunch(conn, fs, paths, pid)
		return Result{}, fmt.Errorf("bootstrap: write state: %w", err)
	}
	opts.progress("ready", addr)
	return Result{State: st, Token: token}, nil
}

// Status reads the recorded state and reports whether the process is alive.
func Status(ctx context.Context, conn Conn, workspace string) (ServeState, bool, error) {
	fs, err := conn.SFTP()
	if err != nil {
		return ServeState{}, false, err
	}
	home, err := fs.RealPath(ctx, "~")
	if err != nil {
		return ServeState{}, false, err
	}
	ws, err := resolveWorkspace(ctx, fs, workspace, home)
	if err != nil {
		return ServeState{}, false, err
	}
	paths := pathsFor(home, ws)
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil {
		return ServeState{}, false, nil // no state => not running
	}
	alive := st.Workspace == ws && validServeAddr(st.Addr) && pidIsServe(ctx, conn, st.PID, paths)
	return st, alive, nil
}

// Stop terminates the recorded process and removes its state files.
func Stop(ctx context.Context, conn Conn, workspace string) error {
	fs, err := conn.SFTP()
	if err != nil {
		return err
	}
	home, err := fs.RealPath(ctx, "~")
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(ctx, fs, workspace, home)
	if err != nil {
		return err
	}
	paths := pathsFor(home, ws)
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil {
		return nil // nothing recorded
	}
	// Only signal the pid if it is still OUR serve: a recycled PID now owned by
	// an unrelated process must never be TERM/KILLed.
	if st.PID > 0 {
		if _, err := conn.Exec(ctx, StopCommand(st.PID, paths)); err != nil {
			return fmt.Errorf("bootstrap: stop pid %d: %w", st.PID, err)
		}
	}
	_ = fs.Remove(ctx, paths.StateJSON, false)
	_ = fs.Remove(ctx, paths.TokenFile, false)
	_ = fs.Remove(ctx, paths.PortFile, false)
	_ = fs.Remove(ctx, paths.PidFile, false)
	return nil
}

// Logs writes up to n tail lines of the serve log to w.
func Logs(ctx context.Context, conn Conn, workspace string, n int, w io.Writer) error {
	fs, err := conn.SFTP()
	if err != nil {
		return err
	}
	home, err := fs.RealPath(ctx, "~")
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(ctx, fs, workspace, home)
	if err != nil {
		return err
	}
	paths := pathsFor(home, ws)
	res, err := conn.Exec(ctx, LogsCommand(paths.LogFile, n))
	if err != nil {
		return err
	}
	_, err = w.Write(res.Stdout)
	return err
}

func tryReuse(ctx context.Context, conn Conn, fs *sftpfs.FS, paths StatePaths, workspace ...string) (ServeState, string, bool) {
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil || st.PID <= 0 || st.Addr == "" {
		return ServeState{}, "", false
	}
	if len(workspace) > 0 && st.Workspace != workspace[0] {
		return ServeState{}, "", false
	}
	if !validServeAddr(st.Addr) || !pidIsServe(ctx, conn, st.PID, paths) {
		return ServeState{}, "", false
	}
	// The state record is informational; the workspace-derived path is the
	// authority, so a tampered record cannot make us read an arbitrary file.
	tok, err := readToken(ctx, fs, paths.TokenFile)
	if err != nil {
		return ServeState{}, "", false
	}
	return st, tok, true
}

// pidIsServe reports whether pid is running AND is a reasonix serve process,
// so PID reuse cannot make an unrelated process look like a live serve.
func pidIsServe(ctx context.Context, conn Conn, pid int, paths StatePaths) bool {
	if pid <= 0 {
		return false
	}
	res, err := conn.Exec(ctx, ServeAliveCommand(pid, paths))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(res.Stdout)) == "1"
}

func readState(ctx context.Context, fs *sftpfs.FS, path string) (ServeState, error) {
	data, _, _, err := fs.ReadFile(ctx, path, 1<<20)
	if err != nil {
		return ServeState{}, err
	}
	return UnmarshalState(data)
}

func readToken(ctx context.Context, fs *sftpfs.FS, path string) (string, error) {
	data, _, _, err := fs.ReadFile(ctx, path, 64<<10)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return "", errors.New("bootstrap: empty token file")
	}
	return tok, nil
}

func pollPortFile(ctx context.Context, fs *sftpfs.FS, portFile string, clock func() time.Time) (string, error) {
	deadline := clock().Add(20 * time.Second)
	for {
		data, _, _, err := fs.ReadFile(ctx, portFile, 128)
		if err == nil {
			addr := strings.TrimSpace(string(data))
			if validServeAddr(addr) {
				return addr, nil
			}
		}
		if clock().After(deadline) {
			return "", errors.New("bootstrap: timed out waiting for serve to report its port")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func validServeAddr(addr string) bool {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil || host != "127.0.0.1" {
		return false
	}
	port, err := strconv.Atoi(portText)
	return err == nil && port > 0 && port <= 65535
}

func readPIDFile(ctx context.Context, fs *sftpfs.FS, pidFile string) (int, error) {
	data, _, _, err := fs.ReadFile(ctx, pidFile, 64)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, errors.New("bootstrap: invalid serve pid file")
	}
	return pid, nil
}

func cleanupFailedLaunch(conn Conn, fs *sftpfs.FS, paths StatePaths, pid int) {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	if pid <= 0 {
		pid, _ = readPIDFile(ctx, fs, paths.PidFile)
	}
	if pid > 0 {
		_, _ = conn.Exec(ctx, StopCommand(pid, paths))
	}
	_ = fs.Remove(ctx, paths.StateJSON, false)
	_ = fs.Remove(ctx, paths.TokenFile, false)
	_ = fs.Remove(ctx, paths.PortFile, false)
	_ = fs.Remove(ctx, paths.PidFile, false)
}

func resolveWorkspace(ctx context.Context, fs *sftpfs.FS, workspace, home string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return home, nil
	}
	if workspace == "~" {
		return home, nil
	}
	if strings.HasPrefix(workspace, "~/") {
		return strings.TrimRight(home, "/") + "/" + strings.TrimPrefix(workspace, "~/"), nil
	}
	if strings.HasPrefix(workspace, "/") {
		return workspace, nil
	}
	// Relative to home.
	return strings.TrimRight(home, "/") + "/" + workspace, nil
}

func generateToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
