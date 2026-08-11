package remote

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"reasonix/internal/remote/sshtest"
)

func TestNewSSHClientPrefersRecordedHostKeyAlgorithm(t *testing.T) {
	knownED25519 := generateED25519Signer(t)
	otherECDSA := generateECDSASigner(t)
	server := sshtest.Start(t, sshtest.Options{
		HostKeys: []ssh.Signer{otherECDSA, knownED25519},
	})
	systemPath := filepath.Join(t.TempDir(), "known_hosts")
	managedPath := filepath.Join(t.TempDir(), "known_hosts")
	writeKnownHost(t, systemPath, server.Addr, knownED25519.PublicKey())

	policy := &HostKeyPolicy{
		SystemKnownHosts: []string{systemPath},
		ManagedPath:      managedPath,
		Prompt: func(context.Context, HostKeyQuestion) (bool, error) {
			t.Fatal("known multi-algorithm host must not prompt")
			return false, nil
		},
	}

	client := connectTestServer(t, server, policy)
	defer client.Close()
}

func TestNewSSHClientReconnectsToRecordedLegacyRSAHost(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	restricted, err := ssh.NewSignerWithAlgorithms(signer.(ssh.AlgorithmSigner), []string{ssh.KeyAlgoRSA})
	if err != nil {
		t.Fatal(err)
	}
	server := sshtest.Start(t, sshtest.Options{HostKeys: []ssh.Signer{restricted}})
	prompted := 0
	policy := &HostKeyPolicy{
		SystemKnownHosts: []string{filepath.Join(t.TempDir(), "missing")},
		ManagedPath:      filepath.Join(t.TempDir(), "known_hosts"),
		Prompt: func(context.Context, HostKeyQuestion) (bool, error) {
			prompted++
			return true, nil
		},
	}

	client := connectTestServer(t, server, policy)
	_ = client.Close()
	client = connectTestServer(t, server, policy)
	defer client.Close()
	if prompted != 1 {
		t.Fatalf("prompt count = %d, want 1", prompted)
	}
}

func TestNewSSHClientPrefersTrustedHostCertificate(t *testing.T) {
	caSigner := generateED25519Signer(t)
	hostSigner := generateECDSASigner(t)
	certificate := &ssh.Certificate{
		Key:             hostSigner.PublicKey(),
		CertType:        ssh.HostCert,
		ValidPrincipals: []string{"127.0.0.1"},
		ValidBefore:     ssh.CertTimeInfinity,
	}
	if err := certificate.SignCert(rand.Reader, caSigner); err != nil {
		t.Fatal(err)
	}
	certificateSigner, err := ssh.NewCertSigner(certificate, hostSigner)
	if err != nil {
		t.Fatal(err)
	}
	otherED25519 := generateED25519Signer(t)
	server := sshtest.Start(t, sshtest.Options{
		HostKeys: []ssh.Signer{otherED25519, certificateSigner},
	})
	systemPath := filepath.Join(t.TempDir(), "known_hosts")
	writeKnownHostAuthority(t, systemPath, server.Addr, caSigner.PublicKey())
	policy := &HostKeyPolicy{
		SystemKnownHosts: []string{systemPath},
		ManagedPath:      filepath.Join(t.TempDir(), "known_hosts"),
		Prompt: func(context.Context, HostKeyQuestion) (bool, error) {
			t.Fatal("certified host must not prompt")
			return false, nil
		},
	}

	client := connectTestServer(t, server, policy)
	defer client.Close()
}

func TestHostKeyPolicyRejectsChangedKeyAcrossAlgorithms(t *testing.T) {
	hostname := "example.test:2222"
	knownED25519 := generateED25519Signer(t)
	presentedECDSA := generateECDSASigner(t)
	systemPath := filepath.Join(t.TempDir(), "known_hosts")
	managedPath := filepath.Join(t.TempDir(), "known_hosts")
	writeKnownHost(t, systemPath, hostname, knownED25519.PublicKey())

	prompted := false
	policy := &HostKeyPolicy{
		SystemKnownHosts: []string{systemPath},
		ManagedPath:      managedPath,
		Prompt: func(context.Context, HostKeyQuestion) (bool, error) {
			prompted = true
			return true, nil
		},
	}
	callback, err := policy.Callback(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	err = callback(hostname, &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 2222}, presentedECDSA.PublicKey())
	if !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("error = %v, want ErrHostKeyMismatch", err)
	}
	if prompted {
		t.Fatal("cross-algorithm mismatch must not be promptable")
	}
}

