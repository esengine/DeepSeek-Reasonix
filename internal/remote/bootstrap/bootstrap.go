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
	Workspace      string                                                        // remote workspace path (may start with ~)
	Install        string                                                        // auto|npm|upload|never
	LocalBinary    string                                                        // path to the running reasonix binary, for same-platform upload
	LocalGOOS      string                                                        // GOOS of LocalBinary
	LocalGOARCH    string                                                        // GOARCH of LocalBinary
	ProductVersion string                                                        // exact local release used for a cross-platform official download
	FetchBinary    func(context.Context, string, string, string) ([]byte, error) // local verified release fetcher
	MinVersion     string                                                        // minimum acceptable remote version
	Progress       func(step, detail string)                                     // optional progress callback
	Clock          func() time.Time                                              // nil => time.Now
	// CredentialProxy installs a tunnel-backed provider and a scoped virtual
	// token on the remote. The real provider key never leaves the desktop.
	CredentialProxy *CredentialProxyOptions
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
	// CredentialConfigChanged is true when the credential-proxy heal rewrote
	// the remote config while REUSING a serve — that serve's in-memory
	// providers still reflect the previous config and must be reloaded.
	CredentialConfigChanged bool
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

	requireLaunchArgs, credentialChanged, err := prepareCredentialProxy(ctx, conn, fs, opts, home, workspace, paths)
	if err != nil {
		return Result{}, err
	}
	// 1. Reuse a live process if the recorded pid is still running and exposes
	// every Serve contract required by this desktop.
	if st, tok, ok := tryReuse(ctx, conn, fs, paths, workspace, requireLaunchArgs...); ok {
		opts.progress("reuse", st.Addr)
		return Result{State: st, Token: tok, Reused: true, CredentialConfigChanged: credentialChanged}, nil
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
	if st, tok, ok := tryReuse(ctx, conn, fs, paths, workspace, requireLaunchArgs...); ok {
		opts.progress("reuse", st.Addr)
		return Result{State: st, Token: tok, Reused: true, CredentialConfigChanged: credentialChanged}, nil
	}

	// 5. Stage the replacement token, retire incompatible Serve, then publish.
	freshToken, err := generateToken()
	if err != nil {
		return Result{}, err
	}
	stagedTokenFile, err := stageServeToken(ctx, fs, paths, freshToken)
	if err != nil {
		return Result{}, err
	}
	defer cleanupStagedServeToken(fs, stagedTokenFile)
	// Retire an incompatible Serve only after its replacement is ready to launch
	// inside the lock, so preparation failures do not interrupt existing work.
	if err := retireIncompatibleServe(ctx, conn, fs, paths, workspace, requireLaunchArgs); err != nil {
		return Result{}, err
	}
	if err := fs.Rename(ctx, stagedTokenFile, paths.TokenFile); err != nil {
		return Result{}, fmt.Errorf("bootstrap: publish token: %w", err)
	}
	opts.progress("launch", "")
	launchRes, err := conn.Exec(ctx, LaunchCommand(bin, workspace, paths, opts.CredentialProxy))
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
		ServeCaps: ServeCapsToken,
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
	return Result{State: st, Token: freshToken}, nil
}

func stageServeToken(ctx context.Context, fs *sftpfs.FS, paths StatePaths, token string) (string, error) {
	if err := fs.MkdirAll(ctx, paths.Dir); err != nil {
		return "", err
	}
	staged := paths.TokenFile + ".next"
	if err := fs.WriteFileAtomic(ctx, staged, []byte(token+"\n"), 0o600); err != nil {
		cleanupStagedServeToken(fs, staged)
		return "", fmt.Errorf("bootstrap: stage token: %w", err)
	}
	return staged, nil
}

func cleanupStagedServeToken(fs *sftpfs.FS, staged string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = fs.Remove(ctx, staged, false)
}

