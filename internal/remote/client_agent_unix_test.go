//go:build !windows

package remote

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"reasonix/internal/remote/sshtest"
)

func TestClientFallsBackFromEmptyAgentToIdentityFile(t *testing.T) {
	pemBytes, authorized, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: authorized})
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := writeFile0600(keyPath, pemBytes); err != nil {
		t.Fatal(err)
	}

	// A running but empty agent reproduces the desktop failure: the first
	// publickey source has no signers, so the explicit identity must be tried
	// as a second publickey attempt.
	agentDir, err := os.MkdirTemp("", "reasonix-agent-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(agentDir) })
	sock := filepath.Join(agentDir, "agent.sock")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = agent.ServeAgent(agent.NewKeyring(), conn)
	}()
	t.Setenv("SSH_AUTH_SOCK", sock)

	c := newTestClient(t, srv, Options{})
	c.opts.Host.IdentityFile = keyPath

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start with empty agent and explicit identity: %v", err)
	}
	defer c.Close()
	if c.Status().Status != StatusConnected {
		t.Fatalf("status = %v, want connected", c.Status().Status)
	}
}

func TestIdentitiesOnlyUsesOnlyConfiguredAgentIdentity(t *testing.T) {
	correctPEM, authorized, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	wrongPEM, _, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: authorized})
	keyPath := filepath.Join(t.TempDir(), "id_ed25519.pub")
	if err := writeFile0600(keyPath, ssh.MarshalAuthorizedKey(authorized)); err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	for _, pemBytes := range [][]byte{wrongPEM, correctPEM} {
		privateKey, err := ssh.ParseRawPrivateKey(pemBytes)
		if err != nil {
			t.Fatal(err)
		}
		if err := keyring.Add(agent.AddedKey{PrivateKey: privateKey}); err != nil {
			t.Fatal(err)
		}
	}

	agentDir, err := os.MkdirTemp("", "reasonix-identities-only-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(agentDir) })
	sock := filepath.Join(agentDir, "agent.sock")
	listener, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = agent.ServeAgent(keyring, conn)
			}()
		}
	}()
	t.Setenv("SSH_AUTH_SOCK", sock)

	c := newTestClient(t, srv, Options{})
	c.opts.Host.IdentityFile = keyPath
	c.opts.Host.IdentityFiles = []string{keyPath}
	c.opts.Host.IdentitiesOnly = true
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start with configured agent identity and IdentitiesOnly: %v", err)
	}
	defer c.Close()
	if c.Status().Status != StatusConnected {
		t.Fatalf("status = %v, want connected", c.Status().Status)
	}
}
