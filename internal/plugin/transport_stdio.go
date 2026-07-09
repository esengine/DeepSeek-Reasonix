package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

const closeWaitBudget = 5 * time.Second

// stdioTransport speaks newline-delimited JSON-RPC 2.0 over a subprocess's
// stdin/stdout — the MCP stdio convention (one JSON message per line, no
// embedded newlines). A dedicated reader goroutine owns stdout and demuxes each
// response to the waiting call by id, so a call can abandon a blocking read the
// moment its context is cancelled (the subprocess is bound to the session, not
// the turn, so a hung server would otherwise hang a cancelled turn forever).
// callMu serialises a request/response round-trip over the shared pipe.
type stdioTransport struct {
	name   string
	cmd    *exec.Cmd
	job    uintptr // Windows Job Object handle (0 elsewhere); reaps detached grandchildren on close
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *tailBuffer

	callMu sync.Mutex // one in-flight request/response at a time over the shared pipe

	mu      sync.Mutex
	nextID  int
	pending map[int]chan rpcResponse
	readErr error // set once the reader goroutine exits; further calls fail fast

	waitOnce    sync.Once
	releaseSlot func() // returns a bounded instance slot (e.g. CodeGraph) on close; nil when unbounded
}

func newStdioTransport(ctx context.Context, s Spec) (*stdioTransport, error) {
	if strings.TrimSpace(s.Command) == "" {
		return nil, fmt.Errorf("stdio plugin %q: command is required", s.Name)
	}
	var releaseSlot func()
	if isCodeGraphSpecName(s.Name) {
		release, err := acquireCodeGraphSlot()
		if err != nil {
			return nil, err
		}
		releaseSlot = release
	}
	defer func() {
		// Release the reserved slot if construction fails before the transport
		// takes ownership of it (set to nil on the success path below).
		if releaseSlot != nil {
			releaseSlot()
		}
	}()
	env := mergeEnv(secrets.ProcessEnv(), s.Env)
	exe, env, err := resolveStdioExecutable(ctx, s, env)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, exe, s.Args...)
	proc.HideWindow(cmd)
	if s.LowPriority {
		proc.LowPriority(cmd)
	}
	cmd.Env = env
	if s.Dir != "" {
		cmd.Dir = s.Dir // pin cwd-aware servers (e.g. CodeGraph) to the project root
	}
	stderr := &tailBuffer{limit: 16 * 1024}
	cmd.Stderr = stderr
	if s.Stderr != nil {
		cmd.Stderr = io.MultiWriter(stderr, s.Stderr)
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	job, err := proc.StartTracked(cmd)
	if err != nil {
		return nil, err
	}
	if s.LowPriority {
		proc.LowPriorityStarted(cmd)
	}
	t := &stdioTransport{
		name:        s.Name,
		cmd:         cmd,
		job:         job,
		stdin:       stdin,
		stdout:      bufio.NewReader(stdout),
		stderr:      stderr,
		pending:     map[int]chan rpcResponse{},
		releaseSlot: releaseSlot,
	}
	releaseSlot = nil // ownership transferred to t; close() releases it
	go t.readLoop()
	return t, nil
}

var stdioShellPATH = cachedShellPATH(defaultStdioShellPATH)

// cachedShellPATH memoizes the first completed shell-PATH probe: the user's
// interactive PATH is stable for the process, and resolveStdioExecutable now
// probes for every stdio plugin, so caching avoids a login shell per server.
// The probe runs up to three login shells with a 2s timeout each, so it must
// not run under the lock; concurrent spawns share the in-flight probe instead
// of each running (or queueing behind) their own. Empty results are cached too
// — a host without a usable login shell must not re-probe on every spawn —
// except when the probe's context was cancelled, since that empty reflects the
// aborted caller rather than the host, and caching it would pin "" for the
// rest of the process.
func cachedShellPATH(probe func(context.Context) string) func(context.Context) string {
	var (
		mu       sync.Mutex
		cached   string
		done     bool
		inflight chan struct{} // non-nil while a probe runs; closed when it settles
	)
	return func(ctx context.Context) string {
		for {
			mu.Lock()
			if done {
				p := cached
				mu.Unlock()
				return p
			}
			if inflight != nil {
				wait := inflight
				mu.Unlock()
				select {
				case <-wait:
					continue // re-check: the probe may not have cached (cancelled)
				case <-ctx.Done():
					return ""
				}
			}
			ch := make(chan struct{})
			inflight = ch
			mu.Unlock()

			p := probe(ctx)

			mu.Lock()
			inflight = nil
			if p != "" || ctx.Err() == nil {
				cached, done = p, true
			}
			mu.Unlock()
			close(ch)
			return p
		}
	}
}