func TestHostKeyPolicyRejectsChangedKeyOfSameAlgorithm(t *testing.T) {
	hostname := "example.test:2222"
	knownKey := generateED25519Signer(t)
	presentedKey := generateED25519Signer(t)
	systemPath := filepath.Join(t.TempDir(), "known_hosts")
	managedPath := filepath.Join(t.TempDir(), "known_hosts")
	writeKnownHost(t, systemPath, hostname, knownKey.PublicKey())

	prompted := false
	policy := &HostKeyPolicy{
		SystemKnownHosts: []string{systemPath},
		ManagedPath:      managedPath,
		Prompt: func(context.Context, HostKeyQuestion) (bool, error) {
			prompted = true
			return true, nil
		},
	}
	callback, err := policy.Callback(context.Background(), "example")
	if err != nil {
		t.Fatal(err)
	}
	err = callback(hostname, &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 2222}, presentedKey.PublicKey())
	if !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("error = %v, want ErrHostKeyMismatch", err)
	}
	if prompted {
		t.Fatal("same-algorithm mismatch must not be promptable")
	}
}

func TestHostKeyPolicyObservesOnlyVerifiedPeer(t *testing.T) {
	hostname := "example.test:2222"
	knownKey := generateED25519Signer(t)
	changedKey := generateED25519Signer(t)
	systemPath := filepath.Join(t.TempDir(), "known_hosts")
	writeKnownHost(t, systemPath, hostname, knownKey.PublicKey())
	var verified []HostKeyQuestion
	policy := &HostKeyPolicy{
		SystemKnownHosts: []string{systemPath},
		ManagedPath:      filepath.Join(t.TempDir(), "managed_known_hosts"),
		Verified: func(q HostKeyQuestion) {
			verified = append(verified, q)
		},
	}
	callback, err := policy.Callback(context.Background(), "saved-host")
	if err != nil {
		t.Fatal(err)
	}
	remoteAddr := &net.TCPAddr{IP: net.ParseIP("192.0.2.10"), Port: 2222}
	if err := callback(hostname, remoteAddr, knownKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if len(verified) != 1 || verified[0].Fingerprint != ssh.FingerprintSHA256(knownKey.PublicKey()) || verified[0].Host != "saved-host" {
		t.Fatalf("verified observations = %+v", verified)
	}
	if err := callback(hostname, remoteAddr, changedKey.PublicKey()); !errors.Is(err, ErrHostKeyMismatch) {
		t.Fatalf("changed key error = %v", err)
	}
	if len(verified) != 1 {
		t.Fatalf("mismatched key was observed as verified: %+v", verified)
	}
}

func writeKnownHost(t *testing.T, path, hostname string, key ssh.PublicKey) {
	t.Helper()
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	if err := os.WriteFile(path, []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeKnownHostAuthority(t *testing.T, path, hostname string, key ssh.PublicKey) {
	t.Helper()
	keyText := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	line := fmt.Sprintf("@cert-authority %s %s\n", knownhosts.Normalize(hostname), keyText)
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

func connectTestServer(t *testing.T, server *sshtest.Server, policy *HostKeyPolicy) *ssh.Client {
	t.Helper()
	_, hostName, port, err := ParseTarget(server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", server.Addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client, err := newSSHClient(context.Background(), conn, ResolvedHost{
		Name: server.Addr, HostName: hostName, Port: port, User: "test",
	}, &AuthOptions{DisableAgent: true}, policy, time.Second)
	if err != nil {
		t.Fatalf("connect to test SSH server: %v", err)
	}
	return client
}

func generateED25519Signer(t *testing.T) ssh.Signer {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func generateECDSASigner(t *testing.T) ssh.Signer {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}
