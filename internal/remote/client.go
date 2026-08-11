package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/netclient"
	"reasonix/internal/remote/forward"
	"reasonix/internal/remote/sftpfs"
)

// Options configures a Client. Host, Auth, and HostKeys are required; the rest
// default sensibly.
type Options struct {
	Host        ResolvedHost
	Auth        AuthOptions
	JumpHosts   []JumpHostOptions // resolved ProxyJump hosts in chain order
	HostKeys    *HostKeyPolicy
	Dialer      netclient.StreamDialer // first-hop transport; nil => direct
	DialTimeout time.Duration          // default 15s
	Keepalive   KeepalivePolicy
	Backoff     BackoffPolicy
	Clock       Clock // nil => real clock
	Rand        *rand.Rand
}

// JumpHostOptions binds one resolved ProxyJump host to credentials owned by
// that hop. Target credentials are never inherited implicitly.
type JumpHostOptions struct {
	Host ResolvedHost
	Auth AuthOptions
}

// Client is a supervised SSH connection: it dials, verifies the host key,
// attaches forwards, keeps the link alive, and reconnects with backoff. It
// shares one SFTP session and one forward.Set across its lifetime.
type Client struct {
	opts     Options
	clock    Clock
	rng      *rand.Rand
	hub      *statusHub
	forwards *forward.Set

	mu          sync.Mutex
	ssh         *ssh.Client
	hops        []*ssh.Client
	sftp        *sftpfs.FS
	generation  uint64 // bumps on every (re)connect; SFTP handles carry it
	status      Status
	closed      bool
	hopHosts    map[string]ResolvedHost
	hopAuths    map[string]*AuthOptions // fallback auth cache, keyed by user+addr
	hopRawAuths map[string]*AuthOptions // configured auth by alias; aliases may share an endpoint

	cancel context.CancelFunc
	done   chan struct{}
}

// hopAuthFor returns a persistent AuthOptions for a jump host. It deliberately
// omits the target's Password/Passphrase closures and gives each jump host its
// own secret cache, so the target's password_env is never sent to a jump host
// and one hop's typed secret is never reused for another. The instance persists
// for the Client's lifetime so reconnects do not re-prompt for jump secrets.
func (c *Client) hopAuthFor(hop ResolvedHost) *AuthOptions {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hopAuths == nil {
		c.hopAuths = map[string]*AuthOptions{}
	}
	key := hopAuthKey(hop)
	if a, ok := c.hopAuths[key]; ok {
		return a
	}
	a := &AuthOptions{
		SecretPrompt: c.opts.Auth.SecretPrompt,
		DisableAgent: c.opts.Auth.DisableAgent,
	}
	c.hopAuths[key] = a
	return a
}

func hopAuthKey(hop ResolvedHost) string { return hop.User + "\x00" + hop.Addr() }

// resolveHop returns the pre-resolved config/ssh_config host when the assembly
// layer supplied one, with a conservative ad-hoc fallback for low-level users.
func (c *Client) resolveHop(raw string) (ResolvedHost, *AuthOptions, error) {
	c.mu.Lock()
	hop, ok := c.hopHosts[raw]
	auth := c.hopRawAuths[raw]
	c.mu.Unlock()
	if ok {
		return hop, auth, nil
	}
	userName, hostName, port, err := ParseTarget(raw)
	if err != nil {
		return ResolvedHost{}, nil, err
	}
	hop = ResolvedHost{Name: raw, HostName: hostName, Port: port, User: userName}
	applyHostDefaults(&hop)
	return hop, c.hopAuthFor(hop), nil
}

