package forward

import (
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"reasonix/internal/remote/sshtest"
)

// dialSSHClient connects to the sshtest server as a real ssh client.
func dialSSHClient(t *testing.T, srv *sshtest.Server) *ssh.Client {
	t.Helper()
	cfg := &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	cl, err := ssh.Dial("tcp", srv.Addr, cfg)
	if err != nil {
		t.Fatalf("ssh dial: %v", err)
	}
	t.Cleanup(func() { cl.Close() })
	return cl
}

// echoServer starts a local TCP echo server for -L target testing.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); c.Close() }()
		}
	}()
	return ln.Addr().String()
}

func TestLocalForwardEndToEnd(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{})
	cl := dialSSHClient(t, srv)
	target := echoServer(t)

	set := NewSet(nil)
	defer set.Close()
	if err := set.Attach(cl); err != nil {
		t.Fatalf("attach: %v", err)
	}
	bound, err := set.Add(Spec{Direction: Local, BindAddr: "127.0.0.1:0", TargetAddr: target})
	if err != nil {
		t.Fatalf("add local forward: %v", err)
	}
	if bound == "" {
		t.Fatal("no bound address")
	}

	conn, err := net.Dial("tcp", bound)
	if err != nil {
		t.Fatalf("dial forward: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("echo = %q, want ping", buf)
	}
}

func TestLocalListenerPersistsAcrossReattach(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{})
	target := echoServer(t)

	set := NewSet(nil)
	defer set.Close()
	cl1 := dialSSHClient(t, srv)
	if err := set.Attach(cl1); err != nil {
		t.Fatal(err)
	}
	bound, err := set.Add(Spec{Direction: Local, BindAddr: "127.0.0.1:0", TargetAddr: target})
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a connection drop then reconnect on a new client.
	set.Detach()
	cl2 := dialSSHClient(t, srv)
	if err := set.Attach(cl2); err != nil {
		t.Fatal(err)
	}

	// The bound address must be unchanged (listener stayed open).
	entries := set.List()
	if len(entries) != 1 || entries[0].BoundAddr != bound {
		t.Fatalf("bound address changed across reattach: %+v (was %s)", entries, bound)
	}

	// And traffic works again through the new connection.
	conn, err := net.Dial("tcp", bound)
	if err != nil {
		t.Fatalf("dial after reattach: %v", err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("pong"))
	buf := make([]byte, 4)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo after reattach: %v", err)
	}
	if string(buf) != "pong" {
		t.Fatalf("echo = %q", buf)
	}
}

func TestDuplicateForwardRejected(t *testing.T) {
	set := NewSet(nil)
	defer set.Close()
	spec := Spec{Name: "web", Direction: Local, BindAddr: "127.0.0.1:0", TargetAddr: "svc:80"}
	if _, err := set.Add(spec); err != nil {
		t.Fatal(err)
	}
	if _, err := set.Add(spec); err != ErrDuplicateForward {
		t.Fatalf("second add err = %v, want ErrDuplicateForward", err)
	}
}

func TestReplaceSwapsLiveForwardAfterReplacementStarts(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{})
	cl := dialSSHClient(t, srv)
	firstTarget := echoServer(t)
	secondTarget := echoServer(t)
	set := NewSet(nil)
	defer set.Close()
	if err := set.Attach(cl); err != nil {
		t.Fatal(err)
	}
	firstBound, err := set.Add(Spec{Name: "serve", Direction: Local, BindAddr: "127.0.0.1:0", TargetAddr: firstTarget})
	if err != nil {
		t.Fatal(err)
	}
	secondBound, err := set.Replace(Spec{Name: "serve", Direction: Local, BindAddr: "127.0.0.1:0", TargetAddr: secondTarget})
	if err != nil {
		t.Fatal(err)
	}
	if firstBound == secondBound {
		t.Fatalf("replacement reused old listener %q", firstBound)
	}
	entries := set.List()
	if len(entries) != 1 || entries[0].Spec.TargetAddr != secondTarget || !entries[0].Up {
		t.Fatalf("replacement registry = %+v", entries)
	}
	if conn, err := net.DialTimeout("tcp", firstBound, 100*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatalf("old listener %q is still accepting", firstBound)
	}
	conn, err := net.Dial("tcp", secondBound)
	if err != nil {
		t.Fatalf("dial replacement: %v", err)
	}
	_ = conn.Close()
}