func prepareCredentialProxy(ctx context.Context, conn Conn, fs *sftpfs.FS, opts Options, home, workspace string, paths StatePaths) ([]string, bool, error) {
	if opts.CredentialProxy == nil {
		return nil, false, nil
	}
	opts.progress("credential_proxy", "")
	changed, err := ensureCredentialProvider(ctx, fs, home, opts.CredentialProxy)
	if err != nil {
		return nil, false, err
	}
	required := []string{"--model " + opts.CredentialProxy.Provider}
	return required, changed, nil
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

func tryReuse(ctx context.Context, conn Conn, fs *sftpfs.FS, paths StatePaths, workspace string, requireArgs ...string) (ServeState, string, bool) {
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil || st.PID <= 0 || st.Addr == "" {
		return ServeState{}, "", false
	}
	if workspace != "" && st.Workspace != workspace {
		return ServeState{}, "", false
	}
	if !validServeAddr(st.Addr) || !pidIsServe(ctx, conn, st.PID, paths, requireArgs...) {
		return ServeState{}, "", false
	}
	if st.ServeCaps != ServeCapsToken && !supportsRequiredServeCapabilities(ctx, conn, st.PID) {
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

// stopMismatchedServe TERMs a live serve whose command line lacks the
// required launch args: reuse would route model calls under the wrong
// credential setup, and a plain relaunch would orphan the process.
func stopMismatchedServe(ctx context.Context, conn Conn, fs *sftpfs.FS, paths StatePaths, workspace string, requireArgs []string) error {
	if len(requireArgs) == 0 {
		return nil
	}
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil || st.PID <= 0 || !validServeAddr(st.Addr) || st.Workspace != workspace {
		return nil
	}
	if pidIsServe(ctx, conn, st.PID, paths) && !pidIsServe(ctx, conn, st.PID, paths, requireArgs...) {
		if _, err := conn.Exec(ctx, StopCommand(st.PID, paths)); err != nil {
			return fmt.Errorf("bootstrap: stop mismatched serve: %w", err)
		}
	}
	return nil
}

func retireIncompatibleServe(ctx context.Context, conn Conn, fs *sftpfs.FS, paths StatePaths, workspace string, requireArgs []string) error {
	if err := stopMismatchedServe(ctx, conn, fs, paths, workspace, requireArgs); err != nil {
		return err
	}
	return stopOutdatedServe(ctx, conn, fs, paths, workspace)
}

// stopOutdatedServe retires a live process whose binary lacks the wire and
// healing contracts required by the desktop. Leaving it alive would retain the
// workspace lease and race the replacement process.
func stopOutdatedServe(ctx context.Context, conn Conn, fs *sftpfs.FS, paths StatePaths, workspace string) error {
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil || st.PID <= 0 || !validServeAddr(st.Addr) || st.Workspace != workspace {
		return nil
	}
	if !pidIsServe(ctx, conn, st.PID, paths) {
		return nil
	}
	if st.ServeCaps == ServeCapsToken || supportsRequiredServeCapabilities(ctx, conn, st.PID) {
		return nil
	}
	if _, stopErr := conn.Exec(ctx, StopCommand(st.PID, paths)); stopErr != nil {
		return fmt.Errorf("bootstrap: stop outdated serve: %w", stopErr)
	}
	return nil
}

func supportsRequiredServeCapabilities(ctx context.Context, conn Conn, pid int) bool {
	res, err := conn.Exec(ctx, SupportsRequiredServeCapabilitiesCommand(pid))
	return err == nil && strings.TrimSpace(string(res.Stdout)) == "yes"
}

// pidIsServe reports whether pid is running AND is a reasonix serve process,
// so PID reuse cannot make an unrelated process look like a live serve.
func pidIsServe(ctx context.Context, conn Conn, pid int, paths StatePaths, requireArgs ...string) bool {
	if pid <= 0 {
		return false
	}
	res, err := conn.Exec(ctx, ServeAliveCommand(pid, paths, requireArgs...))
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
	if after, ok0 := strings.CutPrefix(workspace, "~/"); ok0 {
		return strings.TrimRight(home, "/") + "/" + after, nil
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
