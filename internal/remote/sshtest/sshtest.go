// Package sshtest is an in-process SSH server for exercising the remote
// module without a real sshd. It supports publickey and password auth, session
// exec with scripted responses, direct-tcpip (for -L forwards), tcpip-forward
// (for -R forwards), and an SFTP subsystem via pkg/sftp's server. It is
// test-only.
package sshtest

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Server is a running in-process SSH server.
type Server struct {
	Addr      string
	HostKey   ssh.Signer
	config    *ssh.ServerConfig
	listener  net.Listener
	execFunc  func(cmd string) (stdout string, stderr string, exit int)
	sftpRoot  string
	enableSFT bool

	mu        sync.Mutex
	conns     []net.Conn
	listeners []net.Listener
	wg        sync.WaitGroup
}

// Options configures a test server.
type Options struct {
	// HostKeys, when non-empty, are offered by the server. The first key is
	// also exposed as Server.HostKey. Empty generates one ed25519 key.
	HostKeys []ssh.Signer
	// Password, when non-empty, enables password auth accepting (any user,
	// this password).
	Password string
	// AuthorizedKey, when set, enables publickey auth accepting this key.
	AuthorizedKey ssh.PublicKey
	// Exec handles `exec` requests; nil => a default echoing the command.
	Exec func(cmd string) (stdout string, stderr string, exit int)
	// SFTPRoot enables the SFTP subsystem rooted at this directory.
	SFTPRoot string
}

// Start launches a server on 127.0.0.1:0.
func Start(t *testing.T, opts Options) *Server {
	t.Helper()
	hostKeys := opts.HostKeys
	if len(hostKeys) == 0 {
		hostKey, err := generateHostKey()
		if err != nil {
			t.Fatalf("host key: %v", err)
		}
		hostKeys = []ssh.Signer{hostKey}
	}
	cfg := &ssh.ServerConfig{}
	for _, hostKey := range hostKeys {
		if hostKey == nil {
			t.Fatal("host key must not be nil")
		}
		cfg.AddHostKey(hostKey)
	}
	if opts.Password != "" {
		cfg.PasswordCallback = func(conn ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if string(pass) == opts.Password {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("bad password")
		}
	}
	if opts.AuthorizedKey != nil {
		want := opts.AuthorizedKey.Marshal()
		cfg.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(want) {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unknown key")
		}
	}
	if opts.Password == "" && opts.AuthorizedKey == nil {
		cfg.NoClientAuth = true
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &Server{
		Addr:      ln.Addr().String(),
		HostKey:   hostKeys[0],
		config:    cfg,
		listener:  ln,
		execFunc:  opts.Exec,
		sftpRoot:  opts.SFTPRoot,
		enableSFT: opts.SFTPRoot != "",
	}
	s.wg.Add(1)
	go s.serve()
	t.Cleanup(s.Close)
	return s
}

// Close stops the server and all active connections.
func (s *Server) Close() {
	_ = s.listener.Close()
	s.mu.Lock()
	for _, c := range s.conns {
		_ = c.Close()
	}
	for _, ln := range s.listeners {
		_ = ln.Close()
	}
	s.conns = nil
	s.listeners = nil
	s.mu.Unlock()
	s.wg.Wait()
}

// DropConnections closes every currently-open client connection without
// stopping the server, simulating a network drop so a supervised Client must
// reconnect.
func (s *Server) DropConnections() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		nConn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, nConn)
		s.mu.Unlock()
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(nConn)
		}()
	}
}

func (s *Server) handleConn(nConn net.Conn) {
	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, s.config)
	if err != nil {
		return
	}
	defer sshConn.Close()
	go s.handleGlobalRequests(sshConn, reqs)
	for newCh := range chans {
		switch newCh.ChannelType() {
		case "session":
			go s.handleSession(newCh)
		case "direct-tcpip":
			go s.handleDirectTCPIP(newCh)
		default:
			_ = newCh.Reject(ssh.UnknownChannelType, "unsupported")
		}
	}
}

