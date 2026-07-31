// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package test

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"golang.org/x/crypto/internal/testenv"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/testdata"
)

func sshClient(t *testing.T) string {
	if testing.Short() {
		t.Skip("Skipping test that executes OpenSSH in -short mode")
	}
	sshCLI := os.Getenv("SSH_CLI_PATH")
	if sshCLI == "" {
		sshCLI = "ssh"
	}
	var err error
	sshCLI, err = exec.LookPath(sshCLI)
	if err != nil {
		t.Skipf("Can't find an ssh(1) client to test against: %v", err)
	}
	return sshCLI
}

// setupSSHCLIKeys writes the provided key files to a temporary directory and
// returns the path to the private key.
func setupSSHCLIKeys(t *testing.T, keyFiles map[string][]byte, privKeyName string) string {
	tmpDir := t.TempDir()
	for fn, content := range keyFiles {
		if err := os.WriteFile(filepath.Join(tmpDir, fn), content, 0600); err != nil {
			t.Fatalf("WriteFile(%q): %v", fn, err)
		}
	}
	return filepath.Join(tmpDir, privKeyName)
}

func TestSSHCLIAuth(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skipf("always fails on Windows, see #64403")
	}
	sshCLI := sshClient(t)
	keyFiles := map[string][]byte{
		"rsa":          testdata.PEMBytes["rsa"],
		"rsa.pub":      ssh.MarshalAuthorizedKey(testPublicKeys["rsa"]),
		"rsa-cert.pub": testdata.SSHCertificates["rsa-user-testcertificate"],
	}
	keyPrivPath := setupSSHCLIKeys(t, keyFiles, "rsa")

	certChecker := ssh.CertChecker{
		IsUserAuthority: func(k ssh.PublicKey) bool {
			return bytes.Equal(k.Marshal(), testPublicKeys["ca"].Marshal())
		},
		UserKeyFallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() == "testpubkey" && bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				return nil, nil
			}

			return nil, fmt.Errorf("pubkey for %q not acceptable", conn.User())
		},
	}

	config := &ssh.ServerConfig{
		PublicKeyCallback: certChecker.Authenticate,
	}
	config.AddHostKey(testSigners["rsa"])

	server, err := newTestServer(config)
	if err != nil {
		t.Fatalf("unable to start test server: %v", err)
	}
	defer server.Close()

	port, err := server.port()
	if err != nil {
		t.Fatalf("unable to get server port: %v", err)
	}

	// test public key authentication.
	cmd := testenv.Command(t, sshCLI, "-F", "none", "-vvv", "-i", keyPrivPath,
		"-o", "StrictHostKeyChecking=no", "-o", "IdentityAgent=none",
		"-p", port, "testpubkey@127.0.0.1", "true")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("public key authentication failed, error: %v, command output %q", err, string(out))
	}
	// Test SSH user certificate authentication.
	// The username must match one of the principals included in the certificate.
	// The certificate "rsa-user-testcertificate" has "testcertificate" as principal.
	cmd = testenv.Command(t, sshCLI, "-F", "none", "-vvv", "-i", keyPrivPath,
		"-o", "StrictHostKeyChecking=no", "-o", "IdentityAgent=none",
		"-p", port, "testcertificate@127.0.0.1", "true")
	out, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("user certificate authentication failed, error: %v, command output %q", err, string(out))
	}
}

func TestSSHCLIKeyExchanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skipf("always fails on Windows, see #64403")
	}
	sshCLI := sshClient(t)
	keyFiles := map[string][]byte{
		"rsa":     testdata.PEMBytes["rsa"],
		"rsa.pub": ssh.MarshalAuthorizedKey(testPublicKeys["rsa"]),
	}
	keyPrivPath := setupSSHCLIKeys(t, keyFiles, "rsa")

	keyExchanges := append(ssh.SupportedAlgorithms().KeyExchanges, ssh.InsecureAlgorithms().KeyExchanges...)
	for _, kex := range keyExchanges {
		t.Run(kex, func(t *testing.T) {
			cmd := testenv.Command(t, sshCLI, "-F", "none", "-Q", "kex")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed to check if the KEX is supported, error: %v, command output %q", kex, err, string(out))
			}
			if !bytes.Contains(out, []byte(kex)) {
				t.Skipf("KEX %q is not supported in the installed ssh CLI", kex)
			}
			config := &ssh.ServerConfig{
				Config: ssh.Config{
					KeyExchanges: []string{kex},
				},
				PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
					if conn.User() == "testpubkey" && bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
						return nil, nil
					}

					return nil, fmt.Errorf("pubkey for %q not acceptable", conn.User())
				},
			}
			config.AddHostKey(testSigners["rsa"])

			server, err := newTestServer(config)
			if err != nil {
				t.Fatalf("unable to start test server: %v", err)
			}
			defer server.Close()

			port, err := server.port()
			if err != nil {
				t.Fatalf("unable to get server port: %v", err)
			}

			cmd = testenv.Command(t, sshCLI, "-F", "none", "-vvv", "-i", keyPrivPath,
				"-o", "StrictHostKeyChecking=no", "-o", "IdentityAgent=none",
				"-o", fmt.Sprintf("KexAlgorithms=%s", kex), "-p", port, "testpubkey@127.0.0.1", "true")
			out, err = cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed, error: %v, command output %q", kex, err, string(out))
			}
		})
	}
}

func TestSSHCLIControlClientConn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skipf("always fails on Windows, see #64403")
	}
	sshCLI := sshClient(t)
	keyFiles := map[string][]byte{
		"rsa":     testdata.PEMBytes["rsa"],
		"rsa.pub": ssh.MarshalAuthorizedKey(testPublicKeys["rsa"]),
	}
	keyPrivPath := setupSSHCLIKeys(t, keyFiles, "rsa")

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if conn.User() == "testcontrolproxy" && bytes.Equal(key.Marshal(), testPublicKeys["rsa"].Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("pubkey for %q not acceptable", conn.User())
		},
	}
	config.AddHostKey(testSigners["rsa"])

	server, err := newTestServer(config)
	if err != nil {
		t.Fatalf("unable to start test server: %v", err)
	}
	defer server.Close()

	port, err := server.port()
	if err != nil {
		t.Fatalf("unable to get server port: %v", err)
	}

	dir, err := os.MkdirTemp("", "controlSocket")
	if err != nil {
		t.Fatalf("unable to create temp dir for control socket: %v", err)
	}
	defer os.RemoveAll(dir)
	csPath := filepath.Join(dir, "c")
	cmd := testenv.Command(t, sshCLI, "-vvv", "-i", keyPrivPath, "-o", "StrictHostKeyChecking=no",
		"-p", port, "-o", "ControlPath="+csPath, "-o", "ControlMaster=yes", "-N", "testcontrolproxy@127.0.0.1")
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatalf("control socket master start failed, error: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("OpenSSH output:\n\n%s", cmd.Stdout)
		}
	}()
	for i := range 10 {
		if _, err := os.Stat(csPath); err == nil {
			break
		} else if !os.IsNotExist(err) {
			t.Fatalf("unable to stat control socket: %v", err)
		}
		time.Sleep((1 << i) * 5 * time.Millisecond)
	}

	conn, err := net.Dial("unix", csPath)
	if err != nil {
		t.Fatalf("unable to open control socket: %v", err)
	}
	defer conn.Close()
	cc, chans, reqs, err := ssh.NewControlClientConn(conn)
	if err != nil {
		t.Fatalf("unable to create client: %v", err)
	}
	client := ssh.NewClient(cc, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("unable to create session: %v", err)
	}
	defer session.Close()
	out, err := session.CombinedOutput("true")
	if err != nil {
		t.Fatalf("command execution failed, error: %v, command output %q", err, string(out))
	}
}