func resolveStdioExecutable(ctx context.Context, s Spec, env []string) (string, []string, error) {
	// Unconditionally enrich PATH with the user's shell PATH so every
	// subprocess—including wrapper scripts that invoke npx, uvx, etc.—
	// inherits the expected tool locations even under a GUI launch.
	env = enrichStdioShellPATH(ctx, env)

	if hasPathSeparator(s.Command) {
		return s.Command, env, nil
	}
	if exe, ok := lookPathInEnv(s.Command, env); ok {
		return exe, env, nil
	}

	currentPath, _ := envValue(env, "PATH")
	if runtime.GOOS == "windows" {
		fallbackPath := mergePathLists(windowsStdioFallbackPATH(env), currentPath)
		if fallbackPath != currentPath {
			fallbackEnv := setEnvValue(env, "PATH", fallbackPath)
			if exe, ok := lookPathInEnv(s.Command, fallbackEnv); ok {
				return exe, fallbackEnv, nil
			}
			env = fallbackEnv
			currentPath = fallbackPath
		}
	}

	return "", env, fmt.Errorf("stdio plugin %q: command %q not found on PATH; GUI launches and non-interactive sessions may not inherit your shell PATH. Use an absolute command path or set PATH in the MCP server env. PATH=%q",
		s.Name, s.Command, currentPath)
}

// enrichStdioShellPATH probes the user's interactive login shell for its PATH
// and prepends those directories to the current environment. The result is the
// subprocess environment with a PATH that matches what the user sees in their
// terminal, even when Reasonix was launched from the Finder / Dock / open(1).
func enrichStdioShellPATH(ctx context.Context, env []string) []string {
	currentPath, _ := envValue(env, "PATH")
	if shellPath := strings.TrimSpace(stdioShellPATH(ctx)); shellPath != "" {
		if fallbackPath := mergePathLists(shellPath, currentPath); fallbackPath != currentPath {
			env = setEnvValue(env, "PATH", fallbackPath)
		}
	}
	return env
}

