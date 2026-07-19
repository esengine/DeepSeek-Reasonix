package remote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/remote/sshtest"
)

// managedOnlyPolicy points the host-key policy at an isolated managed file and
// no system files, with an accept-all prompt, so tests never touch ~/.ssh.
func managedOnlyPolicy(t *testing.T, accept bool) *HostKeyPolicy {
	t.Helper()
	return &HostKeyPolicy{
		SystemKnownHosts: []string{filepath.Join(t.TempDir(), "none")},
		ManagedPath:      filepath.Join(t.TempDir(), "known_hosts"),
		Prompt: func(context.Context, HostKeyQuestion) (bool, error) {
			return accept, nil
		},
	}
}

func newTestClient(t *testing.T, srv *sshtest.Server, opts Options) *Client {
	t.Helper()
	host, err := ResolveHost(nil, "test@"+srv.Addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	opts.Host = host
	if opts.HostKeys == nil {
		opts.HostKeys = managedOnlyPolicy(t, true)
	}
	c, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClientConnectPasswordAuth(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "hunter2"})
	c := newTestClient(t, srv, Options{
		Auth: AuthOptions{
			DisableAgent: true,
			Password:     func() (string, error) { return "hunter2", nil },
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()
	if c.Status().Status != StatusConnected {
		t.Fatalf("status = %v, want connected", c.Status().Status)
	}
	res, err := c.Exec(ctx, "echo hello")
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if strings.TrimSpace(string(res.Stdout)) != "echo hello" {
		t.Fatalf("exec stdout = %q", res.Stdout)
	}
}

func TestClientConnectPublicKeyAuth(t *testing.T) {
	pemBytes, pub, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: pub})
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := writeFile0600(keyPath, pemBytes); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(t, srv, Options{})
	c.opts.Host.IdentityFile = keyPath
	c.opts.Auth.DisableAgent = true

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()
	if c.Status().Status != StatusConnected {
		t.Fatalf("status = %v", c.Status().Status)
	}
}

func TestClientAuthFailureStops(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "correct"})
	c := newTestClient(t, srv, Options{
		Auth: AuthOptions{
			DisableAgent: true,
			Password:     func() (string, error) { return "wrong", nil },
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.Start(ctx)
	if err == nil {
		t.Fatal("expected auth failure")
	}
	if c.Status().Status != StatusStopped {
		t.Fatalf("status = %v, want stopped", c.Status().Status)
	}
}

func TestClientHostKeyRejectedStops(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "x"})
	c := newTestClient(t, srv, Options{
		HostKeys: managedOnlyPolicy(t, false), // reject TOFU
		Auth: AuthOptions{
			DisableAgent: true,
			Password:     func() (string, error) { return "x", nil },
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := c.Start(ctx)
	if err == nil {
		t.Fatal("expected host key rejection")
	}
}

func TestClientHostKeyTOFUPersistsAndReconnectsSilently(t *testing.T) {
	srv := sshtest.Start(t, sshtest.Options{Password: "x"})
	managed := filepath.Join(t.TempDir(), "known_hosts")
	prompted := 0
	policy := &HostKeyPolicy{
		SystemKnownHosts: []string{filepath.Join(t.TempDir(), "none")},
		ManagedPath:      managed,
		Prompt: func(context.Context, HostKeyQuestion) (bool, error) {
			prompted++
			return true, nil
		},
	}
	host, _ := ResolveHost(nil, "test@"+srv.Addr, nil)
	mkClient := func() *Client {
		c, err := New(Options{
			Host:     host,
			HostKeys: policy,
			Auth:     AuthOptions{DisableAgent: true, Password: func() (string, error) { return "x", nil }},
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	ctx := context.Background()
	c1 := mkClient()
	if err := c1.Start(ctx); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	c1.Close()
	if prompted != 1 {
		t.Fatalf("expected exactly 1 prompt on first connect, got %d", prompted)
	}

	// Second connect should find the key in the managed file: no prompt.
	c2 := mkClient()
	if err := c2.Start(ctx); err != nil {
		t.Fatalf("second connect: %v", err)
	}
	c2.Close()
	if prompted != 1 {
		t.Fatalf("second connect re-prompted (count=%d); TOFU key was not persisted", prompted)
	}
}

func writeFile0600(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
