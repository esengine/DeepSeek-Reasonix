package remote

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"reasonix/internal/base/testenv"
	"reasonix/internal/platform/remote/sshtest"
)

// keyringDialer serves an in-memory agent over a pipe. Where the real agent
// lives is the one thing the platforms disagree about, so everything above the
// dial can be exercised on any of them — which is what the Windows half never
// had.
func keyringDialer(t *testing.T, keys ...agent.AddedKey) agentDialer {
	t.Helper()
	keyring := agent.NewKeyring()
	for _, key := range keys {
		if err := keyring.Add(key); err != nil {
			t.Fatal(err)
		}
	}
	return func() (io.ReadWriteCloser, error) {
		client, server := net.Pipe()
		go func() {
			_ = agent.ServeAgent(keyring, server)
			_ = server.Close()
		}()
		return client, nil
	}
}

func agentKey(t *testing.T) (agent.AddedKey, ssh.PublicKey, []byte) {
	t.Helper()
	pem, pub, err := sshtest.GenerateKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ssh.ParseRawPrivateKey(pem)
	if err != nil {
		t.Fatal(err)
	}
	return agent.AddedKey{PrivateKey: raw}, pub, pem
}

func dialWith(t *testing.T, srv *sshtest.Server, method ssh.AuthMethod) error {
	t.Helper()
	client, err := ssh.Dial("tcp", srv.Addr, &ssh.ClientConfig{
		User:            "t",
		Auth:            []ssh.AuthMethod{method},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		return err
	}
	return client.Close()
}

// The whole point of an agent: a key it holds authenticates without this
// process ever seeing the private half.
func TestAgentAuthOffersWhatTheAgentHolds(t *testing.T) {
	held, pub, _ := agentKey(t)
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: pub})

	method, closeAll := agentAuthOver(keyringDialer(t, held), nil, false)
	defer closeAll()
	if err := dialWith(t, srv, method); err != nil {
		t.Fatalf("agent key was not accepted: %v", err)
	}
}

// IdentitiesOnly is OpenSSH's "offer these and nothing else". An agent full of
// unrelated keys must not spend the server's attempt budget on them.
func TestIdentitiesOnlyOffersOnlyTheConfiguredKey(t *testing.T) {
	wanted, wantedPub, wantedPEM := agentKey(t)
	other, _, _ := agentKey(t)
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: wantedPub})

	dir := testenv.TempDir(t)
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, wantedPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	// Named: the agent holds both, and only this one may be offered.
	method, closeAll := agentAuthOver(keyringDialer(t, other, wanted), []string{path}, true)
	defer closeAll()
	if err := dialWith(t, srv, method); err != nil {
		t.Fatalf("the configured key was filtered out of its own agent: %v", err)
	}

	// Unnamed: the same agent, nothing configured, so nothing is offered — the
	// keys are there, and IdentitiesOnly says they are not the ones asked for.
	blind, closeBlind := agentAuthOver(keyringDialer(t, other, wanted), nil, true)
	defer closeBlind()
	if err := dialWith(t, srv, blind); err == nil {
		t.Fatal("an unconfigured agent key was offered under IdentitiesOnly")
	}
}

// An agent that is not running is the ordinary case, not a failure — most of
// all on Windows, where the pipe name is fixed so this method is registered
// whether or not the service exists. It must not consume "publickey" and leave
// the configured key file unattempted, which is what the auth callback is for.
func TestAnAbsentAgentDoesNotConsumePublicKeyAuth(t *testing.T) {
	method, closeAll := agentAuthOver(func() (io.ReadWriteCloser, error) {
		return nil, os.ErrNotExist
	}, nil, false)
	defer closeAll()

	held, pub, _ := agentKey(t)
	srv := sshtest.Start(t, sshtest.Options{AuthorizedKey: pub})
	working, closeWorking := agentAuthOver(keyringDialer(t, held), nil, false)
	defer closeWorking()

	// Offered the way the real assembly does it — through the callback, since
	// x/crypto/ssh takes only the first static method of a protocol name. The
	// dead source must not take the live one down with it.
	client, err := ssh.Dial("tcp", srv.Addr, &ssh.ClientConfig{
		User:            "t",
		AuthCallback:    publicKeyAuthCallback([]ssh.AuthMethod{method, working}),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("an absent agent failed the whole handshake: %v", err)
	}
	_ = client.Close()
}