func (s *Server) handleGlobalRequests(conn *ssh.ServerConn, reqs <-chan *ssh.Request) {
	for req := range reqs {
		switch req.Type {
		case "keepalive@openssh.com":
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		case "tcpip-forward":
			s.handleTCPIPForward(conn, req)
		case "cancel-tcpip-forward":
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func (s *Server) handleSession(newCh ssh.NewChannel) {
	ch, reqs, err := newCh.Accept()
	if err != nil {
		return
	}
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "exec":
			cmd := parseStringPayload(req.Payload)
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			s.runExec(ch, cmd)
			return
		case "subsystem":
			name := parseStringPayload(req.Payload)
			if name == "sftp" && s.enableSFT {
				if req.WantReply {
					_ = req.Reply(true, nil)
				}
				s.runSFTP(ch)
				return
			}
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		case "shell", "pty-req", "env":
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func (s *Server) runExec(ch ssh.Channel, cmd string) {
	stdout, stderr, exit := "", "", 0
	if s.execFunc != nil {
		stdout, stderr, exit = s.execFunc(cmd)
	} else {
		stdout = cmd
	}
	_, _ = io.WriteString(ch, stdout)
	if stderr != "" {
		_, _ = io.WriteString(ch.Stderr(), stderr)
	}
	sendExitStatus(ch, exit)
}

func (s *Server) runSFTP(ch ssh.Channel) {
	var server *sftp.Server
	var err error
	if s.sftpRoot != "" {
		server, err = sftp.NewServer(ch, sftp.WithServerWorkingDirectory(s.sftpRoot))
	} else {
		server, err = sftp.NewServer(ch)
	}
	if err != nil {
		return
	}
	_ = server.Serve()
	_ = server.Close()
}

// handleDirectTCPIP implements -L forwards: dial the requested target and
// splice.
func (s *Server) handleDirectTCPIP(newCh ssh.NewChannel) {
	var payload struct {
		HostToConnect  string
		PortToConnect  uint32
		OriginatorHost string
		OriginatorPort uint32
	}
	if err := ssh.Unmarshal(newCh.ExtraData(), &payload); err != nil {
		_ = newCh.Reject(ssh.ConnectionFailed, "bad payload")
		return
	}
	target := net.JoinHostPort(payload.HostToConnect, fmt.Sprintf("%d", payload.PortToConnect))
	dst, err := net.Dial("tcp", target)
	if err != nil {
		_ = newCh.Reject(ssh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := newCh.Accept()
	if err != nil {
		_ = dst.Close()
		return
	}
	go ssh.DiscardRequests(reqs)
	splice(ch, dst)
}

// handleTCPIPForward implements -R forwards: listen locally on the server and
// open a forwarded-tcpip channel back to the client for each accepted conn.
func (s *Server) handleTCPIPForward(conn *ssh.ServerConn, req *ssh.Request) {
	var payload struct {
		BindAddr string
		BindPort uint32
	}
	if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(payload.BindAddr, fmt.Sprintf("%d", payload.BindPort)))
	if err != nil {
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return
	}
	boundPort := uint32(ln.Addr().(*net.TCPAddr).Port)
	s.mu.Lock()
	s.listeners = append(s.listeners, ln)
	s.mu.Unlock()
	if req.WantReply {
		_ = req.Reply(true, ssh.Marshal(struct{ Port uint32 }{boundPort}))
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				origPort := uint32(1)
				if ta, ok := c.RemoteAddr().(*net.TCPAddr); ok && ta.Port > 0 {
					origPort = uint32(ta.Port)
				}
				msg := struct {
					ConnHost string
					ConnPort uint32
					OrigHost string
					OrigPort uint32
				}{payload.BindAddr, boundPort, "127.0.0.1", origPort}
				ch, reqs, err := conn.OpenChannel("forwarded-tcpip", ssh.Marshal(msg))
				if err != nil {
					_ = c.Close()
					return
				}
				go ssh.DiscardRequests(reqs)
				splice(ch, c)
			}()
		}
	}()
}

func splice(a io.ReadWriteCloser, b net.Conn) {
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	<-done
	_ = a.Close()
	_ = b.Close()
}

func parseStringPayload(p []byte) string {
	if len(p) < 4 {
		return ""
	}
	n := int(p[0])<<24 | int(p[1])<<16 | int(p[2])<<8 | int(p[3])
	if 4+n > len(p) {
		return ""
	}
	return string(p[4 : 4+n])
}

func sendExitStatus(ch ssh.Channel, code int) {
	_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{uint32(code)}))
}
