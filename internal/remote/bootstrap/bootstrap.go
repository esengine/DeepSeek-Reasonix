// Package bootstrap starts and manages a detached `reasonix serve` process on
// a remote host over an established SSH connection. It detects the remote
// OS/arch, locates or installs reasonix, launches serve bound to a random
// loopback port with a file-based token (never in argv), and records the
// result under the remote ~/.reasonix/remote so a later reconnect can reuse
// it. Linux, macOS and Windows remotes are supported; which one a machine is
// decides the shell every command below is written in.
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

	"reasonix/internal/releaseasset"
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

// MinPaneVersion is the oldest kernel a pane can be driven by: a pane drives
// the hub surface under /runtimes, which landed in the 2.x line, and a 1.x
// kernel routes those calls to its page and answers them 405. The floor is the
// pane's, not the bootstrap's — forwarding a kernel's own page to a browser
// asks nothing a 1.x kernel cannot do, so those callers pass none.
const MinPaneVersion = "2.0.0"

// MinBrokeredPaneVersion is the oldest kernel a pane can be driven by when its
// models resolve through the broker. An older one starts sessions on its own
// default_model and lists its own providers, which the broker then refuses.
const MinBrokeredPaneVersion = "2.18.1"

// PaneFloor is the version floor for a pane's kernel, by whether the connect
// publishes a broker to it. A connect and a probe of it both read it here, so
// neither can call usable a kernel the other would replace.
func PaneFloor(brokered bool) string {
	if brokered {
		return MinBrokeredPaneVersion
	}
	return MinPaneVersion
}

// Broker points a bootstrapped serve at the provider broker on the machine
// starting it: Addr is the remote loopback address an -R forward publishes it
// on, and Token authenticates to it. A zero Broker leaves that host resolving
// providers from its own config, which is what makes it need its own API key.
type Broker struct {
	Addr  string
	Token string
}

func (b Broker) configured() bool { return b.Addr != "" && b.Token != "" }

// Options configures EnsureServe.
type Options struct {
	Workspace       string                                                                          // remote workspace path (may start with ~)
	Install         string                                                                          // auto|npm|upload|never
	LocalBinary     string                                                                          // path to the running reasonix binary, for same-platform upload
	LocalGOOS       string                                                                          // GOOS of LocalBinary
	LocalGOARCH     string                                                                          // GOARCH of LocalBinary
	ProductVersion  string                                                                          // exact local release used for a cross-platform official download
	FetchBinary     func(context.Context, string, string, string) ([]byte, error)                   // local verified release fetcher
	ResolveDownload func(context.Context, string, string, string) (releaseasset.CLIDownload, error) // names the release archive so the remote can fetch it itself
	MinVersion      string                                                                          // version floor; empty leaves the flag probe as the only gate
	Broker          Broker                                                                          // resolve providers back over the tunnel; zero leaves the remote on its own credentials
	Progress        func(step, detail string)                                                       // optional progress callback
	Clock           func() time.Time                                                                // nil => time.Now
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
	// Workspace as the remote kernel spells it. State.Workspace is the file
	// layer's spelling, and on Windows the two are different strings — handing
	// that one back to the kernel asks it to open C:\C:\Users\...
	Workspace string
}

