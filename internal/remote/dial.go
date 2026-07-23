package remote

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/netclient"
)

// dialConfig carries everything a single dial (or one hop of a jump chain)
// needs. It is assembled by Client.Start from Options.
type dialConfig struct {
	host        ResolvedHost
	auth        *AuthOptions // target auth (holds the target's credentials + cache)
	resolveHop  func(string) (ResolvedHost, *AuthOptions, error)
	hostKeys    *HostKeyPolicy
	dialer      netclient.StreamDialer // first-hop transport; nil => direct
	dialTimeout time.Duration
}

// hopAuthFor returns the auth to use for a jump host. It never carries the
// target's Password/Passphrase closures: a jump host must not be authenticated
// with the target's stored credentials.
func (cfg dialConfig) hopAuthFor(hop ResolvedHost) *AuthOptions {
	return &AuthOptions{SecretPrompt: cfg.auth.SecretPrompt, DisableAgent: cfg.auth.DisableAgent}
}

func (cfg dialConfig) resolvedHop(raw string) (ResolvedHost, *AuthOptions, error) {
	if cfg.resolveHop != nil {
		return cfg.resolveHop(raw)
	}
	userName, hostName, port, err := ParseTarget(raw)
	if err != nil {
		return ResolvedHost{}, nil, err
	}
	hop := ResolvedHost{Name: raw, HostName: hostName, Port: port, User: userName}
	applyHostDefaults(&hop)
	return hop, cfg.hopAuthFor(hop), nil
}

// dialSSH establishes an *ssh.Client to cfg.host, walking any ProxyJump chain
// left-to-right. The netclient proxy (cfg.dialer) applies only to the first
// hop, matching OpenSSH semantics; subsequent hops are dialed through the
// preceding hop's SSH connection. Each hop's host key is verified.
//
// It returns the target client and the ordered list of intermediary clients
// (jump hosts) so the caller can close them when the target connection ends.
func dialSSH(ctx context.Context, cfg dialConfig) (*ssh.Client, []*ssh.Client, error) {
	timeout := cfg.dialTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	base := cfg.dialer
	if base == nil {
		base = netclient.DialerFunc((&net.Dialer{Timeout: timeout}).DialContext)
	}

	var hops []*ssh.Client
	// dialThrough dials addr using either the base transport (first hop) or the
	// previous SSH hop's context-aware Dial.
	dialThrough := func(prev *ssh.Client, addr string) (net.Conn, error) {
		dctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if prev == nil {
			return base.DialContext(dctx, "tcp", addr)
		}
		return prev.DialContext(dctx, "tcp", addr)
	}

	var prev *ssh.Client
	// Resolve and connect each jump host in order.
	for i, jump := range cfg.host.ProxyJump {
		hop, hopAuth, err := cfg.resolvedHop(jump)
		if err != nil {
			closeAll(hops)
			return nil, nil, fmt.Errorf("proxy jump %q: %w", jump, err)
		}
		conn, derr := dialThrough(prev, hop.Addr())
		if derr != nil {
			closeAll(hops)
			return nil, nil, fmt.Errorf("proxy jump %d (%s): %w", i+1, hop.Label(), derr)
		}
		// Each jump host authenticates with its own credential-free auth, so the
		// target's password_env is never sent upstream to a jump host.
		client, cerr := newSSHClient(ctx, conn, hop, hopAuth, cfg.hostKeys, timeout)
		if cerr != nil {
			closeAll(hops)
			return nil, nil, fmt.Errorf("proxy jump %d (%s): %w", i+1, hop.Label(), cerr)
		}
		hops = append(hops, client)
		prev = client
	}

	conn, err := dialThrough(prev, cfg.host.Addr())
	if err != nil {
		closeAll(hops)
		return nil, nil, fmt.Errorf("dial %s: %w", cfg.host.Label(), err)
	}
	target, err := newSSHClient(ctx, conn, cfg.host, cfg.auth, cfg.hostKeys, timeout)
	if err != nil {
		closeAll(hops)
		return nil, nil, err
	}
	return target, hops, nil
}

// newSSHClient performs the SSH handshake over an established conn. It bounds
// the handshake with a deadline (ssh.ClientConfig.Timeout only covers the TCP
// dial, not the version/key exchange, so a host that accepts TCP but never
// sends a banner would otherwise hang NewClientConn — and Close — forever).
func newSSHClient(ctx context.Context, conn net.Conn, host ResolvedHost, auth *AuthOptions, hostKeys *HostKeyPolicy, timeout time.Duration) (*ssh.Client, error) {
	methods, authCallback, cleanupAuth, err := buildAuthMethods(ctx, host, auth)
	if err != nil {
		conn.Close()
		return nil, err
	}
	defer cleanupAuth()
	hkCallback, err := hostKeys.Callback(ctx, host.Label())
	if err != nil {
		conn.Close()
		return nil, err
	}
	hostKeyAlgorithms, err := hostKeys.HostKeyAlgorithms(host.Addr(), conn.RemoteAddr())
	if err != nil {
		conn.Close()
		return nil, err
	}
	clientCfg := &ssh.ClientConfig{
		User:              host.User,
		Auth:              methods,
		AuthCallback:      authCallback,
		HostKeyCallback:   hkCallback,
		HostKeyAlgorithms: hostKeyAlgorithms,
		Timeout:           timeout,
	}
	// Bound the handshake even for ProxyJump channel connections, whose
	// SetDeadline method returns "deadline not supported". A watcher closes the
	// connection on timeout/cancellation; the acknowledgement prevents a late
	// watcher from closing a successfully established client.
	hsCtx, cancel := context.WithTimeout(ctx, handshakeTimeout(timeout))
	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-hsCtx.Done():
			_ = conn.Close()
		case <-stopWatch:
		}
	}()
	if deadline, ok := hsCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, host.Addr(), clientCfg)
	close(stopWatch)
	<-watchDone
	cancel()
	if err != nil {
		conn.Close()
		return nil, classifyDialError(err)
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(c, chans, reqs), nil
}

func handshakeTimeout(dialTimeout time.Duration) time.Duration {
	if dialTimeout <= 0 {
		return 15 * time.Second
	}
	return dialTimeout
}

func closeAll(clients []*ssh.Client) {
	for i := len(clients) - 1; i >= 0; i-- {
		_ = clients[i].Close()
	}
}
