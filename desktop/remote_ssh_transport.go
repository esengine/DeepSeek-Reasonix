package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	remoteclient "reasonix/internal/remote/client"
)

const defaultRemoteSSHStderrLimit = 64 << 10

type RemoteSSHFailureCode string

const (
	RemoteSSHCLINotFound    RemoteSSHFailureCode = "CLI_NOT_FOUND"
	RemoteSSHAuthFailed     RemoteSSHFailureCode = "AUTH_FAILED"
	RemoteSSHHostKeyChanged RemoteSSHFailureCode = "HOST_KEY_CHANGED"
	RemoteSSHTransportLost  RemoteSSHFailureCode = "TRANSPORT_LOST"
)

type RemoteSSHFailureStage string

const (
	RemoteSSHStageStart          RemoteSSHFailureStage = "ssh_start"
	RemoteSSHStageHostKey        RemoteSSHFailureStage = "host_key"
	RemoteSSHStageAuthentication RemoteSSHFailureStage = "authentication"
	RemoteSSHStageBootstrap      RemoteSSHFailureStage = "bootstrap"
	RemoteSSHStageTransport      RemoteSSHFailureStage = "transport"
)

// RemoteSSHConnectionFailure is safe for a connection view. It intentionally
// contains neither raw stderr nor command output; callers branch only on Code.
type RemoteSSHConnectionFailure struct {
	Code      RemoteSSHFailureCode
	Stage     RemoteSSHFailureStage
	Retryable bool
	Message   string
}

func (e *RemoteSSHConnectionFailure) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// RemoteSSHDiagnostic proves stderr was drained and bounded without exposing it
// to RPC/UI data. Category is the structured failure code when one was found.
type RemoteSSHDiagnostic struct {
	Category      RemoteSSHFailureCode
	CapturedBytes int
	TotalBytes    int64
	Truncated     bool
}

type remoteSSHCommandContext func(context.Context, string, ...string) *exec.Cmd

// RemoteSSHTransportFactory starts the system OpenSSH client directly. No
// shell participates, and the only remote command is the frozen attach entry.
type RemoteSSHTransportFactory struct {
	SSHPath        string
	SSHConfigPath  string
	AskPass        *RemoteAskPassBroker
	AskPassHelper  string
	StderrLimit    int
	commandContext remoteSSHCommandContext // test seam; nil uses exec.CommandContext
}

func (f *RemoteSSHTransportFactory) Start(ctx context.Context, alias string) (*RemoteSSHTransport, error) {
	if err := ValidateRemoteHostAlias(alias); err != nil {
		return nil, err
	}
	args := make([]string, 0, 22)
	if f.SSHConfigPath != "" {
		if err := validateRemoteSSHConfigPath(f.SSHConfigPath); err != nil {
			return nil, err
		}
		args = append(args, "-F", f.SSHConfigPath)
	}
	args = append(args, remoteSSHPolicyArgs()...)
	args = append(args, "--", alias, "reasonix", "remote", "attach", "--stdio")
	return f.start(ctx, args)
}

func (f *RemoteSSHTransportFactory) StartDirect(ctx context.Context, destination string, port int) (*RemoteSSHTransport, error) {
	target, err := ParseRemoteSSHDirectDestination(destination)
	if err != nil {
		return nil, err
	}
	if err := ValidateRemoteSSHPort(port); err != nil {
		return nil, err
	}
	args := make([]string, 0, 24)
	args = append(args, remoteSSHPolicyArgs()...)
	args = append(args,
		"-l", target.Username,
		"-p", strconv.Itoa(port),
		"--", target.Host,
		"reasonix", "remote", "attach", "--stdio",
	)
	return f.start(ctx, args)
}

func remoteSSHPolicyArgs() []string {
	return []string{
		"-T",
		"-o", "RequestTTY=no",
		"-o", "StrictHostKeyChecking=ask",
		"-o", "ClearAllForwardings=yes",
		"-o", "PermitLocalCommand=no",
		"-o", "RemoteCommand=none",
	}
}