// New creates a Client. It does not dial; call Start.
func New(opts Options) (*Client, error) {
	if opts.Host.HostName == "" {
		return nil, errors.New("remote: Options.Host has no hostname")
	}
	if opts.HostKeys == nil {
		opts.HostKeys = &HostKeyPolicy{}
	}
	clock := opts.Clock
	if clock == nil {
		clock = realClock{}
	}
	rng := opts.Rand
	if rng == nil {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	c := &Client{
		opts:        opts,
		clock:       clock,
		rng:         rng,
		hub:         newStatusHub(),
		status:      StatusIdle,
		done:        make(chan struct{}),
		hopHosts:    map[string]ResolvedHost{},
		hopAuths:    map[string]*AuthOptions{},
		hopRawAuths: map[string]*AuthOptions{},
	}
	if len(opts.JumpHosts) > 0 && len(opts.JumpHosts) != len(opts.Host.ProxyJump) {
		return nil, fmt.Errorf("remote: %d resolved jump hosts for %d ProxyJump entries", len(opts.JumpHosts), len(opts.Host.ProxyJump))
	}
	for i, jump := range opts.JumpHosts {
		if jump.Host.HostName == "" {
			return nil, fmt.Errorf("remote: ProxyJump %d has no hostname", i+1)
		}
		raw := opts.Host.ProxyJump[i]
		auth := jump.Auth
		c.hopHosts[raw] = jump.Host
		c.hopRawAuths[raw] = &auth
	}
	c.forwards = forward.NewSet(nil)
	return c, nil
}

// Subscribe registers a status callback; it receives the current event
// immediately and every subsequent transition. Callbacks must not block.
func (c *Client) Subscribe(fn func(StatusEvent)) (cancel func()) {
	return c.hub.subscribe(fn)
}

// Status returns the last published status event.
func (c *Client) Status() StatusEvent { return c.hub.current() }

// Forwards returns the port-forward registry bound to this client's lifecycle.
func (c *Client) Forwards() *forward.Set { return c.forwards }

// SSH returns the current ssh client, or ErrNotConnected while down.
func (c *Client) SSH() (*ssh.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ssh == nil {
		return nil, ErrNotConnected
	}
	return c.ssh, nil
}

// SFTP returns the shared SFTP handle, lazily created for the current
// connection. Handles obtained before a reconnect return ErrDisconnected on
// use; re-fetch via SFTP.
func (c *Client) SFTP() (*sftpfs.FS, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ssh == nil {
		return nil, ErrNotConnected
	}
	if c.sftp != nil {
		return c.sftp, nil
	}
	fsys, err := sftpfs.New(c.ssh)
	if err != nil {
		return nil, err
	}
	c.sftp = fsys
	return c.sftp, nil
}

// ExecResult is the outcome of a one-shot remote command.
type ExecResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Exec runs cmd via `sh -c` on a fresh session and collects its output.
func (c *Client) Exec(ctx context.Context, cmd string) (ExecResult, error) {
	cl, err := c.SSH()
	if err != nil {
		return ExecResult{}, err
	}
	type res struct {
		out ExecResult
		err error
	}
	ch := make(chan res, 1)
	go func() {
		sess, serr := cl.NewSession()
		if serr != nil {
			ch <- res{err: serr}
			return
		}
		defer sess.Close()
		var stdout, stderr bytes.Buffer
		sess.Stdout = &stdout
		sess.Stderr = &stderr
		runErr := sess.Run(cmd)
		out := ExecResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
		if runErr != nil {
			var ee *ssh.ExitError
			if errors.As(runErr, &ee) {
				out.ExitCode = ee.ExitStatus()
				ch <- res{out: out}
				return
			}
			ch <- res{out: out, err: runErr}
			return
		}
		ch <- res{out: out}
	}()
	select {
	case <-ctx.Done():
		return ExecResult{}, ctx.Err()
	case r := <-ch:
		return r.out, r.err
	}
}

// Start dials and blocks until the first Connected (returns nil) or an
// unrecoverable error / ctx cancellation (returns the error). The supervisor
// keeps running after a successful Start; call Close to stop it.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("remote: client closed")
	}
	superCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.mu.Unlock()

	firstResult := make(chan error, 1)
	go c.supervise(superCtx, firstResult)

	select {
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	case err := <-firstResult:
		return err
	}
}

// Close stops the supervisor and releases the connection, forwards, and SFTP.
func (c *Client) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	cancel := c.cancel
	c.mu.Unlock()

	if cancel != nil {
		cancel()
		<-c.done
	} else {
		c.teardownConn()
		c.publish(StatusStopped, 0, nil)
	}
	c.forwards.Close()
	return nil
}