// EnsureServe returns a running serve for (host, workspace), starting one if
// needed. It is also the reconnect path: an existing live process is reused.
func EnsureServe(ctx context.Context, conn Conn, opts Options) (Result, error) {
	fs, err := conn.SFTP()
	if err != nil {
		return Result{}, err
	}
	// 1. Which machine is this? Everything below names a path or runs a
	// command, and both are spelled differently depending on the answer.
	opts.progress("detect", "")
	target, goos, goarch, home, err := remoteFor(ctx, conn, fs)
	if err != nil {
		return Result{}, err
	}
	workspace, err := resolveWorkspace(ctx, fs, opts.Workspace, home)
	if err != nil {
		return Result{}, err
	}
	paths := target.Paths(home, workspace)

	// 2. Reuse a live process if the recorded pid is still running.
	if st, tok, ok := tryReuse(ctx, conn, target, fs, paths, opts.MinVersion, opts.Broker, false, opts.clock(), workspace); ok {
		opts.progress("reuse", st.Addr)
		return Result{State: st, Token: tok, Reused: true, Workspace: target.NativePath(st.Workspace)}, nil
	}

	// 3. Locate or install a usable reasonix.
	bin, version, err := ensureBinary(ctx, conn, target, fs, opts, home, goos, goarch, paths)
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
	if st, tok, ok := tryReuse(ctx, conn, target, fs, paths, opts.MinVersion, opts.Broker, true, opts.clock(), workspace); ok {
		opts.progress("reuse", st.Addr)
		return Result{State: st, Token: tok, Reused: true, Workspace: target.NativePath(st.Workspace)}, nil
	}
	// The recorded kernel was just declined, and the record pointing at it is
	// about to be replaced. Stop it here or nothing ever will: the pid would
	// outlive the only note this side keeps of it.
	retireReplaced(ctx, conn, target, fs, paths)

	// 5. Generate token, write it 0600, and launch detached serve.
	token, err := generateToken()
	if err != nil {
		return Result{}, err
	}
	if err := fs.MkdirAll(ctx, paths.Dir); err != nil {
		return Result{}, err
	}
	_ = fs.Chmod(ctx, paths.Dir, 0o700)
	if err := fs.WriteFileAtomic(ctx, paths.TokenFile, []byte(token+"\n"), 0o600); err != nil {
		return Result{}, fmt.Errorf("bootstrap: write token: %w", err)
	}
	if opts.Broker.configured() {
		if err := writeBroker(ctx, fs, paths, opts.Broker); err != nil {
			return Result{}, err
		}
	}
	opts.progress("launch", "")
	launchRes, err := conn.Exec(ctx, target.Launch(LaunchSpec{Bin: bin, Workspace: workspace, BrokerAddr: opts.Broker.Addr}, paths))
	if err != nil {
		cleanupFailedLaunch(conn, target, fs, paths, 0)
		return Result{}, fmt.Errorf("bootstrap: launch: %w", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(launchRes.Stdout)))

	// 6. Poll the newly-created port file for the real bound address. The launch
	// command removes stale port/pid files before forking.
	opts.progress("health_check", "")
	addr, err := pollPortFile(ctx, fs, paths.PortFile, opts.clock())
	if err != nil {
		cleanupFailedLaunch(conn, target, fs, paths, pid)
		return Result{}, err
	}
	if filePID, perr := readPIDFile(ctx, fs, paths.PidFile); perr == nil {
		pid = filePID // --pid-file is authoritative when available.
	}
	if pid <= 0 || !pidIsServe(ctx, conn, target, pid, paths) {
		cleanupFailedLaunch(conn, target, fs, paths, pid)
		return Result{}, fmt.Errorf("%w: the launched process is not a reasonix serve", ErrServeDidNotStart)
	}

	st := ServeState{
		PID:        pid,
		Addr:       addr,
		Workspace:  workspace,
		Version:    version,
		Broker:     opts.Broker.Addr,
		BrokerFile: opts.Broker.configured(),
		TokenFile:  paths.TokenFile,
		LogFile:    paths.LogFile,
		StartedAt:  nowUnix(opts.clock()),
	}
	data, err := MarshalState(st)
	if err != nil {
		cleanupFailedLaunch(conn, target, fs, paths, pid)
		return Result{}, err
	}
	if err := fs.WriteFileAtomic(ctx, paths.StateJSON, data, 0o600); err != nil {
		cleanupFailedLaunch(conn, target, fs, paths, pid)
		return Result{}, fmt.Errorf("bootstrap: write state: %w", err)
	}
	opts.progress("ready", addr)
	return Result{State: st, Token: token, Workspace: target.NativePath(workspace)}, nil
}

// Status reads the recorded state and reports whether the process is alive.
func Status(ctx context.Context, conn Conn, workspace string) (ServeState, bool, error) {
	fs, err := conn.SFTP()
	if err != nil {
		return ServeState{}, false, err
	}
	target, _, _, home, err := remoteFor(ctx, conn, fs)
	if err != nil {
		return ServeState{}, false, err
	}
	ws, err := resolveWorkspace(ctx, fs, workspace, home)
	if err != nil {
		return ServeState{}, false, err
	}
	paths := target.Paths(home, ws)
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil {
		return ServeState{}, false, nil // no state => not running
	}
	alive := st.Workspace == ws && validServeAddr(st.Addr) && pidIsServe(ctx, conn, target, st.PID, paths)
	return st, alive, nil
}