func hasPathSeparator(s string) bool {
	return strings.ContainsAny(s, `/\`)
}

func lookPathInEnv(command string, env []string) (string, bool) {
	path, _ := envValue(env, "PATH")
	pathext, _ := envValue(env, "PATHEXT")
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		for _, name := range executableNames(command, pathext) {
			candidate := filepath.Join(dir, name)
			if isExecutableFile(candidate) {
				return candidate, true
			}
		}
	}
	return "", false
}

func executableNames(command, pathext string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(command) != "" {
		return []string{command}
	}
	if strings.TrimSpace(pathext) == "" {
		pathext = ".COM;.EXE;.BAT;.CMD"
	}
	names := []string{command}
	seen := map[string]bool{strings.ToLower(command): true}
	for _, ext := range strings.Split(pathext, ";") {
		ext = strings.TrimSpace(ext)
		if ext == "" {
			continue
		}
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		name := command + ext
		key := strings.ToLower(name)
		if !seen[key] {
			seen[key] = true
			names = append(names, name)
		}
	}
	return names
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

func windowsStdioFallbackPATH(env []string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	programFiles, _ := envValue(env, "ProgramFiles")
	programFilesX86, _ := envValue(env, "ProgramFiles(x86)")
	localAppData, _ := envValue(env, "LOCALAPPDATA")
	appData, _ := envValue(env, "APPDATA")
	userProfile, _ := envValue(env, "USERPROFILE")
	chocolatey, _ := envValue(env, "ChocolateyInstall")
	if localAppData == "" && userProfile != "" {
		localAppData = filepath.Join(userProfile, "AppData", "Local")
	}
	if appData == "" && userProfile != "" {
		appData = filepath.Join(userProfile, "AppData", "Roaming")
	}
	candidates := []string{
		filepath.Join(programFiles, "nodejs"),
		filepath.Join(programFilesX86, "nodejs"),
		filepath.Join(localAppData, "Programs", "nodejs"),
		filepath.Join(appData, "npm"),
		filepath.Join(localAppData, "Microsoft", "WindowsApps"),
		filepath.Join(userProfile, "scoop", "shims"),
		filepath.Join(userProfile, ".bun", "bin"),
		filepath.Join(userProfile, ".cargo", "bin"),
		filepath.Join(chocolatey, "bin"),
	}
	var existing []string
	for _, dir := range candidates {
		if isDir(dir) {
			existing = append(existing, dir)
		}
	}
	return strings.Join(existing, string(os.PathListSeparator))
}

func isDir(path string) bool {
	if path == "" {
		return false
	}
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func defaultStdioShellPATH(ctx context.Context) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	shell := stdioShell()
	if shell == "" {
		return ""
	}
	const marker = "__REASONIX_PATH__="
	script := "printf '\\n" + marker + "%s\\n' \"$PATH\""
	for _, args := range [][]string{
		{"-l", "-i", "-c", script},
		{"-l", "-c", script},
		{"-c", script},
	} {
		out := runShellPATHCommand(ctx, shell, args)
		if path := parseShellPATH(out, marker); path != "" {
			return path
		}
	}
	return ""
}

func stdioShell() string {
	if shell := strings.TrimSpace(os.Getenv("SHELL")); shell != "" {
		if hasPathSeparator(shell) {
			if isExecutableFile(shell) {
				return shell
			}
		} else if exe, ok := lookPathInEnv(shell, secrets.ProcessEnv()); ok {
			return exe
		}
	}
	for _, shell := range []string{"/bin/zsh", "/bin/bash", "/bin/sh"} {
		if isExecutableFile(shell) {
			return shell
		}
	}
	return ""
}

func runShellPATHCommand(parent context.Context, shell string, args []string) []byte {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, args...)
	// Explicit env so the login-shell probe honors [secrets]
	// filter_subprocess_env instead of inheriting the full environment.
	cmd.Env = secrets.ProcessEnv()
	prepareStdioShellPATHProbe(cmd)
	cmd.Stdin = strings.NewReader("")
	out, _ := cmd.CombinedOutput()
	return out
}

func prepareStdioShellPATHProbe(cmd *exec.Cmd) {
	proc.PrepareShellPATHProbe(cmd)
}

func parseShellPATH(out []byte, marker string) string {
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], marker) {
			return strings.TrimSpace(strings.TrimPrefix(lines[i], marker))
		}
	}
	return ""
}

func mergeEnv(base []string, overrides map[string]string) []string {
	out := append([]string(nil), base...)
	for k, v := range overrides {
		out = setEnvValue(out, k, v)
	}
	return out
}

func setEnvValue(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, kv := range env {
		k, _, ok := strings.Cut(kv, "=")
		if ok && envKeyEqual(k, key) {
			if !replaced {
				out = append(out, key+"="+value)
				replaced = true
			}
			continue
		}
		out = append(out, kv)
	}
	if !replaced {
		out = append(out, key+"="+value)
	}
	return out
}

func envValue(env []string, key string) (string, bool) {
	for i := len(env) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(env[i], "=")
		if ok && envKeyEqual(k, key) {
			return v, true
		}
	}
	return "", false
}

func envKeyEqual(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func mergePathLists(primary, secondary string) string {
	var out []string
	seen := map[string]bool{}
	for _, path := range []string{primary, secondary} {
		for _, dir := range filepath.SplitList(path) {
			if dir == "" || seen[dir] {
				continue
			}
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return strings.Join(out, string(os.PathListSeparator))
}

// readLoop owns stdout for the transport's lifetime: it reads one JSON-RPC
// message per line, drops server-initiated notifications/requests (they carry a
// method), and hands each response to the call waiting on its id. On any read
// error it fails every pending call and exits.
func (t *stdioTransport) readLoop() {
	for {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			t.failAll(err)
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var probe struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(line, &probe)
		if probe.Method != "" {
			continue // server notification/request, not a response to one of our calls
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // unparseable line with no id — can't route it, skip
		}
		t.mu.Lock()
		ch := t.pending[resp.ID]
		delete(t.pending, resp.ID)
		t.mu.Unlock()
		if ch != nil {
			ch <- resp // buffered(1): never blocks, even if the caller already left
		}
	}
}

// failAll records the terminal read error and unblocks every pending call by
// closing its channel; a caller distinguishes this from a real response by the
// closed-channel receive.
func (t *stdioTransport) failAll(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.readErr == nil {
		t.readErr = err
	}
	for id, ch := range t.pending {
		close(ch)
		delete(t.pending, id)
	}
}

func (t *stdioTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	t.callMu.Lock()
	defer t.callMu.Unlock()

	t.mu.Lock()
	if t.readErr != nil {
		t.mu.Unlock()
		return nil, t.withStderr(fmt.Errorf("plugin %q: read: %w", t.name, t.readErr))
	}
	t.nextID++
	id := t.nextID
	ch := make(chan rpcResponse, 1)
	t.pending[id] = ch
	t.mu.Unlock()

	defer func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}()

	if err := t.write(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		return nil, fmt.Errorf("plugin %q: write %s: %w", t.name, method, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp, ok := <-ch:
		if !ok {
			return nil, t.withStderr(fmt.Errorf("plugin %q: read: %w", t.name, t.readErr))
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("plugin %q: %w", t.name, resp.Error)
		}
		return resp.Result, nil
	}
}

func (t *stdioTransport) notify(_ context.Context, method string, params any) error {
	return t.write(rpcRequest{JSONRPC: "2.0", Method: method, Params: params})
}

func (t *stdioTransport) write(v any) error {
	b, err := json.Marshal(v) // marshaled JSON never contains a literal newline
	if err != nil {
		return err
	}
	if _, err = t.stdin.Write(append(b, '\n')); err != nil {
		return t.withStderr(err)
	}
	return nil
}

func (t *stdioTransport) withStderr(err error) error {
	if t.stderr == nil {
		return err
	}
	// Reap the exited child so its stderr copy goroutine has flushed the tail.
	// Budgeted: a surviving grandchild keeps cmd.Wait blocked forever (see
	// close), and this path runs with callMu held — an unbounded wait here
	// would wedge every future call on this transport.
	waitWithBudget(t.wait, closeWaitBudget)
	msg := t.stderr.String()
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: stderr: %s", err, msg)
}

// wait reaps the child exactly once; cmd.Wait blocks until the stderr-copy
// goroutine completes, so the tail buffer is settled before anyone reads it.
func (t *stdioTransport) wait() {
	t.waitOnce.Do(func() {
		if t.cmd != nil && t.cmd.Process != nil {
			_ = t.cmd.Wait()
		}
	})
}

// waitWithBudget runs wait in a goroutine and returns once it finishes or the
// budget elapses, whichever comes first. On timeout the goroutine is left to
// complete the reap in the background, so wait must be safe to abandon
// (stdioTransport.wait is single-shot via waitOnce).
func waitWithBudget(wait func(), budget time.Duration) {
	done := make(chan struct{})
	go func() { wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(budget):
	}
}

// close kills the whole process tree (a launcher's surviving grandchild keeps
// the inherited stdio pipes open, so a plain Process.Kill leaves cmd.Wait
// blocking forever) and reaps it under a budget so one wedged server can never
// stall a boot or a turn teardown.
func (t *stdioTransport) close() {
	if t.releaseSlot != nil {
		t.releaseSlot() // idempotent; frees the bounded CodeGraph instance slot
	}
	if t.stdin != nil {
		_ = t.stdin.Close()
	}
	if t.cmd == nil || t.cmd.Process == nil {
		return
	}
	proc.KillTracked(t.cmd, t.job)
	waitWithBudget(t.wait, closeWaitBudget)
}

type tailBuffer struct {
	mu    sync.Mutex
	limit int
	buf   []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if b.limit > 0 && len(b.buf) > b.limit {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.limit:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}
