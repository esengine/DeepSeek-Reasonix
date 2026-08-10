package sidecar

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/pluginpkg"
	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

// Bounded-close budgets, mirrored from internal/plugin's stdio transport: a
// short stdin-EOF grace for protocol-aware sidecars, then a hard tree kill
// with a bounded reap so one wedged sidecar can never stall teardown.
const (
	gracefulCloseWaitBudget = 750 * time.Millisecond
	closeWaitBudget         = 5 * time.Second
	// stderrTailBytes bounds the ring of sidecar stderr retained for
	// diagnostics, mirrored from internal/plugin's tailBuffer.
	stderrTailBytes = 16 << 10
)

// pluginEnvVarPrefix is the well-known environment block every sidecar sees.
const (
	envPluginRoot    = "REASONIX_PLUGIN_ROOT"
	envPluginName    = "REASONIX_PLUGIN_NAME"
	envPluginVersion = "REASONIX_PLUGIN_VERSION"
)

// shellExecutables are interpreter names a runtime command may not resolve
// to. The runtime contract is exec form: the command IS the extension
// executable and args are its argv. Routing through a shell would smuggle
// shell semantics (pipes, &&, $ expansion) into a contract that promises
// there are none.
var shellExecutables = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true,
	"cmd": true, "cmd.exe": true, "powershell": true, "powershell.exe": true,
	"pwsh": true, "pwsh.exe": true,
}

// startupFailure carries bounded diagnostics for a sidecar that failed to
// start or hand shake: the stage, elapsed time, and the redacted stderr tail.
// The shape mirrors internal/plugin's startupFailure.
type startupFailure struct {
	Stage   string
	Elapsed time.Duration
	Stderr  string
	Err     error
}

func (e *startupFailure) Error() string {
	if e == nil {
		return "extension sidecar startup failed"
	}
	stage := strings.TrimSpace(e.Stage)
	if stage == "" {
		stage = "unknown"
	}
	msg := fmt.Sprintf("extension sidecar startup %s failed after %s: %s", stage, formatElapsed(e.Elapsed), secrets.RedactError(e.Err))
	if stderr := strings.TrimSpace(e.Stderr); stderr != "" {
		msg += "; stderr: " + stderr
	}
	return msg
}

func (e *startupFailure) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newStartupFailure(stage string, started time.Time, stderr string, err error) error {
	if err == nil {
		return nil
	}
	var existing *startupFailure
	if errors.As(err, &existing) {
		return err
	}
	elapsed := max(time.Since(started), 0)
	return &startupFailure{
		Stage:   strings.TrimSpace(stage),
		Elapsed: elapsed,
		Stderr:  secrets.RedactCredentials(strings.TrimSpace(stderr)),
		Err:     err,
	}
}

func formatElapsed(elapsed time.Duration) string {
	if elapsed < time.Millisecond {
		return elapsed.String()
	}
	return elapsed.Round(time.Millisecond).String()
}

// tailBuffer is a bounded ring holding the most recent stderr bytes. Writes
// never block the child; only the tail is ever surfaced, and only after
// credential redaction.
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

// process is one spawned sidecar OS process with its pipes and tracked
// process-tree handle.
type process struct {
	pluginID string
	cmd      *exec.Cmd
	job      uintptr
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   *tailBuffer

	waitOnce sync.Once
	waitDone chan struct{}
	jobOnce  sync.Once
}

// resolveRuntimeCommand expands ${REASONIX_PLUGIN_ROOT} and enforces the exec
// contract: the resolved command must be an absolute path to the extension
// executable itself, never a relative name (no PATH lookup — the package must
// know exactly what it runs) and never a shell.
func resolveRuntimeCommand(rt *pluginpkg.RuntimeSpec, root string) (string, error) {
	command := strings.TrimSpace(pluginpkg.ExpandRuntimeCommand(rt.Command, root))
	if command == "" {
		return "", errors.New("runtime command is empty after expansion")
	}
	if !filepath.IsAbs(command) {
		return "", fmt.Errorf("runtime command %q is not an absolute path after expansion (use %s to address the installed package)", rt.Command, pluginpkg.PluginRootEnvVar)
	}
	base := strings.ToLower(filepath.Base(command))
	if shellExecutables[base] {
		return "", fmt.Errorf("runtime command %q is a shell; the runtime contract is exec form (the command is the extension executable, args are its argv)", rt.Command)
	}
	return command, nil
}