// Stop terminates the recorded process and removes its state files.
func Stop(ctx context.Context, conn Conn, workspace string) error {
	fs, err := conn.SFTP()
	if err != nil {
		return err
	}
	target, _, _, home, err := remoteFor(ctx, conn, fs)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(ctx, fs, workspace, home)
	if err != nil {
		return err
	}
	paths := target.Paths(home, ws)
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil {
		return nil // nothing recorded
	}
	// Only signal the pid if it is still OUR serve: a recycled PID now owned by
	// an unrelated process must never be TERM/KILLed.
	if st.PID > 0 {
		if _, err := conn.Exec(ctx, target.Stop(st.PID, paths)); err != nil {
			return fmt.Errorf("bootstrap: stop pid %d: %w", st.PID, err)
		}
	}
	removeServeState(ctx, fs, paths)
	return nil
}

// RepointBroker points the serve recorded for workspace at broker, when that
// serve follows its broker file. A link that came back on a different remote
// port calls this so the running kernel reaches the broker where it now is.
// A serve that does not follow the file is left as it is; the next connect
// replaces it.
func RepointBroker(ctx context.Context, conn Conn, workspace string, broker Broker) error {
	if !broker.configured() {
		return nil
	}
	fs, err := conn.SFTP()
	if err != nil {
		return err
	}
	target, _, _, home, err := remoteFor(ctx, conn, fs)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(ctx, fs, workspace, home)
	if err != nil {
		return err
	}
	paths := target.Paths(home, ws)
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil || !st.BrokerFile {
		return nil
	}
	if _, ok := rebind(ctx, fs, paths, st, broker, false, time.Now); !ok {
		return fmt.Errorf("bootstrap: re-point the serve for %s at its broker", ws)
	}
	return nil
}

// Logs writes up to n tail lines of the serve log to w.
func Logs(ctx context.Context, conn Conn, workspace string, n int, w io.Writer) error {
	fs, err := conn.SFTP()
	if err != nil {
		return err
	}
	target, _, _, home, err := remoteFor(ctx, conn, fs)
	if err != nil {
		return err
	}
	ws, err := resolveWorkspace(ctx, fs, workspace, home)
	if err != nil {
		return err
	}
	paths := target.Paths(home, ws)
	res, err := conn.Exec(ctx, target.Logs(paths.LogFile, n))
	if err != nil {
		return err
	}
	_, err = w.Write(res.Stdout)
	return err
}

func tryReuse(ctx context.Context, conn Conn, target remoteOS, fs *sftpfs.FS, paths StatePaths, minVersion string, broker Broker, held bool, clock func() time.Time, workspace ...string) (ServeState, string, bool) {
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil || st.PID <= 0 || st.Addr == "" {
		return ServeState{}, "", false
	}
	if len(workspace) > 0 && st.Workspace != workspace[0] {
		return ServeState{}, "", false
	}
	// A serve reading its broker from a file follows the latest connect; any
	// other keeps dialling the address it started with, which may be gone.
	follows := broker.configured() && st.BrokerFile
	if st.Broker != broker.Addr && !follows {
		return ServeState{}, "", false
	}
	// Alive is not the same question as usable. Handing back a kernel from a
	// line that has no pane hub is how a remote workspace opened onto one that
	// answered every call a pane made with 405.
	if !meetsMinVersion(st.Version, minVersion) {
		return ServeState{}, "", false
	}
	if !validServeAddr(st.Addr) || !pidIsServe(ctx, conn, target, st.PID, paths) {
		return ServeState{}, "", false
	}
	// The state record is informational; the workspace-derived path is the
	// authority, so a tampered record cannot make us read an arbitrary file.
	tok, err := readToken(ctx, fs, paths.TokenFile)
	if err != nil {
		return ServeState{}, "", false
	}
	if follows {
		// Written on every reuse, not only when the address moved: a broker
		// restarted on the same port holds a new token.
		if st, ok := rebind(ctx, fs, paths, st, broker, held, clock); ok {
			return st, tok, true
		}
		return ServeState{}, "", false
	}
	return st, tok, true
}