// supervise is the single goroutine that owns the connection lifecycle.
func (c *Client) supervise(ctx context.Context, firstResult chan<- error) {
	defer close(c.done)
	firstDone := false
	sendFirst := func(err error) {
		if !firstDone {
			firstDone = true
			firstResult <- err
		}
	}

	attempt := 0
	for {
		if attempt == 0 {
			c.publish(StatusConnecting, 0, nil)
		} else {
			c.publish(StatusReconnecting, attempt, nil)
		}

		cl, hops, err := dialSSH(ctx, dialConfig{
			host:        c.opts.Host,
			auth:        &c.opts.Auth,
			resolveHop:  c.resolveHop,
			hostKeys:    c.opts.HostKeys,
			dialer:      c.opts.Dialer,
			dialTimeout: c.opts.DialTimeout,
		})
		if err != nil {
			if ctx.Err() != nil {
				c.publish(StatusStopped, attempt, ctx.Err())
				sendFirst(ctx.Err())
				return
			}
			if errors.Is(err, ErrAuthFailed) || errors.Is(err, ErrHostKeyMismatch) || errors.Is(err, ErrHostKeyRejected) {
				// Unrecoverable: stop rather than loop.
				c.publish(StatusStopped, attempt, err)
				sendFirst(err)
				return
			}
			if !firstDone {
				// The very first connect failed on a transient error; report it
				// so callers get immediate feedback instead of a silent retry.
				c.publish(StatusStopped, attempt, err)
				sendFirst(err)
				return
			}
			attempt++
			if !c.sleepBackoff(ctx, attempt) {
				c.publish(StatusStopped, attempt, ctx.Err())
				return
			}
			continue
		}

		// Connected. Install the connection and attach forwards.
		c.installConn(cl, hops)
		if attachErr := c.forwards.Attach(cl); attachErr != nil {
			c.publish(StatusDegraded, attempt, attachErr)
		} else {
			c.publish(StatusConnected, attempt, nil)
		}
		sendFirst(nil)

		// Block until the connection dies, ctx ends, or Close.
		reason := c.watch(ctx, cl)
		c.forwards.Detach()
		c.teardownConn()

		if ctx.Err() != nil || reason == watchClosed {
			c.publish(StatusStopped, attempt, ctx.Err())
			return
		}
		// Connection dropped: reconnect with backoff.
		attempt++
		if !c.sleepBackoff(ctx, attempt) {
			c.publish(StatusStopped, attempt, ctx.Err())
			return
		}
	}
}

type watchReason int

const (
	watchConnLost watchReason = iota
	watchClosed
)

// watch runs the keepalive loop and returns when the connection dies or ctx
// ends.
func (c *Client) watch(ctx context.Context, cl *ssh.Client) watchReason {
	closed := make(chan struct{})
	go func() {
		_ = cl.Wait() // always non-nil at disconnect; the signal is the return itself
		close(closed)
	}()

	interval := c.opts.Keepalive.interval()
	misses := 0
	for {
		var tick <-chan time.Time
		if interval > 0 {
			tick = c.clock.After(interval)
		}
		select {
		case <-ctx.Done():
			return watchClosed
		case <-closed:
			return watchConnLost
		case <-tick:
			if c.keepaliveOK(cl) {
				misses = 0
				continue
			}
			misses++
			if misses >= c.opts.Keepalive.maxMisses() {
				return watchConnLost
			}
		}
	}
}

func (c *Client) keepaliveOK(cl *ssh.Client) bool {
	type res struct{ err error }
	ch := make(chan res, 1)
	go func() {
		_, _, err := cl.SendRequest("keepalive@openssh.com", true, nil)
		ch <- res{err}
	}()
	select {
	case <-c.clock.After(c.opts.Keepalive.timeout()):
		return false
	case r := <-ch:
		return r.err == nil
	}
}

// sleepBackoff waits a full-jitter backoff for attempt, returning false if ctx
// ended during the wait.
func (c *Client) sleepBackoff(ctx context.Context, attempt int) bool {
	ceil := c.opts.Backoff.delay(attempt - 1)
	d := time.Duration(c.rng.Int63n(int64(ceil) + 1))
	select {
	case <-ctx.Done():
		return false
	case <-c.clock.After(d):
		return true
	}
}

func (c *Client) installConn(cl *ssh.Client, hops []*ssh.Client) {
	c.mu.Lock()
	c.ssh = cl
	c.hops = hops
	c.sftp = nil
	c.generation++
	c.mu.Unlock()
}

func (c *Client) teardownConn() {
	c.mu.Lock()
	sftpH, cl, hops := c.sftp, c.ssh, c.hops
	c.sftp, c.ssh, c.hops = nil, nil, nil
	c.mu.Unlock()
	if sftpH != nil {
		_ = sftpH.Close()
	}
	if cl != nil {
		_ = cl.Close()
	}
	closeAll(hops)
}

func (c *Client) publish(s Status, attempt int, err error) {
	c.mu.Lock()
	c.status = s
	c.mu.Unlock()
	c.hub.publish(StatusEvent{
		Host:    c.opts.Host.Name,
		Status:  s,
		Attempt: attempt,
		Err:     err,
		At:      c.clock.Now(),
	})
}