// runtimeEnv builds the sidecar's environment: the UNFILTERED inherited
// process environment (full-trust contract — see the package doc), the
// manifest's env, and the well-known plugin identity variables. Later entries
// win over earlier ones for duplicate keys (exec.Cmd.Env semantics), so the
// manifest can tune but the identity variables cannot be forged by it.
func runtimeEnv(rt *pluginpkg.RuntimeSpec, pkg pluginpkg.Package, installed pluginpkg.InstalledPlugin) []string {
	env := append([]string(nil), os.Environ()...)
	for key, value := range rt.Env {
		env = append(env, key+"="+value)
	}
	version := strings.TrimSpace(installed.Version)
	if version == "" {
		version = strings.TrimSpace(pkg.Manifest.Version)
	}
	env = append(env,
		envPluginRoot+"="+pkg.Root,
		envPluginName+"="+installed.Name,
		envPluginVersion+"="+version,
	)
	return env
}

// startProcess spawns the sidecar process. It never goes through a shell:
// exec.Command takes the resolved executable and the argv vector directly.
func startProcess(pkg pluginpkg.Package, installed pluginpkg.InstalledPlugin) (*process, error) {
	started := time.Now()
	rt := pkg.Manifest.Runtime
	if rt == nil {
		return nil, fmt.Errorf("plugin %q declares no runtime", installed.Name)
	}
	command, err := resolveRuntimeCommand(rt, pkg.Root)
	if err != nil {
		return nil, newStartupFailure("resolve", started, "", err)
	}
	cmd := exec.Command(command, rt.Args...)
	cmd.Env = runtimeEnv(rt, pkg, installed)
	proc.HideWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, newStartupFailure("pipes", started, "", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, newStartupFailure("pipes", started, "", err)
	}
	stderr := &tailBuffer{limit: stderrTailBytes}
	cmd.Stderr = stderr

	job, err := proc.StartTracked(cmd)
	if err != nil {
		return nil, newStartupFailure("spawn", started, stderr.String(), err)
	}
	p := &process{
		pluginID: installed.Name,
		cmd:      cmd,
		job:      job,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		waitDone: make(chan struct{}),
	}
	return p, nil
}

// wait blocks until the process exits, exactly once; later callers observe
// the same completed wait. Safe to abandon: the first caller owns cmd.Wait.
func (p *process) wait() {
	p.waitOnce.Do(func() {
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Wait()
		}
		p.finishJob()
		close(p.waitDone)
	})
}

// finishJob releases the Windows Job Object once the process is known to be
// gone (a no-op off Windows and after KillTracked already released it).
func (p *process) finishJob() {
	p.jobOnce.Do(func() { proc.FinishTracked(p.job) })
}

// kill terminates the whole process tree.
func (p *process) kill() {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	proc.KillTracked(p.cmd, p.job)
	p.finishJob()
}

// close stops the sidecar with the bounded sequence: close stdin, grant a
// short EOF grace for protocol-aware processes, kill the tree, and wait a
// bounded time for the reap. It is idempotent and never blocks longer than
// gracefulCloseWaitBudget + closeWaitBudget.
func (p *process) close() {
	if p.stdin != nil {
		_ = p.stdin.Close()
	}
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}
	if waitFinishedWithinBudget(p.wait, gracefulCloseWaitBudget) {
		return
	}
	p.kill()
	waitWithBudget(p.wait, closeWaitBudget)
}

func waitWithBudget(wait func(), budget time.Duration) {
	_ = waitFinishedWithinBudget(wait, budget)
}

func waitFinishedWithinBudget(wait func(), budget time.Duration) bool {
	done := make(chan struct{})
	go func() { wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(budget):
		return false
	}
}