func (f *RemoteSSHTransportFactory) start(ctx context.Context, args []string) (*RemoteSSHTransport, error) {
	if ctx == nil {
		return nil, errors.New("SSH context is required")
	}
	sshPath, err := resolveRemoteSSHExecutable(f.SSHPath)
	if err != nil {
		return nil, err
	}
	builder := f.commandContext
	if builder == nil {
		builder = exec.CommandContext
	}
	cmd := builder(ctx, sshPath, args...)
	if cmd == nil {
		return nil, errors.New("OpenSSH command builder returned nil")
	}
	configureRemoteSSHProcess(cmd)
	cmd.Env = sanitizeRemoteSSHEnvironment(os.Environ())
	if f.AskPass != nil {
		overrides, err := f.AskPass.SSHEnvironment(f.AskPassHelper)
		if err != nil {
			return nil, err
		}
		overrides = append(overrides, "DISPLAY=reasonix-askpass")
		cmd.Env = mergeRemoteSSHEnvironment(cmd.Env, overrides)
	}
	stderrLimit := f.StderrLimit
	if stderrLimit <= 0 {
		stderrLimit = defaultRemoteSSHStderrLimit
	}
	stderr := newBoundedRemoteSSHStderr(stderrLimit)
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open OpenSSH stdin: %w", err)
	}
	stdout, stdoutWriter := io.Pipe()
	cmd.Stdout = stdoutWriter
	transport := &RemoteSSHTransport{
		cmd:          cmd,
		stdin:        stdin,
		stdout:       stdout,
		stdoutWriter: stdoutWriter,
		stderr:       stderr,
		ctx:          ctx,
		processWait:  make(chan error, 1),
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stdoutWriter.Close()
		failure := classifyRemoteSSHStartFailure(err)
		transport.waitErr = failure
		transport.waitDone = true
		return nil, failure
	}
	go func() {
		err := cmd.Wait()
		transport.processExited.Store(true)
		_ = stdoutWriter.Close()
		transport.processWait <- err
	}()
	return transport, nil
}

// RemoteSSHHostTransportFactory binds the immutable saved Host connection to the
// transport-neutral client.TransportFactory surface. It contains no reconnect,
// lease, Runtime or product business rules.
type RemoteSSHHostTransportFactory struct {
	SSH         *RemoteSSHTransportFactory
	EntryID     string
	Mode        RemoteHostConnectionMode
	Destination string
	Port        int
	Alias       string
}

func NewRemoteSSHHostTransportFactory(factory *RemoteSSHTransportFactory, entry RemoteHostEntry) (RemoteSSHHostTransportFactory, error) {
	if factory == nil {
		return RemoteSSHHostTransportFactory{}, errors.New("SSH transport factory is required")
	}
	if err := validateRemoteHostEntry(entry); err != nil {
		return RemoteSSHHostTransportFactory{}, err
	}
	boundFactory := *factory
	boundFactory.SSHConfigPath = entry.SSHConfigPath
	return RemoteSSHHostTransportFactory{
		SSH: &boundFactory, EntryID: entry.ID, Mode: entry.Mode,
		Destination: entry.Destination, Port: entry.Port, Alias: entry.Alias,
	}, nil
}

func (f RemoteSSHHostTransportFactory) Open(ctx context.Context) (remoteclient.Transport, error) {
	if f.SSH == nil {
		return nil, errors.New("SSH transport factory is required")
	}
	if err := ValidateRemoteHostEntryID(f.EntryID); err != nil {
		return nil, err
	}
	switch f.Mode {
	case RemoteHostConnectionDirect:
		return f.SSH.StartDirect(ctx, f.Destination, f.Port)
	case RemoteHostConnectionConfig:
		return f.SSH.Start(ctx, f.Alias)
	default:
		return nil, errors.New("remote Host connection mode is invalid")
	}
}

// RemoteSSHTransport exposes protocol-only stdin/stdout. Authentication and
// diagnostics use separate AskPass/stderr paths and can never corrupt NDJSON.
type RemoteSSHTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *io.PipeReader
	// stdoutWriter is owned by the exec copy goroutine and closed after Wait.
	stdoutWriter *io.PipeWriter
	stderr       *boundedRemoteSSHStderr
	ctx          context.Context
	processWait  chan error

	bootstrapStarted atomic.Bool
	processExited    atomic.Bool
	waitMu           sync.Mutex
	waitDone         bool
	waitErr          error
	closeOnce        sync.Once
}

