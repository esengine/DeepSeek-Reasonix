package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/config"
	"reasonix/internal/netclient"
	"reasonix/internal/remote"
	"reasonix/internal/remote/workbench/transport"
)

// newWorkbenchSSHFactory returns a transport factory for the workbench path.
// Windows uses system OpenSSH + AskPass + Job Object; other platforms use Go SSH
// stdio running `reasonix remote attach-workspace --stdio`.
func newWorkbenchSSHFactory(entry config.RemoteHostEntry, askPassHandler RemoteAskPassHandler) (transport.Factory, error) {
	if runtime.GOOS == "windows" {
		return newWindowsWorkbenchSSHFactory(entry, askPassHandler)
	}
	return newGoSSHWorkbenchFactory(entry, askPassHandler)
}

type windowsWorkbenchSSHFactory struct {
	entry      config.RemoteHostEntry
	boundEntry RemoteHostEntry
	helperPath string
	handler    RemoteAskPassHandler
	mu         sync.Mutex
	transport  *RemoteSSHTransport
}

func newWindowsWorkbenchSSHFactory(entry config.RemoteHostEntry, handler RemoteAskPassHandler) (transport.Factory, error) {
	if handler == nil {
		return nil, fmt.Errorf("AskPass handler is required")
	}
	hostEntry, err := mapConfigHostToWorkbenchEntry(entry)
	if err != nil {
		return nil, err
	}
	helperPath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate Desktop AskPass helper: %w", err)
	}
	helperPath, err = filepath.Abs(helperPath)
	if err != nil {
		return nil, fmt.Errorf("resolve Desktop AskPass helper: %w", err)
	}
	return &windowsWorkbenchSSHFactory{entry: entry, boundEntry: hostEntry, helperPath: helperPath, handler: handler}, nil
}

func (f *windowsWorkbenchSSHFactory) Open(ctx context.Context) (transport.Stream, error) {
	broker, err := StartRemoteAskPassBroker(ctx, 10*time.Minute, f.handler)
	if err != nil {
		return nil, err
	}
	sshFactory := &RemoteSSHTransportFactory{AskPass: broker, AskPassHelper: f.helperPath}
	var stream transport.Stream
	stream, err = openWindowsWorkbenchSSH(ctx, sshFactory, f.entry, f.boundEntry)
	if err != nil {
		_ = broker.Close()
		return nil, err
	}
	sshStream, ok := stream.(*RemoteSSHTransport)
	if !ok {
		_ = stream.Close()
		_ = broker.Close()
		return nil, fmt.Errorf("system OpenSSH factory returned an unexpected stream")
	}
	f.mu.Lock()
	f.transport = sshStream
	f.mu.Unlock()
	return &askPassOwnedStream{Stream: stream, broker: broker}, nil
}

func openWindowsWorkbenchSSH(ctx context.Context, sshFactory *RemoteSSHTransportFactory, entry config.RemoteHostEntry, bound RemoteHostEntry) (transport.Stream, error) {
	if entry.UseSSHConfig {
		return sshFactory.StartConfigured(ctx, entry.Host, entry.User, entry.Port, entry.IdentityFile, entry.ProxyJump)
	}
	return sshFactory.StartDirectConfigured(ctx, bound.Destination, bound.Port, entry.IdentityFile, entry.ProxyJump)
}

func (f *windowsWorkbenchSSHFactory) PeerIdentity() (workbenchPeerIdentity, bool) {
	f.mu.Lock()
	stream := f.transport
	f.mu.Unlock()
	if stream == nil {
		return workbenchPeerIdentity{}, false
	}
	keyType, fingerprint, ok := stream.PeerIdentity()
	return workbenchPeerIdentity{KeyType: keyType, Fingerprint: fingerprint}, ok
}

type askPassOwnedStream struct {
	transport.Stream
	broker *RemoteAskPassBroker
	once   sync.Once
}

func (s *askPassOwnedStream) Close() error {
	var streamErr error
	s.once.Do(func() {
		if s.Stream != nil {
			streamErr = s.Stream.Close()
		}
		if s.broker != nil {
			_ = s.broker.Close()
		}
	})
	return streamErr
}

func mapConfigHostToWorkbenchEntry(entry config.RemoteHostEntry) (RemoteHostEntry, error) {
	label := entry.Host
	if entry.User != "" {
		label = entry.User + "@" + entry.Host
	}
	port := entry.PortOrDefault()
	if entry.UseSSHConfig {
		return NewRemoteHostEntry(entry.Host, label)
	}
	dest := entry.Host
	if entry.User != "" {
		dest = entry.User + "@" + entry.Host
	}
	return NewRemoteDirectHostEntry(dest, port, label)
}

type goSSHWorkbenchFactory struct {
	entry   config.RemoteHostEntry
	handler RemoteAskPassHandler
	mu      sync.Mutex
	peer    remote.HostKeyQuestion
}

type workbenchPeerIdentity struct {
	KeyType     string
	Fingerprint string
}