func TestReplaceFailurePreservesExistingForward(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{})
	cl := dialSSHClient(t, srv)
	target := echoServer(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	set := NewSet(nil)
	defer set.Close()
	if err := set.Attach(cl); err != nil {
		t.Fatal(err)
	}
	bound, err := set.Add(Spec{Name: "serve", Direction: Local, BindAddr: "127.0.0.1:0", TargetAddr: target})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := set.Replace(Spec{Name: "serve", Direction: Local, BindAddr: occupied.Addr().String(), TargetAddr: "other:80"}); err == nil {
		t.Fatal("Replace unexpectedly bound an occupied address")
	}
	entries := set.List()
	if len(entries) != 1 || entries[0].BoundAddr != bound || entries[0].Spec.TargetAddr != target || !entries[0].Up {
		t.Fatalf("failed replacement disturbed existing forward: %+v", entries)
	}
}

func TestReplaceWhileDetachedPreservesExistingForward(t *testing.T) {
	set := NewSet(nil)
	defer set.Close()
	old := Spec{Name: "serve", Direction: Local, BindAddr: "127.0.0.1:0", TargetAddr: "old:80"}
	if _, err := set.Add(old); err != nil {
		t.Fatal(err)
	}
	if _, err := set.Replace(Spec{Name: "serve", Direction: Local, BindAddr: "127.0.0.1:0", TargetAddr: "new:80"}); err != ErrNotAttached {
		t.Fatalf("Replace error = %v, want ErrNotAttached", err)
	}
	entries := set.List()
	if len(entries) != 1 || entries[0].Spec.TargetAddr != old.TargetAddr {
		t.Fatalf("detached replacement disturbed old forward: %+v", entries)
	}
}

func TestBindBusyReported(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{})
	cl := dialSSHClient(t, srv)
	// Occupy a port.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	busyAddr := occupied.Addr().String()

	set := NewSet(nil)
	defer set.Close()
	if err := set.Attach(cl); err != nil {
		t.Fatal(err)
	}
	_, err = set.Add(Spec{Direction: Local, BindAddr: busyAddr, TargetAddr: "svc:80"})
	if err == nil {
		t.Fatal("expected bind-busy error")
	}
	if err != ErrBindBusy && !containsErr(err, ErrBindBusy) {
		t.Fatalf("err = %v, want ErrBindBusy", err)
	}
}

func TestRemoteForwardEndToEnd(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{})
	cl := dialSSHClient(t, srv)
	target := echoServer(t)

	events := make(chan Event, 8)
	set := NewSet(func(e Event) { events <- e })
	defer set.Close()
	if err := set.Attach(cl); err != nil {
		t.Fatal(err)
	}
	// -R: sshtest listens on its side and forwards back to our local target.
	if _, err := set.Add(Spec{Direction: Remote, BindAddr: "127.0.0.1:0", TargetAddr: target}); err != nil {
		t.Fatalf("add remote forward: %v", err)
	}

	// Find the remote bound address from the registry.
	var bound string
	deadline := time.After(5 * time.Second)
	for bound == "" {
		select {
		case <-deadline:
			t.Fatal("remote forward never came up")
		default:
		}
		for _, e := range set.List() {
			if e.Up {
				bound = e.BoundAddr
			}
		}
		if bound == "" {
			time.Sleep(20 * time.Millisecond)
		}
	}

	conn, err := net.Dial("tcp", bound)
	if err != nil {
		t.Fatalf("dial remote-forward bind: %v", err)
	}
	defer conn.Close()
	_, _ = conn.Write([]byte("rrrr"))
	buf := make([]byte, 4)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echo via -R: %v", err)
	}
	if string(buf) != "rrrr" {
		t.Fatalf("echo = %q", buf)
	}
}

func containsErr(err, target error) bool {
	type wrapper interface{ Unwrap() []error }
	if w, ok := err.(wrapper); ok {
		for _, e := range w.Unwrap() {
			if e == target || containsErr(e, target) {
				return true
			}
		}
	}
	type single interface{ Unwrap() error }
	if s, ok := err.(single); ok {
		return containsErr(s.Unwrap(), target)
	}
	return err == target
}

var _ = fmt.Sprintf