// writeBroker points a serve at this connect's broker. Address and token go in
// one file replaced whole, so no read can pair one connect's token with
// another's port; the directory is kept private for the moment the temporary
// file exists.
func writeBroker(ctx context.Context, fs *sftpfs.FS, paths StatePaths, broker Broker) error {
	_ = fs.Chmod(ctx, paths.Dir, 0o700)
	body := []byte(broker.Addr + "\n" + broker.Token + "\n")
	if err := fs.WriteFileAtomic(ctx, paths.BrokerEndpoint, body, 0o600); err != nil {
		return fmt.Errorf("bootstrap: write broker endpoint: %w", err)
	}
	return nil
}

// rebind points the recorded serve at broker under the serve lock, so it cannot
// overwrite a record another client is replacing. held says the caller already
// owns the lock. It re-reads the record and gives up if the pid moved.
func rebind(ctx context.Context, fs *sftpfs.FS, paths StatePaths, st ServeState, broker Broker, held bool, clock func() time.Time) (ServeState, bool) {
	if !held {
		lock, err := acquireServeLock(ctx, fs, paths, clock)
		if err != nil {
			return ServeState{}, false
		}
		defer lock.release()
		now, err := readState(ctx, fs, paths.StateJSON)
		if err != nil || now.PID != st.PID {
			return ServeState{}, false
		}
		st = now
	}
	if err := writeBroker(ctx, fs, paths, broker); err != nil {
		return ServeState{}, false
	}
	if st.Broker != broker.Addr {
		st.Broker = broker.Addr
		if data, err := MarshalState(st); err == nil {
			_ = fs.WriteFileAtomic(ctx, paths.StateJSON, data, 0o600)
		}
	}
	return st, true
}

// retireReplaced stops the kernel named by the record this launch is about to
// overwrite. Reuse was already refused — too old, bound to a retired broker,
// its token gone — and which of those it was does not change that the record
// naming the pid is the only note this side keeps. Best effort: a machine that
// will not answer is not a reason to refuse the pane the caller asked for.
func retireReplaced(ctx context.Context, conn Conn, target remoteOS, fs *sftpfs.FS, paths StatePaths) {
	st, err := readState(ctx, fs, paths.StateJSON)
	if err != nil || st.PID <= 0 {
		return
	}
	if !pidIsServe(ctx, conn, target, st.PID, paths) {
		return
	}
	_, _ = conn.Exec(ctx, target.Stop(st.PID, paths))
}

// pidIsServe reports whether pid is running AND is a reasonix serve process,
// so PID reuse cannot make an unrelated process look like a live serve.
func pidIsServe(ctx context.Context, conn Conn, target remoteOS, pid int, paths StatePaths) bool {
	if pid <= 0 {
		return false
	}
	res, err := conn.Exec(ctx, target.Alive(pid, paths))
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
			return "", fmt.Errorf("%w: it did not within 20s", ErrServeDidNotStart)
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

func cleanupFailedLaunch(conn Conn, target remoteOS, fs *sftpfs.FS, paths StatePaths, pid int) {
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	if pid <= 0 {
		pid, _ = readPIDFile(ctx, fs, paths.PidFile)
	}
	if pid > 0 {
		_, _ = conn.Exec(ctx, target.Stop(pid, paths))
	}
	removeServeState(ctx, fs, paths)
}

// removeServeState deletes every file one serve's record is made of. One list,
// because the two callers that clear it had a copy each: the broker token was
// added to what a launch writes and to neither of them, so a failed launch left
// this machine's provider credential on the remote.
func removeServeState(ctx context.Context, fs *sftpfs.FS, paths StatePaths) {
	for _, p := range []string{
		paths.StateJSON, paths.TokenFile, paths.BrokerTokenFile, paths.BrokerEndpoint, paths.PortFile, paths.PidFile,
	} {
		if p != "" {
			_ = fs.Remove(ctx, p, false)
		}
	}
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
