package remote

import (
	"context"
	"errors"
	"net"
	"testing"

	"reasonix/internal/base/testenv"
	"time"
)

func TestClientUsesResolvedPerHopCredentials(t *testing.T) {
	targetAuth := AuthOptions{Password: func() (string, error) { return "target", nil }}
	hopAuth := AuthOptions{Password: func() (string, error) { return "jump", nil }}
	c, err := New(Options{
		Host:      ResolvedHost{HostName: "target", Port: 22, User: "target-user", ProxyJump: []string{"bastion"}},
		Auth:      targetAuth,
		JumpHosts: []JumpHostOptions{{Host: ResolvedHost{HostName: "10.0.0.8", Port: 2202, User: "jump-user"}, Auth: hopAuth}},
	})
	if err != nil {
		t.Fatal(err)
	}
	hop, auth, err := c.resolveHop("bastion")
	if err != nil {
		t.Fatal(err)
	}
	if hop.Addr() != "10.0.0.8:2202" || hop.User != "jump-user" {
		t.Fatalf("resolved hop = %+v", hop)
	}
	if auth.Password == nil {
		t.Fatal("configured jump password was dropped")
	}
	jumpSecret, err := auth.Password()
	if err != nil || jumpSecret != "jump" {
		t.Fatalf("jump password = %q, %v", jumpSecret, err)
	}
	targetSecret, _ := targetAuth.Password()
	if jumpSecret == targetSecret {
		t.Fatal("jump auth reused the target credential")
	}
}

func TestClientKeepsAliasCredentialsDistinctForSharedEndpoint(t *testing.T) {
	password := func(value string) func() (string, error) {
		return func() (string, error) { return value, nil }
	}
	endpoint := ResolvedHost{HostName: "10.0.0.8", Port: 22, User: "jump-user"}
	c, err := New(Options{
		Host: ResolvedHost{HostName: "target", Port: 22, User: "target-user", ProxyJump: []string{"primary", "backup"}},
		JumpHosts: []JumpHostOptions{
			{Host: endpoint, Auth: AuthOptions{Password: password("primary-secret")}},
			{Host: endpoint, Auth: AuthOptions{Password: password("backup-secret")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, primary, err := c.resolveHop("primary")
	if err != nil {
		t.Fatal(err)
	}
	_, backup, err := c.resolveHop("backup")
	if err != nil {
		t.Fatal(err)
	}
	primarySecret, _ := primary.Password()
	backupSecret, _ := backup.Password()
	if primarySecret != "primary-secret" || backupSecret != "backup-secret" {
		t.Fatalf("shared endpoint credentials collided: primary=%q backup=%q", primarySecret, backupSecret)
	}
}

type noDeadlineConn struct{ net.Conn }

func (noDeadlineConn) SetDeadline(time.Time) error      { return errors.New("unsupported") }
func (noDeadlineConn) SetReadDeadline(time.Time) error  { return errors.New("unsupported") }
func (noDeadlineConn) SetWriteDeadline(time.Time) error { return errors.New("unsupported") }

// The 40ms context is the subject — the handshake must honour a deadline it
// cannot install on the socket — so only the watchdog scales with the runner.
// A race stands: a slow enough runner expires that context before the
// SetDeadline branch is reached. Closing it needs noDeadlineConn to report
// that call, and a context still carrying a real deadline production reads.
func TestSSHHandshakeHonorsContextWhenDeadlinesUnsupported(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := newSSHClient(ctx, noDeadlineConn{client},
			ResolvedHost{HostName: "target", Port: 22, User: "u"},
			&AuthOptions{DisableAgent: true}, &HostKeyPolicy{}, time.Second)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("banner-less handshake unexpectedly succeeded")
		}
	case <-time.After(testenv.Budget(t)):
		t.Fatal("handshake outlived its context on a ProxyJump-style connection")
	}
}