func (t *RemoteSSHTransport) Read(p []byte) (int, error) {
	if t == nil || t.stdout == nil {
		return 0, io.ErrClosedPipe
	}
	n, err := t.stdout.Read(p)
	if n > 0 {
		t.MarkBootstrapStarted()
	}
	if n == 0 && errors.Is(err, io.EOF) {
		if waitErr := t.Wait(); waitErr != nil {
			return 0, waitErr
		}
	}
	return n, err
}

func (t *RemoteSSHTransport) Write(p []byte) (int, error) {
	if t == nil || t.stdin == nil {
		return 0, io.ErrClosedPipe
	}
	return t.stdin.Write(p)
}

func (t *RemoteSSHTransport) Stdin() io.WriteCloser { return t.stdin }

func (t *RemoteSSHTransport) Stdout() io.ReadCloser { return t.stdout }

// MarkBootstrapStarted prevents a later remote diagnostic line from being
// misclassified as "CLI missing" after valid attach protocol bytes were seen.
func (t *RemoteSSHTransport) MarkBootstrapStarted() { t.bootstrapStarted.Store(true) }

func (t *RemoteSSHTransport) Wait() error {
	if t == nil || t.cmd == nil {
		return errors.New("nil OpenSSH transport")
	}
	t.waitMu.Lock()
	defer t.waitMu.Unlock()
	if t.waitDone {
		return t.waitErr
	}
	err := <-t.processWait
	t.waitDone = true
	if err == nil {
		t.waitErr = nil
		return nil
	}
	raw, _, _ := t.stderr.snapshot()
	t.waitErr = classifyRemoteSSHExit(raw, t.bootstrapStarted.Load(), t.ctx.Err())
	return t.waitErr
}

func (t *RemoteSSHTransport) Diagnostic() RemoteSSHDiagnostic {
	if t == nil || t.stderr == nil {
		return RemoteSSHDiagnostic{}
	}
	raw, total, truncated := t.stderr.snapshot()
	if total == 0 {
		return RemoteSSHDiagnostic{}
	}
	failure := classifyRemoteSSHExit(raw, t.bootstrapStarted.Load(), nil)
	return RemoteSSHDiagnostic{
		Category:      failure.Code,
		CapturedBytes: len(raw),
		TotalBytes:    total,
		Truncated:     truncated,
	}
}

func (t *RemoteSSHTransport) Close() error {
	if t == nil {
		return nil
	}
	var closeErr error
	t.closeOnce.Do(func() {
		if t.stdin != nil {
			_ = t.stdin.Close()
		}
		if t.cmd != nil && t.cmd.Process != nil && !t.processExited.Load() {
			closeErr = terminateRemoteSSHProcess(t.cmd)
		}
		if t.stdout != nil {
			_ = t.stdout.Close()
		}
	})
	return closeErr
}

func classifyRemoteSSHStartFailure(err error) *RemoteSSHConnectionFailure {
	message := "Unable to start the system OpenSSH client."
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		message = "The system OpenSSH client was not found."
	}
	return &RemoteSSHConnectionFailure{
		Code: RemoteSSHCLINotFound, Stage: RemoteSSHStageStart,
		Retryable: false, Message: message,
	}
}

func classifyRemoteSSHExit(stderr []byte, bootstrapStarted bool, contextErr error) *RemoteSSHConnectionFailure {
	lower := strings.ToLower(string(stderr))
	if containsAnyRemoteSSHDiagnostic(lower,
		"remote host identification has changed",
		"possible dns spoofing detected",
		"host key verification failed",
		"offending ed25519 key", "offending ecdsa key", "offending rsa key") {
		return &RemoteSSHConnectionFailure{
			Code: RemoteSSHHostKeyChanged, Stage: RemoteSSHStageHostKey,
			Retryable: false, Message: "The SSH Host key changed. Verify and repair known_hosts before reconnecting.",
		}
	}
	if !bootstrapStarted && containsAnyRemoteSSHDiagnostic(lower,
		"reasonix: command not found", "reasonix: not found",
		"'reasonix' is not recognized", "exec: reasonix: not found") {
		return &RemoteSSHConnectionFailure{
			Code: RemoteSSHCLINotFound, Stage: RemoteSSHStageBootstrap,
			Retryable: false, Message: "The Reasonix CLI was not found on the remote Host.",
		}
	}
	if containsAnyRemoteSSHDiagnostic(lower,
		"permission denied", "authentication failed", "too many authentication failures",
		"no supported authentication methods", "no more authentication methods to try") {
		return &RemoteSSHConnectionFailure{
			Code: RemoteSSHAuthFailed, Stage: RemoteSSHStageAuthentication,
			Retryable: true, Message: "SSH authentication failed.",
		}
	}
	if contextErr != nil {
		return &RemoteSSHConnectionFailure{
			Code: RemoteSSHTransportLost, Stage: RemoteSSHStageTransport,
			Retryable: true, Message: "The SSH connection was cancelled or timed out.",
		}
	}
	return &RemoteSSHConnectionFailure{
		Code: RemoteSSHTransportLost, Stage: RemoteSSHStageTransport,
		Retryable: true, Message: "The SSH transport closed before Remote completed.",
	}
}

