//go:build !windows

package sandbox

import (
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// The probe runs inside the OS sandbox as this test binary, so the boundary is
// measured with no interpreter the host might lack.
const egressProbeEnv = "REASONIX_SANDBOX_EGRESS_PROBE"

func TestEgressProbeHelper(t *testing.T) {
	spec := os.Getenv(egressProbeEnv)
	if spec == "" {
		t.Skip("helper process only")
	}
	kind, target, _ := strings.Cut(spec, " ")
	var err error
	switch kind {
	case "tcp", "unix":
		var c net.Conn
		if c, err = net.DialTimeout(kind, target, 3*time.Second); err == nil {
			_ = c.Close()
		}
	case "echo":
		var c net.Conn
		if c, err = net.DialTimeout("tcp", target, 3*time.Second); err == nil {
			_ = c.SetDeadline(time.Now().Add(3 * time.Second))
			buf := make([]byte, 4)
			if _, err = c.Write([]byte("ping")); err == nil {
				if _, err = io.ReadFull(c, buf); err == nil && string(buf) != "pong" {
					err = io.ErrUnexpectedEOF
				}
			}
			_ = c.Close()
		}
	case "self":
		var ln net.Listener
		if ln, err = net.Listen("tcp", target); err == nil {
			var c net.Conn
			if c, err = net.DialTimeout("tcp", ln.Addr().String(), 3*time.Second); err == nil {
				_ = c.Close()
			}
			_ = ln.Close()
		}
	}
	if err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func listenLoopback(t *testing.T) (net.Listener, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return ln, ln.Addr().(*net.TCPAddr).Port
}

type fixedRoute struct {
	port   int
	socket string
}

func (r fixedRoute) Port() int                            { return r.port }
func (r fixedRoute) SocketPath() string                   { return r.socket }
func (fixedRoute) Env(string) []string                    { return nil }
func (fixedRoute) Ask(string, func(string) (bool, error)) {}
func (fixedRoute) Refusals(string) []string               { return nil }

// pongSocket answers "pong" to "ping" on a Unix socket under dir.
func pongSocket(t *testing.T, dir string) string {
	t.Helper()
	sock := dir + "/p.sock"
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 4)
				if _, err := io.ReadFull(c, buf); err == nil && string(buf) == "ping" {
					_, _ = c.Write([]byte("pong"))
				}
			}()
		}
	}()
	return sock
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
