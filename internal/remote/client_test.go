package remote

import (
	"context"
	"errors"
	"fmt"
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

func TestIdentityFileNoneSuppressesDefaultKeys(t *testing.T) {
	pemBytes, pub, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeFile0600(filepath.Join(sshDir, "id_ed25519"), pemBytes); err != nil {
		t.Fatal(err)
	}
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: pub})
	c := newTestClient(t, srv, Options{Auth: AuthOptions{DisableAgent: true}})
	c.opts.Host.IdentityFileNone = true
	c.opts.Host.IdentitiesOnly = true

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err == nil {
		defer c.Close()
		t.Fatal("IdentityFile none unexpectedly offered a default private key")
	}
}

func TestClientTriesMultipleIdentityFilesInOrder(t *testing.T) {
	wrongPEM, _, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	correctPEM, correctPublic, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	// The server accepts the second configured identity, not the first.
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: correctPublic})
	dir := t.TempDir()
	wrongPath := filepath.Join(dir, "id_wrong")
	correctPath := filepath.Join(dir, "id_correct")
	if err := writeFile0600(wrongPath, wrongPEM); err != nil {
		t.Fatal(err)
	}
	if err := writeFile0600(correctPath, correctPEM); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(t, srv, Options{})
	c.opts.Host.IdentityFile = wrongPath
	c.opts.Host.IdentityFiles = []string{wrongPath, correctPath}
	c.opts.Auth.DisableAgent = true

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start with second valid identity: %v", err)
	}
	defer c.Close()
	if c.Status().Status != StatusConnected {
		t.Fatalf("status = %v, want connected", c.Status().Status)
	}
}

func TestClientFallsBackFromUnavailableAgentToIdentityFile(t *testing.T) {
	pemBytes, authorized, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: authorized})
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	if err := writeFile0600(keyPath, pemBytes); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SSH_AUTH_SOCK", filepath.Join(t.TempDir(), "missing-agent.sock"))

	c := newTestClient(t, srv, Options{})
	c.opts.Host.IdentityFile = keyPath

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start with unavailable agent and explicit identity: %v", err)
	}
	defer c.Close()
	if c.Status().Status != StatusConnected {
		t.Fatalf("status = %v, want connected", c.Status().Status)
	}
}

func TestClientConnectEncryptedPublicKeyAuth(t *testing.T) {
	pemBytes, pub, err := sshtest.GenerateEncryptedKeyPEM("correct horse battery staple")
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
	c.opts.Auth = AuthOptions{
		DisableAgent: true,
		Passphrase:   func() (string, error) { return "correct horse battery staple", nil },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer c.Close()
	if c.Status().Status != StatusConnected {
		t.Fatalf("status = %v, want connected", c.Status().Status)
	}
}

func TestClientPromptsPerEncryptedIdentity(t *testing.T) {
	wrongPEM, _, err := sshtest.GenerateEncryptedKeyPEM("first-key-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	correctPEM, authorized, err := sshtest.GenerateEncryptedKeyPEM("second-key-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: authorized})
	dir := t.TempDir()
	wrongPath := filepath.Join(dir, "id_wrong_encrypted")
	correctPath := filepath.Join(dir, "id_correct_encrypted")
	if err := writeFile0600(wrongPath, wrongPEM); err != nil {
		t.Fatal(err)
	}
	if err := writeFile0600(correctPath, correctPEM); err != nil {
		t.Fatal(err)
	}
	prompts := map[string]int{}
	c := newTestClient(t, srv, Options{})
	c.opts.Host.IdentityFile = wrongPath
	c.opts.Host.IdentityFiles = []string{wrongPath, correctPath}
	c.opts.Auth = AuthOptions{
		DisableAgent: true,
		SecretPrompt: func(_ context.Context, kind SecretKind, _ string, identityFile string) (string, error) {
			if kind != SecretPassphrase {
				t.Fatalf("prompt kind = %v, want passphrase", kind)
			}
			prompts[identityFile]++
			switch identityFile {
			case wrongPath:
				return "first-key-passphrase", nil
			case correctPath:
				return "second-key-passphrase", nil
			default:
				return "", fmt.Errorf("unexpected identity %q", identityFile)
			}
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start with separately encrypted identities: %v", err)
	}
	defer c.Close()
	if prompts[wrongPath] != 1 || prompts[correctPath] != 1 {
		t.Fatalf("passphrase prompts = %v, want one per identity", prompts)
	}
}

func TestClientFallsBackFromStoredPassphraseToPerIdentityPrompt(t *testing.T) {
	wrongPEM, _, err := sshtest.GenerateEncryptedKeyPEM("first-key-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	correctPEM, authorized, err := sshtest.GenerateEncryptedKeyPEM("second-key-passphrase")
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: authorized})
	dir := t.TempDir()
	wrongPath := filepath.Join(dir, "id_wrong_encrypted")
	correctPath := filepath.Join(dir, "id_correct_encrypted")
	if err := writeFile0600(wrongPath, wrongPEM); err != nil {
		t.Fatal(err)
	}
	if err := writeFile0600(correctPath, correctPEM); err != nil {
		t.Fatal(err)
	}
	var prompted []string
	c := newTestClient(t, srv, Options{})
	c.opts.Host.IdentityFile = wrongPath
	c.opts.Host.IdentityFiles = []string{wrongPath, correctPath}
	c.opts.Auth = AuthOptions{
		DisableAgent: true,
		// The saved host-level value unlocks the second key only.
		Passphrase: func() (string, error) { return "second-key-passphrase", nil },
		SecretPrompt: func(_ context.Context, kind SecretKind, _ string, identityFile string) (string, error) {
			if kind != SecretPassphrase || identityFile != wrongPath {
				return "", fmt.Errorf("unexpected prompt kind=%v identity=%q", kind, identityFile)
			}
			prompted = append(prompted, identityFile)
			return "first-key-passphrase", nil
		},
	}

	// This handshake performs three passphrase KDFs (stored + prompted for the
	// first identity, then stored for the second). Under full -race package
	// parallelism on a constrained CI runner, ten seconds is too close to the CPU
	// bound work even though the in-process SSH server remains responsive.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Start(ctx); err != nil {
		t.Fatalf("Start with stored and per-identity passphrases: %v", err)
	}
	defer c.Close()
	if len(prompted) != 1 || prompted[0] != wrongPath {
		t.Fatalf("identity prompts = %v, want only %q", prompted, wrongPath)
	}
}

func TestRejectedPublicKeyDoesNotReportMissingPasswordPrompt(t *testing.T) {
	_, authorized, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	wrongPEM, _, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: authorized})
	keyPath := filepath.Join(t.TempDir(), "wrong_id_ed25519")
	if err := writeFile0600(keyPath, wrongPEM); err != nil {
		t.Fatal(err)
	}
	c := newTestClient(t, srv, Options{})
	c.opts.Host.IdentityFile = keyPath
	c.opts.Auth.DisableAgent = true

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = c.Start(ctx)
	if err == nil {
		t.Fatal("expected authentication failure")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("error = %v, want ErrAuthFailed", err)
	}
	if strings.Contains(err.Error(), "password required") || strings.Contains(err.Error(), "no prompt available") {
		t.Fatalf("public-key rejection was masked by a password-prompt error: %v", err)
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