func containsAnyRemoteSSHDiagnostic(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func sanitizeRemoteSSHEnvironment(base []string) []string {
	blocked := map[string]struct{}{
		"SSH_ASKPASS": {}, "SSH_ASKPASS_REQUIRE": {},
		remoteAskPassModeEnv: {}, remoteAskPassEndpointEnv: {},
		remoteAskPassKeyEnv: {}, remoteAskPassDeadlineEnv: {},
	}
	result := make([]string, 0, len(base))
	for _, item := range base {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		remove := false
		for blockedKey := range blocked {
			if key == blockedKey || (runtime.GOOS == "windows" && strings.EqualFold(key, blockedKey)) {
				remove = true
				break
			}
		}
		if !remove {
			result = append(result, item)
		}
	}
	return result
}

func mergeRemoteSSHEnvironment(base, overrides []string) []string {
	for _, override := range overrides {
		key, _, ok := strings.Cut(override, "=")
		if !ok {
			continue
		}
		filtered := base[:0]
		for _, item := range base {
			existingKey, _, exists := strings.Cut(item, "=")
			if exists && (existingKey == key || (runtime.GOOS == "windows" && strings.EqualFold(existingKey, key))) {
				continue
			}
			filtered = append(filtered, item)
		}
		base = append(filtered, override)
	}
	return base
}

type boundedRemoteSSHStderr struct {
	mu        sync.Mutex
	limit     int
	headLimit int
	head      []byte
	tail      []byte
	total     int64
}

func newBoundedRemoteSSHStderr(limit int) *boundedRemoteSSHStderr {
	if limit <= 0 {
		limit = defaultRemoteSSHStderrLimit
	}
	headLimit := (limit + 1) / 2
	return &boundedRemoteSSHStderr{
		limit: limit, headLimit: headLimit,
		head: make([]byte, 0, headLimit), tail: make([]byte, 0, limit-headLimit),
	}
}

func (b *boundedRemoteSSHStderr) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total += int64(len(p))
	originalLength := len(p)
	if len(b.head) < b.headLimit {
		take := min(len(p), b.headLimit-len(b.head))
		b.head = append(b.head, p[:take]...)
		p = p[take:]
	}
	tailLimit := b.limit - b.headLimit
	if tailLimit == 0 || len(p) == 0 {
		return originalLength, nil
	}
	if len(p) >= tailLimit {
		b.tail = append(b.tail[:0], p[len(p)-tailLimit:]...)
		return originalLength, nil
	}
	overflow := len(b.tail) + len(p) - tailLimit
	if overflow > 0 {
		copy(b.tail, b.tail[overflow:])
		b.tail = b.tail[:len(b.tail)-overflow]
	}
	b.tail = append(b.tail, p...)
	return originalLength, nil
}

func (b *boundedRemoteSSHStderr) snapshot() ([]byte, int64, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	copyOfData := make([]byte, 0, len(b.head)+len(b.tail))
	copyOfData = append(copyOfData, b.head...)
	copyOfData = append(copyOfData, b.tail...)
	return copyOfData, b.total, b.total > int64(len(copyOfData))
}

var _ io.Writer = (*boundedRemoteSSHStderr)(nil)
var _ remoteclient.Transport = (*RemoteSSHTransport)(nil)
var _ remoteclient.TransportFactory = RemoteSSHHostTransportFactory{}

// Keep this package's default cancellation delay bounded even if a caller
// forgets to close protocol pipes after cancelling its target generation.
const remoteSSHWaitDelay = 2 * time.Second