func newGoSSHWorkbenchFactory(entry config.RemoteHostEntry, handler RemoteAskPassHandler) (*goSSHWorkbenchFactory, error) {
	if strings.TrimSpace(entry.Name) == "" {
		return nil, fmt.Errorf("remote host name is required")
	}
	if handler == nil {
		return nil, fmt.Errorf("SSH secret prompt handler is required")
	}
	return &goSSHWorkbenchFactory{entry: entry, handler: handler}, nil
}

func (f *goSSHWorkbenchFactory) Open(ctx context.Context) (transport.Stream, error) {
	return openGoSSHAttachWorkspace(ctx, f.entry, f.handler, func(q remote.HostKeyQuestion) {
		f.mu.Lock()
		f.peer = q
		f.mu.Unlock()
	})
}

func (f *goSSHWorkbenchFactory) PeerIdentity() (workbenchPeerIdentity, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return workbenchPeerIdentity{KeyType: f.peer.KeyType, Fingerprint: f.peer.Fingerprint}, strings.TrimSpace(f.peer.Fingerprint) != ""
}

// goSSHStream adapts a Go SSH session's stdio to transport.Stream.
type goSSHStream struct {
	client  *remote.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	cancel  context.CancelFunc
}

func (s *goSSHStream) Read(p []byte) (int, error)  { return s.stdout.Read(p) }
func (s *goSSHStream) Write(p []byte) (int, error) { return s.stdin.Write(p) }
func (s *goSSHStream) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.stdin != nil {
		_ = s.stdin.Close()
	}
	if s.session != nil {
		_ = s.session.Close()
	}
	if s.client != nil {
		_ = s.client.Close()
	}
	return nil
}

func openGoSSHAttachWorkspace(ctx context.Context, entry config.RemoteHostEntry, handler RemoteAskPassHandler, verified func(remote.HostKeyQuestion)) (transport.Stream, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	sshCfg, err := remote.LoadUserSSHConfig()
	if err != nil {
		return nil, fmt.Errorf("load SSH config: %w", err)
	}
	resolved, err := remote.ResolveHost(cfg, entry.Name, sshCfg)
	if err != nil {
		return nil, err
	}
	resolvedJumps, err := remote.ResolveJumpHosts(cfg, resolved.ProxyJump, sshCfg)
	if err != nil {
		return nil, err
	}
	dialer, err := netclient.NewStreamDialer(cfg.NetworkProxySpec())
	if err != nil {
		return nil, fmt.Errorf("remote: network proxy is misconfigured: %w", err)
	}
	secretPrompt := func(promptCtx context.Context, kind remote.SecretKind, host, identityFile string) (string, error) {
		var env string
		if kind == remote.SecretPassphrase {
			env = resolved.PassphraseEnv
		} else {
			env = resolved.PasswordEnv
		}
		if env == "" {
			promptKind := RemoteAskPassPassword
			if kind == remote.SecretPassphrase {
				promptKind = RemoteAskPassKeyPassphrase
			}
			answer, promptErr := handler(promptCtx, RemoteAskPassPrompt{
				Kind: promptKind, Message: kind.String(), HostLabel: host,
			})
			if promptErr != nil {
				return "", promptErr
			}
			if !answer.Accepted {
				return "", fmt.Errorf("remote: %s prompt rejected", kind)
			}
			return answer.Value, nil
		}
		value := config.ResolveCredential(env).Value
		if value == "" {
			return "", fmt.Errorf("remote: configured %s credential is empty", kind)
		}
		return value, nil
	}
	auth := desktopAuthForHost(resolved, secretPrompt)
	jumpHosts := make([]remote.JumpHostOptions, 0, len(resolvedJumps))
	for _, jump := range resolvedJumps {
		jumpHosts = append(jumpHosts, remote.JumpHostOptions{Host: jump, Auth: desktopAuthForHost(jump, secretPrompt)})
	}
	policy := &remote.HostKeyPolicy{Verified: func(q remote.HostKeyQuestion) {
		if q.Host == resolved.Label() && verified != nil {
			verified(q)
		}
	}}
	opts := remote.Options{Host: resolved, Auth: auth, JumpHosts: jumpHosts, HostKeys: policy, Dialer: dialer}
	c, err := remote.New(opts)
	if err != nil {
		return nil, err
	}
	if err := c.Start(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	cl, err := c.SSH()
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	sess, err := cl.NewSession()
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	cmd := "reasonix remote attach-workspace --stdio"
	if ws := strings.TrimSpace(entry.Workspace); ws != "" {
		cmd = "REASONIX_ATTACH_WORKSPACE=" + shellSingleQuote(ws) + " " + cmd
	}
	if err := sess.Start(cmd); err != nil {
		_ = c.Close()
		return nil, err
	}
	ctx2, cancel := context.WithCancel(ctx)
	go func() {
		<-ctx2.Done()
		_ = sess.Close()
	}()
	go func() { _ = sess.Wait(); cancel() }()
	return &goSSHStream{client: c, session: sess, stdin: stdin, stdout: stdout, cancel: cancel}, nil
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
